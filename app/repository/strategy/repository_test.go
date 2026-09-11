package repository_test

import (
	"context"
	"testing"
	"time"

	"go-trade-bot/app/entities"
	repository "go-trade-bot/app/repository/strategy"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	saved, err := repo.Save(context.Background(), strategy)
	assert.NoError(t, err)
	assert.NotZero(t, saved.ID)

	var result entities.Strategy
	err = db.First(&result, "name = ?", "Test Strategy").Error
	assert.NoError(t, err)
	assert.Equal(t, "Test Strategy", result.Name)
}

func setupPerformanceDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&entities.Strategy{}, &entities.Signal{}, &entities.Order{}))
	return db
}

// TestStrategyRepository_GetPerformanceInRange covers backend-05's data
// source for the daily snapshot job: only orders CLOSED (UpdatedAt) within
// [from, to) contribute, matching AC#1/AC#7 (window-parameterized, not
// "since last run").
func TestStrategyRepository_GetPerformanceInRange(t *testing.T) {
	db := setupPerformanceDB(t)
	repo := repository.NewStrategyRepository(db)
	ctx := context.Background()

	strat := entities.Strategy{Name: "Bollinger BTC"}
	require.NoError(t, db.Create(&strat).Error)

	sig := entities.Signal{StrategyID: strat.ID, Symbol: "BTCUSDT", Status: entities.Closed}
	require.NoError(t, db.Create(&sig).Error)

	inRange := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	outOfRange := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)

	order1 := entities.Order{SignalID: sig.ID, Profit: 20, EntryPrice: 1, ExitPrice: 1, Quantity: 1, InvestedAmount: 1}
	require.NoError(t, db.Create(&order1).Error)
	require.NoError(t, db.Model(&order1).UpdateColumn("updated_at", inRange).Error)

	order2 := entities.Order{SignalID: sig.ID, Profit: 25, EntryPrice: 1, ExitPrice: 1, Quantity: 1, InvestedAmount: 1}
	require.NoError(t, db.Create(&order2).Error)
	require.NoError(t, db.Model(&order2).UpdateColumn("updated_at", inRange).Error)

	// This order closed on a different day - must not be included.
	order3 := entities.Order{SignalID: sig.ID, Profit: 999, EntryPrice: 1, ExitPrice: 1, Quantity: 1, InvestedAmount: 1}
	require.NoError(t, db.Create(&order3).Error)
	require.NoError(t, db.Model(&order3).UpdateColumn("updated_at", outOfRange).Error)

	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)

	perf, err := repo.GetPerformanceInRange(ctx, from, to)
	require.NoError(t, err)
	require.Len(t, perf, 1)
	assert.Equal(t, strat.ID, perf[0].StrategyID)
	assert.Equal(t, "BTCUSDT", perf[0].Symbol)
	assert.Equal(t, 45.0, perf[0].Profit)
	assert.Equal(t, 2, perf[0].Trades)
}

func TestStrategyRepository_GetPerformanceInRange_NoOrdersInWindow(t *testing.T) {
	db := setupPerformanceDB(t)
	repo := repository.NewStrategyRepository(db)
	ctx := context.Background()

	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)

	perf, err := repo.GetPerformanceInRange(ctx, from, to)
	require.NoError(t, err)
	assert.Empty(t, perf)
}
