package optimize_test

import (
	"context"
	"testing"
	"time"

	"go-trade-bot/app/entities"
	"go-trade-bot/app/repository/optimize"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)

	err = db.AutoMigrate(&entities.OptimizationRun{})
	require.NoError(t, err)

	return db
}

func TestOptimizationRepository_CreateAndGetByID(t *testing.T) {
	db := setupTestDB(t)
	repo := optimize.NewOptimizationRepository(db)
	ctx := context.Background()

	run := &entities.OptimizationRun{
		StrategyID:        1,
		Symbol:            "BTCUSDT",
		Timeframe:         "1m",
		StartDate:         time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:           time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		InitialCapital:    1000,
		Status:            entities.OptimizationPending,
		TotalCombinations: 55,
	}

	require.NoError(t, repo.Create(ctx, run))
	assert.NotZero(t, run.ID)

	fetched, err := repo.GetByID(ctx, run.ID)
	require.NoError(t, err)
	assert.Equal(t, entities.OptimizationPending, fetched.Status)
	assert.Equal(t, 55, fetched.TotalCombinations)
}

func TestOptimizationRepository_Update(t *testing.T) {
	db := setupTestDB(t)
	repo := optimize.NewOptimizationRepository(db)
	ctx := context.Background()

	run := &entities.OptimizationRun{StrategyID: 1, Symbol: "BTCUSDT", Status: entities.OptimizationPending}
	require.NoError(t, repo.Create(ctx, run))

	run.Status = entities.OptimizationRunning
	run.Progress = 10
	require.NoError(t, repo.Update(ctx, *run))

	fetched, err := repo.GetByID(ctx, run.ID)
	require.NoError(t, err)
	assert.Equal(t, entities.OptimizationRunning, fetched.Status)
	assert.Equal(t, 10, fetched.Progress)
}

func TestOptimizationRepository_ListByStrategy(t *testing.T) {
	db := setupTestDB(t)
	repo := optimize.NewOptimizationRepository(db)
	ctx := context.Background()

	require.NoError(t, repo.Create(ctx, &entities.OptimizationRun{StrategyID: 1, Symbol: "BTCUSDT"}))
	require.NoError(t, repo.Create(ctx, &entities.OptimizationRun{StrategyID: 1, Symbol: "ETHUSDT"}))
	require.NoError(t, repo.Create(ctx, &entities.OptimizationRun{StrategyID: 2, Symbol: "BTCUSDT"}))

	runs, err := repo.ListByStrategy(ctx, 1)
	require.NoError(t, err)
	assert.Len(t, runs, 2)
}

func TestOptimizationRepository_GetByID_NotFound(t *testing.T) {
	db := setupTestDB(t)
	repo := optimize.NewOptimizationRepository(db)
	ctx := context.Background()

	_, err := repo.GetByID(ctx, 999)
	assert.Error(t, err)
}
