package repository

import (
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
