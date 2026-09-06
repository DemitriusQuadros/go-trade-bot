# Spec backend-05 — Strategy Performance History (Time-Bucketed P&L)

## Overview

Blueprint §4 Phase 4 flags real uncertainty about `entities.StrategyPerformance`'s shape: "verify existing
shape supports time-bucketed history; extend if it's currently a single running total." This spec's audit
finds the actual situation is more fundamental than "extend a running total": **`StrategyPerformance` isn't
persisted at all today** — it's a live-computed aggregate view with no time dimension whatsoever. This spec
designs the first-ever persisted, periodically-snapshotted history table, plus the exact API contract the
frontend-specialist agent's P&L-history sparkline needs — delivered as a finished contract now, not a
placeholder, per the coordinator's request.

## Current Behavior (verified)

- `app/entities/strategyperformance.go` (unchanged since Phase 1, re-verified this phase):
  ```go
  type StrategyPerformance struct {
      Name   string
      Symbol string
      Profit float64
      Trades int
  }
  ```
  No `gorm` tags, no `ID`, no timestamp field of any kind — this type has never been a persisted entity; it
  exists purely as a query-result DTO.
- `app/repository/strategy/repository.go:57-69` (`GetStrategyPerformanceBySymbol`) computes this via a raw
  SQL join across `orders`/`signals`/`strategies`, `GROUP BY st.name, s.symbol`, with **no date filter of
  any kind** — it's an all-time-to-date aggregate, recomputed fresh on every call. There is no code path
  anywhere that captures what this aggregate's value *was* at any point in the past — asking "what was
  strategy X's profit as of last Tuesday" is **not answerable** from today's schema, because nothing snapshots
  the value over time; re-running the same query later only ever gives you the *current* all-time total.
- Phase 3's `backend-01` spec already added JSON tags to `StrategyPerformance` and exposed it via
  `GET /strategy/performance` (all-time, unfiltered by date) — that endpoint is unaffected by this spec; it
  keeps answering "all-time totals," while this spec adds the orthogonal "history over time" capability.
- **Conclusion**: this is not a schema *extension* (there is nothing to extend — no time-related field
  exists to widen), it's the **introduction of periodic snapshotting**, since a live aggregate has no way to
  represent history without something capturing point-in-time values as time passes.

## Target Behavior

```go
// app/entities/strategyperformancesnapshot.go (NEW)
package entities

type PerformanceBucket string

const (
    BucketDaily   PerformanceBucket = "daily"
    BucketWeekly  PerformanceBucket = "weekly"
    BucketMonthly PerformanceBucket = "monthly"
)

// StrategyPerformanceSnapshot is a point-in-time capture of one strategy's
// profit/trade-count for one symbol, taken at each bucket boundary. Unlike
// StrategyPerformance (a live, unbounded all-time aggregate), each snapshot
// row represents ONLY the activity within its own [PeriodStart, PeriodEnd)
// window - a strategy's full history is the sequence of these rows, not a
// single mutable running total.
type StrategyPerformanceSnapshot struct {
    ID          uint              `gorm:"primaryKey"`
    StrategyID  uint              `gorm:"uniqueIndex:idx_perf_snapshot_period"`
    Symbol      string            `gorm:"uniqueIndex:idx_perf_snapshot_period"`
    Bucket      PerformanceBucket `gorm:"uniqueIndex:idx_perf_snapshot_period"`
    PeriodStart time.Time         `gorm:"uniqueIndex:idx_perf_snapshot_period"`
    PeriodEnd   time.Time
    Profit      float64 // sum of Order.Profit for orders CLOSED within [PeriodStart, PeriodEnd)
    Trades      int     // count of the same
    CreatedAt   time.Time
}
```

The composite unique index `(strategy_id, symbol, bucket, period_start)` makes snapshot generation
idempotent — re-running the snapshot job for a period that's already been captured updates the existing row
rather than duplicating it (the same `Upsert` idempotency pattern Phase 2 Spec 01 established for candles).

