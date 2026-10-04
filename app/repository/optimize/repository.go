package optimize

import (
	"context"

	"go-trade-bot/app/entities"

	"gorm.io/gorm"
)

type Repository interface {
	Create(ctx context.Context, run *entities.OptimizationRun) error
	GetByID(ctx context.Context, id uint) (entities.OptimizationRun, error)
	Update(ctx context.Context, run entities.OptimizationRun) error
	ListByStrategy(ctx context.Context, strategyID uint) ([]entities.OptimizationRun, error)
}

type OptimizationRepository struct {
	db *gorm.DB
}

func NewOptimizationRepository(db *gorm.DB) *OptimizationRepository {
	return &OptimizationRepository{db: db}
}

func (r *OptimizationRepository) Create(ctx context.Context, run *entities.OptimizationRun) error {
	return r.db.WithContext(ctx).Create(run).Error
}

func (r *OptimizationRepository) GetByID(ctx context.Context, id uint) (entities.OptimizationRun, error) {
	var run entities.OptimizationRun
	err := r.db.WithContext(ctx).First(&run, id).Error
	return run, err
}

func (r *OptimizationRepository) Update(ctx context.Context, run entities.OptimizationRun) error {
	return r.db.WithContext(ctx).Save(&run).Error
}

func (r *OptimizationRepository) ListByStrategy(ctx context.Context, strategyID uint) ([]entities.OptimizationRun, error) {
	var runs []entities.OptimizationRun
	err := r.db.WithContext(ctx).
		Where("strategy_id = ?", strategyID).
		Order("created_at desc").
		Find(&runs).Error
	return runs, err
}
