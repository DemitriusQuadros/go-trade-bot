# Spec backend-01 — Optimization API Endpoints

## Overview

PRD §4.3 wants hyperparameter optimization: grid search over strategy config param ranges (e.g. RSI period
10–20, stop-loss 1–3%), reporting the best-performing config. This spec defines the REST contract the
frontend-specialist agent's optimization-results page consumes — written first, per the coordinator's
instruction, since that TUI work depends on it. Unlike Phase 2/3's synchronous `/backtest*` endpoints
(explicitly accepted as blocking for a single run), a grid search is many backtest runs chained together and
must be **asynchronous** — this spec introduces this codebase's first async job pattern, built on the
already-present `asynq` task-queue infrastructure Phase 1 uses for strategy cycles, exactly as Phase 2's own
spec flagged as "the natural mechanism to introduce async job handling later without new infrastructure."

## Current Behavior (verified)

- `app/usecase/backtest/usecase.go:122-211` (`Run`) and `:213-332` (`RunWalkForward`) are both fully
  synchronous — the HTTP handler (`app/handler/web/backtest/handler.go:59-82`) blocks until the entire
  backtest/walk-forward completes before writing a response. No job/task/polling pattern exists anywhere in
  `app/handler/web/` today.
- `app/usecase/backtest/usecase.go:50-58` (`RunRequest`) has no field for overriding a strategy's
  `StrategyConfiguration.Configuration` — `Run` always loads the **persisted** config via
  `u.strategyRepo.GetByID(ctx, req.StrategyID)` (`usecase.go:145`). There is no mechanism today to run a
  backtest against a hypothetical/modified config without first `PATCH`/`PUT`-ing the real strategy row —
  which would be destructive and unworkable for a grid search evaluating dozens of candidate configs.
- `cmd/worker/main.go` (Phase 1, current) already constructs an `*asynq.Server`/`*asynq.Client` pair and
  registers task handlers via `asynq.ServeMux` — this spec's async job reuses that exact same
  infrastructure (a new task type, not a new queue/broker/worker process).
- No `entities.OptimizationRun` or `app/repository/optimize` exists (fully greenfield for this phase).

## Target Behavior

```go
// app/entities/optimizationrun.go (NEW)
package entities

type OptimizationStatus string

const (
    OptimizationPending   OptimizationStatus = "pending"
    OptimizationRunning   OptimizationStatus = "running"
    OptimizationCompleted OptimizationStatus = "completed"
    OptimizationFailed    OptimizationStatus = "failed"
)

type OptimizationRun struct {
    ID              uint   `gorm:"primaryKey"`
    StrategyID      uint
    Symbol          string
    Timeframe       string
    StartDate       time.Time
    EndDate         time.Time
    ParamGridJSON   datatypes.JSON // the requested parameter ranges, see below
    Status          OptimizationStatus
    Progress        int    // combinations completed
    TotalCombinations int
    BestConfigJSON  datatypes.JSON // the winning combination's config
    BestMetrics     metrics_provider.BacktestMetrics `gorm:"embedded;embeddedPrefix:best_"`
    ResultsGridJSON datatypes.JSON // full per-combination results, for the heatmap (backend-02 defines shape)
    ErrorMessage    string
    CreatedAt       time.Time
    CompletedAt     *time.Time
}
```

```
POST /optimize
  Auth: none
  Body: {
    "strategy_id": number,
    "symbol": string,
    "timeframe": string,
    "start_date": string,       // RFC3339
    "end_date": string,
    "initial_capital": number,  // optional, defaults per Phase 2's RunRequest convention (1000.0)
    "param_grid": {
      "rsi_period":     { "min": 10, "max": 20, "step": 1 },
      "stop_loss_pct":  { "min": 1,  "max": 3,  "step": 0.5 }
      // arbitrary keys, matching whatever config fields the target strategy's
      // Configuration JSON exposes - see backend-02 for how keys map onto a
      // strategy's actual config shape
    }
  }
  Returns: 202 Accepted, { "id": number, "status": "pending", "total_combinations": number }
  Errors: 400 (missing required fields, malformed param_grid, non-positive step),
          413 (total_combinations exceeds the configured hard cap - see backend-02's grid-size limit)
  Side effects: enqueues an asynq task (new task type "optimize:execute"); returns immediately,
    does NOT block for the grid search to run (the key behavioral difference from Phase 2/3's
    synchronous /backtest*).

GET /optimize/{id}
  Returns: 200, {
    "id": number, "status": "pending"|"running"|"completed"|"failed",
    "progress": number, "total_combinations": number,
    "best_config": object | null,       // null until status == "completed"
    "best_metrics": BacktestMetrics | null,
    "error_message": string | null      // populated only if status == "failed"
  }
  Errors: 404
  Notes: this is the poll target for the TUI's progress bar (PRD SS4.5 Page 4: "Progress bar with ETA
    appears"). progress/total_combinations give the TUI enough to render both a percentage and an ETA
    (elapsed_time / progress * (total - progress), computed client-side - this endpoint does not compute
    an ETA server-side, keeping the contract simple).

GET /optimize/{id}/results
  Returns: 200, {
    "id": number, "best_config": object, "best_metrics": BacktestMetrics,
    "grid": [ { "params": object, "metrics": BacktestMetrics } ]   // full per-combination results for the heatmap
  }
  Errors: 404, 409 (status != "completed" - results aren't ready yet; caller should keep polling
    GET /optimize/{id} instead)

GET /optimize?strategy_id={id}
  Returns: 200, [] { id, status, created_at, completed_at, best_config, best_metrics }  (list, no grid detail -
    history list, mirrors GET /backtest?strategy_id= 's shape/purpose from Phase 2/3)
  Errors: 400 if strategy_id missing (same rule as GET /backtest?strategy_id=, Phase 3 backend-01)
```

