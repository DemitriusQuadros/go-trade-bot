# Spec 09 — Dry-Run Mode (`app/engine/dryrun.go`)

## Overview

Dry-Run runs against **real live prices** (via `LiveFeed`, Phase 1) with **simulated** order fills —
PRD §4.4's second validation-pipeline stage, sitting between Backtest and Paper Trading. This spec reuses
Spec 04's `SimulatedFillExchange`/`ReplayDriver` machinery, differing only in `MarketDataSource` (backed by
a real `ExchangeClient` instead of stored candles) and `FillPolicy` (configurable slippage/fee/fill-delay
instead of Backtest's zero-friction defaults) — exactly the two axes ADR-001 names as the only allowed
differences between execution modes.

## Current Behavior (verified)

- `internal/feed.LiveFeed` (Phase 1) exists and is fully implemented (WebSocket subscription, reconnect
  backoff, buffered channel) but is **not currently wired into `Engine.Run` or any driver loop** — Phase 1's
  live cycle uses REST `ListKline` polling (see Spec 04 Current Behavior), not `LiveFeed`. This spec's
  `ReplayDriver` (Spec 04, this phase) is the **first actual consumer** of `LiveFeed` in the codebase.
- `internal/configuration/configuration.go` (Phase 1, current) has no slippage/fee/fill-delay configuration
  fields — only `Mode`, `ConfirmLive`, `Testnet`, `WebhookURL` exist (all Phase 1 Spec 10 additions).
- No `app/engine/dryrun.go` exists yet.

## Target Behavior

```go
// internal/configuration/configuration.go — additive fields (Phase 2)
type Configuration struct {
    // ...existing Phase 1 fields...
    DryRun DryRunConfig
}

type DryRunConfig struct {
    SlippagePct float64       // e.g. 0.05 = 0.05% unfavorable price adjustment per fill
    FeePct      float64       // overrides the hardcoded 0.1% entry/exit fee for Dry-Run runs specifically
    FillDelay   time.Duration // e.g. 500ms - see "Fill delay semantics" below
}
```

```go
// app/engine/dryrun.go
package engine

// NewDryRunDriver constructs a ReplayDriver (Spec 04) wired for Dry-Run:
// Feed is a Phase 1 LiveFeed for the strategy's symbol/interval, and the
// SimulatedFillExchange's MarketDataSource wraps a real exchange.ExchangeClient
// (production BinanceAdapter is fine here - Dry-Run reads real prices but
// never places real orders, so hitting production for READS is safe and
// intended, unlike ModeLive/ModePaper's write-path testnet/production split
// from Phase 1 Spec 10).
func NewDryRunDriver(
    liveFeed *feed.LiveFeed,
    realExchange exchange.ExchangeClient, // read-only usage - see above
    fillCfg DryRunConfig,
    engine *Engine,
    strategy strategies.Strategy,
    dbStrategy entities.Strategy,
    symbol string,
) *ReplayDriver
```

### Fill delay semantics (the specific mechanic the coordinator flagged as needing precision)

A real exchange fill is not instantaneous — Dry-Run's `FillDelay` models this without needing an actual
async wait inside a single strategy-cycle evaluation (which would stall the driver's candle-consumption
loop for no benefit, since `LiveFeed`'s `Next()` already paces delivery at real market speed). Instead:
- When `SimulatedFillExchange.PlaceOrder` is called under a Dry-Run `FillPolicy`, it does **not** fill using
  the *current* candle's price immediately. It records the order as "pending" and fills it using the price
  from the candle that arrives **after** `FillDelay` has elapsed in **simulated-live time** (i.e. the
  `OpenTime` of the next candle `LiveFeed` delivers whose timestamp is `>= orderTime + FillDelay`) — this
  approximates real order-latency without literally blocking a goroutine for `FillDelay` wall-clock time
  (though since `LiveFeed` candles are already real-time-paced, `FillDelay` values shorter than one candle
  interval will typically resolve on the very next candle regardless — the mechanism is correct either way,
  it's just that sub-candle-interval delays are indistinguishable from "next candle" in practice, which is
  an accepted simplification given candle-level granularity is this engine's finest resolution).
- The fill price at that later point is the candle's `Close`, then **slippage-adjusted**: for a buy,
  `fillPrice = candleClose * (1 + SlippagePct/100)` (worse for the buyer); for a sell,
  `fillPrice = candleClose * (1 - SlippagePct/100)` (worse for the seller) — slippage always moves the fill
  price against the trader, modeling real market impact/spread cost, never in the trader's favor.
- `FeePct` (Dry-Run's config) replaces the hardcoded `0.1%` in `calculateEntryFee`/`calculateExitFee` **only
  for Dry-Run-mode runs** — Backtest, Paper, and Live all keep the existing Phase 1 hardcoded `0.1%`
  (Backtest per Spec 04's explicit "existing fee calc unchanged"; Paper/Live because those hit a real
  exchange whose actual fee schedule applies regardless of any locally-configured value). This is the one
  point where Dry-Run's `SimulatedFillExchange` usage diverges from a pure "inject different config" story
  and requires `app/usecase/signal.SignalUseCase`'s fee calculation to be parameterized rather than using
  its two hardcoded `feePct := 0.1` constants unconditionally — flagged explicitly since it's a **required
  modification to Phase 1's shipped `usecase.go`**, not a purely additive Phase 2 change.

## Acceptance Criteria

1. **Given** a Dry-Run driver receives a `GoLong` signal at candle `T` with `FillDelay=0`, **when** the
   order is processed, **then** it fills using candle `T`'s own close price (slippage-adjusted) — a zero
   delay degenerates to "fill on the triggering candle," matching Backtest's instant-fill behavior modulo
   slippage.
2. **Given** `FillDelay` spans exactly 2 candle intervals, **when** an order is placed at candle `T`,
   **then** it fills using the candle at `T + 2 intervals`'s close price, not `T`'s or `T+1`'s.
3. **Given** `SlippagePct = 0.1` and a buy order fills at a candle close of `50000`, **when** slippage is
   applied, **then** the recorded fill price is `50050` (0.1% worse for the buyer), never `49950`.
4. **Given** `DryRunConfig.FeePct = 0.05`, **when** a Dry-Run trade's entry fee is calculated, **then** it
   uses `0.05%`, not the Phase 1 hardcoded `0.1%` — verified by a test asserting the fee differs from a
   Backtest-mode run of the identical trade (which keeps `0.1%`).
5. **Given** a Dry-Run driver running against `LiveFeed`, **when** the underlying WebSocket disconnects and
   reconnects (Phase 1 Spec 04's existing reconnect logic, unmodified), **then** the Dry-Run driver simply
   experiences the same `Next()`-blocks-until-reconnected behavior any `LiveFeed` consumer would — no
   Dry-Run-specific reconnect handling is needed, confirming this spec adds zero new behavior to `LiveFeed`
   itself (pure reuse, per the Current Behavior note that this is `LiveFeed`'s first actual consumer).
6. **Edge case — order pending across a mode switch**: **given** a strategy's `Mode` changes from `dryrun`
   to `paper` (via a future Phase 3 TUI action) while a Dry-Run order is still pending its `FillDelay`,
   **when** this occurs, **then** — this spec explicitly does **not** define resolved behavior for it,
   since Phase 2 has no mid-run mode-switching mechanism at all (that's Phase 3, per blueprint's own open
   question on mode-switch timing) — flagging this as a real gap for Phase 3 to address, not silently
   assuming "finish the pending fill" or "abandon it" without a decision.
7. **Edge case — negative or zero `FillDelay`**: **given** `DryRunConfig.FillDelay` is `0` or unset, **when**
   the driver processes orders, **then** it behaves per Acceptance Criterion #1 (immediate, same-candle
   fill) — a missing/zero config value is a valid, safe default, not a startup validation error (unlike
   Phase 1 Spec 10's stricter treatment of unparseable `Mode` values, since a zero fill delay is a
   legitimate configuration choice, not a data-corruption signal).

## Out of Scope

- Wiring `LiveFeed` into the **Live**-mode (real capital) execution path — Phase 1's live cycle remains
  REST-poll-based; migrating Live mode itself to `LiveFeed`-driven execution (as the blueprint's original
  Phase 2 engine.go note speculated) is **not** required by this spec, since Spec 04 achieves ADR-001's
  "one engine, shared" goal without that migration (see Spec 04's judgment call). Revisit only if a future
  phase specifically needs Live mode to be feed-driven rather than cycle-driven.
- Partial fills or rejected orders in Dry-Run — Dry-Run always fills fully at the (delayed, slippage-
  adjusted) price; modeling liquidity-driven partial fills is not requested by the PRD's Dry-Run
  description and is out of scope.

## Dependencies

- Spec 04 (Backtest Engine) — `SimulatedFillExchange`/`ReplayDriver`/`FillPolicy` are reused directly.
- Phase 1's `internal/feed.LiveFeed` — consumed here for the first time, unmodified.
- **Judgment call**: requiring a modification to Phase 1's already-shipped
  `app/usecase/signal/usecase.go` (parameterizing the fee calculation) is flagged explicitly — every other
  Phase 2 spec so far has been purely additive on top of Phase 1 code; this is the one exception, and it's
  called out here rather than left implicit.
