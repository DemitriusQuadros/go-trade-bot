package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"go-trade-bot/app/entities"
	agentusecase "go-trade-bot/app/usecase/agent"
	backtestusecase "go-trade-bot/app/usecase/backtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type mockBacktestQueue struct{ mock.Mock }

func (m *mockBacktestQueue) Enqueue(ctx context.Context, req backtestusecase.RunRequest) (entities.BacktestRun, error) {
	args := m.Called(ctx, req)
	return args.Get(0).(entities.BacktestRun), args.Error(1)
}

func (m *mockBacktestQueue) MarkEnqueueFailed(ctx context.Context, runID uint, cause error) error {
	return m.Called(ctx, runID, cause).Error(0)
}

type mockBacktestWorker struct{ mock.Mock }

func (m *mockBacktestWorker) EnqueueBacktestTask(runID uint) error {
	return m.Called(runID).Error(0)
}

const runBacktestArgs = `{"strategy_id":1,"symbol":"SOLUSDT","timeframe":"15m","start_date":"2026-01-01T00:00:00Z","end_date":"2026-07-01T00:00:00Z"}`

func newBacktestAsyncAgentUseCase() (*agentusecase.AgentUseCase, *mockBacktestUseCase, *mockBacktestQueue, *mockBacktestWorker) {
	backtestUC := &mockBacktestUseCase{}
	uc := agentusecase.NewAgentUseCase(&mockModelProvider{}, &mockAgentRepository{}, &mockStrategyUseCase{}, backtestUC, &mockSignalUseCase{}, &mockSnapshotUseCase{})
	queue := &mockBacktestQueue{}
	worker := &mockBacktestWorker{}
	uc.BacktestQueue = queue
	uc.BacktestWorker = worker
	return uc, backtestUC, queue, worker
}

func TestRunBacktestTool_Async_QueuesAndReturnsImmediately(t *testing.T) {
	uc, backtestUC, queue, worker := newBacktestAsyncAgentUseCase()
	tool := findTool(t, uc, "run_backtest")

	queue.On("Enqueue", mock.Anything, mock.MatchedBy(func(req backtestusecase.RunRequest) bool {
		return req.StrategyID == 1 && req.Symbol == "SOLUSDT" && req.Timeframe == "15m"
	})).Return(entities.BacktestRun{ID: 77, Status: entities.BacktestQueued}, nil)
	worker.On("EnqueueBacktestTask", uint(77)).Return(nil)

	out, err := tool.Execute(context.Background(), json.RawMessage(runBacktestArgs))
	require.NoError(t, err)
	assert.Contains(t, out, "backtest_id=77")
	assert.Contains(t, out, "status=queued")
	assert.Contains(t, out, "get_backtest")
	assert.Contains(t, tool.Def.Description, "asynchronously")
	backtestUC.AssertNotCalled(t, "Run", mock.Anything, mock.Anything)
	queue.AssertExpectations(t)
	worker.AssertExpectations(t)
}

func TestRunBacktestTool_Async_ValidationErrorsReachTheModel(t *testing.T) {
	uc, _, queue, worker := newBacktestAsyncAgentUseCase()
	tool := findTool(t, uc, "run_backtest")

	queue.On("Enqueue", mock.Anything, mock.Anything).Return(entities.BacktestRun{}, errors.New("no SOLUSDT 15m candles in range"))

	_, err := tool.Execute(context.Background(), json.RawMessage(runBacktestArgs))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no SOLUSDT 15m candles")
	worker.AssertNotCalled(t, "EnqueueBacktestTask", mock.Anything)
}

func TestRunBacktestTool_Async_EnqueueFailureMarksRunFailed(t *testing.T) {
	uc, _, queue, worker := newBacktestAsyncAgentUseCase()
	tool := findTool(t, uc, "run_backtest")

	queue.On("Enqueue", mock.Anything, mock.Anything).Return(entities.BacktestRun{ID: 5, Status: entities.BacktestQueued}, nil)
	worker.On("EnqueueBacktestTask", uint(5)).Return(errors.New("redis down"))
	queue.On("MarkEnqueueFailed", mock.Anything, uint(5), mock.Anything).Return(nil)

	_, err := tool.Execute(context.Background(), json.RawMessage(runBacktestArgs))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to enqueue")
	queue.AssertCalled(t, "MarkEnqueueFailed", mock.Anything, uint(5), mock.Anything)
}

func TestRunBacktestTool_StaysSynchronousWhenQueueNotWired(t *testing.T) {
	backtestUC := &mockBacktestUseCase{}
	uc := agentusecase.NewAgentUseCase(&mockModelProvider{}, &mockAgentRepository{}, &mockStrategyUseCase{}, backtestUC, &mockSignalUseCase{}, &mockSnapshotUseCase{})
	tool := findTool(t, uc, "run_backtest")

	backtestUC.On("Run", mock.Anything, mock.Anything).Return(entities.BacktestRun{ID: 9, Status: entities.BacktestDone, Symbol: "SOLUSDT"}, nil)

	out, err := tool.Execute(context.Background(), json.RawMessage(runBacktestArgs))
	require.NoError(t, err)
	assert.Contains(t, out, "backtest_id=9")
	assert.Contains(t, out, "total_trades=")
	assert.Contains(t, tool.Def.Description, "Blocks")
}

func TestGetBacktestTool_ReportsStatusUntilDone(t *testing.T) {
	uc, backtestUC, _, _ := newBacktestAsyncAgentUseCase()
	tool := findTool(t, uc, "get_backtest")

	backtestUC.On("GetByID", mock.Anything, uint(1)).Return(entities.BacktestRun{ID: 1, StrategyID: 3, Symbol: "SOLUSDT", Status: entities.BacktestRunning}, nil)
	backtestUC.On("GetByID", mock.Anything, uint(2)).Return(entities.BacktestRun{ID: 2, StrategyID: 3, Symbol: "SOLUSDT", Status: entities.BacktestFailed, ErrorMessage: "backtest execution failed: boom"}, nil)
	backtestUC.On("GetByID", mock.Anything, uint(3)).Return(entities.BacktestRun{ID: 3, StrategyID: 3, Symbol: "SOLUSDT", Status: entities.BacktestDone, TotalTrades: 12}, nil)

	out, err := tool.Execute(context.Background(), json.RawMessage(`{"backtest_id":1}`))
	require.NoError(t, err)
	assert.Contains(t, out, "status=running")
	assert.NotContains(t, out, "total_trades")

	out, err = tool.Execute(context.Background(), json.RawMessage(`{"backtest_id":2}`))
	require.NoError(t, err)
	assert.Contains(t, out, "status=failed")
	assert.Contains(t, out, "boom")

	out, err = tool.Execute(context.Background(), json.RawMessage(`{"backtest_id":3}`))
	require.NoError(t, err)
	assert.Contains(t, out, "total_trades=12")
}
