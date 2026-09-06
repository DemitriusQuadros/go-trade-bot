package optimize

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"go-trade-bot/app/engine"
	"go-trade-bot/app/entities"
	"go-trade-bot/internal/metrics_provider"

	"gorm.io/datatypes"
)

// DefaultMaxCombinations is backend-02's hard cap on grid size (its own
// "Grid size limit and parallelism" section): at Phase 2's realistic
// single-symbol-year backtest runtime, 500 sequential runs is already a
// multi-hour job - the outer edge of what a "run it and check back later"
// asynchronous job is comfortable for on the homelab. Configurable via
// NewOptimizeUseCase's maxCombinations parameter, but this is the default
// when the caller passes 0.
const DefaultMaxCombinations = 500

var (
	ErrMissingStrategyID = errors.New("strategy_id is required")
	ErrMissingSymbol     = errors.New("symbol is required")
	ErrEmptyParamGrid    = errors.New("param_grid must contain at least one parameter")
	ErrInvalidStep       = errors.New("param_grid step must be greater than zero")
	// ErrGridTooLarge is returned by Create when the Cartesian product of the
	// requested param_grid exceeds maxCombinations - backend-01's handler
	// maps this to HTTP 413, rejecting the request before any task is
	// enqueued (spec AC#3).
	ErrGridTooLarge = errors.New("param_grid combination count exceeds the maximum allowed")
	ErrRunNotFound  = errors.New("optimization run not found")
)

// ParamRange describes one config field's search range: min, min+step,
// min+2*step, ..., <= max.
type ParamRange struct {
	Min  float64 `json:"min"`
	Max  float64 `json:"max"`
	Step float64 `json:"step"`
}

// ParamGrid maps a strategy Configuration JSON field name (e.g. "rsi_period")
// to the range of values to search over it.
type ParamGrid map[string]ParamRange

// GridPoint is one evaluated combination's result - either populated Metrics
// (success) or a populated Error (that combination's evaluation failed, but
// the search continues - spec AC#7).
type GridPoint struct {
	Params  map[string]float64                `json:"params"`
	Metrics *metrics_provider.BacktestMetrics `json:"metrics"`
	Error   string                            `json:"error,omitempty"`
}

// CreateRequest is the input to Create - the validated, use-case-shaped
// version of backend-01's POST /optimize body.
type CreateRequest struct {
	StrategyID     uint
	Symbol         string
	Timeframe      string
	StartDate      time.Time
	EndDate        time.Time
	InitialCapital float64
	ParamGrid      ParamGrid
}

type StrategyRepository interface {
	GetByID(ctx context.Context, id uint) (entities.Strategy, error)
}

type OptimizationRepository interface {
	Create(ctx context.Context, run *entities.OptimizationRun) error
	GetByID(ctx context.Context, id uint) (entities.OptimizationRun, error)
	Update(ctx context.Context, run entities.OptimizationRun) error
	ListByStrategy(ctx context.Context, strategyID uint) ([]entities.OptimizationRun, error)
}

// BacktestRunner is the narrow slice of app/usecase/backtest.BacktestUseCase
// this package depends on - just the ephemeral, no-artifact evaluation
// method (backend-02's central design problem: grid search must not persist
// a BacktestRun row per combination).
type BacktestRunner interface {
	RunEphemeral(
		ctx context.Context,
		baseStrategy entities.Strategy,
		symbol, timeframe string,
		from, to time.Time,
		initialCapital float64,
		fillPolicy engine.FillPolicy,
	) (metrics_provider.BacktestMetrics, error)
}

type OptimizeUseCase struct {
	backtestRunner  BacktestRunner
	strategyRepo    StrategyRepository
	optimizeRepo    OptimizationRepository
	maxCombinations int
}

func NewOptimizeUseCase(
	backtestRunner BacktestRunner,
	strategyRepo StrategyRepository,
	optimizeRepo OptimizationRepository,
	maxCombinations int,
) *OptimizeUseCase {
	if maxCombinations <= 0 {
		maxCombinations = DefaultMaxCombinations
	}
	return &OptimizeUseCase{
		backtestRunner:  backtestRunner,
		strategyRepo:    strategyRepo,
		optimizeRepo:    optimizeRepo,
		maxCombinations: maxCombinations,
	}
}

func (u *OptimizeUseCase) MaxCombinations() int {
	return u.maxCombinations
}

