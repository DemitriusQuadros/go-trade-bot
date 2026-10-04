package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"go-trade-bot/app/entities"
	taskagent "go-trade-bot/app/handler/tasks/agent"
	"go-trade-bot/app/handler/tasks/agent/mocks"
	"go-trade-bot/app/repository/agentplatform"
	agentusecase "go-trade-bot/app/usecase/agent"
	agentworker "go-trade-bot/app/workers/agent"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type recordedMetric struct {
	name   string
	labels map[string]string
	value  float64
}

type fakeMetrics struct {
	mu  sync.Mutex
	got []recordedMetric
}

func (f *fakeMetrics) IncrementCounter(name string, labels map[string]string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.got = append(f.got, recordedMetric{name, labels, 1})
}
func (f *fakeMetrics) AddCounter(name string, labels map[string]string, v float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.got = append(f.got, recordedMetric{name, labels, v})
}
func (f *fakeMetrics) ObserveHistogram(name string, labels map[string]string, v float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.got = append(f.got, recordedMetric{name, labels, v})
}

func task(t *testing.T, p agentworker.RunPayload) *asynq.Task {
	task, err := agentworker.NewTask(p)
	require.NoError(t, err)
	return task
}

func TestProcessTask_ManualRunWithSingleBinding(t *testing.T) {
	uc := mocks.NewUseCase(t)
	repo := mocks.NewAgentRepository(t)
	m := &fakeMetrics{}
	agent := entities.Agent{ID: 4, Name: "Risk"}
	repo.On("GetAgent", mock.Anything, uint(4)).Return(agent, nil)
	repo.On("ListBindingsByAgent", mock.Anything, uint(4)).Return([]entities.AgentStrategyBinding{{AgentID: 4, StrategyID: 9}}, nil)
	uc.On("Run", mock.Anything, mock.MatchedBy(func(req agentusecase.RunRequest) bool {
		var detail map[string]string
		_ = json.Unmarshal(req.TriggerDetail, &detail)
		return req.Agent.ID == 4 && req.Trigger == "manual" && req.StrategyID != nil && *req.StrategyID == 9 &&
			req.UserInput == agentusecase.BuildScheduledInput("check BTC") && detail["requested_by"] == "operator" && detail["prompt"] == "check BTC"
	})).Return(entities.AgentRun{ID: 1, Status: entities.AgentRunOK, CostUSD: 0.02, InputTokens: 100, OutputTokens: 50}, nil)

	p := taskagent.NewProcessor(uc, repo, m)
	require.NoError(t, p.ProcessTask(context.Background(), task(t, agentworker.RunPayload{AgentID: 4, Trigger: "manual", Prompt: "check BTC"})))

	names := map[string]float64{}
	for _, r := range m.got {
		names[r.name+"/"+r.labels["direction"]] += r.value
	}
	assert.Equal(t, 1.0, names[taskagent.MetricRunsTotal+"/"])
	assert.InDelta(t, 0.02, names[taskagent.MetricCostUSDTotal+"/"], 1e-9)
	assert.Equal(t, 100.0, names[taskagent.MetricTokensTotal+"/input"])
	assert.Equal(t, 50.0, names[taskagent.MetricTokensTotal+"/output"])
}

func TestProcessTask_CronRunWithManyBindingsHasNoContextStrategy(t *testing.T) {
	uc := mocks.NewUseCase(t)
	repo := mocks.NewAgentRepository(t)
	repo.On("GetAgent", mock.Anything, uint(4)).Return(entities.Agent{ID: 4, Name: "Risk"}, nil)
	repo.On("ListBindingsByAgent", mock.Anything, uint(4)).Return([]entities.AgentStrategyBinding{{StrategyID: 1}, {StrategyID: 2}}, nil)
	uc.On("Run", mock.Anything, mock.MatchedBy(func(req agentusecase.RunRequest) bool {
		return req.Trigger == "cron" && req.StrategyID == nil && req.UserInput == agentusecase.EvaluationPrompt &&
			string(req.TriggerDetail) == `{"cron":"0 * * * *"}`
	})).Return(entities.AgentRun{ID: 2, Status: entities.AgentRunOK}, nil)

	p := taskagent.NewProcessor(uc, repo, nil)
	require.NoError(t, p.ProcessTask(context.Background(), task(t, agentworker.RunPayload{AgentID: 4, Trigger: "cron", Detail: json.RawMessage(`{"cron":"0 * * * *"}`)})))
}

