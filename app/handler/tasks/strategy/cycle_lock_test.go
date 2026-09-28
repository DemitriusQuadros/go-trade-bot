package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"go-trade-bot/app/entities"
	handler "go-trade-bot/app/handler/tasks/strategy"
	"go-trade-bot/app/handler/tasks/strategy/mocks"
	"go-trade-bot/app/strategies"
	signalmocks "go-trade-bot/app/usecase/signal/mocks"
	"go-trade-bot/internal/lock"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// errLock fails every Acquire (Redis down).
type errLock struct{}

func (errLock) Acquire(context.Context, uint, string, time.Duration) (bool, error) {
	return false, errors.New("redis down")
}
func (errLock) Release(context.Context, uint, string) error { return nil }

func cycleTask(id uint) *asynq.Task {
	payload, _ := json.Marshal(entities.Strategy{ID: id, Name: "S"})
	return asynq.NewTask(handler.StrategyTask+"S", payload)
}

// B-01 §5: while an apply holds strategy-cycle:<id>, the worker does not
// run the cycle (no engine run, no execution row) and re-schedules it.
func TestHandleStrategyTask_CycleLockHeld_HoldsCycle(t *testing.T) {
	repo := new(mocks.StrategyRepository)
	worker := new(mocks.StrategyWorker)
	eng := new(mocks.Engine)
	worker.On("EnqueueStrategyTaskWithDelay", mock.Anything, mock.Anything).Return(nil)

	l := lock.NewMemoryStrategyLock()
	ok, err := l.Acquire(context.Background(), 21, "apply-proposal:1", time.Minute)
	require.NoError(t, err)
	require.True(t, ok)

	p := handler.NewStrategyProcessor(nil, worker, repo, eng, eng, new(signalmocks.NotificationSender), strategies.ModeDryRun, true)
	p.SetCycleLock(l)
	require.NoError(t, p.HandleStrategyTask(context.Background(), cycleTask(21)))

	worker.AssertCalled(t, "EnqueueStrategyTaskWithDelay", mock.MatchedBy(func(s entities.Strategy) bool { return s.ID == 21 }), mock.Anything)
	repo.AssertNotCalled(t, "GetByID", mock.Anything, mock.Anything)
	eng.AssertNotCalled(t, "Run", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "SaveExecution", mock.Anything, mock.Anything)
}

// The lock is held for the whole cycle (an apply cannot take it while the
// engine runs) and released afterwards.
func TestHandleStrategyTask_CycleLockHeldDuringEngineRunAndReleased(t *testing.T) {
	repo := new(mocks.StrategyRepository)
	worker := new(mocks.StrategyWorker)
	eng := new(mocks.Engine)
	l := lock.NewMemoryStrategyLock()

	dbStrategy := entities.Strategy{ID: 22, Name: "S", StrategyName: "registered-test-strategy", Status: entities.Testing, Mode: "dryrun", MonitoredSymbols: []string{"BTCUSDT"}}
	repo.On("GetByID", mock.Anything, uint(22)).Return(dbStrategy, nil)
	repo.On("SaveExecution", mock.Anything, mock.Anything).Return(nil)
	worker.On("EnqueueStrategyTask", mock.Anything).Return(nil)

	var applyGotLock *bool
	var mu sync.Mutex
	eng.On("Run", mock.Anything, mock.Anything, mock.Anything, "BTCUSDT", strategies.ModeDryRun).Run(func(mock.Arguments) {
		ok, _ := l.Acquire(context.Background(), 22, "apply-proposal:9", time.Minute)
		mu.Lock()
		applyGotLock = &ok
		mu.Unlock()
	}).Return(nil)

	p := handler.NewStrategyProcessor(nil, worker, repo, eng, eng, new(signalmocks.NotificationSender), strategies.ModeDryRun, true)
	p.SetCycleLock(l)
	require.NoError(t, p.HandleStrategyTask(context.Background(), cycleTask(22)))

	require.NotNil(t, applyGotLock)
	assert.False(t, *applyGotLock, "an apply must not get the lock while the cycle runs")
	ok, err := l.Acquire(context.Background(), 22, "apply-proposal:9", time.Minute)
	require.NoError(t, err)
	assert.True(t, ok, "the worker releases the lock after the cycle")
	eng.AssertNumberOfCalls(t, "Run", 1)
}

// A lock infrastructure error fails open: the cycle runs exactly as it
// did before Phase B (the apply side fails closed instead).
func TestHandleStrategyTask_CycleLockError_RunsCycle(t *testing.T) {
	repo := new(mocks.StrategyRepository)
	worker := new(mocks.StrategyWorker)
	eng := new(mocks.Engine)
	dbStrategy := entities.Strategy{ID: 23, Name: "S", StrategyName: "registered-test-strategy", Status: entities.Testing, Mode: "dryrun", MonitoredSymbols: []string{"BTCUSDT"}}
	repo.On("GetByID", mock.Anything, uint(23)).Return(dbStrategy, nil)
	repo.On("SaveExecution", mock.Anything, mock.Anything).Return(nil)
	worker.On("EnqueueStrategyTask", mock.Anything).Return(nil)
	eng.On("Run", mock.Anything, mock.Anything, mock.Anything, "BTCUSDT", strategies.ModeDryRun).Return(nil)

	p := handler.NewStrategyProcessor(nil, worker, repo, eng, eng, new(signalmocks.NotificationSender), strategies.ModeDryRun, true)
	p.SetCycleLock(errLock{})
	require.NoError(t, p.HandleStrategyTask(context.Background(), cycleTask(23)))
	eng.AssertNumberOfCalls(t, "Run", 1)
	worker.AssertCalled(t, "EnqueueStrategyTask", mock.Anything)
}

// If the lock is busy and the re-enqueue fails, the task errors so asynq
// retries it - the strategy's self-re-enqueue chain is never dropped.
func TestHandleStrategyTask_CycleLockBusyAndReenqueueFails_ReturnsError(t *testing.T) {
	worker := new(mocks.StrategyWorker)
	worker.On("EnqueueStrategyTaskWithDelay", mock.Anything, mock.Anything).Return(errors.New("redis down"))
	l := lock.NewMemoryStrategyLock()
	_, _ = l.Acquire(context.Background(), 24, "apply-proposal:1", time.Minute)

	p := handler.NewStrategyProcessor(nil, worker, new(mocks.StrategyRepository), new(mocks.Engine), new(mocks.Engine), new(signalmocks.NotificationSender), strategies.ModeDryRun, true)
	p.SetCycleLock(l)
	assert.Error(t, p.HandleStrategyTask(context.Background(), cycleTask(24)))
}
