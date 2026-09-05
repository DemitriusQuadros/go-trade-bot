# Spec 08 — Strategy Porting: Grid, Bollinger, Scalping

## Overview

Per ADR-003, the three existing algorithms are ported into the new `Strategy` hook shape (Spec 05) with
behavior held **exactly** constant — this spec is the acceptance-criteria source for that constancy,
written directly from the current `algorithm.go` source (not paraphrased from the PRD, which doesn't
describe the strategies' actual logic at all). Each strategy's current trigger logic is transcribed below
as the bit-for-bit reproduction target, followed by the characterization-test approach that must pass
before the old files are deleted.

## Current Behavior (verified, per strategy)

### Grid (`app/services/algorithm/grid/algorithm.go`)

Two-phase behavior keyed by whether a grid has been built yet (`internal/memcache`, key `"grid-" + symbol`):

- **Build phase** (`buildGridForSymbol`, no grid cached): fetches 100 klines at the strategy's cycle
  interval; reads config `grid_levels`, `grid_spacing_pct`, `volume_filter`, `take_profit_pct`,
  `rsi_period`, `rsi_buy_threshold`, `rsi_sell_threshold`; computes `rsi = talib.Rsi(closes, rsi_period)`,
  `currentRSI = rsi[len(rsi)-1]`; if `volume_filter > 0` and `Get24hVolume < volume_filter`, **skip symbol
  entirely** (no grid built, function returns nil); if `currentRSI > rsi_sell_threshold`, **skip symbol**
  (line 187-190); otherwise builds `gridLevels` price points centered on `latestClose`, spaced by
  `latestClose * grid_spacing_pct / 100`: for each price below `latestClose`, if `currentRSI <
  rsi_buy_threshold`, add a `"buy"` grid order at that price (and set `hasBuySignal = true`); for each price
  above `latestClose`, if `((price - latestClose) / latestClose) * 100 >= take_profit_pct AND
  hasBuySignal`, add a `"sell"` grid order at that price. If any grid entries were produced, cache them
  under `"grid-" + symbol`.
- **Monitor phase** (`monitore`, grid already cached): reads `stop_loss_pct` from config; fetches current
  ticker price; if an open signal exists, computes `pnl = (current - entryPrice)/entryPrice * 100`; if `pnl
  <= -stop_loss_pct`, sells immediately (stop-loss, first check, before grid iteration). Then iterates
  cached grid orders in order: for a `"buy"` order, if `current <= order.Price`, triggers
  `GenerateBuySignal` and **returns immediately** (`return p.usecase.GenerateBuySignal(...)` — stops
  iterating the rest of the grid on the first triggered buy). For a `"sell"` order, if `current >=
  order.Price` **and** an open signal exists, triggers `GenerateSellSignal` and returns immediately.
  Grid orders are never removed from the cache after triggering — the same cached grid persists until the
  next full rebuild is somehow triggered (which, per current code, only happens if the cache entry is
  cleared some other way — there is no explicit expiry/rebuild-trigger logic visible in this file).

### Bollinger (`app/services/algorithm/bollinger/algorithm.go`)

Single-phase, no cross-cycle cache. Fetches 30 klines at cycle interval; requires `≥20` klines or returns
an error (`"not enough candles to analyze"`); computes `upper, _, lower :=
talib.BBands(closes, 20, 2.0, 2.0, talib.EMA)` (period/stdDev/matype **hardcoded**, not from
config, despite `take_profit_pct`/`stop_loss_pct` being read from config); `current = closes[len(closes)-1]`.
If an open signal exists, calls `generateSell`: fetches a fresh ticker price (not the kline close),
computes `pnl`, and sells if `pnl >= take_profit_pct OR pnl <= -stop_loss_pct OR current > upper[last]`
(three independent exit conditions in one OR). If no open signal exists, buys if `current < lower[len(lower)-1]`
(price below the lower Bollinger band).

### Scalping (`app/services/algorithm/scalping/algorithm.go`)

Single-phase, no cross-cycle cache. Fetches 60 klines at cycle interval; requires `≥10` or errors. If an
open signal exists, calls `generateSell`: fetches fresh ticker price, sells if `pnl >= take_profit_pct OR
pnl <= -stop_loss_pct` (two conditions, no Bollinger-style third condition). If no open signal, entry logic
requires **all three** to be true: `validateVolume(klines)` (latest candle's volume ≥ the simple average
volume across all fetched klines), `validateRSI(klines)` (`talib.Rsi(closes, 14)`'s last value `≤ 70`, i.e.
not overbought — note this is a much looser gate than Grid's config-driven thresholds), and
`p.isUptrend(ctx, symbol)` (separate 15m/50-candle fetch, `talib.Ema(longCloses, 20)`, true if
`currentPrice > currentEMA`). If all three pass, **and** `latestClose > prevClose` (last two closes of the
cycle-interval kline series are rising), buys at `latestClose`.

## Target Behavior

Each strategy becomes a package under `app/strategies/{grid,bollinger,scalping}` implementing the frozen
`Strategy` interface (Spec 05). The porting mapping (not a redesign):

| Current construct | Ported construct |
|---|---|
| `NewGridProcessor(strategy, broker, usecase, cache).Execute()` looping `MonitoredSymbols` | Engine (not the strategy) loops symbols, building one `Context` per symbol per cycle, calling hooks (Spec 05) — the strategy struct itself becomes stateless per PRD §8's "no global mutable state" principle, **except** Grid's built-grid cache, which is explicitly allowed to remain a struct-level or `Context.Config`-adjacent state per Spec 05's "stateful only via explicit struct fields" allowance. `internal/memcache` is retained as the storage for it (blueprint §3.2: `internal/memcache/memcache.go` `[KEEP]`), now injected into the `GridStrategy` struct directly rather than passed as a per-call parameter. |
| Grid's build-phase RSI/volume/sell-threshold gating (`buildGridForSymbol`) | Happens inside `Before(ctx)` when `ctx.Position == nil` and no cached grid exists for the symbol — populates the strategy's cache exactly as today, using `ctx.Indicators.RSI` (Spec 07) instead of `talib.Rsi` directly. |
| Grid's monitor-phase stop-loss check (`monitore`'s first branch) | **Removed from the strategy entirely** — stop-loss is now a real exchange order (Spec 03), submitted by the engine at `GoLong` time, not evaluated per-cycle by the strategy. This is the one place ADR-003's "no behavioral change" is **intentionally** superseded, because Spec 03 is itself a Phase 1 MUST-have that structurally replaces software-polled stop-loss — the take-profit-equivalent grid `"sell"` trigger logic is preserved, the stop-loss-percent check is not (it becomes redundant with the exchange-side stop). |
| Grid's monitor-phase buy/sell grid-order iteration | `ShouldLong`/`GoLong` (buy branch) and `UpdatePosition` (sell branch) respectively, preserving the "first match wins, stop iterating" semantics exactly. |
| Bollinger's single-function buy/sell logic | `ShouldLong` = `current < lower[last]`; `GoLong` returns `Signal{Buy: &Order{Qty: ctx.Account.Available / ctx.Price, Price: ctx.Price}}` (quantity-sizing formula preserved from `AccountUseCase.GetDisponibleAmout`'s existing division-based sizing); `UpdatePosition` reproduces the three-condition OR exit (`take_profit_pct` / `stop_loss_pct`-as-take-profit-only-now-see-below / `current > upper[last]`). |
| Bollinger's `stop_loss_pct` condition inside `generateSell`'s OR | **Removed from the strategy entirely, matching Grid's treatment** — resolved by explicit sign-off (2026-08-31): all three strategies treat Spec 03's real exchange `STOP_MARKET` order as the sole stop-loss mechanism, replacing rather than supplementing each strategy's software `stop_loss_pct` check. `UpdatePosition` retains only the `take_profit_pct` and band-cross (`current > upper[last]`) conditions from the original three-condition OR. |
| Scalping's volume/RSI/uptrend/rising-close entry gate | `ShouldLong` reproduces all four conditions exactly, including the separate 15m EMA fetch (Spec 07 Acceptance Criterion #6's flagged exception). `GoLong` mirrors Bollinger's sizing formula. |
| Scalping's two-condition exit OR | **`stop_loss_pct` removed, same as Grid and Bollinger** — `UpdatePosition` retains only the `take_profit_pct` condition from the original `take_profit_pct OR stop_loss_pct` OR; the exchange-side stop (Spec 03) is the sole stop-loss mechanism for all three ported strategies. |

