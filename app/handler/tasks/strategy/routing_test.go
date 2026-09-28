package handler_test

import (
	"context"
	"encoding/json"
	"testing"

	"go-trade-bot/app/entities"
	handler "go-trade-bot/app/handler/tasks/strategy"
	"go-trade-bot/app/handler/tasks/strategy/mocks"
	"go-trade-bot/app/strategies"
	signalmocks "go-trade-bot/app/usecase/signal/mocks"
	"go-trade-bot/internal/notifier"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func routingTask(id uint) *asynq.Task {
	payload, _ := json.Marshal(entities.Strategy{ID: id, Name: "R"})
	return asynq.NewTask(handler.StrategyTask+"R", payload)
}

func anyRun() []interface{} {
	return []interface{}{mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything}
}

// fix-01: each effective mode is routed to exactly one engine.
func TestHandleStrategyTask_RoutesByEffectiveMode(t *testing.T) {
	cases := []struct {
		name         string
		strategyMode string
		ceiling      strategies.ExecutionMode
		testnet      bool
		wantMode     strategies.ExecutionMode
		wantDryRun   bool
	}{
		{"dryrun runs on the simulated engine", "dryrun", strategies.ModeLive, false, strategies.ModeDryRun, true},
		{"paper capped to dryrun runs simulated", "paper", strategies.ModeDryRun, true, strategies.ModeDryRun, true},
		{"live runs on the real engine", "live", strategies.ModeLive, false, strategies.ModeLive, false},
		{"paper runs on the real (testnet) engine", "paper", strategies.ModePaper, true, strategies.ModePaper, false},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id := uint(100 + i)
			repo := new(mocks.StrategyRepository)
			worker := new(mocks.StrategyWorker)
			realEng := new(mocks.Engine)
			dryEng := new(mocks.Engine)

			dbStrategy := entities.Strategy{ID: id, Name: "R", StrategyName: "registered-test-strategy", Status: entities.Productive, Mode: tc.strategyMode, MonitoredSymbols: []string{"BTCUSDT"}}
			repo.On("GetByID", mock.Anything, id).Return(dbStrategy, nil)
			repo.On("SaveExecution", mock.Anything, mock.MatchedBy(func(e entities.StrategyExecution) bool {
				return e.Status == entities.ExecutionStatus(entities.OK)
			})).Return(nil)
			worker.On("EnqueueStrategyTask", dbStrategy).Return(nil)

			used, unused := realEng, dryEng
			if tc.wantDryRun {
				used, unused = dryEng, realEng
			}
			used.On("Run", mock.Anything, mock.Anything, mock.Anything, "BTCUSDT", tc.wantMode).Return(nil).Once()

			p := handler.NewStrategyProcessor(nil, worker, repo, realEng, dryEng, new(signalmocks.NotificationSender), tc.ceiling, tc.testnet)
			assert.NoError(t, p.HandleStrategyTask(context.Background(), routingTask(id)))

			used.AssertExpectations(t)
			unused.AssertNotCalled(t, "Run", anyRun()...)
			worker.AssertExpectations(t)
		})
	}
}

// fix-01 AC5: a backtest-mode strategy is refused by the worker - no hook
// execution, no re-enqueue, one notification, an Error execution row.
func TestHandleStrategyTask_BacktestMode_RefusedNotReenqueued(t *testing.T) {
	cases := []struct {
		name         string
		strategyMode string
		ceiling      strategies.ExecutionMode
	}{
		{"strategy mode backtest", "backtest", strategies.ModeLive},
		{"process ceiling backtest", "dryrun", strategies.ModeBacktest},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id := uint(200 + i)
			repo := new(mocks.StrategyRepository)
			worker := new(mocks.StrategyWorker)
			realEng := new(mocks.Engine)
			dryEng := new(mocks.Engine)
			notifySender := new(signalmocks.NotificationSender)

			dbStrategy := entities.Strategy{ID: id, Name: "R", StrategyName: "registered-test-strategy", Status: entities.Testing, Mode: tc.strategyMode, MonitoredSymbols: []string{"BTCUSDT"}}
			repo.On("GetByID", mock.Anything, id).Return(dbStrategy, nil)
			repo.On("SaveExecution", mock.Anything, mock.MatchedBy(func(e entities.StrategyExecution) bool {
				return e.Status == entities.ExecutionStatus(entities.Error)
			})).Return(nil).Once()
			notifySender.On("Send", mock.Anything, mock.MatchedBy(func(e notifier.Event) bool {
				return e.Type == notifier.EventStrategyError && e.Data["context"] == "backtest mode refusal"
			})).Return(nil).Once()

			p := handler.NewStrategyProcessor(nil, worker, repo, realEng, dryEng, notifySender, tc.ceiling, false)
			assert.NoError(t, p.HandleStrategyTask(context.Background(), routingTask(id)))

			realEng.AssertNotCalled(t, "Run", anyRun()...)
			dryEng.AssertNotCalled(t, "Run", anyRun()...)
			worker.AssertNotCalled(t, "EnqueueStrategyTask", mock.Anything)
			worker.AssertNotCalled(t, "EnqueueStrategyTaskWithDelay", mock.Anything, mock.Anything)
			repo.AssertExpectations(t)
			notifySender.AssertExpectations(t)
		})
	}
}

// fix-01: without a simulated engine, dryrun cycles are refused - never
// routed to the real engine.
func TestHandleStrategyTask_NoDryRunEngine_RefusesDryRunCycle(t *testing.T) {
	repo := new(mocks.StrategyRepository)
	worker := new(mocks.StrategyWorker)
	realEng := new(mocks.Engine)
	notifySender := new(signalmocks.NotificationSender)

	dbStrategy := entities.Strategy{ID: 300, Name: "R", StrategyName: "registered-test-strategy", Status: entities.Productive, Mode: "dryrun", MonitoredSymbols: []string{"BTCUSDT"}}
	repo.On("GetByID", mock.Anything, uint(300)).Return(dbStrategy, nil)
	repo.On("SaveExecution", mock.Anything, mock.MatchedBy(func(e entities.StrategyExecution) bool {
		return e.Status == entities.ExecutionStatus(entities.Error)
	})).Return(nil).Once()
	worker.On("EnqueueStrategyTask", dbStrategy).Return(nil)
	notifySender.On("Send", mock.Anything, mock.MatchedBy(func(e notifier.Event) bool {
		return e.Data["context"] == "order routing refusal"
	})).Return(nil).Once()

	p := handler.NewStrategyProcessor(nil, worker, repo, realEng, nil, notifySender, strategies.ModeLive, false)
	assert.NoError(t, p.HandleStrategyTask(context.Background(), routingTask(300)))

	realEng.AssertNotCalled(t, "Run", anyRun()...)
	repo.AssertExpectations(t)
	notifySender.AssertExpectations(t)
}

func TestStrategyProcessor_OrderRoutingSummary(t *testing.T) {
	eng, dry := new(mocks.Engine), new(mocks.Engine)
	cases := []struct {
		ceiling strategies.ExecutionMode
		testnet bool
		want    string
	}{
		{strategies.ModeLive, false, "worker: order routing live=real paper=disabled dryrun=simulated"},
		{strategies.ModePaper, true, "worker: order routing live=disabled paper=testnet dryrun=simulated"},
		{strategies.ModeDryRun, false, "worker: order routing live=disabled paper=disabled dryrun=simulated"},
	}
	for _, tc := range cases {
		p := handler.NewStrategyProcessor(nil, nil, nil, eng, dry, nil, tc.ceiling, tc.testnet)
		assert.Equal(t, tc.want, p.OrderRoutingSummary())
	}
}
