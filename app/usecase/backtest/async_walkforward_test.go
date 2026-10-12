package backtest_test

import (
	"context"
	"testing"
	"time"

	"go-trade-bot/app/entities"
	"go-trade-bot/app/usecase/backtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func wfAsyncRequest(strategyID uint, start time.Time) backtest.WalkForwardRequest {
	return backtest.WalkForwardRequest{
		StrategyID: strategyID, Symbol: "BTCUSDT", Timeframe: "1m",
		StartDate: start, EndDate: start.Add(121 * 24 * time.Hour), TrainMonths: 3, TestMonths: 1,
	}
}

func TestEnqueueWalkForward_StoresQueuedRowWithoutExecuting(t *testing.T) {
	uc, repo, saved, start := walkForwardFixture(t)

	run, err := uc.EnqueueWalkForward(context.Background(), wfAsyncRequest(saved.ID, start))
	require.NoError(t, err)

	assert.Equal(t, entities.BacktestQueued, run.Status)
	assert.True(t, run.IsWalkForward)
	assert.NotEmpty(t, run.WalkForwardParamsJSON, "the worker needs the parameters")
	require.Len(t, repo.runs, 1)
	assert.Zero(t, repo.runs[0].TotalTrades, "nothing replayed yet")
	assert.Nil(t, repo.runs[0].StartedAt)
}

func TestEnqueueWalkForward_RejectsBadInputBeforeQueueing(t *testing.T) {
	uc, repo, saved, start := walkForwardFixture(t)

	req := wfAsyncRequest(saved.ID, start)
	req.Symbol = ""
	_, err := uc.EnqueueWalkForward(context.Background(), req)
	require.Error(t, err)

	req = wfAsyncRequest(0, start)
	_, err = uc.EnqueueWalkForward(context.Background(), req)
	require.Error(t, err)

	req = wfAsyncRequest(saved.ID, noCandleFrom)
	req.EndDate = noCandleTo
	_, err = uc.EnqueueWalkForward(context.Background(), req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no BTCUSDT 1m candles")

	assert.Empty(t, repo.runs)
}

func TestExecuteQueued_WalkForward_FillsTheSameRow(t *testing.T) {
	uc, repo, saved, start := walkForwardFixture(t)
	queued, err := uc.EnqueueWalkForward(context.Background(), wfAsyncRequest(saved.ID, start))
	require.NoError(t, err)

	require.NoError(t, uc.ExecuteQueued(context.Background(), queued.ID))

	require.Len(t, repo.runs, 1, "no second row is created")
	got := repo.runs[0]
	assert.Equal(t, queued.ID, got.ID)
	assert.Equal(t, entities.BacktestDone, got.Status)
	assert.True(t, got.IsWalkForward)
	assert.NotEmpty(t, got.TradeLogJSON, "aggregate OOS payload is stored")
	assert.NotEmpty(t, got.MetricsJSON)
	assert.Equal(t, queued.CreatedAt, got.CreatedAt)
	require.NotNil(t, got.StartedAt)
	require.NotNil(t, got.FinishedAt)
	assert.Empty(t, got.ErrorMessage)
}

func TestExecuteQueued_WalkForward_RecordsFailureOnBadStoredParams(t *testing.T) {
	uc, repo, saved, start := walkForwardFixture(t)
	queued, err := uc.EnqueueWalkForward(context.Background(), wfAsyncRequest(saved.ID, start))
	require.NoError(t, err)
	repo.runs[0].WalkForwardParamsJSON = []byte(`{not json`)

	require.Error(t, uc.ExecuteQueued(context.Background(), queued.ID))

	assert.Equal(t, entities.BacktestFailed, repo.runs[0].Status)
	assert.Contains(t, repo.runs[0].ErrorMessage, "invalid stored walk-forward parameters")
}
