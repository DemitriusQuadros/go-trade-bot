# Spec 10 — Backtest Persistence & API (`app/entities/backtestrun.go`, `app/usecase/backtest`, `cmd/backtest`)

## Overview

This spec ties Specs 04-08 together end-to-end: persist a completed backtest/walk-forward run, orchestrate
the full engine → metrics → report → persistence pipeline, and expose it via `cmd/backtest`'s CLI plus — per
this spec's resolution of the "optional, TUI-first" question the blueprint originally left open — a
non-optional `cmd/api` REST endpoint, since ADR-007 (now settled) commits Phase 3's TUI to being a pure HTTP
client of `cmd/api` for every capability, including backtesting.

## Current Behavior (verified)

- No `app/entities/backtestrun.go`, `app/repository/backtest/`, `app/usecase/backtest/`, or `cmd/backtest/`
  exists (confirmed by directory listings across `app/entities`, `app/repository`, `cmd`).
- `app/handler/web/` (Phase 1, current) contains `account/`, `broker/`, `signal/`, `strategy/` — no
  `backtest/` handler package. ADR-007 (blueprint §5) already commits to `cmd/console` being rebuilt as a
  pure `apiclient` consumer in Phase 3, which is the concrete reason this spec treats the REST endpoint as
  required now rather than deferred — see Judgment Call below.

## Target Behavior

```go
// app/entities/backtestrun.go
package entities

type BacktestRun struct {
    ID             uint   `gorm:"primaryKey"`
    StrategyID     uint
    Strategy       Strategy `gorm:"foreignKey:StrategyID"`
    Symbol         string
    StartDate      time.Time
    EndDate        time.Time
    IsWalkForward  bool
    Sharpe         float64
    MaxDrawdownPct float64
    WinRatePct     float64
    ProfitFactor   float64 // NULL-able / stored as a large sentinel for +Inf - see AC#5
    TotalTrades    int
    TotalReturnPct float64
    Passed         bool   // per the configured threshold policy, below
    HTMLReportPath string // empty if report generation failed (Spec 07 AC#7) or was pruned (retention policy)
    TradeLogJSON   datatypes.JSON // the full []metrics_provider.TradeLogEntry, for history/re-analysis
    CreatedAt      time.Time
}
```

```go
// app/usecase/backtest/usecase.go
package usecase

type ThresholdPolicy struct {
    MinSharpe      float64 // default 0.8, per PRD SS10's suggested starting point
    MaxDrawdownPct float64 // default 20.0, per PRD SS10
}

type BacktestUseCase struct {
    // engine.ReplayDriver factory, metrics_provider.MetricsProvider, report
    // generator, candle repository, backtest repository, ThresholdPolicy,
    // retention-policy N (default 20, Spec 07)
}

// Run orchestrates one full backtest: builds a ReplayDriver (Spec 04),
// executes it, computes metrics (Spec 06), generates + persists the HTML
// report (Spec 07), evaluates Passed against ThresholdPolicy, persists the
// BacktestRun row, and prunes old reports past the retention limit.
func (u BacktestUseCase) Run(ctx context.Context, req RunRequest) (entities.BacktestRun, error)

// RunWalkForward orchestrates Spec 08's walk-forward flow and persists an
// aggregate BacktestRun row (IsWalkForward=true) plus the per-window detail
// (see AC#7 for how per-window results are represented).
func (u BacktestUseCase) RunWalkForward(ctx context.Context, req WalkForwardRequest) (entities.BacktestRun, error)

func (u BacktestUseCase) GetByID(ctx context.Context, id uint) (entities.BacktestRun, error)
func (u BacktestUseCase) ListByStrategy(ctx context.Context, strategyID uint) ([]entities.BacktestRun, error)
```

```go
// app/handler/web/backtest/handler.go (NEW — non-optional, see Judgment Call)
// POST   /backtest              — trigger a run (body: RunRequest); synchronous for Phase 2's realistic
//                                   single-symbol runtime (seconds to low minutes for a year of 1m candles)
//                                   - see AC#6 for why async/polling is deferred, not built now
// GET    /backtest/{id}         — fetch one run's full result (metrics + trade log + report path)
// GET    /backtest?strategy_id= — list runs for a strategy (history list, Phase 3 Page 4's "recent
//                                   backtests" panel)
// POST   /backtest/walkforward  — trigger a walk-forward run (body: WalkForwardRequest)
```

```go
// cmd/backtest/main.go — headless CLI, calls the SAME BacktestUseCase.Run/
// RunWalkForward directly (no HTTP round-trip) - both the CLI and the API
// handler are thin wrappers around one usecase, per this project's existing
// handler -> usecase -> repository layering (CLAUDE.md).
```

### Pass/fail threshold — resolved default (judgment call)

`ThresholdPolicy{MinSharpe: 0.8, MaxDrawdownPct: 20.0}` — the PRD's own §10 "suggested starting point" —
is adopted as the **compiled-in default**, overridable via `internal/configuration` (not hardcoded
unconditionally): `Passed = Metrics.SharpeRatio >= MinSharpe && Metrics.MaxDrawdownPct <= MaxDrawdownPct`.
This resolves the blueprint's open question with a concrete, changeable value rather than leaving
`BacktestRun.Passed` uncomputable.

