package optimize_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"go-trade-bot/app/engine"
	"go-trade-bot/app/entities"
	usecase "go-trade-bot/app/usecase/optimize"
	"go-trade-bot/app/usecase/optimize/mocks"
	"go-trade-bot/internal/metrics_provider"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
)

// ---- ExpandGrid / CountCombinations (backend-01 AC#1, AC#2; backend-02 combination generation) ----

func TestExpandGrid_PRDExample_55Combinations(t *testing.T) {
	grid := usecase.ParamGrid{
		"rsi_period":    {Min: 10, Max: 20, Step: 1},
		"stop_loss_pct": {Min: 1, Max: 3, Step: 0.5},
	}

	combos, err := usecase.ExpandGrid(grid)
	require.NoError(t, err)
	assert.Len(t, combos, 55)

	// Deterministic ordering: first combination is the minimum of every key.
	assert.Equal(t, 10.0, combos[0]["rsi_period"])
	assert.Equal(t, 1.0, combos[0]["stop_loss_pct"])

	// Every combination must be distinct.
	seen := map[string]bool{}
	for _, c := range combos {
		bytes, _ := json.Marshal(c)
		seen[string(bytes)] = true
	}
	assert.Len(t, seen, 55)
}

func TestExpandGrid_ZeroStep_Rejected(t *testing.T) {
	grid := usecase.ParamGrid{"rsi_period": {Min: 10, Max: 20, Step: 0}}
	_, err := usecase.ExpandGrid(grid)
	require.Error(t, err)
	assert.ErrorIs(t, err, usecase.ErrInvalidStep)
}

func TestExpandGrid_EmptyGrid_Rejected(t *testing.T) {
	_, err := usecase.ExpandGrid(usecase.ParamGrid{})
	require.Error(t, err)
	assert.ErrorIs(t, err, usecase.ErrEmptyParamGrid)
}

func TestCountCombinations_MatchesExpandGridLength(t *testing.T) {
	grid := usecase.ParamGrid{"x": {Min: 1, Max: 3, Step: 1}}
	count, err := usecase.CountCombinations(grid)
	require.NoError(t, err)
	assert.Equal(t, 3, count)
}

// ---- Create (backend-01 AC#1, AC#2, AC#3) ----

func TestOptimizeUseCase_Create_MissingStrategyID(t *testing.T) {
	u := usecase.NewOptimizeUseCase(mocks.NewBacktestRunner(t), mocks.NewStrategyRepository(t), mocks.NewOptimizationRepository(t), 0)
	_, err := u.Create(context.Background(), usecase.CreateRequest{
		Symbol:    "BTCUSDT",
		ParamGrid: usecase.ParamGrid{"x": {Min: 1, Max: 2, Step: 1}},
	})
	assert.ErrorIs(t, err, usecase.ErrMissingStrategyID)
}

func TestOptimizeUseCase_Create_MissingSymbol(t *testing.T) {
	u := usecase.NewOptimizeUseCase(mocks.NewBacktestRunner(t), mocks.NewStrategyRepository(t), mocks.NewOptimizationRepository(t), 0)
	_, err := u.Create(context.Background(), usecase.CreateRequest{
		StrategyID: 1,
		ParamGrid:  usecase.ParamGrid{"x": {Min: 1, Max: 2, Step: 1}},
	})
	assert.ErrorIs(t, err, usecase.ErrMissingSymbol)
}

func TestOptimizeUseCase_Create_ZeroStepRejectedBeforePersist(t *testing.T) {
	optimizeRepo := mocks.NewOptimizationRepository(t)
	u := usecase.NewOptimizeUseCase(mocks.NewBacktestRunner(t), mocks.NewStrategyRepository(t), optimizeRepo, 0)
	_, err := u.Create(context.Background(), usecase.CreateRequest{
		StrategyID: 1,
		Symbol:     "BTCUSDT",
		ParamGrid:  usecase.ParamGrid{"x": {Min: 1, Max: 2, Step: 0}},
	})
	assert.ErrorIs(t, err, usecase.ErrInvalidStep)
	optimizeRepo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
}

