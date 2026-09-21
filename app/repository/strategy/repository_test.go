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

// TestStrategyRepository_Update_PreservesCreatedAt guards against a real
// regression: GORM's Save() on a struct with a non-zero primary key issues a
// full-column UPDATE, including zero-valued fields - since the PUT
// /strategy/{id} DTO never carries created_at, every edit was silently
// wiping the row's real creation date to 0001-01-01 before Update() started
// Omit-ing CreatedAt.
func TestStrategyRepository_Update_PreservesCreatedAt(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&entities.Strategy{}))

	repo := repository.NewStrategyRepository(db)

	saved, err := repo.Save(context.Background(), entities.Strategy{Name: "Original"})
	require.NoError(t, err)
	require.False(t, saved.CreatedAt.IsZero(), "sanity check: Save() must set CreatedAt on insert")
	originalCreatedAt := saved.CreatedAt

	// Mirrors what the PUT /strategy/{id} handler actually builds (DTO.ToModel()):
	// every field the DTO carries, but never CreatedAt.
	update := entities.Strategy{ID: saved.ID, Name: "Edited"}
	require.NoError(t, repo.Update(context.Background(), update))

	got, err := repo.GetByID(context.Background(), saved.ID)
	require.NoError(t, err)
	assert.Equal(t, "Edited", got.Name, "the actual edit must still persist")
	assert.WithinDuration(t, originalCreatedAt, got.CreatedAt, time.Second, "CreatedAt must survive an update untouched")
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

// TestStrategyRepository_Delete_CascadesEverything is the load-bearing test
// for this feature: deleting a strategy must actually remove every row
// that references it (signals, orders via signal_id, executions,
// backtests, optimization runs, performance snapshots, script
// state/versions, and AI agent chat history) - not leave any of them
// behind as orphaned rows, which is exactly what "keep the database small"
// required. Every referencing table gets at least one row planted first,
// so a table silently skipped by the cascade would leave a nonzero count
// and fail this test.
func TestStrategyRepository_Delete_CascadesEverything(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&entities.Strategy{},
		&entities.Signal{},
		&entities.Order{},
		&entities.StrategyExecution{},
		&entities.BacktestRun{},
		&entities.OptimizationRun{},
		&entities.StrategyPerformanceSnapshot{},
		&entities.ScriptState{},
		&entities.ScriptVersion{},
		&entities.AgentRun{},
	))

	repo := repository.NewStrategyRepository(db)
	ctx := context.Background()

	strat, err := repo.Save(ctx, entities.Strategy{Name: "To Delete", StrategyName: "script"})
	require.NoError(t, err)
	otherStrat, err := repo.Save(ctx, entities.Strategy{Name: "Untouched", StrategyName: "script"})
	require.NoError(t, err)

	signal := entities.Signal{StrategyID: strat.ID, Symbol: "BTCUSDT", Status: entities.Open}
	require.NoError(t, db.Create(&signal).Error)
	require.NoError(t, db.Create(&entities.Order{SignalID: signal.ID}).Error)
	require.NoError(t, db.Create(&entities.StrategyExecution{StrategyID: strat.ID}).Error)
	require.NoError(t, db.Create(&entities.BacktestRun{StrategyID: strat.ID}).Error)
	require.NoError(t, db.Create(&entities.OptimizationRun{StrategyID: strat.ID}).Error)
	require.NoError(t, db.Create(&entities.StrategyPerformanceSnapshot{StrategyID: strat.ID}).Error)
	require.NoError(t, db.Create(&entities.ScriptState{StrategyID: strat.ID}).Error)
	require.NoError(t, db.Create(&entities.ScriptVersion{StrategyID: strat.ID}).Error)
	require.NoError(t, db.Create(&entities.AgentRun{StrategyID: &strat.ID}).Error)

	// Same shapes for the OTHER strategy - must survive the delete untouched,
	// so the cascade is proven scoped to the deleted strategy's own rows,
	// not "delete every row in every table."
	otherSignal := entities.Signal{StrategyID: otherStrat.ID, Symbol: "ETHUSDT", Status: entities.Open}
	require.NoError(t, db.Create(&otherSignal).Error)
	require.NoError(t, db.Create(&entities.Order{SignalID: otherSignal.ID}).Error)
	require.NoError(t, db.Create(&entities.AgentRun{StrategyID: &otherStrat.ID}).Error)

	require.NoError(t, repo.Delete(ctx, strat.ID))

	assertZeroRowsFor := func(model any, condition string, args ...any) {
		t.Helper()
		var count int64
		require.NoError(t, db.Model(model).Where(condition, args...).Count(&count).Error)
		assert.Zero(t, count, "expected zero rows for %T where %s", model, condition)
	}

	_, err = repo.GetByID(ctx, strat.ID)
	assert.Error(t, err, "the strategy row itself must be gone")

	assertZeroRowsFor(&entities.Signal{}, "strategy_id = ?", strat.ID)
	assertZeroRowsFor(&entities.Order{}, "signal_id = ?", signal.ID)
	assertZeroRowsFor(&entities.StrategyExecution{}, "strategy_id = ?", strat.ID)
	assertZeroRowsFor(&entities.BacktestRun{}, "strategy_id = ?", strat.ID)
	assertZeroRowsFor(&entities.OptimizationRun{}, "strategy_id = ?", strat.ID)
	assertZeroRowsFor(&entities.StrategyPerformanceSnapshot{}, "strategy_id = ?", strat.ID)
	assertZeroRowsFor(&entities.ScriptState{}, "strategy_id = ?", strat.ID)
	assertZeroRowsFor(&entities.ScriptVersion{}, "strategy_id = ?", strat.ID)
	assertZeroRowsFor(&entities.AgentRun{}, "strategy_id = ?", strat.ID)

	// The other strategy and everything under it must be untouched.
	_, err = repo.GetByID(ctx, otherStrat.ID)
	require.NoError(t, err)
	var otherSignalCount, otherOrderCount, otherAgentRunCount int64
	require.NoError(t, db.Model(&entities.Signal{}).Where("strategy_id = ?", otherStrat.ID).Count(&otherSignalCount).Error)
	require.NoError(t, db.Model(&entities.Order{}).Where("signal_id = ?", otherSignal.ID).Count(&otherOrderCount).Error)
	require.NoError(t, db.Model(&entities.AgentRun{}).Where("strategy_id = ?", otherStrat.ID).Count(&otherAgentRunCount).Error)
	assert.Equal(t, int64(1), otherSignalCount)
	assert.Equal(t, int64(1), otherOrderCount)
	assert.Equal(t, int64(1), otherAgentRunCount)
}