## Acceptance Criteria

1. **Given** a `POST /backtest` request for a registered strategy/symbol/date-range, **when** the run
   completes, **then** the response includes the full `BacktestMetrics`, the HTML report path, and
   `Passed` computed against the configured `ThresholdPolicy`.
2. **Given** a completed run with `Sharpe=1.2, MaxDrawdownPct=15.0` against the default policy, **when**
   `Passed` is computed, **then** it is `true` (both thresholds met).
3. **Given** a completed run with `Sharpe=1.2, MaxDrawdownPct=25.0` (drawdown threshold violated), **when**
   `Passed` is computed, **then** it is `false` even though Sharpe alone would pass — both conditions are
   required (AND, not OR).
4. **Given** a run whose HTML report generation failed (Spec 07 AC#7), **when** the `BacktestRun` row is
   persisted, **then** it still contains valid `Sharpe`/`MaxDrawdownPct`/etc. with `HTMLReportPath = ""` —
   the run is not discarded or marked as failed overall just because the report step failed.
5. **Edge case — `ProfitFactor = +Inf` serialization**: **given** a run with zero losing trades (Spec 06
   AC#2), **when** the `BacktestRun` is persisted to Postgres and serialized in the `GET /backtest/{id}`
   JSON response, **then** `+Inf` is represented as a large finite sentinel in the DB column (e.g.
   `math.MaxFloat64`, since Postgres `float8`/GORM can't natively round-trip `+Inf` through JSON without
   explicit handling) **and** as the literal string `"Infinity"` in the JSON API response body (valid per
   most JSON consumers' conventions, though not strict RFC 8259 JSON) — this must be handled explicitly by
   a custom marshal step, not left to a default float encoder that would either error or silently produce
   invalid JSON.
6. **Edge case — long-running walk-forward request**: **given** a walk-forward request spanning 12 months
   with monthly stepping (≈9 windows, Spec 08 AC#1), **when** `POST /backtest/walkforward` is called,
   **then** the request may take multiple minutes to complete — this spec accepts a **synchronous** HTTP
   response for Phase 2 (the request blocks until the full walk-forward finishes) rather than building an
   async job-polling pattern, since Phase 2's realistic data volumes (single symbol, single strategy, one
   maintainer's research sessions) don't yet justify that complexity; **flagged explicitly** as a scope
   call that should be revisited if Phase 3's TUI experience (a single-threaded terminal blocking for
   several minutes on a walk-forward launch) proves unacceptable — the existing `asynq` task-queue
   infrastructure (already used for strategy cycles) is the natural mechanism to introduce async job
   handling later without new infrastructure.
7. **Edge case — walk-forward per-window detail retrieval**: **given** a persisted walk-forward
   `BacktestRun` (aggregate row), **when** a caller needs individual window results (not just the
   aggregate), **then** `TradeLogJSON`'s structure for a walk-forward run additionally embeds the
   per-window `TrainMetrics`/`TestMetrics` breakdown (Spec 08's `WalkForwardResult.Windows`) as a
   sibling field within the same JSON blob, rather than requiring a separate table/endpoint — accepted as
   sufficient for Phase 2, revisit if Phase 3's walk-forward visualization needs indexed per-window
   queries rather than "fetch the one row and parse the embedded JSON."
8. **Given** the retention policy (default 20 reports per strategy, Spec 07), **when** a 21st backtest run
   completes for the same strategy, **then** the oldest report **file** is deleted (its `BacktestRun` row's
   `HTMLReportPath` is cleared to `""`, but the row and its metrics remain queryable) — never the DB row
   itself.

## Out of Scope

- Async job polling for long-running walk-forward requests — explicitly deferred per Acceptance Criterion
  #6, flagged as a Phase 3 revisit candidate.
- Authentication/authorization on the new `POST /backtest*` endpoints — matches this project's existing API
  posture (no auth layer exists on any current endpoint per Phase 1 review; not introduced here either).
- A dedicated walk-forward per-window query endpoint — Acceptance Criterion #7's embedded-JSON approach is
  accepted for Phase 2.

## Dependencies

- Spec 04 (Backtest Engine), Spec 06 (Metrics Provider), Spec 07 (HTML Report), Spec 08 (Walk-Forward) —
  this spec is the orchestration layer sitting on top of all four.
- **Judgment call — REST endpoint is non-optional**: the original blueprint text called
  `app/handler/web/backtest/handler.go` "optional, TUI-first." Given ADR-007 is now settled (Phase 3's TUI
  is a full `cmd/api` HTTP client for reads **and** writes, with no direct DB/repository access permitted at
  all), a TUI-only, non-HTTP path to trigger/poll backtests would contradict ADR-007's own stated rationale.
  This spec therefore makes the REST endpoint a **required** Phase 2 deliverable, not an optional stretch
  goal — flagging this explicitly since it adds handler-layer work (and its own test coverage) beyond what
  the original blueprint scoped for Phase 2, and should be confirmed rather than assumed.
