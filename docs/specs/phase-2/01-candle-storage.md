# Spec 01 — Candle Storage (`app/entities/candle.go`, `app/repository/candle`)

## Overview

Phase 2's entire research subsystem (`ReplayFeed`, the backtest engine, candle import) depends on a
persisted OHLCV table. Nothing in `app/entities/` or `app/repository/` stores candles today — every Phase 1
read is a live REST/WebSocket call through `exchange.ExchangeClient`. This spec defines the `Candle` GORM
entity and its repository, including the no-lookahead range-query method that the backtest engine (Spec 04)
depends on to serve supplementary cross-timeframe reads (Grid's 24h volume, Scalping's 15m trend) without
ever exposing future candles to a strategy under evaluation.

## Current Behavior (verified)

- `app/entities/` contains `account.go`, `signal.go`, `strategy.go`, `strategyperformance.go` — no
  `candle.go`. Confirmed by directory listing.
- `app/repository/` contains `account/`, `signal/`, `strategy/` — no `candle/`. Confirmed by directory listing.
- `internal/db/db.go:13-38` (`NewDatabase`) opens the GORM connection but does not call `AutoMigrate` at
  all in this file — migration is presumably invoked by the caller (`cmd/api/main.go`, per CLAUDE.md's "Runs
  DB migrations on startup via GORM AutoMigrate"); this spec's migration addition is a new `AutoMigrate`
  argument, not a new call site.
- `exchange.Candle` (`internal/exchange/types.go:60-69`) already exists as the ACL's OHLCV shape:
  `Symbol, Timeframe string; OpenTime time.Time; Open, High, Low, Close, Volume float64`. This is the type
  every Phase 1 consumer (`app/strategies/*`, `app/engine/engine.go`) already works with — the persisted
  `entities.Candle` in this spec is a storage-shape mirror of it, not a competing type; repository methods
  return `[]exchange.Candle` (not a distinct `entities.Candle` DTO leaking into callers), converting
  internally.

## Target Behavior

```go
// app/entities/candle.go
package entities

import "time"

// Candle is the persisted OHLCV row. (Symbol, Timeframe, OpenTime) is unique -
// one row per candle per timeframe per symbol, ever.
type Candle struct {
    ID        uint      `gorm:"primaryKey"`
    Symbol    string    `gorm:"type:varchar(20);not null;uniqueIndex:idx_candle_symbol_tf_time"`
    Timeframe string    `gorm:"type:varchar(5);not null;uniqueIndex:idx_candle_symbol_tf_time"`
    OpenTime  time.Time `gorm:"not null;uniqueIndex:idx_candle_symbol_tf_time"`
    Open      float64   `gorm:"not null"`
    High      float64   `gorm:"not null"`
    Low       float64   `gorm:"not null"`
    Close     float64   `gorm:"not null"`
    Volume    float64   `gorm:"not null"`
    CreatedAt time.Time
}
```

```go
// app/repository/candle/repository.go
package repository

import (
    "context"
    "time"

    "go-trade-bot/app/entities"
    "go-trade-bot/internal/exchange"
)

type CandleRepository struct{ db *gorm.DB }

func NewCandleRepository(db *gorm.DB) CandleRepository

// Upsert writes candles idempotently: a row matching (Symbol, Timeframe,
// OpenTime) is updated in place (OHLCV overwritten), never duplicated. This
// is what makes Spec 02's import CLI safely re-runnable/resumable.
func (r CandleRepository) Upsert(ctx context.Context, candles []entities.Candle) error

// RangeBefore returns up to `limit` candles for (symbol, timeframe) with
// OpenTime <= asOf, ordered by OpenTime descending then re-sorted ascending
// before return (i.e. "the most recent `limit` candles as of `asOf`, in
// chronological order") - explicitly the anti-lookahead accessor: it is
// intended to be called from anywhere a backtest needs "what would ListKline
// have returned at this point in simulated time", and must never be able to
// see a candle whose OpenTime is after asOf.
func (r CandleRepository) RangeBefore(ctx context.Context, symbol, timeframe string, asOf time.Time, limit int) ([]exchange.Candle, error)

// Range returns all candles for (symbol, timeframe) with OpenTime in
// [from, to), ordered ascending - the accessor ReplayFeed (Spec 03) iterates.
func (r CandleRepository) Range(ctx context.Context, symbol, timeframe string, from, to time.Time) ([]exchange.Candle, error)

// LatestOpenTime returns the OpenTime of the most recent stored candle for
// (symbol, timeframe), or a zero time.Time if none exist - Spec 02's import
// CLI uses this to resume an interrupted/incremental import without
// re-fetching already-imported history.
func (r CandleRepository) LatestOpenTime(ctx context.Context, symbol, timeframe string) (time.Time, error)

// Count returns the total stored candle count for (symbol, timeframe) -
// used by Spec 02's minimum-history validation and Spec 11's import-lag
// observability.
func (r CandleRepository) Count(ctx context.Context, symbol, timeframe string) (int64, error)
```

### Index & partitioning notes (carried from blueprint §3.3)

- The composite unique index `idx_candle_symbol_tf_time` on `(symbol, timeframe, open_time)` is both the
  idempotency mechanism for `Upsert` (GORM `ON CONFLICT` clause targets this index) and the query-pattern
  index (`RangeBefore`/`Range`/`LatestOpenTime` all filter on exactly these three columns).
- At the PRD's own estimate (~5.2M rows/year for 10 pairs of 1m candles), monthly partitioning by
  `open_time` is flagged as a future operational concern, not a Phase 2 requirement — Postgres handles
  5.2M rows unpartitioned without issue at this scale; partitioning is deferred until it's actually needed
  (avoiding the scope-creep trap of building partition-management tooling for data that doesn't exist yet).

## Acceptance Criteria

1. **Given** `AutoMigrate` runs with `entities.Candle` added, **when** the migration completes, **then** the
   `candles` table exists with a unique constraint spanning `(symbol, timeframe, open_time)`.
2. **Given** `Upsert` is called twice with an overlapping candle (same symbol/timeframe/open_time, possibly
   different OHLCV values from a re-fetch), **when** the second call completes, **then** exactly one row
   exists for that key, with the second call's OHLCV values (last-write-wins), not two rows.
3. **Given** 300 stored 1m candles for `BTCUSDT` spanning `2026-01-01T00:00` to `2026-01-01T05:00`, **when**
   `RangeBefore(ctx, "BTCUSDT", "1m", asOf=2026-01-01T02:30, limit=100)` is called, **then** the returned
   slice contains only candles with `OpenTime <= 2026-01-01T02:30`, ascending order, at most 100 entries —
   never a candle from `02:31` onward regardless of how much data exists after `asOf`.
4. **Given** no candles exist for `(symbol, timeframe)`, **when** `LatestOpenTime` is called, **then** it
   returns a zero `time.Time` and no error (not an error for "no rows found" — this is the expected
   first-import state, not a failure).
5. **Given** `Range(ctx, symbol, tf, from, to)` where `to` is before any stored candle's `OpenTime`, **when**
   called, **then** it returns an empty slice, not an error.
6. **Edge case — partial-write crash recovery**: **given** an import process crashes mid-batch after
   writing candles for `2026-01-01T00:00`-`03:00` but before `03:00`-`06:00`, **when** the import is
   restarted and calls `LatestOpenTime`, **then** it correctly reports `03:00` (the true last-written
   candle) — `Upsert`'s idempotency (Acceptance Criterion #2) guarantees a resumed import that re-fetches
   and re-upserts an overlapping window (e.g. re-fetching from `02:50` to be safe) does not corrupt or
   duplicate the already-written rows.
7. **Edge case — timeframe collision**: **given** candles exist for `BTCUSDT`/`1m` and `BTCUSDT`/`15m`
   covering the same wall-clock period, **when** `RangeBefore` is called for `1m`, **then** it returns only
   `1m` rows — the composite key's `timeframe` column is load-bearing, not decorative (this matters because
   Spec 04's backtest engine will query multiple timeframes for the same symbol concurrently, e.g. Grid's
   `1d` volume check alongside its primary `1m`/cycle-interval window).

## Out of Scope

- Table partitioning implementation — noted as a future concern, not built in Phase 2.
- Multi-exchange candle provenance tracking (e.g. a `Source` column) — single-exchange (Binance) scope per
  PRD §10; not needed.
- On-the-fly timeframe aggregation (deriving `15m` from stored `1m`) — Phase 3's
  `internal/feed/multitimeframe.go` per blueprint §4. Phase 2 requires each timeframe a strategy needs to be
  imported and stored explicitly (see Spec 02 Acceptance Criteria for the concrete implication: the importer
  must support importing more than one timeframe per symbol).

## Dependencies

None — this is the first Phase 2 spec. Specs 02, 03, 04 all depend on it.
