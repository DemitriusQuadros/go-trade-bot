package agent_test

import (
	"context"
	"encoding/json"
	"testing"

	"go-trade-bot/app/entities"
	agentusecase "go-trade-bot/app/usecase/agent"
	optimizeusecase "go-trade-bot/app/usecase/optimize"
	"go-trade-bot/internal/metrics_provider"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// findTool locates one tool by name in the closed registry - buildToolRegistry
// (tools.go) is unexported, so every tool test goes through Tools() the
// same way cmd/mcp/server/tools.go does.
func findTool(t *testing.T, uc *agentusecase.AgentUseCase, name string) agentusecase.Tool {
	t.Helper()
	for _, tool := range uc.Tools() {
		if tool.Def.Name == name {
			return tool
		}
	}
	t.Fatalf("tool %q not found in registry", name)
	return agentusecase.Tool{}
}

func newOptimizeWiredAgentUseCase() (*agentusecase.AgentUseCase, *mockOptimizeUseCase, *mockOptimizeWorker) {
	uc := agentusecase.NewAgentUseCase(&mockModelProvider{}, &mockAgentRepository{}, &mockStrategyUseCase{}, &mockBacktestUseCase{}, &mockSignalUseCase{}, &mockSnapshotUseCase{})
	optimizeUC := &mockOptimizeUseCase{}
	worker := &mockOptimizeWorker{}
	uc.Optimize = optimizeUC
	uc.OptimizeWorker = worker
	return uc, optimizeUC, worker
}

func TestOptimizationTools_RegisteredInClosedRegistry(t *testing.T) {
	uc, _, _ := newOptimizeWiredAgentUseCase()
	names := map[string]bool{}
	for _, tool := range uc.Tools() {
		names[tool.Def.Name] = true
	}
	assert.True(t, names["list_optimizations"])
	assert.True(t, names["run_optimization"])
	assert.True(t, names["get_optimization_results"])
}

func TestListOptimizationsTool(t *testing.T) {
	uc, optimizeUC, _ := newOptimizeWiredAgentUseCase()
	tool := findTool(t, uc, "list_optimizations")

	metricsBytes, _ := json.Marshal(metrics_provider.BacktestMetrics{SharpeRatio: 1.5})
	optimizeUC.On("ListByStrategy", mock.Anything, uint(1)).Return([]entities.OptimizationRun{
		{ID: 7, Status: entities.OptimizationCompleted, Progress: 25, TotalCombinations: 25, BestMetricsJSON: metricsBytes},
		{ID: 8, Status: entities.OptimizationRunning, Progress: 10, TotalCombinations: 25},
	}, nil)

	out, err := tool.Execute(context.Background(), json.RawMessage(`{"strategy_id":1}`))
	require.NoError(t, err)
	assert.Contains(t, out, "optimization_run_id=7")
	assert.Contains(t, out, "best_sharpe=1.5000")
	assert.Contains(t, out, "optimization_run_id=8")
	assert.Contains(t, out, "status=running")
}

func TestRunOptimizationTool_CreatesAndEnqueues(t *testing.T) {
	uc, optimizeUC, worker := newOptimizeWiredAgentUseCase()
	tool := findTool(t, uc, "run_optimization")

	optimizeUC.On("Create", mock.Anything, mock.MatchedBy(func(req optimizeusecase.CreateRequest) bool {
		return req.StrategyID == 1 && req.Symbol == "BTCUSDT" && len(req.ParamGrid) == 1
	})).Return(entities.OptimizationRun{ID: 42, Status: entities.OptimizationPending, TotalCombinations: 5}, nil)
	worker.On("EnqueueOptimizeTask", uint(42)).Return(nil)

	args := `{
		"strategy_id": 1,
		"symbol": "BTCUSDT",
		"timeframe": "1h",
		"start_date": "2026-01-01T00:00:00Z",
		"end_date": "2026-02-01T00:00:00Z",
		"param_grid": {"fast_period": {"min": 5, "max": 25, "step": 5}}
	}`
	out, err := tool.Execute(context.Background(), json.RawMessage(args))
	require.NoError(t, err)
	assert.Contains(t, out, "optimization_run_id=42")
	assert.Contains(t, out, "total_combinations=5")
	optimizeUC.AssertExpectations(t)
	worker.AssertExpectations(t)
}

func TestRunOptimizationTool_Unavailable_WhenNotWired(t *testing.T) {
	// A caller that never wired Optimize/OptimizeWorker (e.g. an older test
	// or a deployment without the optimize module) gets a clear error
	// instead of a nil-pointer panic.
	uc := agentusecase.NewAgentUseCase(&mockModelProvider{}, &mockAgentRepository{}, &mockStrategyUseCase{}, &mockBacktestUseCase{}, &mockSignalUseCase{}, &mockSnapshotUseCase{})
	tool := findTool(t, uc, "run_optimization")

	_, err := tool.Execute(context.Background(), json.RawMessage(`{"strategy_id":1,"symbol":"BTCUSDT","timeframe":"1h","start_date":"2026-01-01T00:00:00Z","end_date":"2026-02-01T00:00:00Z","param_grid":{}}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not available")
}

func TestGetOptimizationResultsTool_Completed(t *testing.T) {
	uc, optimizeUC, _ := newOptimizeWiredAgentUseCase()
	tool := findTool(t, uc, "get_optimization_results")

	bestConfig, _ := json.Marshal(map[string]float64{"fast_period": 10})
	bestMetrics, _ := json.Marshal(metrics_provider.BacktestMetrics{SharpeRatio: 2.1, MaxDrawdownPct: 5, TotalReturnPct: 12})
	grid, _ := json.Marshal([]optimizeusecase.GridPoint{
		{Params: map[string]float64{"fast_period": 10}, Metrics: &metrics_provider.BacktestMetrics{SharpeRatio: 2.1}},
		{Params: map[string]float64{"fast_period": 5}, Metrics: &metrics_provider.BacktestMetrics{SharpeRatio: 0.3}},
	})

	optimizeUC.On("GetByID", mock.Anything, uint(42)).Return(entities.OptimizationRun{
		ID:                42,
		StrategyID:        1,
		Symbol:            "BTCUSDT",
		Status:            entities.OptimizationCompleted,
		Progress:          2,
		TotalCombinations: 2,
		BestConfigJSON:    bestConfig,
		BestMetricsJSON:   bestMetrics,
		ResultsGridJSON:   grid,
	}, nil)

	out, err := tool.Execute(context.Background(), json.RawMessage(`{"optimization_run_id":42}`))
	require.NoError(t, err)
	assert.Contains(t, out, "optimization_run_id=42")
	assert.Contains(t, out, "best_sharpe=2.1000")
	assert.Contains(t, out, "top 2 of 2 combinations")
}

func TestGetOptimizationResultsTool_NotYetCompleted(t *testing.T) {
	uc, optimizeUC, _ := newOptimizeWiredAgentUseCase()
	tool := findTool(t, uc, "get_optimization_results")

	optimizeUC.On("GetByID", mock.Anything, uint(9)).Return(entities.OptimizationRun{
		ID: 9, Status: entities.OptimizationRunning, Progress: 3, TotalCombinations: 10,
	}, nil)

	out, err := tool.Execute(context.Background(), json.RawMessage(`{"optimization_run_id":9}`))
	require.NoError(t, err)
	assert.Contains(t, out, "progress=3/10")
	assert.Contains(t, out, "Not completed yet")
}
