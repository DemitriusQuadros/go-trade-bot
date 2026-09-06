# Spec backend-02 — Hyperparameter Optimization (`app/usecase/optimize`)

## Overview

This spec is the orchestration engine behind backend-01's `POST /optimize`: a grid search over a strategy's
config parameters, reusing Phase 2's backtest execution per combination rather than reimplementing it. The
central design problem — verified against actual Phase 2 code, not assumed — is that `BacktestUseCase.Run`
has no way to execute against a **hypothetical** config without mutating the real persisted `Strategy` row.
This spec adds a minimal, explicit extension point for that, and settles the homelab-scale question the
coordinator raised directly: grid size limits and sequential-only execution.

## Current Behavior (verified)

- `app/usecase/backtest/usecase.go:122-154` (`Run`) always resolves the strategy's config from the
  database: `strat, err := u.strategyRepo.GetByID(ctx, req.StrategyID)`, then passes `strat` (with its
  **persisted** `StrategyConfiguration.Configuration`) into `u.executeReplay(...)`. There is no
  config-override parameter on `RunRequest` (confirmed: its fields are `StrategyID, Symbol, Timeframe,
  StartDate, EndDate, InitialCapital, FillPolicy` only — no config blob).
- `app/usecase/backtest/usecase.go:342-371` (`executeReplay`) is **unexported** — it builds a fresh
  in-memory `SimulatedFillExchange`, `SignalUseCase` (backed by `memSignalRepo`/`memAccountUC`, both
  **in-memory, per-call, isolated** — confirmed at `usecase.go:410-477`, meaning **each backtest run already
  gets a fully isolated, throwaway account/signal state** with no shared mutable state between runs), and a
  `ReplayDriver`, then calls `driver.Run(ctx)`. This isolation property is exactly what makes running many
  grid-search combinations safe to do without any additional isolation work on this spec's part — Phase 2
  already built exactly the right foundation.
- `internal/feed/replay_feed.go` (Phase 2, per that phase's own spec) loads the **entire requested
  `[from, to)` candle range into memory** at construction. This is the concrete, already-existing constraint
  that determines this spec's parallelism answer below — Phase 2's own spec already flagged "streaming... if
  ... push memory usage past comfortable bounds" as a future concern; running N grid combinations
  concurrently multiplies that per-run memory footprint by N, directly threatening the blueprint's own
  <500MB idle / 4GB total homelab budget (PRD §9).

## Target Behavior

```go
// app/usecase/backtest/usecase.go — one new exported method (small, additive change to Phase 2 code)
package backtest

// RunEphemeral executes one backtest against an in-memory strategy config
// override, WITHOUT persisting a BacktestRun row or generating an HTML
// report - the metrics-only, no-artifact sibling of Run(), built for exactly
// this spec's grid-search use case (dozens-to-hundreds of throwaway
// evaluations where persisting each as a full BacktestRun would flood the
// backtest_runs table and the reports/ directory for no operational value).
func (u *BacktestUseCase) RunEphemeral(
    ctx context.Context,
    baseStrategy entities.Strategy, // caller's in-memory clone with Configuration already overridden
    symbol, timeframe string,
    from, to time.Time,
    initialCapital float64,
    fillPolicy engine.FillPolicy,
) (metrics_provider.BacktestMetrics, error)
```

```go
// app/usecase/optimize/usecase.go (NEW)
package optimize

type ParamRange struct {
    Min, Max, Step float64
}

type ParamGrid map[string]ParamRange // key = the strategy Configuration JSON field name, e.g. "rsi_period"

type GridPoint struct {
    Params  map[string]float64                `json:"params"`
    Metrics *metrics_provider.BacktestMetrics  `json:"metrics"` // nil if this point's evaluation errored
    Error   string                             `json:"error,omitempty"`
}

type OptimizeUseCase struct {
    backtestUseCase *backtest.BacktestUseCase
    strategyRepo    StrategyRepository
    optimizeRepo    OptimizationRepository
    maxCombinations int // hard cap - default 500, see rationale below
}

// Run executes the full grid search SEQUENTIALLY (see rationale below),
// updating optimizeRepo's persisted OptimizationRun.Progress after every
// combination so backend-01's GET /optimize/{id} reflects live progress.
// Called from the asynq task handler (backend-01's "optimize:execute" task),
// not directly from the HTTP handler.
func (u *OptimizeUseCase) Run(ctx context.Context, runID uint) error
```

**Combination generation**: the Cartesian product of each `ParamRange`, expanded via
`min, min+step, min+2*step, ..., <= max`. For the PRD's own example (`rsi_period: 10-20 step 1` = 11 values,
`stop_loss_pct: 1-3 step 0.5` = 5 values), this is `11 * 5 = 55` combinations — small enough to run
comfortably sequentially within a few minutes even at Phase 2's realistic single-symbol backtest runtime.

