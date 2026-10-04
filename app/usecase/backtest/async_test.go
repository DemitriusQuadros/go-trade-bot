package backtest_test

import (
	"context"
	"testing"
	"time"

	"go-trade-bot/app/entities"
	"go-trade-bot/app/usecase/backtest"
	"go-trade-bot/internal/metrics_provider"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newAsyncUseCase(t *testing.T, candles []entities.Candle, strat entities.Strategy) (*backtest.BacktestUseCase, *mockBacktestRepo) {
	t.Helper()
	repo := &mockBacktestRepo{}
	uc := backtest.NewBacktestUseCase(
		&mockCandleRepo{candles: candles},
		repo,
		&mockStrategyRepo{strategy: strat},
		&mockMetricsProvider{metrics: metrics_provider.BacktestMetrics{SharpeRatio: 1.2, MaxDrawdownPct: 5, TotalTrades: 3}},
		backtest.ThresholdPolicy{MinSharpe: 0.8, MaxDrawdownPct: 20},
		t.TempDir(), 20,
	)
	return uc, repo
}

func asyncStrategy() entities.Strategy {
	return entities.Strategy{ID: 1, Name: "Script", StrategyName: "script", ScriptSource: noTradeScript}
}

func asyncRequest(candles []entities.Candle) backtest.RunRequest {
	return backtest.RunRequest{
		StrategyID: 1,
		Symbol:     "BTCUSDT",
		Timeframe:  "1m",
		StartDate:  candles[0].OpenTime,
		EndDate:    candles[len(candles)-1].OpenTime.Add(time.Minute),
	}
}

func TestBacktestUseCase_Enqueue_StoresQueuedRunWithoutExecuting(t *testing.T) {
	candles := generateCandles(100)
	uc, repo := newAsyncUseCase(t, candles, asyncStrategy())

	run, err := uc.Enqueue(context.Background(), asyncRequest(candles))
	require.NoError(t, err)

	assert.NotZero(t, run.ID)
	assert.Equal(t, entities.BacktestQueued, run.Status)
	assert.Equal(t, "1m", run.Timeframe)
	assert.Equal(t, 1000.0, run.InitialCapital, "default capital is applied at enqueue time")
	require.Len(t, repo.runs, 1)
	assert.Zero(t, repo.runs[0].TotalTrades, "nothing has been simulated yet")
	assert.Nil(t, repo.runs[0].StartedAt)
}

func TestBacktestUseCase_Enqueue_RejectsBadInputLikeRun(t *testing.T) {
	candles := generateCandles(100)
	uc, repo := newAsyncUseCase(t, candles, asyncStrategy())

	req := asyncRequest(candles)
	req.StartDate = noCandleFrom
	req.EndDate = noCandleTo
	_, err := uc.Enqueue(context.Background(), req)
	require.Error(t, err, "a range with no candles must fail now, not after queueing")
	assert.Contains(t, err.Error(), "no BTCUSDT 1m candles")

	req = asyncRequest(candles)
	req.Symbol = ""
	_, err = uc.Enqueue(context.Background(), req)
	require.Error(t, err)

	assert.Empty(t, repo.runs, "rejected requests leave no queued row behind")
}

func TestBacktestUseCase_ExecuteQueued_FillsTheQueuedRow(t *testing.T) {
	candles := generateCandles(100)
	uc, repo := newAsyncUseCase(t, candles, asyncStrategy())

	queued, err := uc.Enqueue(context.Background(), asyncRequest(candles))
	require.NoError(t, err)

	require.NoError(t, uc.ExecuteQueued(context.Background(), queued.ID))

	require.Len(t, repo.runs, 1, "the queued row is updated in place, no second row")
	got := repo.runs[0]
	assert.Equal(t, queued.ID, got.ID)
	assert.Equal(t, entities.BacktestDone, got.Status)
	assert.True(t, got.Passed)
	assert.Equal(t, 3, got.TotalTrades)
	assert.Equal(t, queued.CreatedAt, got.CreatedAt)
	require.NotNil(t, got.StartedAt)
	require.NotNil(t, got.FinishedAt)
	assert.Empty(t, got.ErrorMessage)
}

func TestBacktestUseCase_ExecuteQueued_RecordsFailure(t *testing.T) {
	candles := generateCandles(100)
	strat := asyncStrategy()
	strat.StrategyName = "does-not-exist"
	uc, repo := newAsyncUseCase(t, candles, strat)

	queued, err := uc.Enqueue(context.Background(), asyncRequest(candles))
	require.NoError(t, err)

	err = uc.ExecuteQueued(context.Background(), queued.ID)
	require.Error(t, err)

	got := repo.runs[0]
	assert.Equal(t, entities.BacktestFailed, got.Status)
	assert.Contains(t, got.ErrorMessage, "not registered")
	require.NotNil(t, got.FinishedAt)
}

func TestBacktestUseCase_ExecuteQueued_SkipsRunsThatAreNotQueued(t *testing.T) {
	candles := generateCandles(100)
	uc, repo := newAsyncUseCase(t, candles, asyncStrategy())

	queued, err := uc.Enqueue(context.Background(), asyncRequest(candles))
	require.NoError(t, err)
	require.NoError(t, uc.ExecuteQueued(context.Background(), queued.ID))
	finishedAt := repo.runs[0].FinishedAt

	// A redelivered task for a finished run must be a no-op.
	require.NoError(t, uc.ExecuteQueued(context.Background(), queued.ID))
	assert.Equal(t, entities.BacktestDone, repo.runs[0].Status)
	assert.Equal(t, finishedAt, repo.runs[0].FinishedAt)
}

func TestBacktestUseCase_Run_PersistsDoneStatus(t *testing.T) {
	candles := generateCandles(100)
	uc, _ := newAsyncUseCase(t, candles, asyncStrategy())

	run, err := uc.Run(context.Background(), asyncRequest(candles))
	require.NoError(t, err)
	assert.Equal(t, entities.BacktestDone, run.Status)
	assert.Equal(t, "1m", run.Timeframe)
	assert.NotNil(t, run.FinishedAt)
}

func TestBacktestUseCase_ExecuteQueued_TimeoutIsRecordedAsFailedWithHint(t *testing.T) {
	candles := generateCandles(100)
	uc, repo := newAsyncUseCase(t, candles, asyncStrategy())
	queued, err := uc.Enqueue(context.Background(), asyncRequest(candles))
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	time.Sleep(2 * time.Millisecond)

	require.Error(t, uc.ExecuteQueued(ctx, queued.ID))
	got := repo.runs[0]
	assert.Equal(t, entities.BacktestFailed, got.Status)
	assert.Contains(t, got.ErrorMessage, "timed out")
	assert.Contains(t, got.ErrorMessage, "Settings")
}
