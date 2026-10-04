package agent_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"go-trade-bot/app/entities"
	agentusecase "go-trade-bot/app/usecase/agent"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
)

type mockInspector struct {
	mock.Mock
}

func (m *mockInspector) Servers() ([]*asynq.ServerInfo, error) {
	args := m.Called()
	return args.Get(0).([]*asynq.ServerInfo), args.Error(1)
}

func (m *mockInspector) Queues() ([]string, error) {
	args := m.Called()
	return args.Get(0).([]string), args.Error(1)
}

func (m *mockInspector) GetQueueInfo(queue string) (*asynq.QueueInfo, error) {
	args := m.Called(queue)
	return args.Get(0).(*asynq.QueueInfo), args.Error(1)
}

func (m *mockInspector) SchedulerEntries() ([]*asynq.SchedulerEntry, error) {
	args := m.Called()
	return args.Get(0).([]*asynq.SchedulerEntry), args.Error(1)
}

type mockExecutionReader struct {
	mock.Mock
}

func (m *mockExecutionReader) GetLatestExecutions(ctx context.Context) (map[uint]entities.StrategyExecution, error) {
	args := m.Called(ctx)
	return args.Get(0).(map[uint]entities.StrategyExecution), args.Error(1)
}

func getSchedulerHealthTool(t *testing.T, uc *agentusecase.AgentUseCase) agentusecase.Tool {
	t.Helper()
	for _, tool := range uc.Tools() {
		if tool.Def.Name == "get_scheduler_health" {
			assert.Equal(t, entities.PermRead, tool.Permission)
			return tool
		}
	}
	t.Fatal("get_scheduler_health not registered")
	return agentusecase.Tool{}
}

