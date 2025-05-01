package repository

import (
	"context"
	"go-trade-bot/app/entities"

	"gorm.io/gorm"
)

type StrategyRepository struct {
	db *gorm.DB
}

func NewStrategyRepository(db *gorm.DB) StrategyRepository {
	return StrategyRepository{
		db: db,
	}
}

func (r StrategyRepository) Save(ctx context.Context, strategy entities.Strategy) error {
	return r.db.WithContext(ctx).Create(&strategy).Error
}

func (r StrategyRepository) GetByID(ctx context.Context, id uint) (entities.Strategy, error) {
	var strategy entities.Strategy
	err := r.db.WithContext(ctx).First(&strategy, id).Error
	return strategy, err
}

func (r StrategyRepository) GetAll(ctx context.Context) ([]entities.Strategy, error) {
	var strategies []entities.Strategy
	err := r.db.WithContext(ctx).Find(&strategies).Error
	return strategies, err
}
