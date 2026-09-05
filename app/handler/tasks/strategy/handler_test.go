package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"go-trade-bot/app/entities"
	handler "go-trade-bot/app/handler/tasks/strategy"
	"go-trade-bot/app/handler/tasks/strategy/mocks"
	"go-trade-bot/app/strategies"
	signalmocks "go-trade-bot/app/usecase/signal/mocks"
	"go-trade-bot/internal/notifier"
	"testing"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type noopStrategy struct{ terminated bool }

func (s *noopStrategy) Name() string                                             { return "registered-test-strategy" }
func (s *noopStrategy) Before(ctx strategies.Context)                            {}
func (s *noopStrategy) ShouldLong(ctx strategies.Context) bool                   { return false }
func (s *noopStrategy) GoLong(ctx strategies.Context) strategies.Signal          { return strategies.Signal{} }
func (s *noopStrategy) ShouldShort(ctx strategies.Context) bool                  { return false }
func (s *noopStrategy) GoShort(ctx strategies.Context) strategies.Signal         { return strategies.Signal{} }
func (s *noopStrategy) UpdatePosition(ctx strategies.Context) *strategies.Signal { return nil }
func (s *noopStrategy) After(ctx strategies.Context)                             {}
func (s *noopStrategy) Terminate(ctx strategies.Context)                         { s.terminated = true }

var registeredTestStrategy = &noopStrategy{}

func init() {
	strategies.Register("registered-test-strategy", func() strategies.Strategy {
		return registeredTestStrategy
	})
}