```go
// app/repository/strategy/repository.go — one new method, additive
// GetPerformanceInRange mirrors GetStrategyPerformanceBySymbol's existing
// join/aggregate query, with a WHERE o.updated_at (the close time - Order
// rows use UpdatedAt as their "closed at" timestamp today, per Phase 1's
// GenerateSellSignal setting Orders[0].UpdatedAt at close time) filter added:
func (r StrategyRepository) GetPerformanceInRange(ctx context.Context, from, to time.Time) ([]entities.StrategyPerformance, error)
```

```go
// app/usecase/performancehistory/usecase.go (NEW)
package performancehistory

type PerformanceHistoryUseCase struct {
    strategyRepo StrategyRepository
    snapshotRepo SnapshotRepository
}

// Snapshot computes and persists one snapshot per (strategy, symbol) pair
// active in [periodStart, periodEnd), for the given bucket. Idempotent (see
// composite index above) - safe to re-run for the same period.
func (u *PerformanceHistoryUseCase) Snapshot(ctx context.Context, bucket entities.PerformanceBucket, periodStart, periodEnd time.Time) error

// GetHistory returns the persisted snapshot series for one strategy+symbol,
// ordered by PeriodStart ascending - directly what the TUI's sparkline needs.
func (u *PerformanceHistoryUseCase) GetHistory(ctx context.Context, strategyID uint, symbol string, bucket entities.PerformanceBucket, limit int) ([]entities.StrategyPerformanceSnapshot, error)
```

**Snapshot scheduling**: `cmd/worker` gains a new asynq **periodic** task (using `asynq`'s existing
scheduler capability — no new infrastructure, same pattern as the strategy-cycle re-enqueue task) that
calls `Snapshot(ctx, BucketDaily, yesterday_00:00, today_00:00)` once daily at midnight UTC. Weekly/monthly
snapshots are **derived by aggregation over the daily rows** at query time (`GetHistory` for `BucketWeekly`
sums the 7 daily rows in each week, rather than requiring a **separate** weekly snapshot job) — this halves
the scheduling surface (one daily job, not three), and is correct because daily buckets are the finest
granularity anything downstream needs; weekly/monthly are always exact aggregations of complete daily
periods, never independently captured, so there's no risk of them drifting from the daily source of truth.

### API contract (for the frontend-specialist's P&L-history sparkline)

```
GET /strategy/{id}/performance/history?symbol={symbol}&bucket=daily|weekly|monthly&limit={n}
  Auth: none
  Returns: 200, [] { "period_start": string, "period_end": string, "profit": number, "trades": number }
           ordered by period_start ascending, at most `limit` entries (default 30, max 365)
  Errors: 400 (invalid bucket value, symbol missing, limit out of [1,365]), 404 (strategy not found)
  Notes: for bucket=weekly/monthly, period_start/period_end reflect the AGGREGATED window boundaries
    (e.g. a weekly entry spans Mon 00:00 to the following Mon 00:00), not individual daily rows -
    the aggregation described in Target Behavior happens server-side, the client never needs to sum
    daily rows itself.
```

## Acceptance Criteria

1. **Given** the daily snapshot job runs for `2026-09-01` (a day with 3 closed orders for strategy `X`/
   symbol `BTCUSDT` totaling `+$45` profit), **when** the job completes, **then** exactly one
   `StrategyPerformanceSnapshot` row exists with `StrategyID=X, Symbol="BTCUSDT", Bucket="daily",
   PeriodStart=2026-09-01T00:00:00Z, Profit=45, Trades=3`.
