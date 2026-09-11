package backtest_test

import (
	"context"
	"testing"
	"time"

	"go-trade-bot/app/entities"
	"go-trade-bot/app/repository/backtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&entities.BacktestRun{}))
	return db
}

func TestBacktestRepository_CreateAndGetByID(t *testing.T) {
	db := setupTestDB(t)
	repo := backtest.NewBacktestRepository(db)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	run := &entities.BacktestRun{
		StrategyID:     1,
		Symbol:         "BTCUSDT",
		StartDate:      now.Add(-24 * time.Hour),
		EndDate:        now,
		IsWalkForward:  false,
		Sharpe:         1.5,
		MaxDrawdownPct: 12.3,
		WinRatePct:     65.0,
		ProfitFactor:   2.1,
		TotalTrades:    10,
		TotalReturnPct: 5.4,
		Passed:         true,
		HTMLReportPath: "/tmp/reports/run1.html",
		TradeLogJSON:   datatypes.JSON(`[{"symbol":"BTCUSDT","profit":100}]`),
		CreatedAt:      now,
	}

	err := repo.Create(ctx, run)
	require.NoError(t, err)
	assert.NotZero(t, run.ID)

	fetched, err := repo.GetByID(ctx, run.ID)
	require.NoError(t, err)
	assert.Equal(t, run.ID, fetched.ID)
	assert.Equal(t, 1, int(fetched.StrategyID))
	assert.Equal(t, "BTCUSDT", fetched.Symbol)
	assert.InDelta(t, 1.5, fetched.Sharpe, 0.001)
	assert.True(t, fetched.Passed)
	assert.Equal(t, "/tmp/reports/run1.html", fetched.HTMLReportPath)
}

func TestBacktestRepository_ListByStrategy_And_RetentionQuery(t *testing.T) {
	db := setupTestDB(t)
	repo := backtest.NewBacktestRepository(db)
	ctx := context.Background()

	baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 1; i <= 5; i++ {
		reportPath := ""
		if i%2 == 0 {
			reportPath = "/tmp/reports/run.html"
		}
		err := repo.Create(ctx, &entities.BacktestRun{
			StrategyID:     10,
			Symbol:         "BTCUSDT",
			HTMLReportPath: reportPath,
			CreatedAt:      baseTime.Add(time.Duration(i) * time.Hour),
		})
		require.NoError(t, err)
	}

	// Create run for different strategy
	err := repo.Create(ctx, &entities.BacktestRun{
		StrategyID: 20,
		Symbol:     "ETHUSDT",
		CreatedAt:  baseTime,
	})
	require.NoError(t, err)

	list10, err := repo.ListByStrategy(ctx, 10)
	require.NoError(t, err)
	assert.Len(t, list10, 5)
	// should be ordered by created_at desc
	assert.True(t, list10[0].CreatedAt.After(list10[4].CreatedAt))

	withReport, err := repo.ListRunsWithReport(ctx, 10)
	require.NoError(t, err)
	assert.Len(t, withReport, 2)
	// should be ordered by created_at asc
	assert.True(t, withReport[0].CreatedAt.Before(withReport[1].CreatedAt))

	// Update first to clear report
	withReport[0].HTMLReportPath = ""
	err = repo.Update(ctx, withReport[0])
	require.NoError(t, err)

	updatedWithReport, err := repo.ListRunsWithReport(ctx, 10)
	require.NoError(t, err)
	assert.Len(t, updatedWithReport, 1)
}
