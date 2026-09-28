// Package proposal is the GORM repository for agents-platform Phase B
// strategy change proposals (entities.StrategyChangeProposal) and the
// deploy gate thresholds singleton (entities.DeployGateConfig).
//
// Nothing in this package can change a strategy's code: applying a
// proposal is StrategyRepository.ReplaceScriptSource, reachable only from
// app/usecase/proposal.Applier (cmd/agent's agent:apply_proposal task).
package proposal

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"go-trade-bot/app/entities"
	"go-trade-bot/internal/customerror"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Filter narrows List. Zero values mean "no filter". StrategyID matches
// the target OR the challenger. Limit <= 0 defaults to 50 (capped at 200).
type Filter struct {
	Statuses   []entities.ProposalStatus
	StrategyID *uint
	AgentID    *uint
	Kind       entities.ProposalKind
	Limit      int
	BeforeID   *uint
}

// Transition carries the optional columns written together with a status
// change. nil = leave unchanged.
type Transition struct {
	DecisionNote    *string
	DecidedAt       *time.Time
	AppliedAt       *time.Time
	FailureReason   *string
	LastFlatCheckAt *time.Time
}

// Repository is the persistence port for proposals and gate thresholds.
type Repository interface {
	Create(ctx context.Context, p entities.StrategyChangeProposal) (entities.StrategyChangeProposal, error)
	Get(ctx context.Context, id uint) (entities.StrategyChangeProposal, error)
	// List returns newest first.
	List(ctx context.Context, f Filter) ([]entities.StrategyChangeProposal, error)
	CountByStatus(ctx context.Context, status entities.ProposalStatus) (int64, error)
	// SupersedePending marks every pending proposal for targetID except
	// exceptID as superseded, returning how many changed.
	SupersedePending(ctx context.Context, targetID, exceptID uint, reason string) (int64, error)
	// TransitionStatus is a compare-and-set: it moves the proposal to `to`
	// only if its current status is one of `from`, and reports whether it
	// did (false = someone else moved it first).
	TransitionStatus(ctx context.Context, id uint, from []entities.ProposalStatus, to entities.ProposalStatus, t Transition) (bool, error)
	// TouchFlatCheck stamps LastFlatCheckAt without changing status.
	TouchFlatCheck(ctx context.Context, id uint, at time.Time) error

	// GetGateConfig returns the singleton, or the defaults if not seeded.
	GetGateConfig(ctx context.Context) (entities.DeployGateConfig, error)
	SaveGateConfig(ctx context.Context, c entities.DeployGateConfig) (entities.DeployGateConfig, error)
	// EnsureGateConfig seeds the defaults if the row does not exist
	// (idempotent, safe across concurrently migrating binaries).
	EnsureGateConfig(ctx context.Context) error
}

// GormRepository implements Repository.
type GormRepository struct {
	db *gorm.DB
}

// NewGormRepository builds a GormRepository.
func NewGormRepository(db *gorm.DB) *GormRepository {
	return &GormRepository{db: db}
}

func (r *GormRepository) Create(ctx context.Context, p entities.StrategyChangeProposal) (entities.StrategyChangeProposal, error) {
	if p.Status == "" {
		p.Status = entities.ProposalPending
	}
	err := r.db.WithContext(ctx).Create(&p).Error
	return p, err
}

func (r *GormRepository) Get(ctx context.Context, id uint) (entities.StrategyChangeProposal, error) {
	var p entities.StrategyChangeProposal
	err := r.db.WithContext(ctx).First(&p, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return p, &customerror.CustomError{Code: http.StatusNotFound, Message: fmt.Sprintf("proposal %d not found", id)}
	}
	return p, err
}

func (r *GormRepository) List(ctx context.Context, f Filter) ([]entities.StrategyChangeProposal, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	q := r.db.WithContext(ctx).Model(&entities.StrategyChangeProposal{})
	if len(f.Statuses) > 0 {
		q = q.Where("status IN ?", f.Statuses)
	}
	if f.StrategyID != nil {
		q = q.Where("target_strategy_id = ? OR challenger_strategy_id = ?", *f.StrategyID, *f.StrategyID)
	}
	if f.AgentID != nil {
		q = q.Where("agent_id = ?", *f.AgentID)
	}
	if f.Kind != "" {
		q = q.Where("kind = ?", f.Kind)
	}
	if f.BeforeID != nil {
		q = q.Where("id < ?", *f.BeforeID)
	}
	var out []entities.StrategyChangeProposal
	err := q.Order("id DESC").Limit(limit).Find(&out).Error
	return out, err
}

func (r *GormRepository) CountByStatus(ctx context.Context, status entities.ProposalStatus) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&entities.StrategyChangeProposal{}).Where("status = ?", status).Count(&n).Error
	return n, err
}

func (r *GormRepository) SupersedePending(ctx context.Context, targetID, exceptID uint, reason string) (int64, error) {
	res := r.db.WithContext(ctx).Model(&entities.StrategyChangeProposal{}).
		Where("target_strategy_id = ? AND status = ? AND id <> ?", targetID, entities.ProposalPending, exceptID).
		Updates(map[string]any{"status": entities.ProposalSuperseded, "failure_reason": reason, "updated_at": time.Now()})
	return res.RowsAffected, res.Error
}

func (r *GormRepository) TransitionStatus(ctx context.Context, id uint, from []entities.ProposalStatus, to entities.ProposalStatus, t Transition) (bool, error) {
	updates := map[string]any{"status": to, "updated_at": time.Now()}
	if t.DecisionNote != nil {
		updates["decision_note"] = *t.DecisionNote
	}
	if t.DecidedAt != nil {
		updates["decided_at"] = *t.DecidedAt
	}
	if t.AppliedAt != nil {
		updates["applied_at"] = *t.AppliedAt
	}
	if t.FailureReason != nil {
		updates["failure_reason"] = *t.FailureReason
	}
	if t.LastFlatCheckAt != nil {
		updates["last_flat_check_at"] = *t.LastFlatCheckAt
	}
	res := r.db.WithContext(ctx).Model(&entities.StrategyChangeProposal{}).
		Where("id = ? AND status IN ?", id, from).Updates(updates)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

func (r *GormRepository) TouchFlatCheck(ctx context.Context, id uint, at time.Time) error {
	return r.db.WithContext(ctx).Model(&entities.StrategyChangeProposal{}).Where("id = ?", id).
		Updates(map[string]any{"last_flat_check_at": at, "updated_at": time.Now()}).Error
}

func (r *GormRepository) GetGateConfig(ctx context.Context) (entities.DeployGateConfig, error) {
	var found []entities.DeployGateConfig
	if err := r.db.WithContext(ctx).Where("id = ?", entities.DeployGateConfigID).Limit(1).Find(&found).Error; err != nil {
		return entities.DeployGateConfig{}, err
	}
	if len(found) == 0 {
		return entities.DefaultDeployGateConfig(), nil
	}
	return found[0], nil
}

func (r *GormRepository) SaveGateConfig(ctx context.Context, c entities.DeployGateConfig) (entities.DeployGateConfig, error) {
	c.ID = entities.DeployGateConfigID
	c.UpdatedAt = time.Now()
	if err := r.db.WithContext(ctx).Save(&c).Error; err != nil {
		return entities.DeployGateConfig{}, err
	}
	return r.GetGateConfig(ctx)
}

func (r *GormRepository) EnsureGateConfig(ctx context.Context) error {
	c := entities.DefaultDeployGateConfig()
	c.UpdatedAt = time.Now()
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&c).Error
}
