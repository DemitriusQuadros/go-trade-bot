package performancehistory_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"go-trade-bot/app/entities"
	usecase "go-trade-bot/app/usecase/performancehistory"
	"go-trade-bot/app/usecase/performancehistory/mocks"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// TestSnapshot_PersistsOneRowPerStrategySymbol covers backend-05 AC#1.
func TestSnapshot_PersistsOneRowPerStrategySymbol(t *testing.T) {
	stratRepo := mocks.NewStrategyRepository(t)
	snapRepo := mocks.NewSnapshotRepository(t)

	periodStart := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := periodStart.AddDate(0, 0, 1)

	stratRepo.On("GetPerformanceInRange", mock.Anything, periodStart, periodEnd).Return([]entities.StrategyPerformance{
		{StrategyID: 1, Symbol: "BTCUSDT", Profit: 45, Trades: 3},
	}, nil)

	snapRepo.On("Upsert", mock.Anything, mock.MatchedBy(func(s entities.StrategyPerformanceSnapshot) bool {
		return s.StrategyID == 1 && s.Symbol == "BTCUSDT" && s.Bucket == entities.BucketDaily &&
			s.PeriodStart.Equal(periodStart) && s.Profit == 45 && s.Trades == 3
	})).Return(nil)

	u := usecase.NewPerformanceHistoryUseCase(stratRepo, snapRepo)
	require.NoError(t, u.Snapshot(context.Background(), entities.BucketDaily, periodStart, periodEnd))
}

// TestSnapshot_NoActivity_NoRowCreated covers AC#5: zero closed orders in the
// window must not produce a row at all.
func TestSnapshot_NoActivity_NoRowCreated(t *testing.T) {
	stratRepo := mocks.NewStrategyRepository(t)
	snapRepo := mocks.NewSnapshotRepository(t)

	periodStart := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := periodStart.AddDate(0, 0, 1)

	stratRepo.On("GetPerformanceInRange", mock.Anything, periodStart, periodEnd).Return([]entities.StrategyPerformance{}, nil)

	u := usecase.NewPerformanceHistoryUseCase(stratRepo, snapRepo)
	require.NoError(t, u.Snapshot(context.Background(), entities.BucketDaily, periodStart, periodEnd))

	snapRepo.AssertNotCalled(t, "Upsert", mock.Anything, mock.Anything)
}

// TestSnapshot_MultipleSymbols_IndependentRows covers AC#6.
func TestSnapshot_MultipleSymbols_IndependentRows(t *testing.T) {
	stratRepo := mocks.NewStrategyRepository(t)
	snapRepo := mocks.NewSnapshotRepository(t)

	periodStart := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := periodStart.AddDate(0, 0, 1)

	stratRepo.On("GetPerformanceInRange", mock.Anything, periodStart, periodEnd).Return([]entities.StrategyPerformance{
		{StrategyID: 1, Symbol: "BTCUSDT", Profit: 10, Trades: 1},
		{StrategyID: 1, Symbol: "ETHUSDT", Profit: 20, Trades: 2},
	}, nil)

	snapRepo.On("Upsert", mock.Anything, mock.Anything).Return(nil).Twice()

	u := usecase.NewPerformanceHistoryUseCase(stratRepo, snapRepo)
	require.NoError(t, u.Snapshot(context.Background(), entities.BucketDaily, periodStart, periodEnd))
}

// TestSnapshot_Backfill_ArbitraryHistoricalWindow covers AC#7: Snapshot is
// parameterized purely by the window it's told to compute, so a manually
// triggered backfill for a missed day works identically to the cron case.
func TestSnapshot_Backfill_ArbitraryHistoricalWindow(t *testing.T) {
	stratRepo := mocks.NewStrategyRepository(t)
	snapRepo := mocks.NewSnapshotRepository(t)

	missedDay := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)
	nextDay := missedDay.AddDate(0, 0, 1)

	stratRepo.On("GetPerformanceInRange", mock.Anything, missedDay, nextDay).Return([]entities.StrategyPerformance{
		{StrategyID: 2, Symbol: "BTCUSDT", Profit: 5, Trades: 1},
	}, nil)
	snapRepo.On("Upsert", mock.Anything, mock.Anything).Return(nil)

	u := usecase.NewPerformanceHistoryUseCase(stratRepo, snapRepo)
	require.NoError(t, u.Snapshot(context.Background(), entities.BucketDaily, missedDay, nextDay))
}

