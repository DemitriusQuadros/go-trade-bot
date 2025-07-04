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

func (r StrategyRepository) Update(ctx context.Context, strategy entities.Strategy) error {
	return r.db.WithContext(ctx).Save(&strategy).Error
}

func (r StrategyRepository) Delete(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&entities.Strategy{}, id).Error
}

func (r StrategyRepository) SaveExecution(ctx context.Context, execution entities.StrategyExecution) error {
	return r.db.WithContext(ctx).Create(&execution).Error
}

func (r StrategyRepository) CountOpenSignals(ctx context.Context, strategy entities.Strategy) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&entities.Signal{}).
		Where("strategy_id = ? AND status = ?", strategy.ID, entities.Open).
		Count(&count).Error
	return count, err
}
