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

func (r SignalRepository) GetOpenSignals(symbol string) (entities.Signal, error) {
	var signals []entities.Signal
	err := r.db.Where("symbol = ? AND status = ?", symbol, entities.Open).Find(&signals).Error
	return signals[0], err
}

func (r SignalRepository) Update(signal entities.Signal) error {
	return r.db.Save(&signal).Error
}
