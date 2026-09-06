# Spec backend-05 — Candle Warm-Up

## Overview

Blueprint §4 Phase 3 asks for "candle pipeline warm-up (pre-load N candles before strategy starts so
indicators have valid values from the first signal)," assigned to `app/handler/tasks/strategy/handler.go`.
This spec's Current Behavior audit finds that file's live-cycle path **already** warms up on every single
cycle by construction — the real gap is elsewhere, in Phase 2's `ReplayDriver` (Dry-Run's `LiveFeed`-driven
path), which starts with a genuinely empty rolling window and has no backfill mechanism. This spec
redirects the work to where it's actually needed and documents why the originally-named file needs no
change.

## Current Behavior (verified)

- `app/engine/engine.go:129-136` (`buildContext`, Phase 1's live-cycle path) calls
  `e.Exchange.ListKline(ctx, symbol, dbStrategy.GetBrokerInterval(), candleWindow)` — a **full 100-candle
  REST fetch — every single cycle**, including a strategy's very first-ever execution. There is no
  incremental/single-candle delivery in this path (Phase 1's live mode is REST-poll-and-refetch, not
  event-driven — confirmed in Phase 2 Spec 04's Current Behavior audit too). This means Phase 1's live path
  **never** computes indicators on an undersized window past whatever each strategy's own "not enough
  candles" guard already handles (Phase 1 Spec 08's existing `≥20`/`≥10` checks for Bollinger/Scalping) —
  a fresh symbol with less than `candleWindow` (100) actual market history is the only scenario where this
  guard still matters, and that's a data-availability problem no warm-up mechanism can solve (there's
  simply no more history to fetch).
- `app/handler/tasks/strategy/handler.go` (Phase 1, current) has no candle-fetching logic of its own at
  all — `processStrategy` resolves the strategy from the registry and calls `p.engine.Run(...)` per symbol;
  all candle access happens inside `Engine.buildContext`. The blueprint's file assignment
  (`app/handler/tasks/strategy/handler.go`) reflects an earlier draft's mental model that predates Phase
  1's actual implementation shape — this spec corrects the target file.
- **Where warm-up genuinely matters**: `docs/specs/phase-2/04-backtest-engine.md`'s `ReplayDriver` (used by
  both Backtest, fed by `ReplayFeed`, and Dry-Run, fed by `LiveFeed` per Phase 2 Spec 09) accumulates its
  rolling `candleWindow`-sized buffer **starting from empty**, one candle at a time, as `Feed.Next()`
  delivers them (Phase 2 Spec 04's Target Behavior). For a **Dry-Run** driver specifically — `LiveFeed`
  only ever delivers candles closing *after* subscription time, with no historical backfill capability
  (confirmed: `internal/feed/live_feed.go`'s `SubscribeKline`-based design, Phase 1, has no REST-backfill
  step) — a freshly started Dry-Run strategy runs its first `candleWindow - 1` cycles against a
  progressively-filling, genuinely undersized window. This is the actual, real instance of "indicators
  computed on empty windows" the blueprint's line item is describing, just manifesting in Phase 2 code
  rather than the Phase 1 file the blueprint originally named.
- Backtest's `ReplayFeed` has a related but distinct version of this same gap: Phase 2 Spec 04 Acceptance
  Criterion #7 already documented this as "accepted, matches live behavior on a fresh symbol" for the case
  where the *imported dataset itself* is shorter than `candleWindow`. This spec adds a warm-up step for the
  **normal** case where more history exists in storage *before* the requested backtest `[from, to)` range
  starts — today's `ReplayFeed` (Phase 2 Spec 03) only loads exactly `[from, to)`, discarding any
  pre-existing history that could otherwise seed a full window from candle 1 of the actual requested range.

## Target Behavior

```go
// app/engine/replay_driver.go (Phase 2 file, modified here)
type ReplayDriver struct {
    // ...existing fields (Phase 2 Spec 04)...
    WarmupSource WarmupCandleSource // NEW
}

// WarmupCandleSource supplies the candleWindow-sized backfill needed before
// the driver starts consuming its primary Feed. For Dry-Run, this is a real
// exchange.ExchangeClient.ListKline call (REST, one-time, at driver
// construction). For Backtest, this is CandleRepository.RangeBefore
// (Phase 2 Spec 01) anchored to the requested range's start - pulling the
// candleWindow candles immediately preceding `from`, so the very first
// candle inside the requested range already has a full window, rather than
// building up from zero at `from` itself.
type WarmupCandleSource interface {
    Warmup(ctx context.Context, symbol, timeframe string, window int) ([]exchange.Candle, error)
}
```

`ReplayDriver.Run` calls `WarmupSource.Warmup` once, before the first `Feed.Next()` call, seeding the
rolling window with up to `candleWindow` pre-existing candles. If fewer than `candleWindow` are available
(a genuinely new symbol/strategy with insufficient history — Backtest Spec 04 AC#7's already-accepted case),
the driver proceeds with whatever it got, unchanged from today's documented behavior — this spec adds the
backfill *attempt*, it does not change what happens when the backfill can't fully satisfy the window.

For Dry-Run (Phase 2 Spec 09's `NewDryRunDriver`), `WarmupSource` wraps the same `realExchange
exchange.ExchangeClient` already passed in for market-data reads — no new dependency, just one additional
call (`ListKline(ctx, symbol, interval, candleWindow)`) at construction time before the `LiveFeed`
subscription's first delivered candle is consumed.

For Backtest (Phase 2 Spec 04's `ReplayDriver` construction), `WarmupSource` wraps `CandleRepository`
(Phase 2 Spec 01), calling `RangeBefore(ctx, symbol, timeframe, from, candleWindow)` — reusing the exact
no-lookahead-safe accessor Spec 04 already established, anchored at the range's start rather than a moving
simulated-time cursor (appropriate here since this is a one-time pre-range seed, not an ongoing per-candle
lookup).

## Acceptance Criteria

1. **Given** a Dry-Run driver is constructed for a strategy needing 20 candles minimum (Bollinger),
   **when** the driver starts, **then** a REST `Warmup` call fetches 100 (`candleWindow`) historical
   candles before the first `LiveFeed`-delivered candle is processed, so the very first `Engine.Run`
   invocation already has a full window, not a 1-candle window.
2. **Given** the warmup fetch itself fails (e.g. transient network error), **when** this occurs, **then**
   the driver logs the failure and proceeds **without** a warmed-up window (falling back to today's
   from-empty accumulation behavior) rather than refusing to start the Dry-Run entirely — a failed
   optimization attempt should degrade gracefully, not block an operator-initiated Dry-Run launch.
3. **Given** a Backtest driver's requested range `[from, to)` has at least `candleWindow` candles of
   history stored **before** `from`, **when** the driver starts, **then** `RangeBefore` supplies a full
   100-candle pre-seed, and the first candle at or after `from` is evaluated with a complete window.
4. **Given** a Backtest driver's requested range starts at the very beginning of a symbol's available
   history (no candles exist before `from`), **when** `RangeBefore` is called, **then** it returns however
   many candles exist (possibly zero, per Phase 2 Spec 01 AC#4's "no rows" case) without error, and the
   driver proceeds with a partial or empty pre-seed — matching Phase 2 Spec 04 AC#7's already-accepted
   degraded-window behavior for this specific edge.
5. **Given** Phase 1's live-cycle path (`Engine.buildContext`, unmodified by this spec), **when** any
   strategy's cycle runs, **then** its behavior is **byte-identical** to before this spec — this spec makes
   zero changes to `app/engine/engine.go`'s live-mode `buildContext` or to
   `app/handler/tasks/strategy/handler.go`, confirming the redirection described in Current Behavior is
   correct and doesn't silently touch the live path while claiming not to.
6. **Edge case — warmup candles include data from before the strategy existed**: **given** a strategy was
   created today but the symbol has years of prior market history, **when** a Dry-Run driver warms up,
   **then** it fetches the most recent `candleWindow` candles regardless of the strategy's own creation
   date — warm-up is about indicator-window completeness, not about respecting when the strategy row was
   created; using older market data to seed indicators for a brand-new strategy is correct and intended.

## Out of Scope

- Warm-up for Phase 1's live REST-poll cycle path — not needed, per Current Behavior's finding that it's
  already effectively warm every cycle.
- Persisting warmup-fetched candles into the `candles` table (Phase 2 Spec 01) for a Dry-Run's REST-sourced
  backfill — the warmup fetch is a one-time, in-memory seed for that driver instance only; it does not
  contribute to the historical dataset future backtests would use (that remains `cmd/candleimport`'s job,
  Phase 2 Spec 02).
- Configurable warm-up window size distinct from `candleWindow` — reuses the existing Phase 1/2 constant
  as-is; no new configuration surface is introduced.

## Dependencies

- Phase 2's `docs/specs/phase-2/04-backtest-engine.md` (`ReplayDriver`), `docs/specs/phase-2/09-dry-run-mode.md`
  (`NewDryRunDriver`), `docs/specs/phase-2/01-candle-storage.md` (`CandleRepository.RangeBefore`) — this
  spec modifies `ReplayDriver` and both its construction call sites, all Phase 2 deliverables.
- **Judgment call**: this spec retargets the blueprint's stated file (`app/handler/tasks/strategy/handler.go`)
  to Phase 2's `ReplayDriver` instead, based on the Current Behavior audit finding the originally-named
  file has no candle-fetching logic to warm up at all. Flagging this retargeting explicitly since it means
  this spec's actual deliverable touches Phase 2 code, not the Phase 1 file the blueprint's Phase 3 section
  literally names — worth confirming this reading is correct before implementation.
