# Spec 04 — Backtest Engine (`app/engine/backtest.go`)

## Overview

This is the spec with the most architectural weight in Phase 2: how does `app/engine.Engine` — which today
(Phase 1) drives exactly one live cycle by pulling candles via `e.Exchange.ListKline` (a real REST call) and
placing real orders via `e.Exchange.PlaceOrder` — get reused, per ADR-001, for an offline backtest that must
never touch the network and must simulate fills instead of placing them? This spec resolves that by keeping
`Engine.Run`'s hook-loop **completely unchanged** and instead supplying it with a different
`exchange.ExchangeClient` implementation: a `SimulatedFillExchange` backed by stored candle data for reads
and in-memory fill simulation for writes. A new `ReplayDriver` component owns the per-candle cycle loop that
Phase 1's timer-based re-enqueue does for live trading.

## Current Behavior (verified)

- `app/engine/engine.go:51-58` — `Engine` struct holds `Exchange exchange.ExchangeClient` as an **interface
  field**, not a concrete `*BinanceAdapter` — this is the seam ADR-001 depends on, and it already exists
  exactly as needed; no structural change to the `Engine` struct itself is required for backtest reuse.
- `app/engine/engine.go:83-122` (`Run`) calls `e.buildContext` once per invocation, which
  (`engine.go:129-181`) unconditionally calls `e.Exchange.ListKline(ctx, symbol, dbStrategy.GetBrokerInterval(),
  candleWindow)` for the primary window, plus two supplementary calls: `e.fetch24hVolume` (`ListKline(...,
  "1d", 1)`) and a hardcoded `ListKline(ctx, symbol, "15m", 50)` for `ConfigKeyLongTermCandles`
  (`engine.go:163-168`). **All three calls go through the same `e.Exchange` field** — meaning a
  `SimulatedFillExchange` swapped in for `e.Exchange` transparently intercepts all three without any change
  to `buildContext`'s logic.
- `app/engine/engine.go:23-30` — `candleWindow = 100` is a package-level constant sized to Grid's build-phase
  need; the same constant is reused by the backtest driver (see Target Behavior) so a backtest strategy
  sees an identically-sized rolling window to what it saw in Phase 1's live cycle.
