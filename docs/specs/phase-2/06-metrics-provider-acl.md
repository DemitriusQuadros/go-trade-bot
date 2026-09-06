# Spec 06 — Metrics Provider ACL (`internal/metrics_provider`)

## Overview

A completed backtest run (Spec 04) produces a trade log; nothing today converts that into the Sharpe Ratio,
Max Drawdown, Win Rate, and Profit Factor figures PRD §4.3 requires as the decision gate for advancing a
strategy toward Dry-Run. This spec defines `MetricsProvider`, the ACL wrapping `cinar/indicator/v2` (not yet
a dependency — verified below), plus the exact trade-log-to-metrics computation each figure requires.

## Current Behavior (verified)

- `go.mod` has no `cinar/indicator/v2` entry (confirmed by grep — no `cinar`, `gonum`, `grpc`, or
  `bubbletea` strings present anywhere in `go.mod`). This is a **net-new dependency addition** for Phase 2,
  exactly as the blueprint anticipated.
- No `internal/metrics_provider` package exists (confirmed by directory listing of `internal/`).
- No trade-log or backtest-run entity exists yet — this spec's input type (`[]TradeLogEntry`, from Spec 04's
  `ReplayDriver.Run`) is itself new in this phase; there is no legacy trade-history computation anywhere in
  the codebase to migrate away from (`app/repository/strategy/repository.go:57-69`'s
  `GetStrategyPerformanceBySymbol` computes a simple `sum(profit)`/`count(*)` from **live** `Order` rows —
  related in spirit but a completely different code path, live-only, not touched by this spec).

## Target Behavior

```go
// internal/metrics_provider/interface.go
package metrics_provider

import "time"

// TradeLogEntry mirrors app/engine's per-trade output (Spec 04) - defined
// here (not in app/engine) so this ACL has no import-time dependency on
// app/engine, keeping the dependency direction internal/* -> app/* clean
// (app/engine imports this package, not the reverse).
type TradeLogEntry struct {
    Symbol     string
    EntryTime  time.Time
    EntryPrice float64
    ExitTime   time.Time
    ExitPrice  float64
    Quantity   float64
    Profit     float64 // net of fees, matches entities.Order.Profit's existing convention
    ExitReason string  // "take_profit" | "stop_loss" | "manual" | "band_cross" | "open_at_end"
}

// BacktestMetrics is the computed summary a completed run produces.
type BacktestMetrics struct {
    SharpeRatio        float64 // annualized
    MaxDrawdownPct     float64
    WinRatePct         float64
    ProfitFactor       float64 // gross profit / gross loss; +Inf if zero losing trades
    TotalTrades        int
    AvgTradeDuration   time.Duration
    TotalReturnPct     float64
    EquityCurve        []EquityPoint // for Spec 07's chart, computed here since it's a byproduct of the same walk
}

type EquityPoint struct {
    Time  time.Time
    Value float64 // cumulative account value at this point
}

// MetricsProvider wraps cinar/indicator/v2. No package outside
// internal/metrics_provider may import github.com/cinar/indicator/v2 -
// matching the ACL pattern established by internal/exchange and
// internal/indicators in Phase 1.
type MetricsProvider interface {
    Compute(trades []TradeLogEntry, startingBalance float64, periodsPerYear float64) BacktestMetrics
}
```

`internal/metrics_provider/cinar_adapter.go` implements `MetricsProvider`, using `cinar/indicator/v2`'s
statistics helpers where they directly apply (e.g. drawdown/return-series utilities) and hand-rolled Go for
figures `cinar/indicator/v2` doesn't compute in the exact shape needed (Profit Factor and Win Rate are
simple enough — gross-profit/gross-loss and win-count/total-count ratios — that they're computed directly
from the `[]TradeLogEntry` rather than forced through a library call that doesn't naturally fit).

### Per-metric computation

- **Sharpe Ratio (annualized)**: computed from the **equity curve's period-over-period returns** (not
  per-trade returns, which would be biased by trade frequency) — `periodsPerYear` is derived from the
  strategy's configured `Cycle`/timeframe (e.g. `1m` → `525600`, `1h` → `8760`), so a Grid strategy on a 1m
  cycle and a Bollinger strategy on a 1h cycle both get correctly annualized Sharpe figures despite
  radically different sampling frequency — annualized as `mean(returns) / stddev(returns) * sqrt(periodsPerYear)`,
  the standard convention.
- **Max Drawdown %**: `max((peak - trough) / peak)` walked across the equity curve — the largest
  peak-to-trough decline, expressed as a percentage of the peak.
