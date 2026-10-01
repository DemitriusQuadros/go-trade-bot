# Cross-cutting performance reports (backend) — design spec

**Status:** approved for implementation planning
**Scope:** backend only (usecase, repository, REST, agent/MCP tool). Frontend dashboard UI is a
separate follow-up spec, to be designed once this API is stable.

## Problem

The bot already persists everything needed to answer "how is my strategy doing in dry-run vs
production?", "what am I paying in fees?", and "which strategies are actually working?" — but only
as per-strategy, per-symbol rows (`Signal`/`Order`, and the daily `StrategyPerformanceSnapshot`
cron). There is no cross-strategy, cross-mode aggregation anywhere in the codebase today: every
existing query (`GetPerformanceInRange`, `ListDaily`, `get_performance_snapshots`) is scoped to a
single strategy+symbol pair. Answering those questions currently means manually querying the DB.

## Goals

- One backend capability, usable from three surfaces (web REST, the built-in agent, and MCP
  clients), answering four perspectives over an arbitrary `[from, to]` date range:
  1. **Performance by execution mode** — dry-run vs paper vs live/production, per strategy or
     aggregated.
  2. **Fees paid** — totals broken down by strategy/symbol/mode.
  3. **Strategy leaderboard** — rank strategies by PnL/Sharpe/win-rate over the period.
  4. **Agent platform cost rollup** — tokens/USD spent by the agents platform itself, separate
     from trading performance.
