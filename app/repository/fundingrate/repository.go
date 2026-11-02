package fundingrate

import (
	"context"
	"errors"
	"time"

	"go-trade-bot/app/entities"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Repository interface {
	Upsert(ctx context.Context, rates []entities.FundingRate) error
	Range(ctx context.Context, symbol string, from, to time.Time) ([]entities.FundingRate, error)
	ListInRange(ctx context.Context, symbol string, from, to time.Time) ([]entities.FundingRate, error)
	LatestFundingTime(ctx context.Context, symbol string) (time.Time, error)
	Count(ctx context.Context, symbol string) (int64, error)
}

type FundingRateRepository struct {
	db *gorm.DB
}

func NewFundingRateRepository(db *gorm.DB) FundingRateRepository {
	return FundingRateRepository{db: db}
}

const upsertBatchSize = 2000

// Upsert writes funding rates idempotently: rows matching (symbol, funding_time)
// are updated with rate and mark_price, never duplicated.
func (r FundingRateRepository) Upsert(ctx context.Context, rates []entities.FundingRate) error {
	if len(rates) == 0 {
		return nil
	}

	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "symbol"}, {Name: "funding_time"}},
		DoUpdates: clause.AssignmentColumns([]string{"rate", "mark_price"}),
	}).CreateInBatches(&rates, upsertBatchSize).Error
}

// Range returns all funding rates for symbol with funding_time in [from, to],
// ordered by funding_time ASC.
func (r FundingRateRepository) Range(ctx context.Context, symbol string, from, to time.Time) ([]entities.FundingRate, error) {
	var rows []entities.FundingRate
	err := r.db.WithContext(ctx).
		Where("symbol = ? AND funding_time >= ? AND funding_time <= ?", symbol, from, to).
		Order("funding_time ASC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// ListInRange returns all funding rates for symbol with funding_time in [from, to],
// ordered by funding_time ASC. Satisfies backtest.FundingRateRepository.
func (r FundingRateRepository) ListInRange(ctx context.Context, symbol string, from, to time.Time) ([]entities.FundingRate, error) {
	return r.Range(ctx, symbol, from, to)
}

// LatestFundingTime returns the FundingTime of the most recent stored funding rate
// for symbol, or a zero time.Time if none exist.
func (r FundingRateRepository) LatestFundingTime(ctx context.Context, symbol string) (time.Time, error) {
	var f entities.FundingRate
	err := r.db.WithContext(ctx).
		Where("symbol = ?", symbol).
		Order("funding_time DESC").
		Limit(1).
		First(&f).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return time.Time{}, nil
		}
		return time.Time{}, err
	}
	return f.FundingTime, nil
}

// Count returns the total stored funding rate count for symbol.
func (r FundingRateRepository) Count(ctx context.Context, symbol string) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&entities.FundingRate{}).
		Where("symbol = ?", symbol).
		Count(&count).Error
	return count, err
}