func TestProcessTask_MissingAgentIsDroppedNotRetried(t *testing.T) {
	uc := mocks.NewUseCase(t)
	repo := mocks.NewAgentRepository(t)
	repo.On("GetAgent", mock.Anything, uint(99)).Return(entities.Agent{}, errors.New("not found"))
	p := taskagent.NewProcessor(uc, repo, nil)
	assert.NoError(t, p.ProcessTask(context.Background(), task(t, agentworker.RunPayload{AgentID: 99, Trigger: "cron"})))
	uc.AssertNotCalled(t, "Run", mock.Anything, mock.Anything)
}

func TestProcessTask_RecordedErrorsAreNotReturned(t *testing.T) {
	uc := mocks.NewUseCase(t)
	repo := mocks.NewAgentRepository(t)
	repo.On("GetAgent", mock.Anything, uint(4)).Return(entities.Agent{ID: 4}, nil)
	repo.On("ListBindingsByAgent", mock.Anything, uint(4)).Return(nil, nil)
	uc.On("Run", mock.Anything, mock.Anything).Return(entities.AgentRun{ID: 3, Status: entities.AgentRunError, ErrorMessage: "halted: agents are paused by the global kill switch"}, errors.New("halted")).Once()
	uc.On("Run", mock.Anything, mock.Anything).Return(entities.AgentRun{}, errors.New("db down")).Once()

	p := taskagent.NewProcessor(uc, repo, &fakeMetrics{})
	assert.NoError(t, p.ProcessTask(context.Background(), task(t, agentworker.RunPayload{AgentID: 4, Trigger: "cron"})), "a halted run is recorded, not retried")
	assert.Error(t, p.ProcessTask(context.Background(), task(t, agentworker.RunPayload{AgentID: 4, Trigger: "cron"})), "no AgentRun row -> surfaced to asynq")
}

func triggers(specs ...string) entities.Agent {
	return entities.Agent{Triggers: agentplatform.MarshalTriggers(entities.AgentTriggers{Cron: specs})}
}

func TestCronProvider_SchedulesOnlyActiveBoundAgents(t *testing.T) {
	src := mocks.NewCronSource(t)
	settings := mocks.NewSettingsReader(t)
	settings.On("Get", mock.Anything).Return(&entities.Settings{}, nil)

	active := triggers("*/1 * * * *", "not-a-cron", "0 */6 * * *")
	active.ID, active.Name = 1, "active"
	paused := triggers("*/1 * * * *")
	paused.ID, paused.Paused = 2, true
	unbound := triggers("*/1 * * * *")
	unbound.ID = 3
	noCron := entities.Agent{ID: 4}

	src.On("ListAgents", mock.Anything).Return([]entities.Agent{active, paused, unbound, noCron}, nil)
	src.On("ListBindingsByAgent", mock.Anything, uint(1)).Return([]entities.AgentStrategyBinding{{AgentID: 1, StrategyID: 3}}, nil)
	src.On("ListBindingsByAgent", mock.Anything, uint(3)).Return([]entities.AgentStrategyBinding{}, nil)

	configs, err := taskagent.NewCronProvider(src, settings).GetConfigs()
	require.NoError(t, err)
	require.Len(t, configs, 2, "invalid spec skipped, paused/unbound/no-cron agents skipped")
	assert.Equal(t, "*/1 * * * *", configs[0].Cronspec)
	assert.Equal(t, "0 */6 * * *", configs[1].Cronspec)

	var p agentworker.RunPayload
	require.NoError(t, json.Unmarshal(configs[0].Task.Payload(), &p))
	assert.Equal(t, uint(1), p.AgentID)
	assert.Equal(t, "cron", p.Trigger)
	assert.Equal(t, taskagent.TaskAgentRun, configs[0].Task.Type())
	var opts []string
	for _, o := range configs[0].Opts {
		opts = append(opts, o.String())
	}
	assert.Contains(t, opts, `Queue("agents")`)
	assert.Contains(t, opts, "MaxRetry(0)")
}

// A-02 AC#4 (scheduling half): the kill switch unschedules every cron run.
func TestCronProvider_KillSwitchSchedulesNothing(t *testing.T) {
	src := mocks.NewCronSource(t)
	settings := mocks.NewSettingsReader(t)
	settings.On("Get", mock.Anything).Return(&entities.Settings{AgentsPaused: true}, nil)
	configs, err := taskagent.NewCronProvider(src, settings).GetConfigs()
	require.NoError(t, err)
	assert.Empty(t, configs)
	src.AssertNotCalled(t, "ListAgents", mock.Anything)
}

func TestCronProvider_FailsClosed(t *testing.T) {
	src := mocks.NewCronSource(t)
	settings := mocks.NewSettingsReader(t)
	settings.On("Get", mock.Anything).Return(nil, errors.New("db down"))
	configs, err := taskagent.NewCronProvider(src, settings).GetConfigs()
	require.NoError(t, err)
	assert.Empty(t, configs)
}
