package candle

import (
	"context"
	"errors"
	"time"

	"go-trade-bot/app/entities"
	"go-trade-bot/internal/exchange"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Repository interface {
	Upsert(ctx context.Context, candles []entities.Candle) error
	RangeBefore(ctx context.Context, symbol, timeframe string, asOf time.Time, limit int) ([]exchange.Candle, error)
	Range(ctx context.Context, symbol, timeframe string, from, to time.Time) ([]exchange.Candle, error)
	LatestOpenTime(ctx context.Context, symbol, timeframe string) (time.Time, error)
	Count(ctx context.Context, symbol, timeframe string) (int64, error)
	CountInRange(ctx context.Context, symbol, timeframe string, from, to time.Time) (int64, error)
}

type CandleRepository struct {
	db *gorm.DB
}

func NewCandleRepository(db *gorm.DB) CandleRepository {
	return CandleRepository{db: db}
}

// Upsert writes candles idempotently: a row matching (Symbol, Timeframe,
// OpenTime) is updated in place (OHLCV overwritten), never duplicated.
func (r CandleRepository) Upsert(ctx context.Context, candles []entities.Candle) error {
	if len(candles) == 0 {
		return nil
	}

	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "symbol"}, {Name: "timeframe"}, {Name: "open_time"}},
		DoUpdates: clause.AssignmentColumns([]string{"open", "high", "low", "close", "volume"}),
	}).Create(&candles).Error
}

// RangeBefore returns up to `limit` candles for (symbol, timeframe) with
// OpenTime <= asOf, ordered by OpenTime descending then re-sorted ascending
// before return (in chronological order). Anti-lookahead accessor.
func (r CandleRepository) RangeBefore(ctx context.Context, symbol, timeframe string, asOf time.Time, limit int) ([]exchange.Candle, error) {
	if limit <= 0 {
		return []exchange.Candle{}, nil
	}

	var rows []entities.Candle
	err := r.db.WithContext(ctx).
		Where("symbol = ? AND timeframe = ? AND open_time <= ?", symbol, timeframe, asOf).
		Order("open_time DESC").
		Limit(limit).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}

	// Re-sort ascending into chronological order
	for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
		rows[i], rows[j] = rows[j], rows[i]
	}

	return toExchangeCandles(rows), nil
}

// Range returns all candles for (symbol, timeframe) with OpenTime in
// [from, to), ordered ascending.
func (r CandleRepository) Range(ctx context.Context, symbol, timeframe string, from, to time.Time) ([]exchange.Candle, error) {
	var rows []entities.Candle
	err := r.db.WithContext(ctx).
		Where("symbol = ? AND timeframe = ? AND open_time >= ? AND open_time < ?", symbol, timeframe, from, to).
		Order("open_time ASC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}

	return toExchangeCandles(rows), nil
}

// LatestOpenTime returns the OpenTime of the most recent stored candle for
// (symbol, timeframe), or a zero time.Time if none exist.
func (r CandleRepository) LatestOpenTime(ctx context.Context, symbol, timeframe string) (time.Time, error) {
	var c entities.Candle
	err := r.db.WithContext(ctx).
		Where("symbol = ? AND timeframe = ?", symbol, timeframe).
		Order("open_time DESC").
		Limit(1).
		First(&c).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return time.Time{}, nil
		}
		return time.Time{}, err
	}
	return c.OpenTime, nil
}

// Count returns the total stored candle count for (symbol, timeframe).
func (r CandleRepository) Count(ctx context.Context, symbol, timeframe string) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&entities.Candle{}).
		Where("symbol = ? AND timeframe = ?", symbol, timeframe).
		Count(&count).Error
	return count, err
}

func toExchangeCandles(rows []entities.Candle) []exchange.Candle {
	result := make([]exchange.Candle, len(rows))
	for i, r := range rows {
		result[i] = exchange.Candle{
			Symbol:    r.Symbol,
			Timeframe: r.Timeframe,
			OpenTime:  r.OpenTime,
			Open:      r.Open,
			High:      r.High,
			Low:       r.Low,
			Close:     r.Close,
			Volume:    r.Volume,
		}
	}
	return result
}

func (r CandleRepository) CountInRange(ctx context.Context, symbol, timeframe string, from, to time.Time) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&entities.Candle{}).
		Where("symbol = ? AND timeframe = ? AND open_time >= ? AND open_time < ?", symbol, timeframe, from, to).
		Count(&count).Error
	return count, err
}
