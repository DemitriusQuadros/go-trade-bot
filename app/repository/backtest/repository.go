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
	ListRecent(ctx context.Context, limit int) ([]entities.BacktestRun, error)
	ListRunsWithReport(ctx context.Context, strategyID uint) ([]entities.BacktestRun, error)
	Update(ctx context.Context, run entities.BacktestRun) error
	Delete(ctx context.Context, id uint) error
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
	err := r.db.WithContext(ctx).Preload("Strategy").First(&run, id).Error
	return run, err
}

func (r *BacktestRepository) ListByStrategy(ctx context.Context, strategyID uint) ([]entities.BacktestRun, error) {
	var runs []entities.BacktestRun
	err := r.db.WithContext(ctx).
		Preload("Strategy").
		Where("strategy_id = ?", strategyID).
		Order("created_at desc").
		Find(&runs).Error
	return runs, err
}

// ListRecent returns the most recently created runs across all strategies,
// for the web frontend's cross-strategy "Recent Runs" panel. Preloads the
// owning Strategy so callers can show which strategy each run belongs to
// without an extra round trip per row.
func (r *BacktestRepository) ListRecent(ctx context.Context, limit int) ([]entities.BacktestRun, error) {
	var runs []entities.BacktestRun
	err := r.db.WithContext(ctx).
		Preload("Strategy").
		Order("created_at desc").
		Limit(limit).
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

func (r *BacktestRepository) Delete(ctx context.Context, id uint) error {
	res := r.db.WithContext(ctx).Delete(&entities.BacktestRun{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
