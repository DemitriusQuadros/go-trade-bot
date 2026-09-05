# Spec 07 — Indicator Provider ACL (`internal/indicators`)

## Overview

All three current strategies import `github.com/markcheno/go-talib` directly — no ACL exists. This spec
defines `IndicatorProvider`, wrapping `go-talib`, scoped precisely to what the three strategies actually
call today (verified below), not just the PRD's illustrative RSI/BBands/EMA/MACD/ATR list. It also resolves
a fidelity gap the PRD's illustrative interface doesn't account for: the current Bollinger strategy uses an
EMA-smoothed middle band, not the plain/SMA-smoothed bands a naive reading of the PRD's signature implies.

## Current Behavior (verified — exact `go-talib` call sites)

- `app/services/algorithm/grid/algorithm.go:170` — `talib.Rsi(closes, int(rsiPeriodFloat))` — period comes
  from strategy config (`rsi_period`, e.g. `14` in `docs/strategy-examples/grid.json`), **not** hardcoded.
- `app/services/algorithm/bollinger/algorithm.go:69` — `talib.BBands(closes, 20, 2.0, 2.0, talib.EMA)` —
  period `20` and both stdDev multipliers `2.0` are **hardcoded** in the algorithm (not read from strategy
  config, despite `docs/strategy-examples/bollinger.json` existing), and critically, **the moving-average
  type parameter is `talib.EMA`**, not the default SMA. This means the "middle band" and the smoothing
  basis for upper/lower bands are EMA-based, not SMA-based.
- `app/services/algorithm/scalping/algorithm.go:120` — `talib.Rsi(closes, 14)` — period hardcoded to `14`
  (used only for a `> 70` overbought filter in `validateRSI`, not stored as a strategy config value).
- `app/services/algorithm/scalping/algorithm.go:174` — `talib.Ema(longCloses, 20)` — period hardcoded to
  `20`, computed over a **separate 15-minute-interval kline fetch** (`ListKline(ctx, symbol, "15m", 50)`,
  `scalping/algorithm.go:163`), independent of the strategy's own configured cycle interval — this is a
  hardcoded cross-timeframe read that Phase 1's single-timeframe `Context.Candles` (Spec 05) does not
  natively support; see Acceptance Criteria #6 and Out of Scope.
- No current code calls `talib.Macd`, `talib.Sma`, or `talib.Atr` — these appear only in the PRD's
  illustrative interface (§5), not in any current strategy.

## Target Behavior

```go
// internal/indicators/interface.go
package indicators

import "go-trade-bot/internal/exchange"

type MAType int
const (
    MATypeSMA MAType = iota
    MATypeEMA
)

type IndicatorProvider interface {
    RSI(candles []exchange.Candle, period int) []float64
    BollingerBands(candles []exchange.Candle, period int, stdDevUp, stdDevDown float64, maType MAType) (upper, mid, lower []float64)
    EMA(candles []exchange.Candle, period int) []float64
    SMA(candles []exchange.Candle, period int) []float64
    MACD(candles []exchange.Candle, fastPeriod, slowPeriod, signalPeriod int) (macd, signal, hist []float64)
    ATR(candles []exchange.Candle, period int) []float64
}
```

**Judgment call / deviation from PRD §5's illustrative signature**: the PRD's `BollingerBands` signature
takes a single `stdDev float64`, and doesn't mention a moving-average-type parameter at all. This spec adds
`stdDevDown float64` (matching `talib.BBands`'s actual up/down asymmetric-multiplier signature — currently
called with the same value `2.0` for both, but the underlying capability exists and costs nothing to
expose) and a `maType MAType` parameter, because **without it, the ported Bollinger strategy cannot
reproduce its current EMA-smoothed band behavior**, which would violate ADR-003's "no behavioral change"
mandate. `talib_adapter.go`'s `BollingerBands` implementation maps `MATypeEMA` → `talib.EMA` and
`MATypeSMA` → `talib.SMA` when calling the underlying `talib.BBands(closes, period, stdDevUp, stdDevDown, matype)`.

`internal/indicators/talib_adapter.go` is the **only file in the repository permitted to import
`github.com/markcheno/go-talib`** (enforced by the same CI import-lint as Spec 01's `go-binance`
restriction). It converts `[]exchange.Candle` to the `[]float64` close-price slices `go-talib` expects
internally — callers never touch raw float slices or know `go-talib` exists.

