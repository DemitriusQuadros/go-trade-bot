package settings

import (
	"context"
	"time"

	"go-trade-bot/app/entities"

	"gorm.io/gorm"
)

type Repository interface {
	Get(ctx context.Context) (*entities.Settings, error)
	// Save persists every field EXCEPT AgentsPaused: the agents kill switch
	// is written only by SetAgentsPaused (PUT /agents/kill-switch), so a
	// normal settings save (which round-trips a possibly stale copy) can
	// never flip it.
	Save(ctx context.Context, settings *entities.Settings) error
	// SetAgentsPaused writes ONLY the agents kill switch column.
	SetAgentsPaused(ctx context.Context, paused bool) error
}

type repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) Repository {
	return &repository{db: db}
}

func (r *repository) Get(ctx context.Context) (*entities.Settings, error) {
	// Find, not First: "no row yet" is a normal state (and the agents
	// RunGuard reads this before every model call), so it must not log a
	// GORM "record not found" error each time.
	var found []entities.Settings
	if err := r.db.WithContext(ctx).Order("id ASC").Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return &entities.Settings{}, nil
	}
	return &found[0], nil
}

func (r *repository) Save(ctx context.Context, settings *entities.Settings) error {
	if settings.ID == 0 {
		settings.ID = 1 // Only ever 1 row
	}
	return r.db.WithContext(ctx).Omit("agents_paused").Save(settings).Error
}

func (r *repository) SetAgentsPaused(ctx context.Context, paused bool) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing entities.Settings
		err := tx.First(&existing).Error
		if err == gorm.ErrRecordNotFound {
			return tx.Create(&entities.Settings{ID: 1, AgentsPaused: paused}).Error
		}
		if err != nil {
			return err
		}
		return tx.Model(&entities.Settings{}).Where("id = ?", existing.ID).
			Updates(map[string]any{"agents_paused": paused, "updated_at": time.Now()}).Error
	})
}
