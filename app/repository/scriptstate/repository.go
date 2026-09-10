package scriptstate

import (
	"context"

	"go-trade-bot/app/entities"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Repository interface {
	// Get returns the row for (strategyID, symbol), or a zero-value
	// entities.ScriptState with ID==0 and gorm.ErrRecordNotFound wrapped in
	// err if none exists yet (first cycle for this strategy/symbol pair) -
	// callers (the store) must treat ErrRecordNotFound as "empty state", not
	// a hard failure.
	Get(ctx context.Context, strategyID uint, symbol string) (entities.ScriptState, error)
	// Upsert creates or updates the (strategyID, symbol) row's StateJSON in
	// one call - ON CONFLICT (strategy_id, symbol) DO UPDATE, using GORM's
	// clause.OnConflict, not a manual get-then-save.
	Upsert(ctx context.Context, state entities.ScriptState) error
}

type repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) Repository {
	return &repository{db: db}
}

func (r *repository) Get(ctx context.Context, strategyID uint, symbol string) (entities.ScriptState, error) {
	var s entities.ScriptState
	err := r.db.WithContext(ctx).
		Where("strategy_id = ? AND symbol = ?", strategyID, symbol).
		First(&s).Error
	return s, err
}

func (r *repository) Upsert(ctx context.Context, state entities.ScriptState) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "strategy_id"}, {Name: "symbol"}},
		DoUpdates: clause.AssignmentColumns([]string{"state_json", "updated_at"}),
	}).Create(&state).Error
}
