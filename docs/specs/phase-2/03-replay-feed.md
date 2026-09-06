# Spec 03 — Replay Feed (`internal/feed/replay_feed.go`)

## Overview

`internal/feed.Feed` (`Next() (exchange.Candle, bool)`) already exists from Phase 1, implemented today only
by `LiveFeed` (WebSocket-backed). This spec adds `ReplayFeed`, the Postgres-backed implementation that lets
the backtest engine (Spec 04) and Dry-Run's reuse of the same driver loop (Spec 09) consume historical
candles through the identical interface a live consumer uses — the entire point of ADR-001/PRD §8's "Feed
interface is the architectural linchpin" claim. This spec also confirms (per the coordinator's explicit
instruction to verify) that Phase 1's `Feed` interface was **not** accidentally designed Live-only.

## Current Behavior (verified)

- `internal/feed/interface.go` (Phase 1, unchanged) — `Feed` interface is exactly `Next() (exchange.Candle,
  bool)`. **Confirmed generic**: it references only `exchange.Candle`, has no method or field referencing
  WebSockets, reconnection, or any live-only concern. `ReplayFeed` can implement it with zero interface
  changes — verifying the coordinator's concern that it wasn't accidentally designed Live-only.
- `internal/feed/live_feed.go` (Phase 1) is the only existing implementation — buffered-channel-backed,
  WebSocket-subscription-driven, with reconnect/backoff logic (per Phase 1 Spec 04). None of that logic is
  relevant to `ReplayFeed`, which has no network dependency at all.
- No `replay_feed.go` file exists yet (confirmed by directory listing: `internal/feed/` contains only
  `interface.go`, `live_feed.go`, `live_feed_test.go`).

## Target Behavior

```go
// internal/feed/replay_feed.go
package feed

import (
    "context"
    "time"

    "go-trade-bot/app/repository/candle"
    "go-trade-bot/internal/exchange"
)

// ReplayFeed implements Feed by iterating stored candles sequentially,
// ascending by OpenTime, for one (symbol, timeframe) pair over a fixed
// [from, to) range. Unlike LiveFeed, Next() never blocks on network I/O and
// never sleeps between candles - Backtest mode is explicitly "Fast as
// possible" (PRD SS4.4's execution-mode table), so ReplayFeed delivers
// candles as fast as the caller consumes them. "Simulates candle arrival"
// (blueprint SS4 Phase 2) means presenting candles one at a time in the
// correct temporal sequence, not wall-clock-paced delivery - see Acceptance
// Criterion #5 for the one case (Dry-Run reuse) where pacing does matter,
// which is handled by LiveFeed's natural real-time delivery instead, not by
// ReplayFeed gaining a sleep.
type ReplayFeed struct {
    // unexported: pre-loaded candle slice (loaded once at construction via
    // CandleRepository.Range, per Spec 01), current read cursor
}

// NewReplayFeed loads the full [from, to) range for (symbol, timeframe) into
// memory once at construction (per-symbol datasets at Phase 2's scale - a
// year of 1m candles for one symbol is ~525k rows, comfortably in-memory
// under the 4GB homelab budget - see Acceptance Criterion #7 for the
// large-range edge case).
func NewReplayFeed(ctx context.Context, repo candle.CandleRepository, symbol, timeframe string, from, to time.Time) (*ReplayFeed, error)

func (f *ReplayFeed) Next() (exchange.Candle, bool)

// Close is a no-op for ReplayFeed (no resource to release, unlike LiveFeed's
// WebSocket connection) but is provided so callers that type-switch/defer
// Close() uniformly across Feed implementations don't need a nil-check per
// concrete type. Not part of the Feed interface itself (LiveFeed's Close()
// isn't either, per Phase 1's interface.go) - this is a concrete-type
// convenience method only.
func (f *ReplayFeed) Close() error
```

