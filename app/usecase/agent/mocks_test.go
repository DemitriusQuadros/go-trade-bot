package agent_test

import (
	"context"
	"encoding/json"

	"go-trade-bot/app/entities"
	backtestusecase "go-trade-bot/app/usecase/backtest"
	"go-trade-bot/internal/modelprovider"

	"github.com/stretchr/testify/mock"
)

// --- modelprovider.ModelProvider ---

type mockModelProvider struct {
	mock.Mock
}

func (m *mockModelProvider) Complete(ctx context.Context, req modelprovider.CompletionRequest) (modelprovider.CompletionResult, error) {
	args := m.Called(ctx, req)
	return args.Get(0).(modelprovider.CompletionResult), args.Error(1)
}

// --- app/repository/agent.Repository ---

type mockAgentRepository struct {
	mock.Mock
}

func (m *mockAgentRepository) GetInstruction(ctx context.Context) (entities.AgentInstruction, error) {
	args := m.Called(ctx)
	return args.Get(0).(entities.AgentInstruction), args.Error(1)
}

func (m *mockAgentRepository) SaveInstruction(ctx context.Context, content string) (entities.AgentInstruction, error) {
	args := m.Called(ctx, content)
	return args.Get(0).(entities.AgentInstruction), args.Error(1)
}

func (m *mockAgentRepository) CreateRun(ctx context.Context, run entities.AgentRun) (entities.AgentRun, error) {
	args := m.Called(ctx, run)
	return args.Get(0).(entities.AgentRun), args.Error(1)
}

func (m *mockAgentRepository) UpdateRun(ctx context.Context, run entities.AgentRun) error {
	args := m.Called(ctx, run)
	return args.Error(0)
}

func (m *mockAgentRepository) GetRun(ctx context.Context, id uint) (entities.AgentRun, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(entities.AgentRun), args.Error(1)
}

func (m *mockAgentRepository) ListRuns(ctx context.Context, limit int, strategyID *uint) ([]entities.AgentRun, error) {
	args := m.Called(ctx, limit, strategyID)
	return args.Get(0).([]entities.AgentRun), args.Error(1)
}

// --- agent.StrategyUseCase (local interface) ---

type mockStrategyUseCase struct {
	mock.Mock
}

func (m *mockStrategyUseCase) Save(ctx context.Context, s entities.Strategy) (entities.Strategy, error) {
	args := m.Called(ctx, s)
	return args.Get(0).(entities.Strategy), args.Error(1)
}

func (m *mockStrategyUseCase) Update(ctx context.Context, s entities.Strategy) error {
	args := m.Called(ctx, s)
	return args.Error(0)
}

func (m *mockStrategyUseCase) GetByID(ctx context.Context, id uint) (entities.Strategy, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(entities.Strategy), args.Error(1)
}

func (m *mockStrategyUseCase) GetAll(ctx context.Context) ([]entities.Strategy, error) {
	args := m.Called(ctx)
	return args.Get(0).([]entities.Strategy), args.Error(1)
}

// --- agent.BacktestUseCase (local interface) ---

type mockBacktestUseCase struct {
	mock.Mock
}

func (m *mockBacktestUseCase) Run(ctx context.Context, req backtestusecase.RunRequest) (entities.BacktestRun, error) {
	args := m.Called(ctx, req)
	return args.Get(0).(entities.BacktestRun), args.Error(1)
}

func (m *mockBacktestUseCase) GetByID(ctx context.Context, id uint) (entities.BacktestRun, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(entities.BacktestRun), args.Error(1)
}

func (m *mockBacktestUseCase) ListByStrategy(ctx context.Context, strategyID uint) ([]entities.BacktestRun, error) {
	args := m.Called(ctx, strategyID)
	return args.Get(0).([]entities.BacktestRun), args.Error(1)
}

// --- agent.SignalUseCase (local interface) ---

type mockSignalUseCase struct {
	mock.Mock
}

func (m *mockSignalUseCase) GetOpenSignals(ctx context.Context) ([]entities.Signal, error) {
	args := m.Called(ctx)
	return args.Get(0).([]entities.Signal), args.Error(1)
}

// --- agent.PerformanceSnapshotUseCase (local interface) ---

type mockSnapshotUseCase struct {
	mock.Mock
}

func (m *mockSnapshotUseCase) ListByStrategy(ctx context.Context, strategyID uint, limit int) ([]entities.StrategyPerformanceSnapshot, error) {
	args := m.Called(ctx, strategyID, limit)
	return args.Get(0).([]entities.StrategyPerformanceSnapshot), args.Error(1)
}

// rawArgs is a small helper for building json.RawMessage tool-call args in
// tests.
func rawArgs(v map[string]any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