// TestOptimizeUseCase_Create_GridTooLarge_RejectsBeforePersist covers backend-01
// AC#3: a request exceeding the cap must be rejected (mapped by the HTTP
// handler to 413) BEFORE any row is created and BEFORE any task would be
// enqueued.
func TestOptimizeUseCase_Create_GridTooLarge_RejectsBeforePersist(t *testing.T) {
	optimizeRepo := mocks.NewOptimizationRepository(t)
	u := usecase.NewOptimizeUseCase(mocks.NewBacktestRunner(t), mocks.NewStrategyRepository(t), optimizeRepo, 10)

	_, err := u.Create(context.Background(), usecase.CreateRequest{
		StrategyID: 1,
		Symbol:     "BTCUSDT",
		ParamGrid:  usecase.ParamGrid{"x": {Min: 1, Max: 20, Step: 1}}, // 20 combinations > cap of 10
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, usecase.ErrGridTooLarge)
	optimizeRepo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
}

func TestOptimizeUseCase_Create_Success_PersistsPendingRunWithTotalCombinations(t *testing.T) {
	optimizeRepo := mocks.NewOptimizationRepository(t)
	optimizeRepo.On("Create", mock.Anything, mock.MatchedBy(func(run *entities.OptimizationRun) bool {
		return run.Status == entities.OptimizationPending && run.TotalCombinations == 55
	})).Return(nil).Run(func(args mock.Arguments) {
		run := args.Get(1).(*entities.OptimizationRun)
		run.ID = 42
	})

	u := usecase.NewOptimizeUseCase(mocks.NewBacktestRunner(t), mocks.NewStrategyRepository(t), optimizeRepo, 0)

	run, err := u.Create(context.Background(), usecase.CreateRequest{
		StrategyID: 1,
		Symbol:     "BTCUSDT",
		ParamGrid: usecase.ParamGrid{
			"rsi_period":    {Min: 10, Max: 20, Step: 1},
			"stop_loss_pct": {Min: 1, Max: 3, Step: 0.5},
		},
	})

	require.NoError(t, err)
	assert.Equal(t, uint(42), run.ID)
	assert.Equal(t, entities.OptimizationPending, run.Status)
	assert.Equal(t, 55, run.TotalCombinations)
}

// ---- Run (backend-02 AC#1, AC#3, AC#5, AC#6, AC#8) ----

func baseStrategy() entities.Strategy {
	return entities.Strategy{
		ID:           1,
		StrategyName: "bollinger",
		StrategyConfiguration: entities.StrategyConfiguration{
			Configuration: datatypes.JSON([]byte(`{"unrelated_field": 42}`)),
		},
	}
}

func gridRun(id uint, grid usecase.ParamGrid) entities.OptimizationRun {
	gridBytes, _ := json.Marshal(grid)
	total, _ := usecase.CountCombinations(grid)
	return entities.OptimizationRun{
		ID:                id,
		StrategyID:        1,
		Symbol:            "BTCUSDT",
		Timeframe:         "1m",
		InitialCapital:    1000,
		ParamGridJSON:     datatypes.JSON(gridBytes),
		Status:            entities.OptimizationPending,
		TotalCombinations: total,
	}
}

// TestOptimizeUseCase_Run_SequentialProgressAndBestSelection covers AC#1
// (progress increments 0->N monotonically as combinations complete) and
// AC#3/AC#4 (best combination selected by highest Sharpe, ties broken by
// lowest MaxDrawdownPct; BestConfigJSON holds only the winning combination's
// params, not the full merged config).
func TestOptimizeUseCase_Run_SequentialProgressAndBestSelection(t *testing.T) {
	grid := usecase.ParamGrid{"x": {Min: 1, Max: 3, Step: 1}} // 3 combinations: x=1,2,3

	run := gridRun(7, grid)

	strategyRepo := mocks.NewStrategyRepository(t)
	strategyRepo.On("GetByID", mock.Anything, uint(1)).Return(baseStrategy(), nil)

	optimizeRepo := mocks.NewOptimizationRepository(t)
	optimizeRepo.On("GetByID", mock.Anything, uint(7)).Return(run, nil).Once()

	var progressSeen []int
	optimizeRepo.On("Update", mock.Anything, mock.MatchedBy(func(r entities.OptimizationRun) bool { return true })).
		Run(func(args mock.Arguments) {
			r := args.Get(1).(entities.OptimizationRun)
			progressSeen = append(progressSeen, r.Progress)
		}).Return(nil)

	backtestRunner := mocks.NewBacktestRunner(t)
	// x=1 -> lower sharpe, x=2 -> tie sharpe with x=3 but higher drawdown, x=3 -> tie sharpe, lower drawdown => winner
	backtestRunner.On("RunEphemeral", mock.Anything, mock.Anything, "BTCUSDT", "1m", mock.Anything, mock.Anything, 1000.0, engine.FillPolicy{}).
		Return(metrics_provider.BacktestMetrics{SharpeRatio: 0.5, MaxDrawdownPct: 1}, nil).Once()
	backtestRunner.On("RunEphemeral", mock.Anything, mock.Anything, "BTCUSDT", "1m", mock.Anything, mock.Anything, 1000.0, engine.FillPolicy{}).
		Return(metrics_provider.BacktestMetrics{SharpeRatio: 1.0, MaxDrawdownPct: 10}, nil).Once()
	backtestRunner.On("RunEphemeral", mock.Anything, mock.Anything, "BTCUSDT", "1m", mock.Anything, mock.Anything, 1000.0, engine.FillPolicy{}).
		Return(metrics_provider.BacktestMetrics{SharpeRatio: 1.0, MaxDrawdownPct: 5}, nil).Once()

	u := usecase.NewOptimizeUseCase(backtestRunner, strategyRepo, optimizeRepo, 0)
	err := u.Run(context.Background(), 7)
	require.NoError(t, err)

	// progressSeen captures every Update call: the initial "running" mark
	// (Progress still 0), then 1/2/3 as each combination completes, then the
	// final "completed" mark (Progress unchanged at 3).
	assert.Equal(t, []int{0, 1, 2, 3, 3}, progressSeen)

	// Inspect the final Update call's persisted state.
	calls := optimizeRepo.Calls
	last := calls[len(calls)-1].Arguments.Get(1).(entities.OptimizationRun)
	assert.Equal(t, entities.OptimizationCompleted, last.Status)
	assert.NotNil(t, last.CompletedAt)

	var bestParams map[string]float64
	require.NoError(t, json.Unmarshal(last.BestConfigJSON, &bestParams))
	assert.Equal(t, map[string]float64{"x": 3}, bestParams, "winner should be x=3: tied Sharpe with x=2, but lower drawdown")

	var bestMetrics metrics_provider.BacktestMetrics
	require.NoError(t, json.Unmarshal(last.BestMetricsJSON, &bestMetrics))
	assert.Equal(t, 1.0, bestMetrics.SharpeRatio)
	assert.Equal(t, 5.0, bestMetrics.MaxDrawdownPct)
}

// TestOptimizeUseCase_Run_OneCombinationFails_ContinuesSearch covers AC#5/AC#7:
// one bad combination must not abort the entire optimization run, and the
// best-config selection only considers successfully-evaluated points.
func TestOptimizeUseCase_Run_OneCombinationFails_ContinuesSearch(t *testing.T) {
	grid := usecase.ParamGrid{"x": {Min: 1, Max: 2, Step: 1}} // 2 combinations

	run := gridRun(8, grid)

	strategyRepo := mocks.NewStrategyRepository(t)
	strategyRepo.On("GetByID", mock.Anything, uint(1)).Return(baseStrategy(), nil)

	optimizeRepo := mocks.NewOptimizationRepository(t)
	optimizeRepo.On("GetByID", mock.Anything, uint(8)).Return(run, nil).Once()
	optimizeRepo.On("Update", mock.Anything, mock.Anything).Return(nil)

	backtestRunner := mocks.NewBacktestRunner(t)
	backtestRunner.On("RunEphemeral", mock.Anything, mock.Anything, "BTCUSDT", "1m", mock.Anything, mock.Anything, 1000.0, engine.FillPolicy{}).
		Return(metrics_provider.BacktestMetrics{}, errors.New("transient replay error")).Once()
	backtestRunner.On("RunEphemeral", mock.Anything, mock.Anything, "BTCUSDT", "1m", mock.Anything, mock.Anything, 1000.0, engine.FillPolicy{}).
		Return(metrics_provider.BacktestMetrics{SharpeRatio: 0.9, MaxDrawdownPct: 3}, nil).Once()

	u := usecase.NewOptimizeUseCase(backtestRunner, strategyRepo, optimizeRepo, 0)
	err := u.Run(context.Background(), 8)
	require.NoError(t, err, "one failed combination must not abort the run")

	calls := optimizeRepo.Calls
	last := calls[len(calls)-1].Arguments.Get(1).(entities.OptimizationRun)
	assert.Equal(t, entities.OptimizationCompleted, last.Status)

	var points []usecase.GridPoint
	require.NoError(t, json.Unmarshal(last.ResultsGridJSON, &points))
	require.Len(t, points, 2)
	assert.Nil(t, points[0].Metrics)
	assert.NotEmpty(t, points[0].Error)
	require.NotNil(t, points[1].Metrics)

	var bestParams map[string]float64
	require.NoError(t, json.Unmarshal(last.BestConfigJSON, &bestParams))
	assert.Equal(t, map[string]float64{"x": 2}, bestParams)
}

// TestOptimizeUseCase_Run_AllCombinationsFail_MarksFailed covers AC#8: if
// every combination errors, the run must be marked "failed" with an
// explanatory message, not silently "completed" with a nonsensical
// best_config.
func TestOptimizeUseCase_Run_AllCombinationsFail_MarksFailed(t *testing.T) {
	grid := usecase.ParamGrid{"x": {Min: 1, Max: 2, Step: 1}}
	run := gridRun(9, grid)

	strategyRepo := mocks.NewStrategyRepository(t)
	strategyRepo.On("GetByID", mock.Anything, uint(1)).Return(baseStrategy(), nil)

	optimizeRepo := mocks.NewOptimizationRepository(t)
	optimizeRepo.On("GetByID", mock.Anything, uint(9)).Return(run, nil).Once()
	optimizeRepo.On("Update", mock.Anything, mock.Anything).Return(nil)

	backtestRunner := mocks.NewBacktestRunner(t)
	backtestRunner.On("RunEphemeral", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(metrics_provider.BacktestMetrics{}, errors.New("boom"))

	u := usecase.NewOptimizeUseCase(backtestRunner, strategyRepo, optimizeRepo, 0)
	err := u.Run(context.Background(), 9)
	require.Error(t, err)

	calls := optimizeRepo.Calls
	last := calls[len(calls)-1].Arguments.Get(1).(entities.OptimizationRun)
	assert.Equal(t, entities.OptimizationFailed, last.Status)
	assert.NotEmpty(t, last.ErrorMessage)
	assert.NotNil(t, last.CompletedAt)
}

// TestOptimizeUseCase_Run_NeverInvokesFullBacktestPersistence documents
// AC#7's "RunEphemeral never writes a BacktestRun row" contract from this
// package's perspective: OptimizeUseCase depends only on the narrow
// BacktestRunner interface (RunEphemeral), which has no method capable of
// persisting a BacktestRun at all - there is no code path in this package
// that could create one, by construction of the interface boundary.
func TestOptimizeUseCase_Run_NeverInvokesFullBacktestPersistence(t *testing.T) {
	grid := usecase.ParamGrid{"x": {Min: 1, Max: 1, Step: 1}}
	run := gridRun(10, grid)

	strategyRepo := mocks.NewStrategyRepository(t)
	strategyRepo.On("GetByID", mock.Anything, uint(1)).Return(baseStrategy(), nil)

	optimizeRepo := mocks.NewOptimizationRepository(t)
	optimizeRepo.On("GetByID", mock.Anything, uint(10)).Return(run, nil).Once()
	optimizeRepo.On("Update", mock.Anything, mock.Anything).Return(nil)

	backtestRunner := mocks.NewBacktestRunner(t)
	backtestRunner.On("RunEphemeral", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(metrics_provider.BacktestMetrics{SharpeRatio: 1}, nil)

	u := usecase.NewOptimizeUseCase(backtestRunner, strategyRepo, optimizeRepo, 0)
	require.NoError(t, u.Run(context.Background(), 10))

	// BacktestRunner only exposes RunEphemeral - assert that's the only
	// method mockery generated for it, which is a compile-time guarantee
	// this test exercises: if a Create/persistence method existed on
	// BacktestRunner, mockery would have generated it and AssertExpectations
	// (registered via t.Cleanup by mocks.NewBacktestRunner) would fail this
	// test for any unmet/unexpected call.
	backtestRunner.AssertExpectations(t)
}

// TestOptimizeUseCase_Run_StrategyLoadFailure_MarksFailed is a basic
// robustness check: an unrelated failure (can't load the base strategy)
// must also route through the same "failed" terminal state, not panic or
// hang.
func TestOptimizeUseCase_Run_StrategyLoadFailure_MarksFailed(t *testing.T) {
	grid := usecase.ParamGrid{"x": {Min: 1, Max: 1, Step: 1}}
	run := gridRun(11, grid)

	strategyRepo := mocks.NewStrategyRepository(t)
	strategyRepo.On("GetByID", mock.Anything, uint(1)).Return(entities.Strategy{}, errors.New("not found"))

	optimizeRepo := mocks.NewOptimizationRepository(t)
	optimizeRepo.On("GetByID", mock.Anything, uint(11)).Return(run, nil).Once()
	optimizeRepo.On("Update", mock.Anything, mock.Anything).Return(nil)

	u := usecase.NewOptimizeUseCase(mocks.NewBacktestRunner(t), strategyRepo, optimizeRepo, 0)
	err := u.Run(context.Background(), 11)
	require.Error(t, err)

	calls := optimizeRepo.Calls
	last := calls[len(calls)-1].Arguments.Get(1).(entities.OptimizationRun)
	assert.Equal(t, entities.OptimizationFailed, last.Status)
}
