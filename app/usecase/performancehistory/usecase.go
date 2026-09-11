package performancehistory

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"go-trade-bot/app/entities"
)

// ErrStrategyNotFound is returned by GetHistory when the requested strategy
// ID doesn't exist - the web handler maps this to HTTP 404, distinct from
// the 400s used for malformed bucket/symbol/limit input.
var ErrStrategyNotFound = errors.New("strategy not found")

type StrategyRepository interface {
	GetPerformanceInRange(ctx context.Context, from, to time.Time) ([]entities.StrategyPerformance, error)
	GetByID(ctx context.Context, id uint) (entities.Strategy, error)
}

type SnapshotRepository interface {
	Upsert(ctx context.Context, snapshot entities.StrategyPerformanceSnapshot) error
	ListDaily(ctx context.Context, strategyID uint, symbol string, from, to time.Time) ([]entities.StrategyPerformanceSnapshot, error)
}

// HistoryPoint is the use-case-shaped, bucket-agnostic output of GetHistory -
// the frontend-specialist agent's P&L-history sparkline consumes exactly
// this shape (mapped 1:1 onto the REST response by the web handler).
type HistoryPoint struct {
	PeriodStart time.Time `json:"period_start"`
	PeriodEnd   time.Time `json:"period_end"`
	Profit      float64   `json:"profit"`
	Trades      int       `json:"trades"`
}

type PerformanceHistoryUseCase struct {
	strategyRepo StrategyRepository
	snapshotRepo SnapshotRepository
}

func NewPerformanceHistoryUseCase(strategyRepo StrategyRepository, snapshotRepo SnapshotRepository) *PerformanceHistoryUseCase {
	return &PerformanceHistoryUseCase{strategyRepo: strategyRepo, snapshotRepo: snapshotRepo}
}

// Snapshot computes and persists one StrategyPerformanceSnapshot row per
// (strategy, symbol) pair that closed at least one order in
// [periodStart, periodEnd) - idempotent (SnapshotRepository.Upsert's
// composite unique index), safe to re-run for the same period (AC#2) or an
// arbitrary historical period (AC#7's backfill case). Only BucketDaily is
// ever persisted by this method; weekly/monthly are derived at query time by
// GetHistory.
//
// AC#5: a (strategy, symbol) pair with zero closed orders in the window
// produces NO row - not a zero-profit row - since GetPerformanceInRange's
// join naturally excludes pairs with no matching orders in range.
func (u *PerformanceHistoryUseCase) Snapshot(ctx context.Context, bucket entities.PerformanceBucket, periodStart, periodEnd time.Time) error {
	performances, err := u.strategyRepo.GetPerformanceInRange(ctx, periodStart, periodEnd)
	if err != nil {
		return fmt.Errorf("failed to compute performance in range: %w", err)
	}

	for _, p := range performances {
		if p.StrategyID == 0 {
			// Defensive: GetPerformanceInRange should always populate
			// StrategyID via its explicit `st.id` select, but a zero value
			// here would silently corrupt the unique index's semantics -
			// skip rather than persist a garbage snapshot.
			continue
		}

		snapshot := entities.StrategyPerformanceSnapshot{
			StrategyID:  p.StrategyID,
			Symbol:      p.Symbol,
			Bucket:      bucket,
			PeriodStart: periodStart,
			PeriodEnd:   periodEnd,
			Profit:      p.Profit,
			Trades:      p.Trades,
			CreatedAt:   time.Now().UTC(),
		}

		if err := u.snapshotRepo.Upsert(ctx, snapshot); err != nil {
			return fmt.Errorf("failed to persist snapshot for strategy %d symbol %s: %w", p.StrategyID, p.Symbol, err)
		}
	}

	return nil
}