2. **Given** the same day's snapshot job is re-run (e.g. manually triggered twice, or a retry after an
   apparent failure that actually succeeded), **when** it completes the second time, **then** still exactly
   **one** row exists for that `(strategy, symbol, bucket, period_start)` — updated in place, not
   duplicated (verifying the composite unique index's idempotency guarantee).
3. **Given** 7 consecutive daily snapshots exist for one strategy/symbol with profits `[10, -5, 20, 0, 15,
   -10, 5]`, **when** `GetHistory(..., BucketWeekly, ...)` is called for the week spanning those 7 days,
   **then** it returns one entry with `profit = 35` (the sum) and `trades` = the sum of each day's trade
   count — confirming server-side aggregation, not a client-side responsibility.
4. **Given** `GET /strategy/5/performance/history?symbol=BTCUSDT&bucket=daily&limit=30`, **when** fewer than
   30 days of snapshot history actually exist (e.g. the feature just shipped 10 days ago), **then** the
   response returns however many exist (10), not an error and not zero-padded entries for the missing days
   — the TUI's sparkline component is responsible for rendering a shorter series gracefully, not this API.
5. **Given** a strategy that closed zero orders on a given day, **when** the daily snapshot job runs for
   that day, **then** **no row is created** for that `(strategy, symbol, day)` (not a zero-profit row) —
   `GetHistory`'s returned series therefore has gaps on inactive days, matching Acceptance Criterion #4's
   "shorter series, not zero-padded" contract consistently.
6. **Edge case — a strategy monitors multiple symbols**: **given** strategy `X` trades both `BTCUSDT` and
   `ETHUSDT`, **when** the daily snapshot runs, **then** it produces **two independent rows** for that day
   (one per symbol) — history is always scoped to a specific `(strategy, symbol)` pair, matching the
   existing all-time `GetStrategyPerformanceBySymbol`'s per-symbol grouping, never aggregated across a
   strategy's full symbol list into one blended figure.
7. **Edge case — snapshot job runs mid-day / backfill for a missed day**: **given** the daily job didn't run
   for some reason on `2026-09-03` (worker downtime) and is manually triggered later with the correct
   `periodStart`/`periodEnd` for that specific missed day, **when** it runs, **then** it correctly
   backfills that day's snapshot using the orders that were actually closed within that historical window
   (filtered by `Order.UpdatedAt`, not "orders closed since the job last ran") — the job's correctness does
   not depend on running exactly once per real calendar day at the right time; it's parameterized by the
   window it's told to compute, making backfill safe and straightforward.

## Confirmation for the frontend-specialist agent

**This contract is final and ready now** — the `GET /strategy/{id}/performance/history` endpoint shape
above will not change based on whether the daily snapshot job has actually accumulated real historical data
yet. The frontend-specialist's sparkline work can build against this exact response shape immediately; it
does not need placeholder assumptions. The only thing that varies over time is how *much* history the
endpoint has to return (Acceptance Criterion #4's "however many exist" behavior), which the sparkline
component should already be designed to handle gracefully regardless of this spec's completion status.

## Out of Scope

- Backfilling historical snapshots for periods before this feature ships (e.g. computing what daily P&L
  "would have been" for all of 2025 from existing `Order` rows) — technically possible (the underlying
  `Order.UpdatedAt` data exists), but not requested; the snapshot job only runs forward from deployment.
  Flagged as a reasonable one-time follow-up if historical backfill is wanted later — it would just mean
  running `Snapshot` once per historical day in a loop, using the same idempotent mechanism.
- Real-time/intraday P&L tracking (as opposed to daily-bucket granularity) — the PRD's own Phase 4 line
  item asks for "daily/weekly/monthly," not intraday; Phase 3's already-shipped `GET /strategy/performance`
  (all-time, live) remains the answer for "right now" P&L.
- Per-account (as opposed to per-strategy-per-symbol) performance history — not requested; scope matches the
  existing `GetStrategyPerformanceBySymbol`'s grouping exactly.

## Dependencies

- Phase 1's `app/entities/signal.go` (`Order.UpdatedAt` as the close-time field), `app/repository/strategy`
  (existing join query pattern, extended with a date filter).
- Phase 3's `backend-01` spec (`GET /strategy/performance`, all-time) — this spec's endpoint is additive and
  distinct, not a replacement.
- Phase 1's `asynq` scheduled-task infrastructure — reused for the daily snapshot job, no new
  scheduling mechanism introduced.
