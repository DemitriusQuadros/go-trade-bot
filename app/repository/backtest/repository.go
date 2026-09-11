package backtest

import (
	"context"
	"go-trade-bot/app/entities"

	"gorm.io/gorm"
)

type Repository interface {
	Create(ctx context.Context, run *entities.BacktestRun) error
	GetByID(ctx context.Context, id uint) (entities.BacktestRun, error)
	ListByStrategy(ctx context.Context, strategyID uint) ([]entities.BacktestRun, error)
	ListRunsWithReport(ctx context.Context, strategyID uint) ([]entities.BacktestRun, error)
	Update(ctx context.Context, run entities.BacktestRun) error
}

type BacktestRepository struct {
	db *gorm.DB
}

func NewBacktestRepository(db *gorm.DB) *BacktestRepository {
	return &BacktestRepository{db: db}
}

func (r *BacktestRepository) Create(ctx context.Context, run *entities.BacktestRun) error {
	return r.db.WithContext(ctx).Create(run).Error
}

func (r *BacktestRepository) GetByID(ctx context.Context, id uint) (entities.BacktestRun, error) {
	var run entities.BacktestRun
	err := r.db.WithContext(ctx).First(&run, id).Error
	return run, err
}

func (r *BacktestRepository) ListByStrategy(ctx context.Context, strategyID uint) ([]entities.BacktestRun, error) {
	var runs []entities.BacktestRun
	err := r.db.WithContext(ctx).
		Where("strategy_id = ?", strategyID).
		Order("created_at desc").
		Find(&runs).Error
	return runs, err
}

func (r *BacktestRepository) ListRunsWithReport(ctx context.Context, strategyID uint) ([]entities.BacktestRun, error) {
	var runs []entities.BacktestRun
	err := r.db.WithContext(ctx).
		Where("strategy_id = ? AND html_report_path != '' AND html_report_path IS NOT NULL", strategyID).
		Order("created_at asc").
		Find(&runs).Error
	return runs, err
}

func (r *BacktestRepository) Update(ctx context.Context, run entities.BacktestRun) error {
	return r.db.WithContext(ctx).Save(&run).Error
}