// GetHistory returns the persisted snapshot series for one strategy+symbol,
// ordered by PeriodStart ascending, at most `limit` entries. bucket=daily
// reads persisted rows directly; bucket=weekly/monthly derive their series
// by aggregating the underlying daily rows at query time (Target Behavior:
// "halves the scheduling surface, correct because daily buckets are the
// finest granularity anything downstream needs").
func (u *PerformanceHistoryUseCase) GetHistory(
	ctx context.Context,
	strategyID uint,
	symbol string,
	bucket entities.PerformanceBucket,
	limit int,
) ([]HistoryPoint, error) {
	if _, err := u.strategyRepo.GetByID(ctx, strategyID); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStrategyNotFound, err)
	}

	if limit <= 0 {
		limit = 30
	}

	switch bucket {
	case entities.BucketDaily:
		return u.dailyHistory(ctx, strategyID, symbol, limit)
	case entities.BucketWeekly:
		return u.aggregatedHistory(ctx, strategyID, symbol, limit, 7, weekBounds)
	case entities.BucketMonthly:
		return u.aggregatedHistory(ctx, strategyID, symbol, limit, 31, monthBounds)
	default:
		return nil, fmt.Errorf("unsupported bucket %q", bucket)
	}
}

func (u *PerformanceHistoryUseCase) dailyHistory(ctx context.Context, strategyID uint, symbol string, limit int) ([]HistoryPoint, error) {
	// Fetch a generous window (limit days back from now) and take the most
	// recent `limit` rows - AC#4: fewer than `limit` days of history simply
	// returns however many exist, no error, no zero-padding.
	to := time.Now().UTC()
	from := to.AddDate(0, 0, -limit-1)

	rows, err := u.snapshotRepo.ListDaily(ctx, strategyID, symbol, from, to)
	if err != nil {
		return nil, err
	}

	if len(rows) > limit {
		rows = rows[len(rows)-limit:]
	}

	points := make([]HistoryPoint, len(rows))
	for i, r := range rows {
		points[i] = HistoryPoint{PeriodStart: r.PeriodStart, PeriodEnd: r.PeriodEnd, Profit: r.Profit, Trades: r.Trades}
	}
	return points, nil
}

// aggregatedHistory fetches enough daily rows to cover `limit` buckets of
// `approxDaysPerBucket` size, groups them via boundsFn (week- or
// month-aligned), sums Profit/Trades per group, and returns the most recent
// `limit` groups ascending.
func (u *PerformanceHistoryUseCase) aggregatedHistory(
	ctx context.Context,
	strategyID uint,
	symbol string,
	limit int,
	approxDaysPerBucket int,
	boundsFn func(time.Time) (time.Time, time.Time),
) ([]HistoryPoint, error) {
	to := time.Now().UTC()
	from := to.AddDate(0, 0, -(limit*approxDaysPerBucket + approxDaysPerBucket))

	rows, err := u.snapshotRepo.ListDaily(ctx, strategyID, symbol, from, to)
	if err != nil {
		return nil, err
	}

	groups := map[time.Time]*HistoryPoint{}
	for _, r := range rows {
		start, end := boundsFn(r.PeriodStart)
		g, ok := groups[start]
		if !ok {
			g = &HistoryPoint{PeriodStart: start, PeriodEnd: end}
			groups[start] = g
		}
		g.Profit += r.Profit
		g.Trades += r.Trades
	}

	points := make([]HistoryPoint, 0, len(groups))
	for _, g := range groups {
		points = append(points, *g)
	}
	sort.Slice(points, func(i, j int) bool { return points[i].PeriodStart.Before(points[j].PeriodStart) })

	if len(points) > limit {
		points = points[len(points)-limit:]
	}
	return points, nil
}

// weekBounds returns the Monday 00:00 UTC start (and following Monday
// 00:00 UTC end) of the ISO week containing t.
func weekBounds(t time.Time) (time.Time, time.Time) {
	t = t.UTC()
	weekday := int(t.Weekday())
	if weekday == 0 { // Sunday -> ISO weekday 7
		weekday = 7
	}
	daysSinceMonday := weekday - 1
	start := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -daysSinceMonday)
	end := start.AddDate(0, 0, 7)
	return start, end
}

// monthBounds returns the first-of-month 00:00 UTC start (and first of the
// following month as end) containing t.
func monthBounds(t time.Time) (time.Time, time.Time) {
	t = t.UTC()
	start := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)
	return start, end
}
