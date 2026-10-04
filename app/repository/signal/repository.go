package repository

import (
	"context"
	"time"

	"go-trade-bot/app/entities"

	"gorm.io/gorm"
)

type SignalRepository struct {
	db *gorm.DB
}

func NewSignalRepository(db *gorm.DB) SignalRepository {
	return SignalRepository{
		db: db,
	}
}

func (r SignalRepository) Create(signal entities.Signal) error {
	return r.db.Create(&signal).Error
}

func (r SignalRepository) GetOpenSignals(symbol string, strategyId uint) (entities.Signal, error) {
	var signals []entities.Signal
	err := r.db.
		Preload("Orders").
		Where("symbol = ? AND status = ? AND strategy_id = ?", symbol, entities.Open, strategyId).
		Find(&signals).Error

	if len(signals) == 0 {
		return entities.Signal{}, nil
	}
	return signals[0], err
}

func (r SignalRepository) GetAllOpenSignals() ([]entities.Signal, error) {
	var signals []entities.Signal
	err := r.db.
		Preload("Orders").
		Preload("Strategy").
		Where("status = ?", entities.Open).
		Find(&signals).Error

	if err != nil {
		return nil, err
	}
	return signals, nil
}

func (r SignalRepository) GetAllClosedSignals() ([]entities.Signal, error) {
	var signals []entities.Signal
	err := r.db.
		Preload("Orders").
		Preload("Strategy").
		Where("status = ?", entities.Closed).
		Find(&signals).Error

	if err != nil {
		return nil, err
	}
	return signals, nil
}

func (r SignalRepository) Update(signal entities.Signal) error {
	err := r.db.Save(&signal).Error

	if err != nil {
		return err
	}
	order := signal.Orders[0]
	return r.db.Save(&order).Error
}

func (r SignalRepository) GetByID(id uint) (entities.Signal, error) {
	var signal entities.Signal
	err := r.db.
		Preload("Orders").
		First(&signal, id).Error
	return signal, err
}

func (r SignalRepository) GetAll() ([]entities.Signal, error) {
	var signals []entities.Signal
	err := r.db.
		Preload("Orders").
		Preload("Strategy").
		Find(&signals).Error

	if err != nil {
		return nil, err
	}
	return signals, nil
}

// ListClosedSince returns strategyID's closed signals (with orders) that
// were opened at or after since, oldest first - the forward-test evidence
// source for agents-platform Phase B's propose_promotion.
func (r SignalRepository) ListClosedSince(ctx context.Context, strategyID uint, since time.Time) ([]entities.Signal, error) {
	var signals []entities.Signal
	err := r.db.WithContext(ctx).
		Preload("Orders").
		Where("strategy_id = ? AND status = ? AND created_at >= ?", strategyID, entities.Closed, since).
		Order("id ASC").
		Find(&signals).Error
	return signals, err
}

// UpdateSimStopEvaluatedAt persists the dryrun simulated-stop watermark
// (fix-01): the OpenTime of the last closed candle evaluated against the
// order's StopLossPrice. Only that one column is written.
func (r SignalRepository) UpdateSimStopEvaluatedAt(orderID uint, evaluatedAt time.Time) error {
	return r.db.Model(&entities.Order{}).
		Where("id = ?", orderID).
		Update("sim_stop_evaluated_at", evaluatedAt).Error
}

// ListClosedBetween returns strategyID's closed signals (with orders) whose
// close time (UpdatedAt, set when the position is closed - the same basis
// GetPerformanceInRange uses via orders.updated_at) is in [from, to), oldest
// close first. Read by cmd/agent's drawdown sweeper (agents-platform C-01
// §2.2).
func (r SignalRepository) ListClosedBetween(ctx context.Context, strategyID uint, from, to time.Time) ([]entities.Signal, error) {
	var signals []entities.Signal
	err := r.db.WithContext(ctx).
		Preload("Orders").
		Where("strategy_id = ? AND status = ? AND updated_at >= ? AND updated_at < ?", strategyID, entities.Closed, from, to).
		Order("updated_at ASC, id ASC").
		Find(&signals).Error
	return signals, err
}

// LastOpenedAt returns when strategyID's most recent signal was opened, or
// nil if it has none (C-01 §2.2 no_signal sweeper).
func (r SignalRepository) LastOpenedAt(ctx context.Context, strategyID uint) (*time.Time, error) {
	var s entities.Signal
	res := r.db.WithContext(ctx).Where("strategy_id = ?", strategyID).Order("created_at DESC").Limit(1).Find(&s)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, nil
	}
	t := s.CreatedAt
	return &t, nil
}