## Acceptance Criteria

1. **Given** `POST /optimize` with a 2-parameter grid (`rsi_period: 10-20 step 1` = 11 values,
   `stop_loss_pct: 1-3 step 0.5` = 5 values), **when** the request is accepted, **then** the response's
   `total_combinations` is `55` (Cartesian product, `11 * 5`) and `status` is `"pending"` — the request
   returns in well under a second, confirming it does not block for the search itself.
2. **Given** a `param_grid` entry with `step: 0`, **when** validated, **then** the request is rejected with
   `400` before any task is enqueued (a zero step would produce an infinite/undefined combination count).
3. **Given** a `param_grid` whose Cartesian product exceeds the configured hard cap (backend-02 sets this;
   this endpoint enforces it), **when** `POST /optimize` is called, **then** the response is `413`, not a
   silently-truncated grid and not an accepted-then-eventually-failed job — reject before enqueueing.
4. **Given** an in-progress optimization at 30/55 combinations, **when** `GET /optimize/{id}` is polled,
   **then** `status: "running"`, `progress: 30`, `total_combinations: 55`, `best_config`/`best_metrics`
   both `null` (not yet finalized until completion — see backend-02 for whether an interim "best so far" is
   exposed, which this endpoint's contract deliberately does **not** promise, keeping the poll response
   simple and avoiding a race between "best so far" and the final result confusing the TUI).
5. **Given** a completed optimization, **when** `GET /optimize/{id}/results` is called, **then** the `grid`
   array has exactly `total_combinations` entries, each with a distinct `params` object and its own
   computed `metrics`.
6. **Given** a request for `GET /optimize/{id}/results` while `status == "running"`, **when** called,
   **then** the response is `409`, distinguishing "not ready yet" from `404` ("doesn't exist at all") —
   the TUI should render these two cases differently (a progress indicator vs. an error).
7. **Edge case — a single grid-point backtest fails mid-run** (e.g. a transient error executing one
   parameter combination): **given** this occurs, **when** the job continues, **then** that one combination
   is recorded in the grid with a `null`/error-marked `metrics` entry and the search **continues** with the
   remaining combinations — one bad combination must not abort the entire optimization run (mirrors Phase 2
   Spec 02's candle-import "one bad symbol doesn't abort the batch" precedent).
8. **Edge case — every combination fails**: **given** all combinations error out (e.g. a misconfigured
   `param_grid` key that doesn't match any real config field the strategy reads), **when** the job
   finishes, **then** `status: "failed"`, `error_message` explains the failure pattern (not a silent
   `"completed"` with an empty/nonsensical `best_config`).

## Out of Scope

- Cancelling an in-progress optimization run (`DELETE /optimize/{id}` or similar) — not requested by the
  PRD; a maintainer-triggered research job running to completion or failure is acceptable for Phase 4's
  scope.
- Real-time push updates (WebSocket/SSE) for progress — polling `GET /optimize/{id}` is sufficient, matching
  this codebase's existing all-HTTP-polling posture (no WebSocket server exists on `cmd/api` anywhere).
- Authentication — matches existing posture, no auth layer anywhere in this API.

## Dependencies

- Phase 2's `app/usecase/backtest`, `internal/metrics_provider.BacktestMetrics` — reused for per-combination
  execution and result shape.
- Phase 1's `asynq` infrastructure (`cmd/worker`) — the async job mechanism this spec introduces reuses it
  directly, no new infrastructure.
- backend-02 (Hyperparameter Optimization) — owns the actual grid-search orchestration and grid-size-limit
  policy this spec's `413`/`400` responses enforce.
