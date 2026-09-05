# Spec 04 — WebSocket Live Feed (`internal/feed.LiveFeed`)

## Overview

The current "cycle" is REST polling with a delay: `app/workers/strategy/strategy.go` re-enqueues the same
asynq task with `asynq.ProcessIn(cycle * time.Minute)`, and every algorithm re-fetches candles via REST on
each wake-up. This spec defines `LiveFeed`, the WebSocket-backed implementation of the `Feed` interface
(`internal/feed/interface.go`), which subscribes via `ExchangeClient.SubscribeKline` and pushes candles to
the engine as they close — the event-driven replacement for timer-based polling. `ReplayFeed` (Postgres) is
explicitly Phase 2; this spec covers only the live side.

## Current Behavior (verified)

- `app/workers/strategy/strategy.go:30-45` — `EnqueueStrategyTask` marshals the `entities.Strategy`,
  computes `cycle := time.Duration(strategy.StrategyConfiguration.Cycle) * time.Minute`, and calls
  `w.client.Enqueue(t1, asynq.ProcessIn(time))`. This is a **delay-based re-poll**, not a subscription.
- `app/handler/tasks/strategy/handler.go:65-105` (`HandleStrategyTask`) re-enqueues itself
  (`p.worker.EnqueueStrategyTask(nStrategy)`) unconditionally after every execution (unless `Disabled`) —
  the mechanism that perpetuates the polling loop.
- Every algorithm (`grid/algorithm.go:145`, `bollinger/algorithm.go:47`, `scalping/algorithm.go:48,163`)
  calls `p.broker.ListKline(ctx, symbol, interval, limit)` — a fresh REST call — on every cycle wake-up.
  There is no persistent connection, no push-based delivery, and no candle buffering of any kind.
- No WebSocket client dependency exists in `go.mod` beyond what `go-binance/v2` bundles internally
  (`gorilla/websocket` is present only as an indirect dependency of `go-binance`, confirmed in `go.mod`).

## Target Behavior

```go
// internal/feed/interface.go
package feed

import "go-trade-bot/internal/exchange"

type Feed interface {
    Next() (exchange.Candle, bool) // bool == false means feed is exhausted/closed
}
```

```go
// internal/feed/live_feed.go
package feed

type LiveFeed struct {
    // unexported: exchange client, symbol, interval, internal buffered channel,
    // reconnect state, metrics collector
}

func NewLiveFeed(client exchange.ExchangeClient, symbol, interval string, collector *metrics.MetricsCollector) (*LiveFeed, error)
func (f *LiveFeed) Next() (exchange.Candle, bool)
func (f *LiveFeed) Close() error
```

`LiveFeed` wraps `ExchangeClient.SubscribeKline(ctx, symbol, interval) (<-chan exchange.Candle, error)`
(Spec 01). Internally it owns a **bounded buffered channel per symbol** (capacity 500 candles — generous
relative to any realistic strategy cycle length of 1-60 minutes, small relative to the 4GB homelab RAM
budget) that `Next()` reads from. A background goroutine reads from the underlying WebSocket subscription
and pushes into this buffer; `Next()` blocks until a candle is available or the feed is closed.

**Reconnect behavior**: `SubscribeKline`'s underlying `go-binance` WebSocket connection can drop (network
blip, Binance-side disconnect — Binance closes WS connections after 24h by policy). `LiveFeed` detects the
channel closing unexpectedly (vs. an explicit `Close()` call) and reconnects with **exponential backoff
with jitter**: initial delay 1s, doubling up to a cap of 30s, retried indefinitely (a live-trading feed
must not give up permanently — a stopped feed with open positions and no fresh price data is worse than a
slow reconnect loop). Each reconnect attempt increments `websocket_reconnects_total{symbol}` (blueprint
§7.2); `websocket_connected{symbol}` gauge is set to `0` the instant a disconnect is detected and back to
`1` only after a successful resubscription confirmed by receiving at least one candle.

**Backpressure**: if the engine's consumer (Spec 05's hook loop) falls behind and the 500-candle buffer
fills, `LiveFeed` **blocks the WebSocket-reading goroutine** rather than dropping candles — for a trading
system, silently dropping a candle is a correctness bug (a strategy could miss the exact candle that would
have triggered a signal), whereas blocking briefly just delays processing. A gauge
`feed_buffer_depth{symbol}` should be considered for Phase 2 observability once buffer saturation becomes
diagnosable against real usage; not required for Phase 1's acceptance criteria below.