## Characterization Test Approach

Before any `app/services/algorithm/*` file is deleted:

1. Record real (or realistic synthetic) kline fixtures for each strategy — at minimum: one fixture that
   triggers a buy, one that triggers a hold, one that triggers a sell via each distinct exit condition
   (e.g. Bollinger needs three separate sell fixtures: take-profit, stop-loss, band-cross).
2. Write a test that runs the **current** `algorithm.go` code against each fixture (using mocked
   `Broker`/`SignalUseCase` per the existing `mocks/` pattern already established for `signal`/`strategy`
   packages) and records the exact `EntrySignal`/`ExitSignal` values passed to
   `GenerateBuySignal`/`GenerateSellSignal` (via `testify/mock`'s call-argument capture).
3. Run the **same fixtures** through the ported `Strategy` hook implementation (via the engine's hook-call
   sequence, Spec 05) and assert the resulting `Signal.Buy`/`Signal.Sell` order `Qty`/`Price` values match
   the captured values from step 2 exactly.
4. Only after all fixtures pass identically for all three strategies are the old `algorithm.go` files
   deleted in the same PR that removes them from `app/handler/tasks/strategy/handler.go`'s switch.

## Acceptance Criteria

1. **Given** the Grid build-phase fixture where `volume_filter > 0` and 24h volume is below it, **when**
   the ported `GridStrategy`'s `Before` hook runs, **then** no grid is built/cached for that symbol,
   matching current behavior's early return.
2. **Given** the Grid build-phase fixture where `currentRSI > rsi_sell_threshold`, **when** `Before` runs,
   **then** no grid is built, matching current behavior.
3. **Given** a cached grid with both `"buy"` and `"sell"` entries and a current price that satisfies the
   first `"buy"` entry in iteration order, **when** `ShouldLong`/`GoLong` run, **then** exactly one buy
   signal is produced and no further grid entries are evaluated in the same cycle (first-match-wins
   preserved).
4. **Given** an open Grid position and a current price crossing a cached `"sell"` grid level, **when**
   `UpdatePosition` runs, **then** it returns `&Signal{Sell: &Order{...}}` at that level's price, matching
   current `monitore`'s sell-trigger behavior — and does **not** additionally evaluate a software
   stop-loss-pct condition (per the table above, Grid's software stop-loss is intentionally removed).
5. **Given** the exact 30-candle Bollinger fixture that currently triggers a buy (`current < lower[last]`
   using `talib.BBands(..., talib.EMA)`), **when** the ported strategy's `ShouldLong`/`GoLong` run using
   `ctx.Indicators.BollingerBands(candles, 20, 2.0, 2.0, indicators.MATypeEMA)` (Spec 07), **then** the
   produced `Order.Price` and computed `Qty` match the characterization-captured values from the current
   implementation exactly.
6. **Given** two separate Bollinger open-position fixtures (take-profit-only, band-cross-only), **when**
   `UpdatePosition` runs on each, **then** each produces a `Sell` signal, reproducing the surviving
   two-condition OR from `generateSell` individually. **Given** a third fixture that would have triggered
   only the original `stop_loss_pct` branch (price down `stop_loss_pct`%, but not past the lower/upper band
   and not yet at take-profit), **when** `UpdatePosition` runs, **then** it produces **no** software sell
   signal — protection for this case now comes exclusively from Spec 03's resting exchange `STOP_MARKET`
   order, not from the ported strategy logic.
7. **Given** the exact Scalping entry fixture satisfying all four gate conditions (volume, RSI, uptrend,
   rising close), **when** `ShouldLong`/`GoLong` run, **then** a buy is produced; **given** the same
   fixture with only the 15m-uptrend condition flipped to false, **when** `ShouldLong` runs, **then** no
   buy is produced (each of the four gates must be independently load-bearing, not accidentally
   short-circuited by the port).
8. **Edge case — insufficient candles**: **given** fewer than 20 candles for Bollinger or fewer than 10 for
   Scalping (matching the current hard-coded minimums), **when** `Before`/`ShouldLong` run, **then** the
   strategy signals an error state equivalent to the current `fmt.Errorf("not enough candles to analyze")`
   (surfaced as a `StrategyExecution{Status: Error}` per Spec 05's error-handling contract), not a panic
   from indexing an undersized slice.
9. **Edge case — Grid cache never rebuilds**: **given** a cached grid whose orders have all triggered
   (Grid's current code has no visible rebuild trigger — see Current Behavior), **when** subsequent cycles
   run, **then** the ported strategy reproduces this exact (arguably buggy but currently-real) behavior of
   not rebuilding the grid automatically — fixing this is explicitly out of scope for a behavior-preserving
   port; note it as a known limitation carried forward, not silently fixed.

## Out of Scope

- Fixing Grid's apparent missing-rebuild-trigger behavior (Acceptance Criterion #9) — carried forward as-is
  per ADR-003.
- Making Bollinger's period/stdDev or Grid's software-stop-loss-removal configurable per-strategy — these
  are structural porting decisions (see table), not user-facing config options.
- Resolving Scalping's cross-timeframe exchange access cleanly — flagged in Spec 07 as an open question,
  not solved here; Phase 1 accepts the strategy doing its own supplementary fetch as an explicit interface
  exception.

## Dependencies

- Spec 05 (Strategy Interface and Context) — hook shapes this porting targets.
- Spec 06 (Strategy Registry) — each ported strategy self-registers via `init()`.
- Spec 07 (Indicator Provider) — `RSI`/`BollingerBands`/`EMA` calls route through this ACL.
- Spec 03 (Stop-Loss) — **all three** strategies' software stop-loss removal is contingent on the real
  exchange stop existing; Spec 03 and this spec must land in the same PR/milestone for every strategy, not
  with a gap where any of the three has neither mechanism active.
- **Resolved judgment call** (2026-08-31 sign-off): ADR-003 says "preserve behavior" but doesn't say whether
  Spec 03's new exchange-side stop should *replace* or *supplement* each strategy's existing software
  `stop_loss_pct` check. Decision: **replace, uniformly across Grid, Bollinger, and Scalping** — the
  exchange `STOP_MARKET` order is the sole stop-loss mechanism post-port; no strategy retains a software
  stop-loss branch. This is a deliberate, signed-off exception to strict bit-for-bit behavior preservation,
  scoped narrowly to the stop-loss condition only — every other exit/entry condition in each strategy is
  preserved exactly per the Acceptance Criteria above.
