package performancesnapshot

import (
	"context"
	"time"

	"go-trade-bot/app/entities"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Repository interface {
	// Upsert writes a snapshot idempotently: a row matching (StrategyID,
	// Symbol, Bucket, PeriodStart) is updated in place (Profit/Trades/
	// PeriodEnd overwritten), never duplicated - backend-05 AC#2.
	Upsert(ctx context.Context, snapshot entities.StrategyPerformanceSnapshot) error
	// ListDaily returns BucketDaily rows for (strategyID, symbol) with
	// PeriodStart in [from, to), ordered ascending - the raw material both
	// the daily view and the weekly/monthly aggregation build on.
	ListDaily(ctx context.Context, strategyID uint, symbol string, from, to time.Time) ([]entities.StrategyPerformanceSnapshot, error)
}

type SnapshotRepository struct {
	db *gorm.DB
}

func NewSnapshotRepository(db *gorm.DB) *SnapshotRepository {
	return &SnapshotRepository{db: db}
}

func (r *SnapshotRepository) Upsert(ctx context.Context, snapshot entities.StrategyPerformanceSnapshot) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "strategy_id"}, {Name: "symbol"}, {Name: "bucket"}, {Name: "period_start"}},
		DoUpdates: clause.AssignmentColumns([]string{"period_end", "profit", "trades"}),
	}).Create(&snapshot).Error
}

func (r *SnapshotRepository) ListDaily(ctx context.Context, strategyID uint, symbol string, from, to time.Time) ([]entities.StrategyPerformanceSnapshot, error) {
	var rows []entities.StrategyPerformanceSnapshot
	err := r.db.WithContext(ctx).
		Where("strategy_id = ? AND symbol = ? AND bucket = ? AND period_start >= ? AND period_start < ?",
			strategyID, symbol, entities.BucketDaily, from, to).
		Order("period_start ASC").
		Find(&rows).Error
	return rows, err
}