**Gap tolerance**: per Spec 02's gap-detection note, imported data may have missing candles. `ReplayFeed`
does **not** synthesize filler candles for a detected gap — it simply yields whatever `OpenTime`-ordered
rows exist. A strategy under backtest evaluation sees the same "the market just skipped ahead" reality a
live strategy would see if the exchange itself had a data gap; no artificial smoothing occurs (matches
PRD's mode table treating backtest fidelity as paramount, not smoothed-over-for-convenience).

## Acceptance Criteria

1. **Given** a `ReplayFeed` constructed for 100 stored candles between `2024-01-01T00:00` and
   `2024-01-01T01:40` (1m interval), **when** `Next()` is called 100 times in sequence, **then** each call
   returns the next candle in ascending `OpenTime` order, and the 101st call returns `(zero-value, false)`.
2. **Given** the underlying `[from, to)` range has zero stored candles (e.g. an unimported symbol/timeframe
   combination), **when** `NewReplayFeed` is called, **then** it returns successfully (not an error) with a
   feed whose first `Next()` call immediately returns `(zero-value, false)` — an empty backtest range is a
   valid (if useless) input, not a construction-time failure; the caller (Spec 04's driver) is responsible
   for treating "zero candles processed" as a reportable backtest-run failure if desired.
3. **Given** the stored data has a gap (per Spec 02's gap detection — e.g. candles at `05:00` then `05:03`,
   skipping `05:01`/`05:02`), **when** `Next()` is called across the gap, **then** it returns the `05:00`
   candle followed directly by the `05:03` candle — no synthetic candle is inserted, and the gap is
   invisible to `ReplayFeed`'s consumer beyond the `OpenTime` discontinuity itself.
4. **Given** `Next()` is called after the feed is exhausted, **when** called any number of additional times,
   **then** it continues returning `(zero-value, false)` indefinitely (idempotent exhaustion, matching
   `LiveFeed`'s post-`Close()` behavior from Phase 1 Spec 04 AC#6) rather than panicking on an
   out-of-bounds read.
5. **Edge case — Dry-Run does NOT use ReplayFeed**: **given** Spec 09 (Dry-Run mode) requires *live* prices
   per PRD §4.4's mode table, **when** the Dry-Run driver is configured, **then** it is wired to `LiveFeed`,
   never `ReplayFeed` — this is stated here explicitly because both feeds satisfy the same `Feed` interface
   and are trivially swappable at the type level, which is exactly the risk: a wiring mistake (constructing
   `ReplayFeed` for what should be a Dry-Run strategy) would silently produce a "Dry-Run" that's actually
   replaying history. Spec 09/10's wiring code must make the Feed choice explicit and traceable to
   `ExecutionMode`, not incidental.
6. **Edge case — `from == to`**: **given** `NewReplayFeed` is called with an empty range (`from == to`),
   **then** it behaves identically to Acceptance Criterion #2 (immediately exhausted, no error).
7. **Edge case — large range memory footprint**: **given** a 2-year, 1m-interval range is requested for a
   single symbol (~1.05M rows, comfortably under a few hundred MB as `exchange.Candle` structs), **when**
   `NewReplayFeed` loads it, **then** construction succeeds within the 4GB homelab budget — this spec
   accepts full in-memory loading as sufficient for Phase 2's realistic single-symbol single-backtest usage
   pattern; a streaming/paginated `ReplayFeed` (query-per-`Next()`-batch rather than load-everything-upfront)
   is flagged as a future optimization if walk-forward validation (Spec 08) or multi-symbol simultaneous
   backtests push memory usage past comfortable bounds — not required for Phase 2's acceptance criteria.

## Out of Scope

- Multi-timeframe delivery through a single `ReplayFeed` instance — one `ReplayFeed` is one
  `(symbol, timeframe)` pair; Spec 04's backtest driver constructs a separate `ReplayFeed` (or a direct
  `CandleRepository.RangeBefore` call, for supplementary non-primary timeframes) per distinct
  `(symbol, timeframe)` it needs.
- Streaming/paginated loading for very large ranges — see Acceptance Criterion #7's note; deferred until
  proven necessary.
- Wall-clock-paced replay (e.g. a "watch the backtest play out in real time" mode for demo purposes) — not
  requested by the PRD; Backtest mode is explicitly "Fast as possible."

## Dependencies

- Spec 01 (Candle Storage) — `CandleRepository.Range` is the data source `NewReplayFeed` loads from.
- Phase 1's `internal/feed.Feed` interface (unchanged, confirmed compatible per Current Behavior above).