**Config override mechanism**: for each combination, `Run` builds an in-memory clone of the loaded
`entities.Strategy` (never mutating or saving the original), merges the combination's parameter values into
a copy of the parsed `Configuration` JSON map, re-marshals it onto the clone's
`StrategyConfiguration.Configuration`, and calls `backtestUseCase.RunEphemeral(ctx, clonedStrategy, ...)`.
An unknown `param_grid` key (one that doesn't correspond to any field the target strategy's `Before`/
`ShouldLong`/etc. actually reads from its config map) is not detectable ahead of time by this generic
mechanism — it simply becomes an unused key in the JSON blob, and that combination's backtest runs
identically to every other combination sharing the same real, read config values (silently redundant, not
an error) — see Acceptance Criteria for how this manifests and why it's an accepted, low-severity gap
rather than something this spec builds detection for.

### Grid size limit and parallelism — resolved (judgment call, per the coordinator's explicit ask)

**Grid size**: hard cap at **500 combinations** (`maxCombinations`, configurable via
`internal/configuration`). Rationale: at Phase 2's realistic single-symbol-year backtest runtime (a
`ReplayFeed` over ~525,600 1m candles, per PRD's own volume estimate, completing in low seconds to a couple
minutes per Phase 2 Spec 10 AC#6's own framing), 500 sequential runs is a multi-hour job — already at the
outer edge of what a "run it and check back later" asynchronous job is comfortable for on a homelab. This
cap rejects requests before they start (`backend-01`'s `413`), rather than silently accepting an
unreasonably large grid that ties up the worker for a full day.

**Parallelism**: **strictly sequential, not parallelized**, for this phase. Rationale, directly from the
Current Behavior audit: `ReplayFeed` loads its entire candle range into memory per instantiation. Running
even 4 combinations concurrently would multiply peak memory by 4x at the exact moment `docker-compose`'s
`worker` container is also running live strategy cycles, `LiveFeed` WebSocket buffers, and asynq's own
in-flight task state — directly risking the blueprint's stated `<500MB idle, 4GB total available` homelab
ceiling (PRD §9). Sequential execution keeps peak memory bounded to **one** `ReplayFeed`'s footprint
regardless of grid size, at the cost of wall-clock time — an explicit, deliberate trade favoring the stated
resource constraint over raw speed, consistent with the "no separate time-series DB" and "share existing
Postgres/Redis" frugality already established elsewhere in the blueprint. If this becomes a real bottleneck
in practice, the natural fix (flagged for a future phase, not built now) is smaller/streaming `ReplayFeed`
memory footprint first, THEN parallelism — not the reverse.

## Acceptance Criteria

1. **Given** a 55-combination grid (PRD's own example), **when** `Run` executes, **then** exactly 55
   `RunEphemeral` calls occur, one at a time (never two in flight concurrently — verified by asserting no
   overlapping start/end timestamps across combinations in a test harness), and `OptimizationRun.Progress`
   increments from `0` to `55` monotonically as they complete.
2. **Given** a `param_grid` requesting `600` combinations, **when** `POST /optimize` validates it
   (backend-01), **then** it's rejected at `413` before this usecase's `Run` is ever invoked.
3. **Given** the grid search completes, **when** the best combination is selected, **then** it is the one
   with the highest `Sharpe Ratio` among all successfully-evaluated (non-error) grid points — ties broken by
   lowest `MaxDrawdownPct` (a documented, deterministic tie-break rule, since "best" is otherwise ambiguous
   between two equal-Sharpe configs).
4. **Given** every grid point's evaluation succeeds, **when** the run completes, **then** `BestConfigJSON`
   contains exactly the winning combination's parameter values (e.g. `{"rsi_period": 14, "stop_loss_pct":
   2.0}`), not the full merged config blob (the caller/TUI is expected to already know the base strategy's
   other, non-grid-searched config values).
5. **Given** one combination out of 55 errors (Current Behavior's isolation guarantees this can't corrupt
   another combination's state), **when** the run completes, **then** 54 grid points have populated
   `Metrics` and 1 has a populated `Error` string with `Metrics: nil`, and the best-config selection
   (Acceptance Criterion #3) considers only the 54 valid points.
6. **Given** a `param_grid` key that doesn't match any field the target strategy actually reads (e.g.
   `"nonexistent_param": {...}` against Bollinger, which never reads that key), **when** the grid runs,
   **then** every combination varying only that key produces **identical** metrics to each other (since the
   unused key has no effect on strategy behavior) — this is the observable symptom of the "no ahead-of-time
   detection" gap named in Target Behavior; it manifests as a suspiciously flat heatmap along that
   parameter's axis, not a hard error, which the TUI's own heatmap rendering (frontend-specialist's spec)
   should be able to visually surface without this usecase needing to detect it programmatically.
7. **Edge case — `RunEphemeral` never writes a `BacktestRun` row**: **given** a 55-combination grid search
   completes, **when** `backtest_runs` table row count is checked before and after, **then** it is
   **unchanged** — confirming `RunEphemeral`'s "no artifact persistence" contract (Target Behavior) actually
   holds, which is the property that makes grid searches safe to run repeatedly without table bloat.
8. **Edge case — process restart mid-optimization**: **given** the worker process restarts while an
   optimization is `"running"` (e.g. a deploy), **when** it comes back up, **then** the `OptimizationRun`
   row remains in `"running"` state indefinitely (this spec does **not** build resume-from-checkpoint or
   stale-job-detection logic) — flagged explicitly as an accepted Phase 4 gap, not silently glossed over;
   an operator must manually notice a stuck `"running"` row and re-trigger if a restart interrupts a job.

## Out of Scope

- Bayesian optimization, random search, or any smarter-than-brute-force search strategy — PRD §4.3 and
  blueprint §6's scope-creep list are explicit that naive grid search is sufficient; nothing more
  sophisticated is requested.
- Ahead-of-time validation that `param_grid` keys correspond to real strategy config fields — accepted as a
  visually-detectable-but-not-programmatically-flagged gap (Acceptance Criterion #6).
- Resume-from-checkpoint after a worker restart mid-run — Acceptance Criterion #8's accepted gap.
- Parallel/concurrent grid evaluation — explicitly rejected for this phase per the homelab memory rationale
  above.

## Dependencies

- backend-01 (Optimization API Endpoints) — the async job wrapper (`asynq` task) that calls this usecase's
  `Run`.
- Phase 2's `app/usecase/backtest.BacktestUseCase` — gains the new `RunEphemeral` method (small, additive
  modification to already-shipped Phase 2 code).
- **Judgment call**: the 500-combination cap and strictly-sequential execution are this spec's two most
  consequential decisions, made explicitly to protect the blueprint's stated homelab resource ceiling over
  optimization speed — flagging both for confirmation, since a maintainer with more headroom than the
  stated 4GB WSL2 constraint might reasonably want a higher cap or opt-in parallelism.