- `app/usecase/signal/usecase.go:99-207` (`GenerateBuySignal`) and `:265-356` (`GenerateSellSignal`) call
  `s.Exchange.PlaceOrder`/`CancelOrder`/`GetOrder` directly — this is the **other** half of the seam: Engine
  itself doesn't call `PlaceOrder` (it calls `SignalUseCase.GenerateBuySignal`, which does). For backtest
  reuse, `SignalUseCase` must **also** be constructed with the same `SimulatedFillExchange`, not a real
  `BinanceAdapter` — confirming that "which order-execution path is injected" (ADR-001's phrase) means
  injecting the exchange at the `SignalUseCase` composition point that already exists in Phase 1's DI wiring,
  not adding a parallel order-execution code path inside `Engine`.
- No `app/engine/backtest.go` exists yet (confirmed by directory listing).

## Target Behavior

```go
// app/engine/simulated_exchange.go (NEW)
package engine

import (
    "context"
    "time"

    "go-trade-bot/app/repository/candle"
    "go-trade-bot/internal/exchange"
)

// FillPolicy parameterizes how SimulatedFillExchange fills an order -
// shared by Backtest (Spec 04, zero friction beyond existing fees) and
// Dry-Run (Spec 09, configurable slippage/fee/delay). This is the single
// type that differentiates the two simulated modes, per ADR-001's "differ
// only in ... which order-execution path is injected."
type FillPolicy struct {
    SlippagePct float64       // applied unfavorably to the fill price; 0 for Backtest
    FillDelay   time.Duration // Backtest: 0 (instant); Dry-Run: configurable (Spec 09)
    // FeePct is NOT part of FillPolicy - the existing 0.1%/0.1% entry/exit
    // fee calculation in app/usecase/signal/usecase.go (calculateEntryFee/
    // calculateExitFee) is unchanged and applies identically in all modes,
    // matching real exchange fee behavior; Spec 09 documents its own
    // override knob separately since it's a Dry-Run-only PRD requirement.
}

// SimulatedFillExchange implements exchange.ExchangeClient. Read methods
// (ListKline, ListTickerPrices, GetAccountBalance, SubscribeKline) delegate
// to an underlying MarketDataSource; PlaceOrder/CancelOrder/GetOrder are
// simulated entirely in-memory according to FillPolicy and the current
// simulated-time candle window.
type MarketDataSource interface {
    // ListKlineBefore mirrors exchange.ExchangeClient.ListKline's shape but
    // is explicitly anchored to "as of the current simulated time" (Spec 01's
    // RangeBefore) rather than "as of wall-clock now" - the no-lookahead
    // guarantee the whole backtest depends on.
    ListKlineBefore(ctx context.Context, symbol, interval string, asOf time.Time, limit int) ([]exchange.Candle, error)
}

func NewSimulatedFillExchange(source MarketDataSource, policy FillPolicy) *SimulatedFillExchange

// SetSimulatedTime advances "now" for the purposes of ListKline's
// asOf-anchoring and for resolving resting STOP_MARKET orders against the
// current candle's High/Low - called once per candle by ReplayDriver
// (below) before invoking Engine.Run.
func (e *SimulatedFillExchange) SetSimulatedTime(t time.Time, currentCandle exchange.Candle)
```

```go
// app/engine/replay_driver.go (NEW)
package engine

// ReplayDriver drives Engine.Run once per candle delivered by a Feed,
// maintaining the rolling candleWindow-sized buffer Engine.buildContext
// expects from ListKline. Shared by Backtest (fed by ReplayFeed, Spec 03)
// and Dry-Run (fed by LiveFeed, Spec 09) - the only difference between the
// two call sites is which feed.Feed and which FillPolicy are constructed.
type ReplayDriver struct {
    Feed       feed.Feed
    Exchange   *SimulatedFillExchange
    Engine     *Engine // unmodified Phase 1 Engine
    Strategy   strategies.Strategy
    DBStrategy entities.Strategy
    Symbol     string
    Mode       strategies.ExecutionMode // ModeBacktest or ModeDryRun
}

// Run consumes the Feed to exhaustion (Backtest) or until ctx is cancelled
// (Dry-Run), calling Engine.Run once per candle. Returns the accumulated
// trade log for Spec 06's metrics computation.
func (d *ReplayDriver) Run(ctx context.Context) ([]TradeLogEntry, error)
```

### Fill simulation semantics (the precision the coordinator flagged as most important)

**Backtest mode** (`FillPolicy{SlippagePct: 0, FillDelay: 0}`):
- A market `PlaceOrder` (buy or sell) fills **instantly, at the current candle's `Close` price** — the same
  price `Engine.buildContext` sets as `Context.Price` (`candles[len(candles)-1].Close`), so a strategy's
  `GoLong`-computed price and the simulated fill price are identical, by construction, in Backtest mode.
  This matches PRD §4.4's Backtest row: "Simulated by engine... Risk: None" — zero slippage is the
  intentional simplification that makes Backtest "evaluate strategy logic," not "predict exact real-world
  fills" (that's what Dry-Run/Paper progressively add friction for).
- A resting `STOP_MARKET` order is checked against **each subsequent candle's `Low`** (for a long-position
  protective sell-stop): if `candle.Low <= stopPrice`, the stop is considered triggered and fills at exactly
  `stopPrice` (the optimistic convention noted in the blueprint's risk section — assumes sufficient
  liquidity at the stop level, consistent with "zero slippage" for Backtest). The check happens as part of
  `SetSimulatedTime`, before `Engine.Run` is invoked for that candle, so a triggered stop is reflected in
  `Context.Position` (specifically, `Position == nil` after the trigger) by the time the strategy's hooks
  run for that candle — a strategy never sees a hook call for a candle where its position was just
  stopped-out mid-candle; the stop's `GenerateSellSignal`-equivalent bookkeeping happens synchronously
  inside `SimulatedFillExchange`/`ReplayDriver`, not through a second `Engine.Run` invocation.
- `CancelOrder`/`GetOrder` operate against the in-memory resting-order table `SimulatedFillExchange` owns —
  no real exchange sentinel errors (`exchange.ErrOrderNotFound`, Spec 03 Phase 1) are needed for the
  *happy* backtest path since there's no real race with a concurrent exchange-side fill, but
  `SimulatedFillExchange` still returns `ErrOrderNotFound` for a `CancelOrder` call against an
  already-stop-triggered order, so `app/usecase/signal.GenerateSellSignal`'s existing
  `reconcileAlreadyStoppedPosition` code path (Phase 1, unchanged) exercises identically in Backtest mode —
  this is deliberate reuse, not a backtest-specific carve-out.

**Dry-Run mode**: see Spec 09 — different `FillPolicy` values, different `MarketDataSource` (backed by a
real `ExchangeClient`, not stored candles), same `SimulatedFillExchange`/`ReplayDriver` types.

### No-lookahead guarantee

`SimulatedFillExchange.ListKline` (satisfying `exchange.ExchangeClient`) internally calls
`MarketDataSource.ListKlineBefore(ctx, symbol, interval, e.currentSimulatedTime, limit)` — **never** a plain
`Range` query — so Grid's `fetch24hVolume` and Scalping's `ConfigKeyLongTermCandles` supplementary fetches
(both routed through `e.Exchange.ListKline` inside `Engine.buildContext`, per Current Behavior) are
automatically anchored to "as of the current backtest candle," not "as of the full imported dataset." This
is the single most important correctness property of this spec: without it, a backtested strategy could see
future 15m/1d data its live counterpart could never have had, silently inflating backtest performance.

## Acceptance Criteria