// TestGetHistory_StrategyNotFound covers the 404 case.
func TestGetHistory_StrategyNotFound(t *testing.T) {
	stratRepo := mocks.NewStrategyRepository(t)
	snapRepo := mocks.NewSnapshotRepository(t)

	stratRepo.On("GetByID", mock.Anything, uint(999)).Return(entities.Strategy{}, errors.New("not found"))

	u := usecase.NewPerformanceHistoryUseCase(stratRepo, snapRepo)
	_, err := u.GetHistory(context.Background(), 999, "BTCUSDT", entities.BucketDaily, 30)
	assert.ErrorIs(t, err, usecase.ErrStrategyNotFound)
}

// TestGetHistory_Daily_ShorterSeriesNotZeroPadded covers AC#4.
func TestGetHistory_Daily_ShorterSeriesNotZeroPadded(t *testing.T) {
	stratRepo := mocks.NewStrategyRepository(t)
	snapRepo := mocks.NewSnapshotRepository(t)

	stratRepo.On("GetByID", mock.Anything, uint(5)).Return(entities.Strategy{ID: 5}, nil)

	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	rows := make([]entities.StrategyPerformanceSnapshot, 10)
	for i := range rows {
		day := base.AddDate(0, 0, i)
		rows[i] = entities.StrategyPerformanceSnapshot{
			StrategyID: 5, Symbol: "BTCUSDT", Bucket: entities.BucketDaily,
			PeriodStart: day, PeriodEnd: day.AddDate(0, 0, 1), Profit: float64(i), Trades: 1,
		}
	}
	snapRepo.On("ListDaily", mock.Anything, uint(5), "BTCUSDT", mock.Anything, mock.Anything).Return(rows, nil)

	u := usecase.NewPerformanceHistoryUseCase(stratRepo, snapRepo)
	history, err := u.GetHistory(context.Background(), 5, "BTCUSDT", entities.BucketDaily, 30)
	require.NoError(t, err)
	assert.Len(t, history, 10, "fewer than `limit` days of history should return however many exist")
}

// TestGetHistory_Weekly_AggregatesDailyRows covers AC#3.
func TestGetHistory_Weekly_AggregatesDailyRows(t *testing.T) {
	stratRepo := mocks.NewStrategyRepository(t)
	snapRepo := mocks.NewSnapshotRepository(t)

	stratRepo.On("GetByID", mock.Anything, uint(5)).Return(entities.Strategy{ID: 5}, nil)

	// A Monday-starting week: 2026-08-31 (Mon) .. 2026-09-06 (Sun).
	monday := time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC)
	profits := []float64{10, -5, 20, 0, 15, -10, 5} // sums to 35
	rows := make([]entities.StrategyPerformanceSnapshot, len(profits))
	for i, p := range profits {
		day := monday.AddDate(0, 0, i)
		rows[i] = entities.StrategyPerformanceSnapshot{
			StrategyID: 5, Symbol: "BTCUSDT", Bucket: entities.BucketDaily,
			PeriodStart: day, PeriodEnd: day.AddDate(0, 0, 1), Profit: p, Trades: 1,
		}
	}
	snapRepo.On("ListDaily", mock.Anything, uint(5), "BTCUSDT", mock.Anything, mock.Anything).Return(rows, nil)

	u := usecase.NewPerformanceHistoryUseCase(stratRepo, snapRepo)
	history, err := u.GetHistory(context.Background(), 5, "BTCUSDT", entities.BucketWeekly, 5)
	require.NoError(t, err)
	require.Len(t, history, 1)
	assert.Equal(t, 35.0, history[0].Profit)
	assert.Equal(t, 7, history[0].Trades)
	assert.True(t, history[0].PeriodStart.Equal(monday))
	assert.True(t, history[0].PeriodEnd.Equal(monday.AddDate(0, 0, 7)))
}

func TestGetHistory_InvalidBucket_ReturnsError(t *testing.T) {
	stratRepo := mocks.NewStrategyRepository(t)
	snapRepo := mocks.NewSnapshotRepository(t)
	stratRepo.On("GetByID", mock.Anything, uint(5)).Return(entities.Strategy{ID: 5}, nil)

	u := usecase.NewPerformanceHistoryUseCase(stratRepo, snapRepo)
	_, err := u.GetHistory(context.Background(), 5, "BTCUSDT", entities.PerformanceBucket("yearly"), 30)
	assert.Error(t, err)
}
