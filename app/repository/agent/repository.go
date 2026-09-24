// Package agent is the GORM-backed repository for the agent subsystem's two
// persisted concepts (Backend Spec 02): AgentInstruction (the operator's
// standing "house rules") and AgentRun (the audit-log row per agent
// invocation).
package agent

import (
	"context"
	"time"

	"go-trade-bot/app/entities"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// singletonInstructionID is the fixed primary key AgentInstruction is always
// upserted against - there is never a second row (Backend Spec 02).
const singletonInstructionID = 1

type Repository interface {
	// GetInstruction returns entities.AgentInstruction{} (empty Content)
	// with a nil error when no row has been saved yet - callers must not
	// need to special-case "not found" as an error.
	GetInstruction(ctx context.Context) (entities.AgentInstruction, error)
	SaveInstruction(ctx context.Context, content string) (entities.AgentInstruction, error)
	CreateRun(ctx context.Context, run entities.AgentRun) (entities.AgentRun, error)
	// UpdateRun is called incrementally as tool calls happen, not just once
	// at the end, so a crash mid-tool-loop leaves a partial audit row.
	UpdateRun(ctx context.Context, run entities.AgentRun) error
	GetRun(ctx context.Context, id uint) (entities.AgentRun, error)
	// ListRuns returns runs newest-first, bounded by limit. strategyID, when
	// non-nil, restricts the result to runs whose StrategyID matches exactly
	// (Frontend Spec 02's per-strategy audit filter) - a straightforward
	// addition to the existing method rather than a second query method.
	ListRuns(ctx context.Context, limit int, strategyID *uint) ([]entities.AgentRun, error)
}

type GormRepository struct {
	db *gorm.DB
}

func NewGormRepository(db *gorm.DB) *GormRepository {
	return &GormRepository{db: db}
}

func (r *GormRepository) GetInstruction(ctx context.Context) (entities.AgentInstruction, error) {
	var instruction entities.AgentInstruction
	err := r.db.WithContext(ctx).First(&instruction, singletonInstructionID).Error
	if err == gorm.ErrRecordNotFound {
		return entities.AgentInstruction{}, nil
	}
	if err != nil {
		return entities.AgentInstruction{}, err
	}
	return instruction, nil
}

// SaveInstruction upserts row ID 1 unconditionally - there is never a
// second row, regardless of how many times this is called.
func (r *GormRepository) SaveInstruction(ctx context.Context, content string) (entities.AgentInstruction, error) {
	instruction := entities.AgentInstruction{
		ID:        singletonInstructionID,
		Content:   content,
		UpdatedAt: time.Now(),
	}
	err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{"content", "updated_at"}),
	}).Create(&instruction).Error
	if err != nil {
		return entities.AgentInstruction{}, err
	}
	return instruction, nil
}

func (r *GormRepository) CreateRun(ctx context.Context, run entities.AgentRun) (entities.AgentRun, error) {
	if run.StartedAt.IsZero() {
		run.StartedAt = time.Now()
	}
	err := r.db.WithContext(ctx).Create(&run).Error
	return run, err
}

func (r *GormRepository) UpdateRun(ctx context.Context, run entities.AgentRun) error {
	return r.db.WithContext(ctx).Save(&run).Error
}

func (r *GormRepository) GetRun(ctx context.Context, id uint) (entities.AgentRun, error) {
	var run entities.AgentRun
	err := r.db.WithContext(ctx).First(&run, id).Error
	return run, err
}

func (r *GormRepository) ListRuns(ctx context.Context, limit int, strategyID *uint) ([]entities.AgentRun, error) {
	if limit <= 0 {
		limit = 20
	}
	query := r.db.WithContext(ctx).Order("id DESC").Limit(limit)
	if strategyID != nil {
		query = query.Where("strategy_id = ?", *strategyID)
	}
	var runs []entities.AgentRun
	err := query.Find(&runs).Error
	return runs, err
}