1. **Given** a `ReplayDriver` wired with `ReplayFeed` over a 100-candle 1m range for `BTCUSDT`, **when**
   `Run` executes, **then** `Engine.Run` is invoked exactly once per candle after the initial
   `candleWindow`-sized warm-up (see Acceptance Criterion #7), each time with `Context.Candles` reflecting
   only candles up to and including that step's current candle.
2. **Given** `ShouldLong` returns `true` and `GoLong` returns a `Buy` order at the current candle's close
   price, **when** the driver processes the resulting signal, **then** `SimulatedFillExchange.PlaceOrder`
   fills at exactly that price with `ExecutedQty` equal to the requested quantity (no partial fills in
   Backtest mode — matches "Risk: None," a backtest doesn't model liquidity constraints).
3. **Given** an open position with a resting simulated stop-loss, **when** a subsequent candle's `Low`
   crosses the stop price, **then** the position is closed at exactly the stop price, `AccountUseCase.AddOrder`
   is called with the resulting P&L (via the unmodified `app/usecase/signal.GenerateSellSignal` reconciliation
   path), and the strategy's `UpdatePosition` hook is **not** called for that candle (the position no longer
   exists by the time hooks run, per the ordering described in Target Behavior).
4. **Given** Grid's `Before` hook needs 24h volume via `ConfigKey24hVolume`, **when** the backtest is running
   at simulated time `T`, **then** the volume figure reflects only candles with `OpenTime <= T` — verified by
   constructing a fixture where a large-volume candle exists *after* `T` and asserting it does not affect the
   computed volume.
5. **Given** the same fixture as Acceptance Criterion #4 but for Scalping's 15m long-term-candle fetch,
   **when** the backtest evaluates `ShouldLong`'s uptrend gate at simulated time `T`, **then** the EMA
   computed from `ConfigKeyLongTermCandles` uses only 15m candles with `OpenTime <= T`.
6. **Given** a full backtest run completes, **when** the trade log is inspected, **then** every trade's
   entry/exit price traces back to an actual candle's `Close` (entries/normal exits) or `stopPrice`
   (stop-loss exits) from the imported dataset — no trade references a price that doesn't correspond to a
   real stored candle or configured stop level.
7. **Edge case — insufficient warm-up data**: **given** a `ReplayFeed` range shorter than `candleWindow`
   (e.g. only 40 candles available for a strategy needing 100 for its RSI/grid-build math), **when** the
   driver runs, **then** it still calls `Engine.Run` per candle as data becomes available (consistent with
   Spec 08 Phase 1's existing "not enough candles" error-handling contract inside each strategy's `Before`/
   `ShouldLong` — the ported strategies already guard against undersized windows), rather than the driver
   itself withholding all execution until exactly `candleWindow` candles have accumulated — this matches
   how a **live** strategy would behave on a fresh symbol with limited history, preserving Backtest/Live
   parity (Spec 05) at the edges too.
8. **Edge case — zero available balance mid-backtest**: **given** the simulated account's balance reaches
   zero partway through a backtest (a losing streak), **when** subsequent buy signals occur, **then** the
   existing `AccountUseCase.CanOpenOrder` gate (unchanged Phase 1 logic inside `GenerateBuySignal`) correctly
   suppresses further entries — the backtest doesn't need its own separate "can afford" check, since it's
   already routed through the same `SignalUseCase` Phase 1 built for live trading.

## Out of Scope

- Multi-symbol backtest orchestration (running the full monitored-symbols list of a strategy concurrently
  within one backtest run) — Spec 10's usecase layer is responsible for looping `ReplayDriver` per symbol
  and aggregating; this spec covers one `(strategy, symbol)` driver instance.
- Partial fills / liquidity modeling in Backtest mode — explicitly zero per PRD's mode table; only Dry-Run
  (Spec 09) and real exchange behavior (Phase 1, live) model anything beyond instant full fills.
- Walk-forward window splitting — Spec 08 builds on top of this engine, not part of it.

## Dependencies

- Spec 01 (Candle Storage), Spec 03 (ReplayFeed) — data sourcing.
- Phase 1's `app/engine/engine.go`, `app/usecase/signal/usecase.go` — reused **unmodified** at the code
  level; this spec's `SimulatedFillExchange`/`ReplayDriver` are purely additive new files, per ADR-001's
  design intent now confirmed achievable without touching either existing file.
- **Judgment call**: the `SimulatedFillExchange` + `ReplayDriver` split (rather than adding backtest-mode
  branches inside `Engine`/`SignalUseCase` themselves) is this spec's central architectural decision. It
  keeps Phase 1's already-shipped, already-tested code completely untouched, at the cost of introducing two
  new abstractions (`MarketDataSource`, `FillPolicy`) not named anywhere in the PRD or blueprint. Flagging
  for explicit sign-off since the blueprint's own text ("app/engine/engine.go — refactored so Run(feed Feed,
  strategy Strategy, ...) is shared...") implied `Engine.Run`'s signature itself would change; this spec
  argues that's unnecessary given how Phase 1 was actually built, and proposes the less invasive path.
