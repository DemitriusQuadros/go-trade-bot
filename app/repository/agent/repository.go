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
	// ListRunsFiltered is ListRuns with every filter of GET /agent/runs
	// (Phase D-01 §1): newest first ("id DESC"), all filters ANDed.
	ListRunsFiltered(ctx context.Context, f RunFilter) ([]entities.AgentRun, error)
}

// RunFilter selects AgentRun rows for ListRunsFiltered. Zero values mean
// "no filter" (Limit <= 0 = 20).
type RunFilter struct {
	Limit int
	// StrategyID restricts to runs of exactly this strategy.
	StrategyID *uint
	// NoStrategy restricts to runs with strategy_id IS NULL.
	NoStrategy bool
	// Triggers restricts to runs whose trigger is one of these values.
	Triggers []string
	// AgentID restricts to runs of this persona.
	AgentID *uint
	// BeforeID is a cursor: only runs with id < BeforeID.
	BeforeID *uint
}

type GormRepository struct {
	db *gorm.DB
}

func NewGormRepository(db *gorm.DB) *GormRepository {
	return &GormRepository{db: db}
}

// GetInstruction returns the house-rules singleton, or an empty instruction
// when the row does not exist. Uses Limit(1).Find rather than First so a
// missing row is not a gorm "record not found" error - First logged one on
// every agent run (fix-02 B4).
func (r *GormRepository) GetInstruction(ctx context.Context) (entities.AgentInstruction, error) {
	var rows []entities.AgentInstruction
	if err := r.db.WithContext(ctx).Where("id = ?", singletonInstructionID).Limit(1).Find(&rows).Error; err != nil {
		return entities.AgentInstruction{}, err
	}
	if len(rows) == 0 {
		return entities.AgentInstruction{}, nil
	}
	return rows[0], nil
}

// EnsureInstruction seeds an empty house-rules row (ID 1) if none exists;
// an existing row is never touched. Called next to EnsureDefaultAgent in
// every Migrate site (fix-02 B4).
func (r *GormRepository) EnsureInstruction(ctx context.Context) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoNothing: true,
	}).Create(&entities.AgentInstruction{ID: singletonInstructionID, UpdatedAt: time.Now()}).Error
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

// ListRuns is a thin wrapper over ListRunsFiltered, kept for existing
// callers (historyFromPersistedRuns' strategy-scoped replay).
func (r *GormRepository) ListRuns(ctx context.Context, limit int, strategyID *uint) ([]entities.AgentRun, error) {
	return r.ListRunsFiltered(ctx, RunFilter{Limit: limit, StrategyID: strategyID})
}

// ListRunsFiltered returns runs matching f newest-first. The limit is not
// capped here - the HTTP handler caps it.
func (r *GormRepository) ListRunsFiltered(ctx context.Context, f RunFilter) ([]entities.AgentRun, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 20
	}
	query := r.db.WithContext(ctx).Order("id DESC").Limit(limit)
	if f.StrategyID != nil {
		query = query.Where("strategy_id = ?", *f.StrategyID)
	}
	if f.NoStrategy {
		query = query.Where("strategy_id IS NULL")
	}
	if len(f.Triggers) > 0 {
		query = query.Where(`"trigger" IN ?`, f.Triggers)
	}
	if f.AgentID != nil {
		query = query.Where("agent_id = ?", *f.AgentID)
	}
	if f.BeforeID != nil {
		query = query.Where("id < ?", *f.BeforeID)
	}
	var runs []entities.AgentRun
	err := query.Find(&runs).Error
	return runs, err
}

// ListRunsByAgent returns an agent's runs newest-first (agents-platform
// A-02 GET /agents/{id}/runs). Not part of Repository - consumers declare
// their own narrow interface.
func (r *GormRepository) ListRunsByAgent(ctx context.Context, agentID uint, limit int, beforeID *uint) ([]entities.AgentRun, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}
	q := r.db.WithContext(ctx).Where("agent_id = ?", agentID)
	if beforeID != nil {
		q = q.Where("id < ?", *beforeID)
	}
	var runs []entities.AgentRun
	err := q.Order("id DESC").Limit(limit).Find(&runs).Error
	return runs, err
}

// LastRunByAgent returns the agent's newest run, or nil if it has none.
func (r *GormRepository) LastRunByAgent(ctx context.Context, agentID uint) (*entities.AgentRun, error) {
	var runs []entities.AgentRun
	if err := r.db.WithContext(ctx).Select("id", "status", "trigger", "started_at", "agent_id").
		Where("agent_id = ?", agentID).Order("id DESC").Limit(1).Find(&runs).Error; err != nil {
		return nil, err
	}
	if len(runs) == 0 {
		return nil, nil
	}
	return &runs[0], nil
}