- **Win Rate %**: `winning_trades / total_trades * 100`, where a "winning trade" is `Profit > 0` (a
  break-even trade at exactly `Profit == 0` counts as **not** a win — a strict `> 0` threshold, documented
  explicitly since a boundary ambiguity here would make the pass/fail threshold discussion (blueprint §8's
  open question) unreliable).
- **Profit Factor**: `sum(profit for winning trades) / abs(sum(profit for losing trades))`. If there are
  zero losing trades, this is `+Inf` (not a divide-by-zero panic) — represented in `BacktestMetrics` as
  `math.Inf(1)`, which Spec 07's report/Spec 10's persistence layer must handle explicitly (JSON-encoding
  `+Inf` requires care — see Spec 10 Acceptance Criteria).
- **Avg Trade Duration**: mean of `ExitTime - EntryTime` across all closed trades; trades still open at
  backtest end (`ExitReason: "open_at_end"`, per Spec 04/05) are **excluded** from this average (an
  unresolved position has no meaningful duration to average in).
- **Total Return %**: `(finalEquity - startingBalance) / startingBalance * 100`.

## Acceptance Criteria

1. **Given** a trade log with 10 trades, 6 winners summing to `+600` profit and 4 losers summing to `-200`
   profit, **when** `Compute` runs, **then** `WinRatePct = 60.0` and `ProfitFactor = 3.0` (`600 / 200`).
2. **Given** a trade log with zero losing trades, **when** `Compute` runs, **then** `ProfitFactor` is
   `+Inf`, not a panic and not `0`.
3. **Given** an equity curve with a peak of `1000` followed by a trough of `700` before recovering, **when**
   `Compute` runs, **then** `MaxDrawdownPct = 30.0`.
4. **Given** a 1-year backtest on a 1m-cycle strategy with a resulting Sharpe input series, **when**
   `Compute` runs with `periodsPerYear = 525600`, **then** the annualization factor
   (`sqrt(525600)`) is applied — verified by comparing against a hand-computed reference value for a fixed
   synthetic returns series (a standard test-fixture approach for Sharpe calculations).
5. **Given** a trade log where one trade has `ExitReason: "open_at_end"`, **when** `Compute` runs, **then**
   `AvgTradeDuration` excludes that trade's (undefined/zero) duration from the average, but
   `TotalTrades`/`WinRatePct`/`ProfitFactor` still **include** it if it has a computed unrealized `Profit`
   value (Spec 04's driver is responsible for supplying a mark-to-market `Profit` for an open-at-end
   position using the last available candle's close, so metrics aren't silently incomplete for a run that
   ends mid-position).
6. **Edge case — empty trade log**: **given** zero trades occurred during the entire backtest range, **when**
   `Compute` runs, **then** it returns a `BacktestMetrics` with `TotalTrades: 0`, `WinRatePct: 0`,
   `ProfitFactor: 0` (not `+Inf` and not `NaN` — a strategy that never traded has no profit factor to speak
   of, and `0` is the documented sentinel for "no data," distinct from the "`+Inf`, traded but never lost"
   case in Acceptance Criterion #2), and `TotalReturnPct: 0`.
7. **Edge case — single trade**: **given** exactly one trade, **when** `Compute` runs, **then** Sharpe Ratio
   is computed from a single-point returns series — `cinar_adapter.go` must not panic on a zero-variance
   (single-sample) standard deviation computation; the documented behavior is to return `0` for Sharpe in
   this case (a single data point carries no meaningful variance-based ratio), not `NaN` propagating into
   downstream JSON serialization (Spec 10).

## Out of Scope

- Hyperparameter optimization's use of these metrics to rank parameter combinations — Phase 4.
- Monte Carlo simulation (trade-order randomization) — Phase 4.
- Walk-forward's per-window metrics aggregation — Spec 08 calls `Compute` once per window; this spec defines
  the single-run computation, not the walk-forward orchestration.

## Dependencies

- Spec 04 (Backtest Engine) — `TradeLogEntry` is produced by `ReplayDriver.Run`.
- **Judgment call**: the PRD's own suggested pass/fail threshold ("Sharpe > 0.8, Max Drawdown < 20%," §10)
  is **not** computed or enforced by this spec — `MetricsProvider.Compute` returns raw figures only. The
  pass/fail verdict (blueprint's open question, "needs a concrete threshold to render pass/fail") is
  resolved in Spec 10 as a configurable default adopting the PRD's own suggested values, not hardcoded here
  — flagging the split so it's clear this ACL stays a pure calculator, with policy layered on top elsewhere.
