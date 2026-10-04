package entities

import "time"

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
//
// Only BucketDaily rows are ever persisted (backend-05's daily asynq job is
// the only writer) - BucketWeekly/BucketMonthly values exist on this type
// for API-contract symmetry with PerformanceBucket, but
// app/usecase/performancehistory derives weekly/monthly figures by
// aggregating daily rows at query time rather than writing separate weekly/
// monthly rows, per the spec's explicit "halves the scheduling surface"
// rationale.
type StrategyPerformanceSnapshot struct {
	ID          uint              `gorm:"primaryKey" json:"id"`
	StrategyID  uint              `gorm:"uniqueIndex:idx_perf_snapshot_period" json:"strategy_id"`
	Symbol      string            `gorm:"uniqueIndex:idx_perf_snapshot_period" json:"symbol"`
	Bucket      PerformanceBucket `gorm:"uniqueIndex:idx_perf_snapshot_period" json:"bucket"`
	PeriodStart time.Time         `gorm:"uniqueIndex:idx_perf_snapshot_period" json:"period_start"`
	PeriodEnd   time.Time         `json:"period_end"`
	Profit      float64           `json:"profit"`
	Trades      int               `json:"trades"`
	CreatedAt   time.Time         `json:"created_at"`
}