func TestGetSchedulerHealthTool_NilDependencies(t *testing.T) {
	uc, _, _, mockStrat, _ := newBareAgentUseCase()
	mockStrat.On("GetAll", mock.Anything).Return([]entities.Strategy{}, nil)

	tool := getSchedulerHealthTool(t, uc)
	assert.Equal(t, "get_scheduler_health", tool.Def.Name)

	out, err := tool.Execute(context.Background(), json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Contains(t, out, "Asynq inspector: not wired on this transport.")
	assert.Contains(t, out, "=== 2. STRATEGY CYCLE EXECUTION HEALTH ===")
	assert.Contains(t, out, "No active strategies found matching criteria.")
	assert.Contains(t, out, "Agent platform repository: not wired on this transport.")
	assert.Contains(t, out, "STATUS: ✅ ALL SCHEDULERS & WORKERS HEALTHY")
}

func TestGetSchedulerHealthTool_HealthyState(t *testing.T) {
	fixedNow := time.Date(2026, 9, 30, 15, 0, 0, 0, time.UTC)

	uc, _, mockRepo, mockStrat, _ := newBareAgentUseCase()

	mockInsp := new(mockInspector)
	mockExecReader := new(mockExecutionReader)

	// Servers: 1 worker on default, 1 agent on agents
	servers := []*asynq.ServerInfo{
		{PID: 101, Host: "worker-host", Status: "active", Concurrency: 10, Queues: map[string]int{"default": 1}, ActiveWorkers: nil},
		{PID: 102, Host: "agent-host", Status: "active", Concurrency: 5, Queues: map[string]int{"agents": 1}, ActiveWorkers: nil},
	}
	mockInsp.On("Servers").Return(servers, nil)
	mockInsp.On("Queues").Return([]string{"default", "agents"}, nil)
	mockInsp.On("GetQueueInfo", "default").Return(&asynq.QueueInfo{Queue: "default", Size: 0, Active: 0, Pending: 0, Latency: 0}, nil)
	mockInsp.On("GetQueueInfo", "agents").Return(&asynq.QueueInfo{Queue: "agents", Size: 0, Active: 0, Pending: 0, Latency: 0}, nil)

	// Scheduler entries
	taskBytes, _ := json.Marshal(map[string]any{"agent_id": 2})
	sampleTask := asynq.NewTask("agent:run", taskBytes)
	schedEntries := []*asynq.SchedulerEntry{
		{Spec: "0 * * * *", Next: fixedNow.Add(15 * time.Minute), Task: sampleTask},
	}
	mockInsp.On("SchedulerEntries").Return(schedEntries, nil)

	// Strategies
	strategies := []entities.Strategy{
		{
			ID:     10,
			Name:   "BTC Scalper",
			Status: entities.Testing,
			Mode:   "dryrun",
			StrategyConfiguration: entities.StrategyConfiguration{
				Cycle: 5,
			},
			MonitoredSymbols: []string{"BTCUSDT"},
		},
	}
	mockStrat.On("GetAll", mock.Anything).Return(strategies, nil)

	// Strategy execution - healthy (ran 2 minutes ago)
	mockExecReader.On("GetLatestExecutions", mock.Anything).Return(map[uint]entities.StrategyExecution{
		10: {
			StrategyID: 10,
			Status:     entities.ExecutionStatus(entities.OK),
			ExecutedAt: fixedNow.Add(-2 * time.Minute),
			Message:    "completed 0 orders",
		},
	}, nil)

	// Agent platform repo
	platform := newFakePlatform()
	_, _ = platform.EnsureDefaultAgent(context.Background())
	platform.addAgent(entities.Agent{
		ID:       2,
		Name:     "Trend Agent",
		Paused:   false,
		Triggers: datatypes.JSON(`{"cron":["0 * * * *"]}`),
	})
	_ = platform.SetBindings(context.Background(), 2, []uint{10})

	// Last run for agent 2
	mockRepo.On("LastRunByAgent", mock.Anything, uint(2)).Return(&entities.AgentRun{
		ID:        1,
		Status:    entities.AgentRunOK,
		Trigger:   "cron",
		StartedAt: fixedNow.Add(-45 * time.Minute),
	}, nil)

	uc.Clock = func() time.Time { return fixedNow }
	uc.Inspector = mockInsp
	uc.ExecutionReader = mockExecReader
	uc.Platform = platform

	tool := getSchedulerHealthTool(t, uc)
	out, err := tool.Execute(context.Background(), json.RawMessage(`{}`))
	require.NoError(t, err)

	assert.Contains(t, out, "PID 101 on worker-host")
	assert.Contains(t, out, "PID 102 on agent-host")
	assert.Contains(t, out, "Queue \"default\" (strategy cycles & backtests)")
	assert.Contains(t, out, "Queue \"agents\" (AI agent runs & proposals)")
	assert.Contains(t, out, "[ID 10] BTC Scalper")
	assert.Contains(t, out, "HEALTHY (last ran 2m ago, cycle 5m)")
	assert.Contains(t, out, "[Agent 2] Trend Agent")
	assert.Contains(t, out, "last_run: 2026-09-30 14:15:00 UTC (45m ago)")
	assert.Contains(t, out, "next_scheduled_run: 2026-09-30 15:15:00 UTC")
	assert.Contains(t, out, "STATUS: ✅ ALL SCHEDULERS & WORKERS HEALTHY")
}

func TestGetSchedulerHealthTool_StalledStrategyAndMissingWorker(t *testing.T) {
	fixedNow := time.Date(2026, 9, 30, 15, 0, 0, 0, time.UTC)

	uc, _, _, mockStrat, _ := newBareAgentUseCase()

	mockInsp := new(mockInspector)
	mockExecReader := new(mockExecutionReader)

	// No worker server on default queue, only agent
	servers := []*asynq.ServerInfo{
		{PID: 102, Host: "agent-host", Status: "active", Concurrency: 5, Queues: map[string]int{"agents": 1}},
	}
	mockInsp.On("Servers").Return(servers, nil)
	mockInsp.On("Queues").Return([]string{"default"}, nil)
	// Default queue has high pending and latency
	mockInsp.On("GetQueueInfo", "default").Return(&asynq.QueueInfo{
		Queue:   "default",
		Size:    25,
		Pending: 25,
		Latency: 5 * time.Minute,
		Retry:   2,
	}, nil)
	mockInsp.On("SchedulerEntries").Return([]*asynq.SchedulerEntry{}, nil)

	// Strategy that stalled (last executed 45 minutes ago on a 5-minute cycle)
	strategies := []entities.Strategy{
		{
			ID:     13,
			Name:   "SOL Scalper",
			Status: entities.Testing,
			Mode:   "dryrun",
			StrategyConfiguration: entities.StrategyConfiguration{
				Cycle: 5,
			},
			MonitoredSymbols: []string{"SOLUSDT"},
		},
	}
	mockStrat.On("GetAll", mock.Anything).Return(strategies, nil)

	mockExecReader.On("GetLatestExecutions", mock.Anything).Return(map[uint]entities.StrategyExecution{
		13: {
			StrategyID: 13,
			Status:     entities.ExecutionStatus(entities.OK),
			ExecutedAt: fixedNow.Add(-45 * time.Minute),
		},
	}, nil)

	uc.Clock = func() time.Time { return fixedNow }
	uc.Inspector = mockInsp
	uc.ExecutionReader = mockExecReader

	tool := getSchedulerHealthTool(t, uc)
	out, err := tool.Execute(context.Background(), json.RawMessage(`{}`))
	require.NoError(t, err)

	assert.Contains(t, out, "CRITICAL: No active worker server consuming the \"default\" queue")
	assert.Contains(t, out, "High latency/backlog in queue \"default\"")
	assert.Contains(t, out, "2 task(s) currently failing and waiting for retry")
	assert.Contains(t, out, "STALLED / OVERDUE (expected every 5m, last ran 45m ago)")
	assert.Contains(t, out, "STATUS: ⚠️ ATTENTION REQUIRED (4 issue(s) detected)")
	assert.Contains(t, out, "Strategy worker process is DOWN")
	assert.Contains(t, out, "Strategy \"SOL Scalper\" (ID 13) is stalled")
}
