# Spec 08 — Walk-Forward Validation

## Overview

A single backtest's Sharpe Ratio is an in-sample figure — the strategy's config parameters may simply be
overfit to that exact historical window. Walk-forward validation splits history into rolling train/test
windows and reports **out-of-sample** performance separately, which is what actually predicts forward
viability. Blueprint §6 calls the risk this mitigates ("Backtest → Dry-Run" overconfidence) directly; this
spec builds on Spec 04's backtest engine rather than introducing a separate execution path.

## Current Behavior (verified)

No walk-forward logic, window-splitting utility, or related entity exists anywhere in the codebase — fully
greenfield, building on top of Specs 01-06.

## Target Behavior

```go
// app/engine/walkforward.go
package engine

type WalkForwardConfig struct {
    TotalRange   TimeRange     // full available history to split
    TrainWindow  time.Duration // e.g. 90 days
    TestWindow   time.Duration // e.g. 30 days
    StepSize     time.Duration // how far the window slides per iteration; if < TestWindow, windows overlap
}

type WalkForwardWindow struct {
    Index          int
    TrainRange     TimeRange
    TestRange      TimeRange
    TrainMetrics   metrics_provider.BacktestMetrics // in-sample - reported for reference/comparison only
    TestMetrics    metrics_provider.BacktestMetrics // out-of-sample - this is the number that matters
}

type WalkForwardResult struct {
    Windows           []WalkForwardWindow
    AggregateOOS       metrics_provider.BacktestMetrics // metrics computed from the CONCATENATED
                                                          // out-of-sample trade logs across all windows -
                                                          // not an average of each window's Sharpe, which
                                                          // would be statistically misleading (see AC#3)
}

// RunWalkForward splits cfg.TotalRange into overlapping/adjacent
// train/test windows, running Spec 04's ReplayDriver+SimulatedFillExchange
// once per window per phase (train, then test), and aggregates.
func RunWalkForward(ctx context.Context, cfg WalkForwardConfig, strategy strategies.Strategy, dbStrategy entities.Strategy, symbol string) (WalkForwardResult, error)
```

**Window generation**: given `TotalRange`, `TrainWindow`, `TestWindow`, `StepSize`, windows are generated
starting at `TotalRange.From`, each window being `[start, start+TrainWindow)` for training immediately
followed by `[start+TrainWindow, start+TrainWindow+TestWindow)` for out-of-sample testing, then the next
window starts at `start + StepSize`. Generation stops once a window's test range would extend past
`TotalRange.To`. **Train windows are informational only in Phase 2** — no parameter re-optimization happens
between windows (that's Phase 4's hyperparameter-optimization territory); a Phase 2 walk-forward run uses
the **same fixed strategy configuration** across every train and test window, and the train-phase run exists
purely so `TrainMetrics` can be shown side-by-side with `TestMetrics` for the same window as a sanity
comparison (a huge in-sample/out-of-sample gap is itself the overfitting signal the PRD wants surfaced, even
without automated re-optimization).

**Aggregation**: `AggregateOOS` is computed by **concatenating every window's out-of-sample trade log** and
running `MetricsProvider.Compute` once over the combined list — not by averaging each window's individually-
computed Sharpe/drawdown figures. Averaging per-window Sharpe ratios is a known statistical error (Sharpe
doesn't average meaningfully across non-contiguous periods of different lengths); concatenating trades and
recomputing once is the correct approach and is called out explicitly since it's an easy mistake to make in
implementation.

## Acceptance Criteria

1. **Given** `TotalRange` spanning 12 months, `TrainWindow=90d`, `TestWindow=30d`, `StepSize=30d`, **when**
   `RunWalkForward` executes, **then** it produces approximately 9 windows (12 months minus the first
   120-day train+test period, stepped monthly) — the exact count verified against the window-generation
   rule in Target Behavior, not a hardcoded expectation.
2. **Given** a completed walk-forward run, **when** `AggregateOOS` is computed, **then** its `TotalTrades`
   equals the sum of each individual window's `TestMetrics.TotalTrades` (concatenation, not double-counting
   or dropping trades at window boundaries).
3. **Given** a synthetic scenario where each window's individual Sharpe is highly volatile (e.g. one window
   Sharpe of `5.0` from just 2 lucky trades, another window Sharpe of `-1.0` from 50 trades), **when**
   `AggregateOOS.SharpeRatio` is computed, **then** it reflects the concatenated-trade-log computation (Spec
   06's method applied once to the full out-of-sample trade set), **not** `mean(5.0, -1.0) = 2.0` — this is
   the acceptance criterion that specifically guards against the averaging mistake called out in Target
   Behavior.
4. **Given** `StepSize < TestWindow` (overlapping windows), **when** windows are generated, **then**
   out-of-sample trades from overlapping windows are **not** deduplicated for `AggregateOOS` — each window's
   test-phase trades are counted once per window they occurred in, since overlapping walk-forward is a
   valid, intentional configuration for finer-grained overfitting detection, and the resulting "trades
   counted more than once across the whole aggregate" is an accepted, documented property of choosing
   overlapping windows (not a bug) — this must be explicit so a QA scenario doesn't mistakenly flag it as
   double-counting.
5. **Edge case — insufficient total history**: **given** `TotalRange` is shorter than
   `TrainWindow + TestWindow`, **when** `RunWalkForward` is called, **then** it returns an error before
   attempting any window (zero windows possible) rather than silently returning an empty
   `WalkForwardResult` that could be mistaken for "ran but found nothing."
6. **Edge case — a window with zero out-of-sample trades**: **given** one specific test window has no
   trades (illiquid period, or the strategy's config produces no signals for that window), **when**
   aggregation runs, **then** that window contributes zero trades to `AggregateOOS` without error (matching
   Spec 06 Acceptance Criterion #6's empty-trade-log handling), and its `TestMetrics` individually reflects
   the "zero trades" sentinel values from that same spec.

## Out of Scope

- Automated parameter re-optimization per window (walk-forward **optimization**, as distinct from
  walk-forward **validation**) — explicitly Phase 4 (`app/usecase/optimize`, blueprint §4).
- TUI visualization of walk-forward results — Phase 3.
- Statistical significance testing (e.g. confidence intervals on the aggregate Sharpe) — not requested by
  the PRD; raw aggregate figures are sufficient for Phase 2's scope.

## Dependencies

- Spec 04 (Backtest Engine) — `RunWalkForward` drives one `ReplayDriver` run per train/test window.
- Spec 06 (Metrics Provider ACL) — `Compute` is called once per window and once for the aggregate.
