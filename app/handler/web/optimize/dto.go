package optimize

import (
	"encoding/json"
	"math"
	"time"

	"go-trade-bot/app/entities"
	usecase "go-trade-bot/app/usecase/optimize"
	"go-trade-bot/internal/metrics_provider"
)

type ParamRangeDTO struct {
	Min  float64 `json:"min"`
	Max  float64 `json:"max"`
	Step float64 `json:"step"`
}

type CreateOptimizationRequestDTO struct {
	StrategyID     uint                     `json:"strategy_id"`
	Symbol         string                   `json:"symbol"`
	Timeframe      string                   `json:"timeframe"`
	StartDate      time.Time                `json:"start_date"`
	EndDate        time.Time                `json:"end_date"`
	InitialCapital float64                  `json:"initial_capital"`
	ParamGrid      map[string]ParamRangeDTO `json:"param_grid"`
}

func (dto CreateOptimizationRequestDTO) ToUseCase() usecase.CreateRequest {
	grid := make(usecase.ParamGrid, len(dto.ParamGrid))
	for k, v := range dto.ParamGrid {
		grid[k] = usecase.ParamRange{Min: v.Min, Max: v.Max, Step: v.Step}
	}
	return usecase.CreateRequest{
		StrategyID:     dto.StrategyID,
		Symbol:         dto.Symbol,
		Timeframe:      dto.Timeframe,
		StartDate:      dto.StartDate,
		EndDate:        dto.EndDate,
		InitialCapital: dto.InitialCapital,
		ParamGrid:      grid,
	}
}

type CreateOptimizationResponse struct {
	ID                uint                        `json:"id"`
	Status            entities.OptimizationStatus `json:"status"`
	TotalCombinations int                         `json:"total_combinations"`
}

func ToCreateResponse(run entities.OptimizationRun) CreateOptimizationResponse {
	return CreateOptimizationResponse{
		ID:                run.ID,
		Status:            run.Status,
		TotalCombinations: run.TotalCombinations,
	}
}

type StatusResponse struct {
	ID                uint                        `json:"id"`
	Status            entities.OptimizationStatus `json:"status"`
	Progress          int                         `json:"progress"`
	TotalCombinations int                         `json:"total_combinations"`
	BestConfig        any                         `json:"best_config"`
	BestMetrics       any                         `json:"best_metrics"`
	ErrorMessage      *string                     `json:"error_message"`
}

// ToStatusResponse is the GET /optimize/{id} poll-target shape (backend-01).
// best_config/best_metrics are deliberately left null until status ==
// "completed" - no "best so far" is exposed mid-run (spec AC#4).
func ToStatusResponse(run entities.OptimizationRun) StatusResponse {
	resp := StatusResponse{
		ID:                run.ID,
		Status:            run.Status,
		Progress:          run.Progress,
		TotalCombinations: run.TotalCombinations,
	}

	if run.Status == entities.OptimizationCompleted {
		if len(run.BestConfigJSON) > 0 {
			var cfg any
			if err := json.Unmarshal(run.BestConfigJSON, &cfg); err == nil {
				resp.BestConfig = cfg
			}
		}
		if len(run.BestMetricsJSON) > 0 {
			var m any
			if err := json.Unmarshal(run.BestMetricsJSON, &m); err == nil {
				resp.BestMetrics = m
			}
		}
	}

	if run.Status == entities.OptimizationFailed && run.ErrorMessage != "" {
		msg := run.ErrorMessage
		resp.ErrorMessage = &msg
	}

	return resp
}

type GridPointResponse struct {
	Params  map[string]float64 `json:"params"`
	Metrics any                `json:"metrics"`
}

type ResultsResponse struct {
	ID          uint                `json:"id"`
	BestConfig  any                 `json:"best_config"`
	BestMetrics any                 `json:"best_metrics"`
	Grid        []GridPointResponse `json:"grid"`
}

func ToResultsResponse(run entities.OptimizationRun) (ResultsResponse, error) {
	resp := ResultsResponse{ID: run.ID}

	if len(run.BestConfigJSON) > 0 {
		if err := json.Unmarshal(run.BestConfigJSON, &resp.BestConfig); err != nil {
			return ResultsResponse{}, err
		}
	}
	if len(run.BestMetricsJSON) > 0 {
		if err := json.Unmarshal(run.BestMetricsJSON, &resp.BestMetrics); err != nil {
			return ResultsResponse{}, err
		}
	}

	if len(run.ResultsGridJSON) > 0 {
		var points []usecase.GridPoint
		if err := json.Unmarshal(run.ResultsGridJSON, &points); err != nil {
			return ResultsResponse{}, err
		}
		resp.Grid = make([]GridPointResponse, len(points))
		for i, p := range points {
			resp.Grid[i] = GridPointResponse{Params: p.Params, Metrics: sanitizeMetrics(p.Metrics)}
		}
	}

	return resp, nil
}

// sanitizeMetrics guards against +Inf ProfitFactor values breaking JSON
// encoding, matching the existing convention in
// app/handler/web/backtest/dto.go's ToRunResponse.
func sanitizeMetrics(m *metrics_provider.BacktestMetrics) any {
	if m == nil {
		return nil
	}
	if math.IsInf(m.ProfitFactor, 1) {
		clone := *m
		clone.ProfitFactor = math.MaxFloat64
		return clone
	}
	return m
}

type ListItemResponse struct {
	ID          uint                        `json:"id"`
	Status      entities.OptimizationStatus `json:"status"`
	CreatedAt   time.Time                   `json:"created_at"`
	CompletedAt *time.Time                  `json:"completed_at"`
	BestConfig  any                         `json:"best_config"`
	BestMetrics any                         `json:"best_metrics"`
}

func ToListItemResponse(run entities.OptimizationRun) ListItemResponse {
	item := ListItemResponse{
		ID:          run.ID,
		Status:      run.Status,
		CreatedAt:   run.CreatedAt,
		CompletedAt: run.CompletedAt,
	}
	if len(run.BestConfigJSON) > 0 {
		var cfg any
		if err := json.Unmarshal(run.BestConfigJSON, &cfg); err == nil {
			item.BestConfig = cfg
		}
	}
	if len(run.BestMetricsJSON) > 0 {
		var m any
		if err := json.Unmarshal(run.BestMetricsJSON, &m); err == nil {
			item.BestMetrics = m
		}
	}
	return item
}