func TestHandleStrategyTask_DisabledStrategy_TerminatesAndDoesNotReenqueue(t *testing.T) {
	registeredTestStrategy.terminated = false

	repo := new(mocks.StrategyRepository)
	worker := new(mocks.StrategyWorker)
	eng := new(mocks.Engine)
	notifySender := new(signalmocks.NotificationSender)

	dbStrategy := entities.Strategy{ID: 1, Name: "S1", StrategyName: "registered-test-strategy", Status: entities.Disabled, Mode: "dryrun"}
	repo.On("GetByID", mock.Anything, uint(1)).Return(dbStrategy, nil)

	processor := handler.NewStrategyProcessor(nil, worker, repo, eng, notifySender, strategies.ModeDryRun, true)

	payload, _ := json.Marshal(entities.Strategy{ID: 1, Name: "S1"})
	task := asynq.NewTask(handler.StrategyTask+"S1", payload)

	err := processor.HandleStrategyTask(context.Background(), task)
	assert.NoError(t, err)
	assert.True(t, registeredTestStrategy.terminated)

	worker.AssertNotCalled(t, "EnqueueStrategyTask", mock.Anything)
	eng.AssertNotCalled(t, "Run", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestHandleStrategyTask_RunsEngineForEachMonitoredSymbol(t *testing.T) {
	repo := new(mocks.StrategyRepository)
	worker := new(mocks.StrategyWorker)
	eng := new(mocks.Engine)
	notifySender := new(signalmocks.NotificationSender)

	dbStrategy := entities.Strategy{
		ID: 2, Name: "S2", StrategyName: "registered-test-strategy", Status: entities.Productive, Mode: "dryrun",
		MonitoredSymbols: []string{"BTCUSDT", "ETHUSDT"},
	}
	repo.On("GetByID", mock.Anything, uint(2)).Return(dbStrategy, nil)
	eng.On("Run", mock.Anything, mock.Anything, mock.Anything, "BTCUSDT", strategies.ModeDryRun).Return(nil)
	eng.On("Run", mock.Anything, mock.Anything, mock.Anything, "ETHUSDT", strategies.ModeDryRun).Return(nil)
	worker.On("EnqueueStrategyTask", dbStrategy).Return(nil)
	repo.On("SaveExecution", mock.Anything, mock.MatchedBy(func(e entities.StrategyExecution) bool {
		return e.Status == entities.ExecutionStatus(entities.OK)
	})).Return(nil)

	processor := handler.NewStrategyProcessor(nil, worker, repo, eng, notifySender, strategies.ModeDryRun, true)

	payload, _ := json.Marshal(entities.Strategy{ID: 2, Name: "S2"})
	task := asynq.NewTask(handler.StrategyTask+"S2", payload)

	err := processor.HandleStrategyTask(context.Background(), task)
	assert.NoError(t, err)

	eng.AssertExpectations(t)
	worker.AssertExpectations(t)
	repo.AssertExpectations(t)
}

func TestHandleStrategyTask_StrategyNotInRegistry_RecordsErrorAndNotifies(t *testing.T) {
	repo := new(mocks.StrategyRepository)
	worker := new(mocks.StrategyWorker)
	eng := new(mocks.Engine)
	notifySender := new(signalmocks.NotificationSender)

	dbStrategy := entities.Strategy{
		ID: 3, Name: "S3", StrategyName: "does-not-exist", Status: entities.Productive, Mode: "dryrun",
		MonitoredSymbols: []string{"BTCUSDT"},
	}
	repo.On("GetByID", mock.Anything, uint(3)).Return(dbStrategy, nil)
	worker.On("EnqueueStrategyTask", dbStrategy).Return(nil)
	repo.On("SaveExecution", mock.Anything, mock.MatchedBy(func(e entities.StrategyExecution) bool {
		return e.Status == entities.ExecutionStatus(entities.Error)
	})).Return(nil)
	notifySender.On("Send", mock.Anything, mock.MatchedBy(func(e notifier.Event) bool {
		return e.Type == notifier.EventStrategyError && e.Data["context"] == "strategy not found in registry"
	})).Return(nil)

	processor := handler.NewStrategyProcessor(nil, worker, repo, eng, notifySender, strategies.ModeDryRun, true)

	payload, _ := json.Marshal(entities.Strategy{ID: 3, Name: "S3"})
	task := asynq.NewTask(handler.StrategyTask+"S3", payload)

	err := processor.HandleStrategyTask(context.Background(), task)
	assert.NoError(t, err) // HandleStrategyTask itself never returns an error to asynq
	eng.AssertNotCalled(t, "Run", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	worker.AssertExpectations(t)
	repo.AssertExpectations(t)
	notifySender.AssertExpectations(t)
}

func TestHandleStrategyTask_EngineErrors_RecordsErrorStatus(t *testing.T) {
	repo := new(mocks.StrategyRepository)
	worker := new(mocks.StrategyWorker)
	eng := new(mocks.Engine)
	notifySender := new(signalmocks.NotificationSender)

	dbStrategy := entities.Strategy{
		ID: 4, Name: "S4", StrategyName: "registered-test-strategy", Status: entities.Productive, Mode: "dryrun",
		MonitoredSymbols: []string{"BTCUSDT"},
	}
	repo.On("GetByID", mock.Anything, uint(4)).Return(dbStrategy, nil)
	eng.On("Run", mock.Anything, mock.Anything, mock.Anything, "BTCUSDT", strategies.ModeDryRun).Return(errors.New("boom"))
	worker.On("EnqueueStrategyTask", dbStrategy).Return(nil)
	repo.On("SaveExecution", mock.Anything, mock.MatchedBy(func(e entities.StrategyExecution) bool {
		return e.Status == entities.ExecutionStatus(entities.Error)
	})).Return(nil)

	processor := handler.NewStrategyProcessor(nil, worker, repo, eng, notifySender, strategies.ModeDryRun, true)

	payload, _ := json.Marshal(entities.Strategy{ID: 4, Name: "S4"})
	task := asynq.NewTask(handler.StrategyTask+"S4", payload)

	err := processor.HandleStrategyTask(context.Background(), task)
	assert.NoError(t, err)
	repo.AssertExpectations(t)
}

func TestHandleStrategyTask_ModeCappedDownByProcessCeiling_RunsAtCeilingTier(t *testing.T) {
	// Spec 10 AC#3: strategy.Mode="paper" under a process ceiling of "dryrun"
	// executes at dryrun (capped down silently), not refused.
	repo := new(mocks.StrategyRepository)
	worker := new(mocks.StrategyWorker)
	eng := new(mocks.Engine)
	notifySender := new(signalmocks.NotificationSender)

	dbStrategy := entities.Strategy{
		ID: 5, Name: "S5", StrategyName: "registered-test-strategy", Status: entities.Productive, Mode: "paper",
		MonitoredSymbols: []string{"BTCUSDT"},
	}
	repo.On("GetByID", mock.Anything, uint(5)).Return(dbStrategy, nil)
	eng.On("Run", mock.Anything, mock.Anything, mock.Anything, "BTCUSDT", strategies.ModeDryRun).Return(nil)
	worker.On("EnqueueStrategyTask", dbStrategy).Return(nil)
	repo.On("SaveExecution", mock.Anything, mock.MatchedBy(func(e entities.StrategyExecution) bool {
		return e.Status == entities.ExecutionStatus(entities.OK)
	})).Return(nil)

	processor := handler.NewStrategyProcessor(nil, worker, repo, eng, notifySender, strategies.ModeDryRun, true)

	payload, _ := json.Marshal(entities.Strategy{ID: 5, Name: "S5"})
	task := asynq.NewTask(handler.StrategyTask+"S5", payload)

	err := processor.HandleStrategyTask(context.Background(), task)
	assert.NoError(t, err)
	eng.AssertExpectations(t)
}

func TestHandleStrategyTask_LiveModeExceedsCeiling_RefusesCycleAndNotifies(t *testing.T) {
	// Spec 10 AC#4: strategy.Mode="live" under a process ceiling of "paper" is
	// refused outright, never downgraded to paper.
	repo := new(mocks.StrategyRepository)
	worker := new(mocks.StrategyWorker)
	eng := new(mocks.Engine)
	notifySender := new(signalmocks.NotificationSender)

	dbStrategy := entities.Strategy{
		ID: 6, Name: "S6", StrategyName: "registered-test-strategy", Status: entities.Productive, Mode: "live",
		MonitoredSymbols: []string{"BTCUSDT"},
	}
	repo.On("GetByID", mock.Anything, uint(6)).Return(dbStrategy, nil)
	worker.On("EnqueueStrategyTask", dbStrategy).Return(nil)
	repo.On("SaveExecution", mock.Anything, mock.MatchedBy(func(e entities.StrategyExecution) bool {
		return e.Status == entities.ExecutionStatus(entities.Error)
	})).Return(nil)
	notifySender.On("Send", mock.Anything, mock.MatchedBy(func(e notifier.Event) bool {
		return e.Type == notifier.EventStrategyError && e.Data["context"] == "mode guard refusal"
	})).Return(nil)

	processor := handler.NewStrategyProcessor(nil, worker, repo, eng, notifySender, strategies.ModePaper, true)

	payload, _ := json.Marshal(entities.Strategy{ID: 6, Name: "S6"})
	task := asynq.NewTask(handler.StrategyTask+"S6", payload)

	err := processor.HandleStrategyTask(context.Background(), task)
	assert.NoError(t, err) // HandleStrategyTask itself never returns an error to asynq
	eng.AssertNotCalled(t, "Run", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	repo.AssertExpectations(t)
	notifySender.AssertExpectations(t)
}

func TestHandleStrategyTask_UnparseableMode_FailsClosedAndNotifies(t *testing.T) {
	// Spec 10 AC#7: an unparseable Mode value is refused (fail closed), never
	// silently defaulted to a safe tier.
	repo := new(mocks.StrategyRepository)
	worker := new(mocks.StrategyWorker)
	eng := new(mocks.Engine)
	notifySender := new(signalmocks.NotificationSender)

	dbStrategy := entities.Strategy{
		ID: 7, Name: "S7", StrategyName: "registered-test-strategy", Status: entities.Productive, Mode: "corrupted-value",
		MonitoredSymbols: []string{"BTCUSDT"},
	}
	repo.On("GetByID", mock.Anything, uint(7)).Return(dbStrategy, nil)
	worker.On("EnqueueStrategyTask", dbStrategy).Return(nil)
	repo.On("SaveExecution", mock.Anything, mock.MatchedBy(func(e entities.StrategyExecution) bool {
		return e.Status == entities.ExecutionStatus(entities.Error)
	})).Return(nil)
	notifySender.On("Send", mock.Anything, mock.MatchedBy(func(e notifier.Event) bool {
		return e.Type == notifier.EventStrategyError && e.Data["context"] == "mode guard refusal"
	})).Return(nil)

	processor := handler.NewStrategyProcessor(nil, worker, repo, eng, notifySender, strategies.ModeLive, true)

	payload, _ := json.Marshal(entities.Strategy{ID: 7, Name: "S7"})
	task := asynq.NewTask(handler.StrategyTask+"S7", payload)

	err := processor.HandleStrategyTask(context.Background(), task)
	assert.NoError(t, err)
	eng.AssertNotCalled(t, "Run", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	repo.AssertExpectations(t)
	notifySender.AssertExpectations(t)
}

func TestHandleStrategyTask_EffectivePaperModeWithoutTestnet_RefusesCycleAndNotifies(t *testing.T) {
	// Spec 01 AC#9 / Spec 10 AC#6: effective mode "paper" with Testnet=false
	// is refused - Paper Trading must never resolve to the production
	// exchange adapter.
	repo := new(mocks.StrategyRepository)
	worker := new(mocks.StrategyWorker)
	eng := new(mocks.Engine)
	notifySender := new(signalmocks.NotificationSender)

	dbStrategy := entities.Strategy{
		ID: 8, Name: "S8", StrategyName: "registered-test-strategy", Status: entities.Productive, Mode: "paper",
		MonitoredSymbols: []string{"BTCUSDT"},
	}
	repo.On("GetByID", mock.Anything, uint(8)).Return(dbStrategy, nil)
	worker.On("EnqueueStrategyTask", dbStrategy).Return(nil)
	repo.On("SaveExecution", mock.Anything, mock.MatchedBy(func(e entities.StrategyExecution) bool {
		return e.Status == entities.ExecutionStatus(entities.Error)
	})).Return(nil)
	notifySender.On("Send", mock.Anything, mock.MatchedBy(func(e notifier.Event) bool {
		return e.Type == notifier.EventStrategyError && e.Data["context"] == "mode guard refusal"
	})).Return(nil)

	processor := handler.NewStrategyProcessor(nil, worker, repo, eng, notifySender, strategies.ModePaper, false)

	payload, _ := json.Marshal(entities.Strategy{ID: 8, Name: "S8"})
	task := asynq.NewTask(handler.StrategyTask+"S8", payload)

	err := processor.HandleStrategyTask(context.Background(), task)
	assert.NoError(t, err)
	eng.AssertNotCalled(t, "Run", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	repo.AssertExpectations(t)
	notifySender.AssertExpectations(t)
}

func TestHandleStrategyTask_EffectivePaperModeWithTestnet_RunsNormally(t *testing.T) {
	// Control case for the test above: the same effective mode "paper" with
	// Testnet=true must execute normally - confirms the new check doesn't
	// over-fire on the valid combination.
	repo := new(mocks.StrategyRepository)
	worker := new(mocks.StrategyWorker)
	eng := new(mocks.Engine)
	notifySender := new(signalmocks.NotificationSender)

	dbStrategy := entities.Strategy{
		ID: 10, Name: "S10", StrategyName: "registered-test-strategy", Status: entities.Productive, Mode: "paper",
		MonitoredSymbols: []string{"BTCUSDT"},
	}
	repo.On("GetByID", mock.Anything, uint(10)).Return(dbStrategy, nil)
	eng.On("Run", mock.Anything, mock.Anything, mock.Anything, "BTCUSDT", strategies.ModePaper).Return(nil)
	worker.On("EnqueueStrategyTask", dbStrategy).Return(nil)
	repo.On("SaveExecution", mock.Anything, mock.MatchedBy(func(e entities.StrategyExecution) bool {
		return e.Status == entities.ExecutionStatus(entities.OK)
	})).Return(nil)

	processor := handler.NewStrategyProcessor(nil, worker, repo, eng, notifySender, strategies.ModePaper, true)

	payload, _ := json.Marshal(entities.Strategy{ID: 10, Name: "S10"})
	task := asynq.NewTask(handler.StrategyTask+"S10", payload)

	err := processor.HandleStrategyTask(context.Background(), task)
	assert.NoError(t, err)
	eng.AssertExpectations(t)
	repo.AssertExpectations(t)
}

func TestHandleStrategyTask_GetByIDFails_ReturnsNilAndDoesNothingElse(t *testing.T) {
	repo := new(mocks.StrategyRepository)
	worker := new(mocks.StrategyWorker)
	eng := new(mocks.Engine)
	notifySender := new(signalmocks.NotificationSender)

	repo.On("GetByID", mock.Anything, uint(9)).Return(entities.Strategy{}, errors.New("db down"))

	processor := handler.NewStrategyProcessor(nil, worker, repo, eng, notifySender, strategies.ModeDryRun, true)

	payload, _ := json.Marshal(entities.Strategy{ID: 9, Name: "S9"})
	task := asynq.NewTask(handler.StrategyTask+"S9", payload)

	err := processor.HandleStrategyTask(context.Background(), task)
	assert.NoError(t, err)
	worker.AssertNotCalled(t, "EnqueueStrategyTask", mock.Anything)
	eng.AssertNotCalled(t, "Run", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}