**Interaction with an in-flight strategy cycle**: `HandleStrategyTask` (Spec 05/06's `engine.Run` call) is
mid-`Strategy.Before()`/hook-evaluation when a WS disconnect happens elsewhere. The disconnect does not
abort the in-flight cycle — the cycle completes using whatever candles it already read from `Next()`. The
*next* call to `Next()` for that symbol simply blocks until reconnection succeeds (bounded by the backoff
schedule above), which is the correct behavior: a strategy should stall waiting for fresh data rather than
proceed on stale/absent data.

## Acceptance Criteria

1. **Given** `NewLiveFeed` is constructed for `BTCUSDT`/`1m`, **when** `SubscribeKline` succeeds, **then**
   `websocket_connected{symbol="BTCUSDT"}` is set to `1` after the first candle arrives.
2. **Given** an active `LiveFeed`, **when** the underlying WebSocket connection drops unexpectedly, **then**
   `websocket_connected{symbol}` is set to `0` immediately and `websocket_reconnects_total{symbol}`
   increments on every reconnect attempt (not just the first).
3. **Given** a dropped connection, **when** reconnection is attempted repeatedly, **then** the delay between
   attempts follows exponential backoff (1s, 2s, 4s, ... capped at 30s) rather than a fixed interval or a
   tight retry loop that would hammer the exchange.
4. **Given** the engine calls `Next()` while the internal buffer is empty and the connection is healthy,
   **then** `Next()` blocks (does not busy-poll) until a candle arrives or `Close()` is called.
5. **Given** the internal buffer is full (500 candles unconsumed) because the engine is processing slowly,
   **when** a new candle arrives from the WebSocket, **then** the reader goroutine blocks writing to the
   buffer (candle is not dropped) rather than discarding the oldest or newest candle.
6. **Given** `Close()` is called on a `LiveFeed`, **when** any goroutine subsequently calls `Next()`,
   **then** it returns `(zero-value, false)` promptly rather than blocking forever.
7. **Edge case — reconnect succeeds mid-cycle**: **Given** a strategy cycle is actively running when a
   disconnect+reconnect cycle completes, **when** the cycle later calls `Next()` again, **then** it
   receives the next live candle normally — no stale-candle replay, no duplicate candle delivery for the
   candle that was in-flight at disconnect time (dedup by `OpenTime`, discarding any candle whose
   `OpenTime` is not strictly greater than the last delivered one for that symbol).
8. **Edge case — symbol subscription fails at startup**: **Given** `SubscribeKline` returns an error
   immediately (e.g. invalid symbol), **when** `NewLiveFeed` is called, **then** it returns a non-nil error
   synchronously rather than silently retrying forever with `websocket_connected` stuck at an ambiguous
   state — a hard subscription failure (bad symbol) is different from a transient network drop and should
   surface at strategy-enable time, not be swallowed into the reconnect loop.

## Out of Scope

- `ReplayFeed` (Postgres-backed) — Phase 2, per blueprint §4 Phase 2 file list.
- Multi-timeframe aggregation (1m → 5m/15m/1h derived candles) — Phase 3 (`internal/feed/multitimeframe.go`
  per blueprint).
- `feed_candle_delay_seconds` metric (data-freshness gauge) — blueprint §7.2 assigns this to Phase 2, tied
  to the simulator-parity work; Phase 1's `LiveFeed` only needs the connectivity metrics above.
- Persisting received candles to the future `candles` table — that table doesn't exist until Phase 2;
  Phase 1's `LiveFeed` is purely in-memory pass-through.

## Dependencies

- Spec 01 (Exchange ACL) — `ExchangeClient.SubscribeKline` must exist.
- Spec 11 (Observability) — the `websocket_reconnects_total`/`websocket_connected` metric configs must be
  registered in `cmd/worker/modules/metrics.go` before `LiveFeed` can call `collector.SetGauge`/
  `IncrementCounter` on them without a no-op (the collector silently no-ops on an unregistered metric name
  per `internal/metrics/collector.go:65-81`'s map-lookup-with-`ok` pattern — this fails silently, not loudly,
  so registration order matters operationally even though it won't panic).
