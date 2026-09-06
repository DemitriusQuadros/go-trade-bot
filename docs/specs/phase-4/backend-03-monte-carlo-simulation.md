# Spec backend-03 — Monte Carlo Simulation (`app/engine/montecarlo.go`)

## Overview

PRD §4.3 wants Monte Carlo simulation: randomize a completed backtest's trade order to test robustness. This
is materially cheaper than the optimization work in backend-02 — it operates on an **already-computed**
trade log (no re-running the engine, no `ReplayFeed`/candle reload), so it can run synchronously within a
single request. This spec reuses Phase 2's `MetricsProvider` ACL to recompute metrics across N randomized
reorderings and reports a **distribution**, not a single value, per the coordinator's explicit ask.

## Current Behavior (verified)

- `app/entities/backtestrun.go:9-27` (`BacktestRun`) already persists `TradeLogJSON datatypes.JSON` — the
  full trade log from a completed run is already durably available; this spec's input data already exists,
  no new storage is needed to retrieve it.
- `internal/metrics_provider/interface.go:36-38` (`MetricsProvider.Compute(trades []TradeLogEntry,
  startingBalance float64, periodsPerYear float64) BacktestMetrics`) is a pure function over a trade list —
  confirmed it has no dependency on trade **order** being the original chronological sequence beyond what
  it needs to build `EquityCurve` (which walks the trades in the order given) — this is exactly the
  property Monte Carlo exploits: feed it the same trades in a shuffled order, get a different equity
  curve/drawdown/Sharpe, same total P&L.
- No `app/engine/montecarlo.go` or any randomization logic exists (fully greenfield).

## Target Behavior

```go
// app/engine/montecarlo.go (NEW)
package engine

import (
    "math/rand"

    "go-trade-bot/internal/metrics_provider"
)

type MonteCarloConfig struct {
    Iterations int   // e.g. 1000
    Seed       int64 // 0 => time-seeded (non-reproducible); explicit non-zero => reproducible for tests
}

type DistributionStats struct {
    Mean, Median, Min, Max, P5, P95 float64
}

type MonteCarloResult struct {
    Iterations              int                              `json:"iterations"`
    OriginalMetrics         metrics_provider.BacktestMetrics `json:"original_metrics"`
    SharpeDistribution      DistributionStats                `json:"sharpe_distribution"`
    MaxDrawdownDistribution DistributionStats                `json:"max_drawdown_distribution"`
    TotalReturnDistribution DistributionStats                `json:"total_return_distribution"`
}

// RunMonteCarlo shuffles `trades` cfg.Iterations times (Fisher-Yates, one
// shuffle per iteration - the SAME set of trades, reordered, never resampled
// with replacement, since this is a reordering-robustness test, not a
// bootstrap), recomputes BacktestMetrics per shuffle via provider.Compute,
// and returns the resulting distribution across all three headline figures.
func RunMonteCarlo(
    trades []metrics_provider.TradeLogEntry,
    startingBalance float64,
    periodsPerYear float64,
    provider metrics_provider.MetricsProvider,
    cfg MonteCarloConfig,
) MonteCarloResult
```

