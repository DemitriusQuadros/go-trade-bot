package repository_test

import (
	"context"
	"testing"

	"go-trade-bot/app/entities"
	repository "go-trade-bot/app/repository/strategy"

	"github.com/stretchr/testify/assert"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestStrategyRepository_Save(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)

	err = db.AutoMigrate(&entities.Strategy{})
	assert.NoError(t, err)

	repo := repository.NewStrategyRepository(db)

	strategy := entities.Strategy{Name: "Test Strategy"}
	err = repo.Save(context.Background(), strategy)
	assert.NoError(t, err)

	var result entities.Strategy
	err = db.First(&result, "name = ?", "Test Strategy").Error
	assert.NoError(t, err)
	assert.Equal(t, "Test Strategy", result.Name)
}