- Agent/MCP exposure must cost at most one new tool (the project already treats "don't bloat the
  tool count" as a hard constraint for the agent registry).
- No new scheduled/pre-aggregated table. Computed on demand from existing rows, per the user's
  explicit choice of flexibility over pre-aggregation speed.

## Non-goals

- Frontend dashboard pages (separate spec).
- Write access of any kind — every surface here is read-only; none of it touches
  `strategy_scope.go` or any strategy-writing tool gate.
- Replacing or changing the existing daily `StrategyPerformanceSnapshot` cron — it continues to
  exist unchanged; this feature queries raw `Signal`/`Order`/`AgentUsage` rows directly instead.
- Balance-accurate portfolio equity tracking. Sharpe/drawdown/return figures use the same
  `startingBalance <= 0 → 1000` fallback `metrics_provider.Compute` already uses for backtests —
  an approximation, not a real account-balance reconstruction.

## Architecture

```
REST handlers (app/handler/web/report/)         Agent tool (get_performance_report)
        \                                              /
         \                                            /
          v                                          v
              app/usecase/report.UseCase
        (PerformanceByMode / Leaderboard / FeeSummary / AgentCostSummary)
                            |
          ------------------------------------------
          |                 |                 |
   signal repo (new      agentplatform repo   metrics_provider
   query method)         (new query method)   (existing, reused
          |                                    unmodified)
     Postgres: Signal + Order                  Postgres: AgentUsage
```

All four methods share one `Filter` struct and are independently callable/testable; there is no
combined "mega summary" DTO.

```go
// app/usecase/report/usecase.go
type Filter struct {
    From       time.Time
    To         time.Time
    StrategyID uint   // 0 = all strategies
    Mode       string // "", "live", "paper", "dryrun"
    Symbol     string // "" = all symbols
}

type UseCase interface {
    PerformanceByMode(ctx context.Context, f Filter) ([]ModePerformance, error)
    Leaderboard(ctx context.Context, f Filter, limit int) ([]StrategyRanking, error)
    FeeSummary(ctx context.Context, f Filter) ([]FeeTotal, error)
    AgentCostSummary(ctx context.Context, f Filter, agentID uint) ([]AgentCost, error)
}
```

## Data flow per perspective

### PerformanceByMode / Leaderboard (need Sharpe/drawdown/win-rate/profit-factor)

1. New repo method fetches closed `Order` rows joined to `Signal` within `Filter`, grouped by
   `Signal.Mode` (mode perspective) or `Signal.StrategyID` (leaderboard perspective).
2. Each group's rows map to `metrics_provider.TradeLogEntry`:
   - `EntryTime` ← `Order.CreatedAt`, `ExitTime` ← `Order.UpdatedAt` (approximation: `UpdatedAt` on
     a closed order reflects the close write; acceptable precision for a reporting Sharpe calc,
     same precision class as other trade-duration uses of these fields in this codebase).
   - `EntryPrice`/`ExitPrice`/`Quantity`/`Profit`/`ExitReason` map directly.
3. Feed the slice into the **existing, unmodified** `MetricsProvider.Compute(trades, 0, periodsPerYear)`
   — the same function backtests use, just fed real trade history instead of simulated fills. This
   reuses all of Sharpe/drawdown/win-rate/profit-factor logic with zero new math.
4. Leaderboard sorts the resulting per-strategy `BacktestMetrics` by a configurable key (default:
   total profit) and truncates to `limit` (default 10, same default-then-clamp pattern as
   `get_performance_snapshots`' limit).

### FeeSummary

Pure SQL aggregation: `SUM(entry_fee + exit_fee)` over closed orders in range, grouped by
strategy/symbol/mode as requested by the filter. No `metrics_provider` involvement.

### AgentCostSummary

Pure SQL aggregation over the existing `AgentUsage` table (`CostUSD`, `InputTokens`,
`OutputTokens`, `Runs`), summed per agent (or across all agents) over the date range. No new
entity.

## REST surface

New package `app/handler/web/report/`, mounted under the existing `/api` subrouter
(`NewServeMux`), `Capability = "view"` (read-only tier, same as viewing strategies/signals):

- `GET /reports/performance-by-mode?from=&to=&strategy_id=&symbol=`
- `GET /reports/leaderboard?from=&to=&mode=&limit=`
- `GET /reports/fees?from=&to=&strategy_id=&mode=&symbol=`
- `GET /reports/agent-costs?from=&to=&agent_id=`

Response DTOs in `response.go` alongside the handler, snake_case, following every other handler's
convention (no raw entity encoding).

## Agent / MCP tool

One new tool, `app/usecase/agent/tools_report.go`:

- Name: `get_performance_report`, permission `PermRead` (same tier as `get_candle_coverage`).
- Args: `{view: "mode"|"fees"|"leaderboard"|"agent_costs", from, to, strategy_id?, mode?, symbol?, limit?}`.
- `Execute` unmarshals args, dispatches to the matching `app/usecase/report` method, and renders a
  plain-text summary (one line per group), matching the existing `get_candle_coverage` /
  `get_performance_snapshots` plain-text convention — not structured JSON, so the model can reason
  over it like every other read-only tool's output.
- Because MCP exposure (`cmd/mcp/server/tools.go`) iterates the same agent tool registry
  (`AgentUseCase.Tools()`) 1:1, this tool is automatically available over MCP (stdio and HTTP) with
  zero additional wiring.
- Net agent tool count: **+1**.

## Error handling

- Empty result set (no closed orders / no usage rows in range) → `200` with zero-valued DTOs for
  REST, and a "no data in range for <view>" string for the agent tool — never an error for "nothing
  happened in this window" (matches `get_performance_snapshots`' "no performance snapshots recorded
  yet" convention).
- Invalid filter (bad date range, unknown `mode`/`view` enum, `to < from`) → `400 customerror` at
  the usecase boundary, surfaced as HTTP 400 by the REST handler and as a tool error by the agent
  tool's `Execute`.

## Testing

- `app/usecase/report`: pure unit tests against mocked repo interfaces (project convention — no DB
  needed at this layer), covering each of the four methods plus empty-range and invalid-filter
  cases.
- New repo query methods (on `signal` and `agentplatform` repos): SQLite in-memory tests exercising
  the real GROUP BY/join SQL, per the project's existing repository-test convention.
- REST handlers: standard handler tests (mocked usecase), per existing handler-test conventions in
  `app/handler/web/`.
- No E2E/Gherkin scenario needed — this is a new read-only surface, not a change to the trading
  execution path.

## Open questions / follow-ups (explicitly deferred, not blocking this spec)

- Frontend dashboard UI consuming these endpoints — separate spec.
- Whether `Leaderboard`'s default ranking key (total profit) should eventually be
  user-configurable (e.g. rank by Sharpe instead) — start with profit, revisit if requested.