`SMA`, `MACD`, `ATR` are implemented as straightforward wrappers (required to satisfy the frozen interface
per ADR-005's "interfaces don't change after first port" principle, and because Phase 2's backtest engine
and future strategies will need them) but are **not exercised by any Phase 1 acceptance criteria below**
since no current strategy calls them — they get a basic smoke test (doesn't panic, returns a slice of the
expected length) rather than the behavioral-fidelity tests RSI/BollingerBands/EMA require.

## Acceptance Criteria

1. **Given** a slice of 100 candles and `period = 14`, **when** `IndicatorProvider.RSI(candles, 14)` is
   called, **then** the returned `[]float64` is identical (within float64 equality, no tolerance needed
   since it's the same underlying `talib.Rsi` call) to calling `talib.Rsi(closes, 14)` directly on the
   candles' `Close` values in order.
2. **Given** the same 30-candle input the current `bollinger/algorithm.go:69` uses, **when**
   `IndicatorProvider.BollingerBands(candles, 20, 2.0, 2.0, MATypeEMA)` is called, **then** the three
   returned slices (`upper, mid, lower`) are identical to the current
   `talib.BBands(closes, 20, 2.0, 2.0, talib.EMA)` call's output — this is the specific fidelity check that
   validates the `maType` parameter addition was necessary and correct.
3. **Given** `IndicatorProvider.BollingerBands(candles, 20, 2.0, 2.0, MATypeSMA)` is called instead,
   **then** the output **differs** from Acceptance Criterion #2's EMA-based result — proving the parameter
   actually changes adapter behavior, not just accepted-and-ignored.
4. **Given** the same 50-candle 15m-interval input the current `scalping/algorithm.go:174` uses, **when**
   `IndicatorProvider.EMA(candles, 20)` is called, **then** the output is identical to
   `talib.Ema(longCloses, 20)`.
5. **Given** `RSI`/`BollingerBands`/`EMA` are called with fewer candles than the period requires (e.g.
   `RSI(candles[:5], 14)`), **when** the underlying `go-talib` function returns a short or
   partially-NaN-leading slice (its documented behavior for insufficient input), **then** the adapter
   passes this through unchanged rather than panicking or silently padding — callers (ported strategies,
   per Spec 08) are responsible for the same "not enough candles" guard they already implement today
   (`bollinger/algorithm.go:51-53`, `scalping/algorithm.go:52-54`).
6. **Edge case — cross-timeframe EMA call** (current `scalping/algorithm.go:163-174`'s separate 15m fetch):
   **given** Phase 1's `Context.Candles` is single-timeframe only (Spec 05, Out of Scope: multi-timeframe
   is Phase 3), **when** the scalping strategy is ported (Spec 08), **then** it must perform its own
   supplementary `ListKline`-equivalent fetch for the 15m trend candles rather than relying on
   `Context.Candles` — this is called out explicitly here because it means the ported scalping strategy is
   the **one exception** to Spec 05's "strategies never touch the exchange directly" principle; resolving
   this cleanly (e.g. via a narrow read-only capability passed through `Context`, distinct from full
   `ExchangeClient` access) is flagged as an open question for Spec 08, not resolved by this indicator spec.
7. **Edge case — MACD/SMA/ATR unused-but-present**: **given** no Phase 1 strategy calls `MACD`, `SMA`, or
   `ATR`, **when** each is smoke-tested with a representative candle slice, **then** each returns a
   non-nil, correctly-shaped result (matching `go-talib`'s own return-slice length semantics) without
   requiring a specific behavioral fixture — these are lower-priority correctness checks, not fidelity
   checks, since there is no "current behavior" to preserve for functions nothing calls yet.

## Out of Scope

- Multi-timeframe candle resolution as a first-class `IndicatorProvider`/`Context` concept — Phase 3.
  Acceptance Criterion #6 documents the Phase 1 workaround, it does not solve the general problem.
- `cinar/indicator/v2`-backed indicators or metrics (Sharpe, Drawdown, etc.) — that's a **separate** ACL
  (`internal/metrics_provider`, Phase 2 per blueprint §3.2/§4), not this one. `IndicatorProvider` wraps
  `go-talib` only, per PRD §6's library-to-role mapping.
- Making Bollinger's period/stdDev strategy-configurable (currently hardcoded `20`/`2.0`/`2.0` in the
  algorithm, not read from `docs/strategy-examples/bollinger.json`'s config despite the config file
  existing) — ADR-003 requires behavior-preserving porting, not improving the strategy; the ported version
  keeps these hardcoded, matching current behavior exactly, even though it's an obvious future improvement.

## Dependencies

- Spec 01 (Exchange ACL) — `exchange.Candle` type is the input shape for all `IndicatorProvider` methods.
- Spec 05 (Strategy Interface) — `Context.Indicators IndicatorProvider` field.
