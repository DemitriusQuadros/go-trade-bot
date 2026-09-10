package settings

import (
	"context"
	"go-trade-bot/app/entities"
	"gorm.io/gorm"
)

type Repository interface {
	Get(ctx context.Context) (*entities.Settings, error)
	Save(ctx context.Context, settings *entities.Settings) error
}

type repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) Repository {
	return &repository{db: db}
}

func (r *repository) Get(ctx context.Context) (*entities.Settings, error) {
	var s entities.Settings
	err := r.db.WithContext(ctx).First(&s).Error
	if err == gorm.ErrRecordNotFound {
		return &entities.Settings{}, nil
	}
	return &s, err
}

func (r *repository) Save(ctx context.Context, settings *entities.Settings) error {
	if settings.ID == 0 {
		settings.ID = 1 // Only ever 1 row
	}
	return r.db.WithContext(ctx).Save(settings).Error
}