**Why reordering changes anything**: `Compute`'s `WinRatePct`/`ProfitFactor`/`TotalTrades` are all
order-invariant (they're simple sums/counts over the unordered trade set) — reordering only affects
`EquityCurve`-derived figures: `MaxDrawdownPct` (a different trade sequence produces different peak-to-
trough paths even with the same total gain) and `SharpeRatio` (computed from the period-over-period equity
returns series, which is sequence-dependent). `TotalReturnPct` is technically order-invariant too (same
trades, same total P&L, same starting/ending balance) — it's included in the distribution anyway for
completeness/sanity-checking (its distribution should collapse to a single repeated value across all
iterations, which is itself a useful implicit verification that the shuffle preserved the trade set
exactly — see Acceptance Criteria).

### API contract

```
POST /backtest/{id}/montecarlo
  Auth: none
  Body: { "iterations": number }  // optional, default 1000; capped at 10000 (see AC#6)
  Returns: 200, MonteCarloResult
  Errors: 404 (backtest run not found), 400 (iterations <= 0 or > cap),
          422 (the backtest run has fewer than 2 trades - Monte Carlo reordering is meaningless
               with 0 or 1 trades, see AC#7)
  Side effects: none - stateless computation over the existing TradeLogJSON; nothing new is persisted
    by default (see below for the one exception)

GET /backtest/{id}/montecarlo
  Returns: 200, MonteCarloResult (the most recently computed result for this run, cached) | 404 if
    never computed for this run
  Notes: POST also caches its result (a new BacktestRun.MonteCarloJSON datatypes.JSON column,
    additive to Phase 2's entities.BacktestRun) so a repeated GET doesn't require re-running 1000+
    metric computations - this is the "one exception" above: POST DOES persist, just onto the
    existing run row, not a new table.
```

This spec deliberately does **not** introduce a dedicated new Phase 4 TUI page for Monte Carlo results (the
coordinator's Phase 4 scope names only an optimization-results page and a P&L-history sparkline as the TUI
additions this phase needs) — `MonteCarloResult` is designed to be renderable as an additional panel within
Phase 3's already-specified Backtest Results page (`tui-*` Phase 3 work) if/when a future keybinding is
added there, but wiring that keybinding is not blocking any Phase 4 TUI spec and is not built here.

## Acceptance Criteria

1. **Given** a completed backtest with 20 trades summing to `+$500` total profit, **when** `POST
   /backtest/{id}/montecarlo` runs with `iterations: 1000`, **then** every one of the 1000 shuffled
   equity-curve computations ends at the same final equity value (`startingBalance + 500`) — verifying
   `TotalReturnDistribution`'s mean/min/max all collapse to the same value (Target Behavior's "implicit
   verification" point).
2. **Given** the same 20-trade backtest, **when** Monte Carlo runs, **then** `MaxDrawdownDistribution.Max`
   (the worst simulated drawdown across all reorderings) is **greater than or equal to**
   `OriginalMetrics.MaxDrawdownPct` — reordering can reveal a worse-than-observed drawdown path (e.g. all
   the losing trades clustered together by chance) but the original chronological sequence is just one
   specific ordering among many, so the simulated worst case should never be provably better than what's
   mathematically possible, only usually different from what was actually observed.
3. **Given** `cfg.Seed` is set to a fixed non-zero value, **when** `RunMonteCarlo` is called twice with
   identical inputs, **then** both calls produce byte-identical `MonteCarloResult`s — reproducibility for
   testing (this is why `Seed` exists as an explicit field rather than always time-seeding).
4. **Given** `cfg.Seed = 0`, **when** called twice, **then** results differ (or are at least not guaranteed
   identical) — confirming the "0 => time-seeded" convention actually produces fresh randomness for real
   operator-triggered runs (as opposed to test runs, which always pass an explicit seed).
5. **Given** `POST /backtest/{id}/montecarlo` completes, **when** `GET /backtest/{id}/montecarlo` is called
   immediately after, **then** it returns the same result without re-running the 1000 iterations (verified
   by asserting the cached `BacktestRun.MonteCarloJSON` column is populated and the GET path doesn't invoke
   `RunMonteCarlo` at all).
6. **Edge case — `iterations` exceeds the cap**: **given** `iterations: 50000`, **when** the request is
   validated, **then** it's rejected at `400` (cap: 10000) — Monte Carlo's per-iteration cost is trivial
   (no candle reload, just a shuffle + `Compute` call over a typically-small trade list), but an unbounded
   value is still rejected as basic input hygiene, not because it threatens the homelab budget the way
   backend-02's grid size does.
7. **Edge case — fewer than 2 trades**: **given** a backtest run with 0 or 1 trades, **when** `POST
   .../montecarlo` is called, **then** the response is `422` — reordering a single-element (or empty) list
   produces no meaningful distribution, and this should be distinguishable from a genuine `400` input error.
8. **Edge case — a backtest run that is itself a walk-forward aggregate**: **given** `BacktestRun.
   IsWalkForward == true` (Phase 2), **when** Monte Carlo is requested against it, **then** it operates on
   the aggregate out-of-sample trade log (`TradeLogJSON`'s embedded `trades` field, per Phase 2's
   `WalkForwardTradeLogPayload` shape) — this spec's endpoint must parse that payload's specific
   walk-forward shape (`{trades, windows}`) rather than assuming every `TradeLogJSON` is a flat
   `[]TradeLogEntry`, since Phase 2's actual implementation stores walk-forward results in that richer
   wrapped shape (verified in `app/usecase/backtest/usecase.go:297-301`).

## Out of Scope

- A dedicated Phase 4 TUI page for Monte Carlo — not requested by the coordinator's stated Phase 4 TUI
  scope; renderable as a future addition to the existing Backtest Results page.
- Bootstrap resampling (sampling trades **with** replacement, potentially including duplicates or omitting
  some trades) — PRD explicitly frames this as "randomize trade order," i.e. permutation, not resampling;
  bootstrap is a different (also valid) technique not requested here.
- Monte Carlo on live/Dry-Run/Paper trade history (as opposed to a completed backtest run) — not requested;
  Phase 4's ask is specifically about validating backtest robustness before further staging-pipeline
  promotion.

## Dependencies

- Phase 2's `internal/metrics_provider.MetricsProvider`, `entities.BacktestRun.TradeLogJSON` — reused
  directly; `BacktestRun` gains one additive `MonteCarloJSON` column.
- No dependency on backend-01/02 (optimization) — Monte Carlo operates on already-completed backtest runs,
  independent of the grid-search machinery.