// ExpandGrid computes the Cartesian product of a ParamGrid, deterministically
// ordered (keys sorted lexicographically, then nested loops in that key
// order) so combination order - and therefore GridPoint index - is
// reproducible across runs and easy to reason about in tests.
func ExpandGrid(grid ParamGrid) ([]map[string]float64, error) {
	if len(grid) == 0 {
		return nil, ErrEmptyParamGrid
	}

	keys := make([]string, 0, len(grid))
	for k := range grid {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	valueSets := make([][]float64, len(keys))
	for i, k := range keys {
		r := grid[k]
		if r.Step <= 0 {
			return nil, fmt.Errorf("%w: key %q", ErrInvalidStep, k)
		}
		var values []float64
		// Small epsilon guards against float accumulation stopping one step
		// short of Max (e.g. 1 + 4*0.5 should include 3.0).
		epsilon := r.Step / 1e6
		for v := r.Min; v <= r.Max+epsilon; v += r.Step {
			values = append(values, v)
		}
		if len(values) == 0 {
			values = []float64{r.Min}
		}
		valueSets[i] = values
	}

	total := 1
	for _, vs := range valueSets {
		total *= len(vs)
	}

	combos := make([]map[string]float64, 0, total)
	indices := make([]int, len(keys))
	for {
		combo := make(map[string]float64, len(keys))
		for i, k := range keys {
			combo[k] = valueSets[i][indices[i]]
		}
		combos = append(combos, combo)

		// odometer increment
		pos := len(keys) - 1
		for pos >= 0 {
			indices[pos]++
			if indices[pos] < len(valueSets[pos]) {
				break
			}
			indices[pos] = 0
			pos--
		}
		if pos < 0 {
			break
		}
	}

	return combos, nil
}

// CountCombinations is a cheap AC#2/AC#3 helper (backend-01's 413 check
// before enqueueing) that avoids materializing the full combination slice
// just to measure its length.
func CountCombinations(grid ParamGrid) (int, error) {
	combos, err := ExpandGrid(grid)
	if err != nil {
		return 0, err
	}
	return len(combos), nil
}

// Create validates a grid-search request, computes its total combination
// count, and persists a "pending" OptimizationRun row. It does NOT enqueue
// the asynq task or run the search - that's the caller's (backend-01's HTTP
// handler's) responsibility, mirroring how app/workers/strategy is a
// separate concern from app/usecase/strategy.
func (u *OptimizeUseCase) Create(ctx context.Context, req CreateRequest) (entities.OptimizationRun, error) {
	if req.StrategyID == 0 {
		return entities.OptimizationRun{}, ErrMissingStrategyID
	}
	if req.Symbol == "" {
		return entities.OptimizationRun{}, ErrMissingSymbol
	}
	if req.Timeframe == "" {
		req.Timeframe = "1m"
	}
	if req.InitialCapital <= 0 {
		req.InitialCapital = 1000.0
	}

	total, err := CountCombinations(req.ParamGrid)
	if err != nil {
		return entities.OptimizationRun{}, err
	}
	if total > u.maxCombinations {
		return entities.OptimizationRun{}, fmt.Errorf("%w: %d combinations requested, max is %d", ErrGridTooLarge, total, u.maxCombinations)
	}

	gridBytes, err := json.Marshal(req.ParamGrid)
	if err != nil {
		return entities.OptimizationRun{}, fmt.Errorf("failed to marshal param_grid: %w", err)
	}

	run := entities.OptimizationRun{
		StrategyID:        req.StrategyID,
		Symbol:            req.Symbol,
		Timeframe:         req.Timeframe,
		StartDate:         req.StartDate,
		EndDate:           req.EndDate,
		InitialCapital:    req.InitialCapital,
		ParamGridJSON:     datatypes.JSON(gridBytes),
		Status:            entities.OptimizationPending,
		Progress:          0,
		TotalCombinations: total,
		CreatedAt:         time.Now().UTC(),
	}

	if err := u.optimizeRepo.Create(ctx, &run); err != nil {
		return entities.OptimizationRun{}, fmt.Errorf("failed to persist optimization run: %w", err)
	}

	return run, nil
}

func (u *OptimizeUseCase) GetByID(ctx context.Context, id uint) (entities.OptimizationRun, error) {
	return u.optimizeRepo.GetByID(ctx, id)
}

func (u *OptimizeUseCase) ListByStrategy(ctx context.Context, strategyID uint) ([]entities.OptimizationRun, error) {
	return u.optimizeRepo.ListByStrategy(ctx, strategyID)
}

// Run executes the full grid search SEQUENTIALLY (backend-02's explicit,
// homelab-memory-motivated decision - never two RunEphemeral calls in
// flight concurrently), updating optimizeRepo's persisted
// OptimizationRun.Progress after every combination. Called from the asynq
// task handler (app/handler/tasks/optimize), not directly from the HTTP
// handler.
func (u *OptimizeUseCase) Run(ctx context.Context, runID uint) error {
	run, err := u.optimizeRepo.GetByID(ctx, runID)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrRunNotFound, err)
	}

	run.Status = entities.OptimizationRunning
	if err := u.optimizeRepo.Update(ctx, run); err != nil {
		return fmt.Errorf("failed to mark optimization run running: %w", err)
	}

	strat, err := u.strategyRepo.GetByID(ctx, run.StrategyID)
	if err != nil {
		return u.fail(ctx, run, fmt.Sprintf("failed to load strategy %d: %v", run.StrategyID, err))
	}

	var grid ParamGrid
	if err := json.Unmarshal(run.ParamGridJSON, &grid); err != nil {
		return u.fail(ctx, run, fmt.Sprintf("failed to parse param_grid: %v", err))
	}

	combos, err := ExpandGrid(grid)
	if err != nil {
		return u.fail(ctx, run, fmt.Sprintf("failed to expand param_grid: %v", err))
	}

	var baseConfig map[string]any
	if len(strat.StrategyConfiguration.Configuration) > 0 {
		if err := json.Unmarshal(strat.StrategyConfiguration.Configuration, &baseConfig); err != nil {
			baseConfig = map[string]any{}
		}
	} else {
		baseConfig = map[string]any{}
	}

	gridPoints := make([]GridPoint, 0, len(combos))
	successCount := 0

	for i, combo := range combos {
		cfgCopy := make(map[string]any, len(baseConfig)+len(combo))
		for k, v := range baseConfig {
			cfgCopy[k] = v
		}
		for k, v := range combo {
			cfgCopy[k] = v
		}

		cfgBytes, marshalErr := json.Marshal(cfgCopy)
		gp := GridPoint{Params: combo}

		if marshalErr != nil {
			gp.Error = fmt.Sprintf("failed to marshal merged config: %v", marshalErr)
		} else {
			clonedStrategy := strat
			clonedStrategy.StrategyConfiguration.Configuration = datatypes.JSON(cfgBytes)

			m, runErr := u.backtestRunner.RunEphemeral(
				ctx,
				clonedStrategy,
				run.Symbol,
				run.Timeframe,
				run.StartDate,
				run.EndDate,
				run.InitialCapital,
				engine.FillPolicy{},
			)
			if runErr != nil {
				gp.Error = runErr.Error()
			} else {
				metricsCopy := m
				gp.Metrics = &metricsCopy
				successCount++
			}
		}

		gridPoints = append(gridPoints, gp)

		run.Progress = i + 1
		resultsBytes, _ := json.Marshal(gridPoints)
		run.ResultsGridJSON = datatypes.JSON(resultsBytes)
		if err := u.optimizeRepo.Update(ctx, run); err != nil {
			return fmt.Errorf("failed to persist optimization progress at combination %d: %w", i, err)
		}
	}

	if successCount == 0 {
		return u.fail(ctx, run, "every grid combination failed to evaluate; see results grid for per-combination errors")
	}

	bestIdx := -1
	for i, gp := range gridPoints {
		if gp.Metrics == nil {
			continue
		}
		if bestIdx == -1 {
			bestIdx = i
			continue
		}
		best := gridPoints[bestIdx].Metrics
		if isBetter(gp.Metrics, best) {
			bestIdx = i
		}
	}

	bestParamsBytes, _ := json.Marshal(gridPoints[bestIdx].Params)
	bestMetricsBytes, _ := json.Marshal(gridPoints[bestIdx].Metrics)

	now := time.Now().UTC()
	run.BestConfigJSON = datatypes.JSON(bestParamsBytes)
	run.BestMetricsJSON = datatypes.JSON(bestMetricsBytes)
	run.Status = entities.OptimizationCompleted
	run.CompletedAt = &now

	if err := u.optimizeRepo.Update(ctx, run); err != nil {
		return fmt.Errorf("failed to persist completed optimization run: %w", err)
	}
	return nil
}

// isBetter implements AC#3's tie-break rule: highest Sharpe Ratio wins;
// ties broken by lowest MaxDrawdownPct.
func isBetter(candidate, current *metrics_provider.BacktestMetrics) bool {
	if candidate.SharpeRatio != current.SharpeRatio {
		return candidate.SharpeRatio > current.SharpeRatio
	}
	return candidate.MaxDrawdownPct < current.MaxDrawdownPct
}

func (u *OptimizeUseCase) fail(ctx context.Context, run entities.OptimizationRun, message string) error {
	now := time.Now().UTC()
	run.Status = entities.OptimizationFailed
	run.ErrorMessage = message
	run.CompletedAt = &now
	if updErr := u.optimizeRepo.Update(ctx, run); updErr != nil {
		return fmt.Errorf("optimization failed (%s) and failed to persist failure state: %w", message, updErr)
	}
	return errors.New(message)
}
