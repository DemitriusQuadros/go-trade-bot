package backtest

import (
	"encoding/json"
	"math"
	"time"

	"go-trade-bot/app/engine"
	"go-trade-bot/app/entities"
	usecase "go-trade-bot/app/usecase/backtest"
	"go-trade-bot/internal/metrics_provider"
)

type RunRequestDTO struct {
	StrategyID     uint              `json:"strategy_id"`
	Symbol         string            `json:"symbol"`
	Timeframe      string            `json:"timeframe"`
	StartDate      time.Time         `json:"start_date"`
	EndDate        time.Time         `json:"end_date"`
	InitialCapital float64           `json:"initial_capital"`
	FillPolicy     engine.FillPolicy `json:"fill_policy"`
}

func (dto RunRequestDTO) ToUseCase() usecase.RunRequest {
	return usecase.RunRequest{
		StrategyID:     dto.StrategyID,
		Symbol:         dto.Symbol,
		Timeframe:      dto.Timeframe,
		StartDate:      dto.StartDate,
		EndDate:        dto.EndDate,
		InitialCapital: dto.InitialCapital,
		FillPolicy:     dto.FillPolicy,
	}
}

type WalkForwardRequestDTO struct {
	StrategyID     uint              `json:"strategy_id"`
	Symbol         string            `json:"symbol"`
	Timeframe      string            `json:"timeframe"`
	StartDate      time.Time         `json:"start_date"`
	EndDate        time.Time         `json:"end_date"`
	InitialCapital float64           `json:"initial_capital"`
	TrainMonths    int               `json:"train_months"`
	TestMonths     int               `json:"test_months"`
	StepMonths     int               `json:"step_months"`
	FillPolicy     engine.FillPolicy `json:"fill_policy"`
}

func (dto WalkForwardRequestDTO) ToUseCase() usecase.WalkForwardRequest {
	return usecase.WalkForwardRequest{
		StrategyID:     dto.StrategyID,
		Symbol:         dto.Symbol,
		Timeframe:      dto.Timeframe,
		StartDate:      dto.StartDate,
		EndDate:        dto.EndDate,
		InitialCapital: dto.InitialCapital,
		TrainMonths:    dto.TrainMonths,
		TestMonths:     dto.TestMonths,
		StepMonths:     dto.StepMonths,
		FillPolicy:     dto.FillPolicy,
	}
}

type BacktestRunResponse struct {
	ID             uint                          `json:"id"`
	StrategyID     uint                          `json:"strategy_id"`
	Symbol         string                        `json:"symbol"`
	StartDate      time.Time                     `json:"start_date"`
	EndDate        time.Time                     `json:"end_date"`
	IsWalkForward  bool                          `json:"is_walk_forward"`
	Sharpe         float64                       `json:"sharpe"`
	MaxDrawdownPct float64                       `json:"max_drawdown_pct"`
	WinRatePct     float64                       `json:"win_rate_pct"`
	ProfitFactor   any                           `json:"profit_factor"`
	TotalTrades    int                           `json:"total_trades"`
	TotalReturnPct float64                       `json:"total_return_pct"`
	Passed         bool                          `json:"passed"`
	HTMLReportPath string                        `json:"html_report_path"`
	EquityCurve    []metrics_provider.EquityPoint `json:"equity_curve"`
	TradeLog       any                           `json:"trade_log,omitempty"`
	CreatedAt      time.Time                     `json:"created_at"`
}

func ToRunResponse(run entities.BacktestRun, includeTradeLog bool) BacktestRunResponse {
	var pf any = run.ProfitFactor
	if math.IsInf(run.ProfitFactor, 1) || run.ProfitFactor == math.MaxFloat64 || run.ProfitFactor >= 1e15 {
		pf = "Infinity"
	}

	res := BacktestRunResponse{
		ID:             run.ID,
		StrategyID:     run.StrategyID,
		Symbol:         run.Symbol,
		StartDate:      run.StartDate,
		EndDate:        run.EndDate,
		IsWalkForward:  run.IsWalkForward,
		Sharpe:         run.Sharpe,
		MaxDrawdownPct: run.MaxDrawdownPct,
		WinRatePct:     run.WinRatePct,
		ProfitFactor:   pf,
		TotalTrades:    run.TotalTrades,
		TotalReturnPct: run.TotalReturnPct,
		Passed:         run.Passed,
		HTMLReportPath: run.HTMLReportPath,
		EquityCurve:    []metrics_provider.EquityPoint{},
		CreatedAt:      run.CreatedAt,
	}

	if len(run.MetricsJSON) > 0 {
		var m metrics_provider.BacktestMetrics
		if err := json.Unmarshal(run.MetricsJSON, &m); err == nil {
			if m.EquityCurve != nil {
				res.EquityCurve = m.EquityCurve
			}
		}
	}

	if includeTradeLog && len(run.TradeLogJSON) > 0 {
		var parsed any
		if err := json.Unmarshal(run.TradeLogJSON, &parsed); err == nil {
			res.TradeLog = parsed
		}
	}

	return res
}
