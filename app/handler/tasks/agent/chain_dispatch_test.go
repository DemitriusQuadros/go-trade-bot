package agent_test

import (
	"context"
	"encoding/json"
	"testing"

	"go-trade-bot/app/entities"
	taskagent "go-trade-bot/app/handler/tasks/agent"
	"go-trade-bot/app/handler/tasks/agent/mocks"
	agentusecase "go-trade-bot/app/usecase/agent"
	agentworker "go-trade-bot/app/workers/agent"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type chainEnv struct {
	agents   *fakeAgents
	settings *fakeKillSwitch
	enq      *fakeRunEnqueuer
	metrics  *counterMetrics
	launcher *agentworker.ChainLauncher
	d        *taskagent.ChainDispatcher
}

func newChainEnv() *chainEnv {
	e := &chainEnv{agents: newFakeAgents(), settings: &fakeKillSwitch{}, enq: &fakeRunEnqueuer{}, metrics: newCounterMetrics()}
	e.launcher = agentworker.NewChainLauncher(e.enq)
	e.launcher.OnFired = func() { e.metrics.IncrementCounter(taskagent.MetricTriggersFired, map[string]string{"kind": "chain"}) }
	e.launcher.OnSuppressed = func(string) {
		e.metrics.IncrementCounter(taskagent.MetricTriggersSuppressed, map[string]string{"kind": "chain"})
	}
	e.d = taskagent.NewChainDispatcher(e.agents, e.settings, e.launcher)
	return e
}

// C-01 AC#7: A -> B with on: report fires B only when A wrote a report.
func TestChainDispatch_OnReportOnlyWhenReportWritten(t *testing.T) {
	e := newChainEnv()
	a := e.agents.add(entities.Agent{ID: 1, Name: "A"}, entities.AgentTriggers{})
	b := e.agents.add(entities.Agent{ID: 2, Name: "B"}, entities.AgentTriggers{ChainFrom: []entities.ChainTrigger{{AgentID: 1, On: "report"}}})
	e.agents.add(entities.Agent{ID: 3, Name: "OnNotify"}, entities.AgentTriggers{ChainFrom: []entities.ChainTrigger{{AgentID: 1, On: "notify"}}})
	e.agents.add(entities.Agent{ID: 4, Name: "PausedSuccess", Paused: true}, entities.AgentTriggers{ChainFrom: []entities.ChainTrigger{{AgentID: 1, On: "success"}}})

	e.d.AfterRun(context.Background(), a, taskagent.RunPayload{AgentID: 1, Trigger: "cron"}, entities.AgentRun{ID: 10, Status: entities.AgentRunOK})
	assert.Zero(t, e.enq.count(), "no report, no notify -> nothing")

	e.d.AfterRun(context.Background(), a, taskagent.RunPayload{AgentID: 1, Trigger: "cron"}, entities.AgentRun{ID: 11, Status: entities.AgentRunOK, ReportIDs: []uint{40}})
	runs := e.enq.runs()
	require.Len(t, runs, 1)
	r := runs[0]
	assert.Equal(t, b.ID, r.AgentID)
	assert.Equal(t, "chain", r.Trigger)
	assert.Equal(t, 1, r.ChainDepth)
	assert.Equal(t, []uint{1}, r.ChainPath)
	assert.Equal(t, uint(11), *r.ParentRunID)
	d := detailMap(t, r.Detail)
	assert.Equal(t, float64(1), d["source_agent_id"])
	assert.Equal(t, float64(11), d["source_run_id"])
	assert.Equal(t, []any{float64(40)}, d["report_ids"])
	assert.Equal(t, "report", d["on"])
	var opts []string
	for _, o := range e.enq.opts[0] {
		opts = append(opts, o.String())
	}
	assert.Contains(t, opts, `TaskID("agent-chain:2:11")`)

	e.d.AfterRun(context.Background(), a, taskagent.RunPayload{AgentID: 1}, entities.AgentRun{ID: 12, Status: entities.AgentRunOK, NotificationsSent: 1})
	require.Equal(t, 2, e.enq.count())
	assert.Equal(t, uint(3), e.enq.runs()[1].AgentID, "on: notify")
	assert.Equal(t, 2.0, e.metrics.get(taskagent.MetricTriggersFired+"/chain"))

	// Kill switch: nothing chains.
	e.settings.set(true)
	e.d.AfterRun(context.Background(), a, taskagent.RunPayload{AgentID: 1}, entities.AgentRun{ID: 13, Status: entities.AgentRunOK, ReportIDs: []uint{1}, NotificationsSent: 1})
	assert.Equal(t, 2, e.enq.count())
}

// C-01 AC#7: a depth-4 chain is suppressed; A -> B -> A at runtime is
// suppressed via ChainPath even though these ChainFrom rows would have been
// refused by API validation.
func TestChainDispatch_RuntimeGuards(t *testing.T) {
	e := newChainEnv()
	a := e.agents.add(entities.Agent{ID: 1, Name: "A"}, entities.AgentTriggers{ChainFrom: []entities.ChainTrigger{{AgentID: 2, On: "success"}}})
	b := e.agents.add(entities.Agent{ID: 2, Name: "B"}, entities.AgentTriggers{ChainFrom: []entities.ChainTrigger{{AgentID: 1, On: "success"}}})
	e.agents.add(entities.Agent{ID: 5, Name: "E"}, entities.AgentTriggers{ChainFrom: []entities.ChainTrigger{{AgentID: 4, On: "success"}}})
	d4 := entities.Agent{ID: 4, Name: "D"}

	// A (cron) -> B.
	e.d.AfterRun(context.Background(), a, taskagent.RunPayload{AgentID: 1, Trigger: "cron"}, entities.AgentRun{ID: 20, Status: entities.AgentRunOK})
	require.Equal(t, 1, e.enq.count())
	bPayload := e.enq.runs()[0]
	// B's run (path [A]) finishing must not start A again.
	e.d.AfterRun(context.Background(), b, bPayload, entities.AgentRun{ID: 21, Status: entities.AgentRunOK})
	assert.Equal(t, 1, e.enq.count(), "A -> B -> A suppressed via ChainPath")
	assert.Equal(t, 1.0, e.metrics.get(taskagent.MetricTriggersSuppressed+"/chain"))

	// D at depth 3 -> E would be depth 4: suppressed.
	e.d.AfterRun(context.Background(), d4, taskagent.RunPayload{AgentID: 4, Trigger: "chain", ChainDepth: 3, ChainPath: []uint{7, 8, 9}}, entities.AgentRun{ID: 30, Status: entities.AgentRunOK})
	assert.Equal(t, 1, e.enq.count())
	assert.Equal(t, 2.0, e.metrics.get(taskagent.MetricTriggersSuppressed+"/chain"))

	// A redelivered parent (same source run) cannot double-fire.
	e.d.AfterRun(context.Background(), a, taskagent.RunPayload{AgentID: 1, Trigger: "cron"}, entities.AgentRun{ID: 20, Status: entities.AgentRunOK})
	assert.Equal(t, 1, e.enq.count())
}

// The processor wires it all: payload strategy/chain fields reach the
// usecase, the chain prompt names the source agent, and chains fire only
// after an ok run.
func TestProcessTask_ChainRunAndDeclarativeChaining(t *testing.T) {
	uc := mocks.NewUseCase(t)
	repo := mocks.NewAgentRepository(t)
	env := newChainEnv()
	src := env.agents.add(entities.Agent{ID: 1, Name: "Risk Monitor"}, entities.AgentTriggers{})
	env.agents.add(entities.Agent{ID: 2, Name: "Fixer"}, entities.AgentTriggers{ChainFrom: []entities.ChainTrigger{{AgentID: 7, On: "success"}}})
	target := entities.Agent{ID: 7, Name: "Analyst"}

	repo.On("GetAgent", mock.Anything, uint(7)).Return(target, nil)
	repo.On("GetAgent", mock.Anything, uint(1)).Return(src, nil)
	repo.On("ListBindingsByAgent", mock.Anything, uint(7)).Return([]entities.AgentStrategyBinding{{StrategyID: 3}, {StrategyID: 4}}, nil)
	parent := uint(123)
	detail, _ := json.Marshal(map[string]any{"source_agent_id": 1, "source_run_id": 123, "report_ids": []uint{9}, "on": "report"})
	uc.On("Run", mock.Anything, mock.MatchedBy(func(req agentusecase.RunRequest) bool {
		return req.Trigger == "chain" && req.ChainDepth == 1 && req.ParentRunID != nil && *req.ParentRunID == 123 &&
			assert.ObjectsAreEqual([]uint{1}, req.ChainPath) && req.StrategyID == nil &&
			assert.Contains(t, req.UserInput, "Agent Risk Monitor finished run #123 and produced reports [9]")
	})).Return(entities.AgentRun{ID: 124, Status: entities.AgentRunOK}, nil).Once()

	p := taskagent.NewProcessor(uc, repo, nil)
	p.SetChainDispatcher(env.d)
	require.NoError(t, p.ProcessTask(context.Background(), task(t, agentworker.RunPayload{
		AgentID: 7, Trigger: "chain", Detail: detail, ChainDepth: 1, ChainPath: []uint{1}, ParentRunID: &parent,
	})))
	runs := env.enq.runs()
	require.Len(t, runs, 1, "Fixer chains from Analyst on success")
	assert.Equal(t, uint(2), runs[0].AgentID)
	assert.Equal(t, 2, runs[0].ChainDepth)
	assert.Equal(t, []uint{1, 7}, runs[0].ChainPath)

	// A failed run never chains.
	uc.On("Run", mock.Anything, mock.Anything).Return(entities.AgentRun{ID: 125, Status: entities.AgentRunError}, assert.AnError).Once()
	require.NoError(t, p.ProcessTask(context.Background(), task(t, agentworker.RunPayload{AgentID: 7, Trigger: "cron"})))
	assert.Len(t, env.enq.runs(), 1)
}

func TestProcessTask_EventPayloadStrategyOverridesBindings(t *testing.T) {
	uc := mocks.NewUseCase(t)
	repo := mocks.NewAgentRepository(t)
	repo.On("GetAgent", mock.Anything, uint(7)).Return(entities.Agent{ID: 7}, nil)
	repo.On("ListBindingsByAgent", mock.Anything, uint(7)).Return([]entities.AgentStrategyBinding{{StrategyID: 3}}, nil)
	sid := uint(5)
	detail := json.RawMessage(`{"event":"stoploss.hit","strategy_id":5,"symbol":"BTCUSDT","occurred_at":"2026-01-01T00:00:00Z","data":{}}`)
	uc.On("Run", mock.Anything, mock.MatchedBy(func(req agentusecase.RunRequest) bool {
		return req.Trigger == "event" && req.StrategyID != nil && *req.StrategyID == 5 &&
			string(req.TriggerDetail) == string(detail) && assert.Contains(t, req.UserInput, "Strategy #5 emitted stoploss.hit")
	})).Return(entities.AgentRun{ID: 1, Status: entities.AgentRunOK}, nil)
	p := taskagent.NewProcessor(uc, repo, &fakeMetrics{})
	require.NoError(t, p.ProcessTask(context.Background(), task(t, agentworker.RunPayload{AgentID: 7, Trigger: "event", Detail: detail, StrategyID: &sid})))
}

func TestCronProvider_SchedulesSweepOnlyWhenNeeded(t *testing.T) {
	src := mocks.NewCronSource(t)
	settings := mocks.NewSettingsReader(t)
	settings.On("Get", mock.Anything).Return(&entities.Settings{}, nil)
	withDD := triggers()
	withDD.ID = 1
	withDD.Triggers = []byte(`{"events":[{"type":"drawdown","threshold_pct":5}]}`)
	src.On("ListAgents", mock.Anything).Return([]entities.Agent{withDD}, nil).Once()
	configs, err := taskagent.NewCronProvider(src, settings).WithEventSweep().GetConfigs()
	require.NoError(t, err)
	require.Len(t, configs, 1)
	assert.Equal(t, taskagent.TaskSweepEvents, configs[0].Task.Type())
	assert.Equal(t, "*/5 * * * *", configs[0].Cronspec)

	onlyOpened := triggers()
	onlyOpened.ID = 2
	onlyOpened.Triggers = []byte(`{"events":["position.opened"]}`)
	src.On("ListAgents", mock.Anything).Return([]entities.Agent{onlyOpened, {ID: 3, Paused: true, Triggers: withDD.Triggers}}, nil).Once()
	configs, err = taskagent.NewCronProvider(src, settings).WithEventSweep().GetConfigs()
	require.NoError(t, err)
	assert.Empty(t, configs, "no active swept trigger -> no sweep")
}
