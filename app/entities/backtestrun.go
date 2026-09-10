package entities

import (
	"time"

	"gorm.io/datatypes"
)

type BacktestRun struct {
	ID             uint           `gorm:"primaryKey" json:"id"`
	StrategyID     uint           `json:"strategy_id"`
	Strategy       Strategy       `gorm:"foreignKey:StrategyID" json:"strategy,omitempty"`
	Symbol         string         `json:"symbol"`
	StartDate      time.Time      `json:"start_date"`
	EndDate        time.Time      `json:"end_date"`
	IsWalkForward  bool           `json:"is_walk_forward"`
	Sharpe         float64        `json:"sharpe"`
	MaxDrawdownPct float64        `json:"max_drawdown_pct"`
	WinRatePct     float64        `json:"win_rate_pct"`
	ProfitFactor   float64        `json:"profit_factor"`
	TotalTrades    int            `json:"total_trades"`
	TotalReturnPct float64        `json:"total_return_pct"`
	Passed         bool           `json:"passed"`
	HTMLReportPath string         `json:"html_report_path"`
	TradeLogJSON   datatypes.JSON `json:"trade_log_json"`
	InitialCapital float64        `json:"initial_capital"`
	// MonteCarloJSON caches the most recently computed engine.MonteCarloResult
	// for this run.
	MonteCarloJSON datatypes.JSON `json:"monte_carlo_json,omitempty" gorm:"type:jsonb"`
	// MetricsJSON persists the full metrics_provider.BacktestMetrics computed
	// for this run (including EquityCurve).
	MetricsJSON datatypes.JSON `json:"metrics_json,omitempty" gorm:"type:jsonb"`
	// ExecutionTraceJSON persists the per-cycle []script.TraceRecord for a
	// script strategy's run (backend-07). Empty for native Go strategies and
	// for walk-forward runs (no aggregation).
	ExecutionTraceJSON datatypes.JSON `json:"execution_trace_json,omitempty" gorm:"type:jsonb"`
	CreatedAt          time.Time      `json:"created_at"`
}
