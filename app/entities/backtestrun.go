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
	// InitialCapital is additive (backend-03): Run()/RunWalkForward() always
	// received an InitialCapital in their request, but never persisted it -
	// backend-03's Monte Carlo simulation needs the run's starting balance to
	// recompute BacktestMetrics per shuffle via the same MetricsProvider used
	// originally, and there was no column to read it back from. Pre-existing
	// rows read back as 0, in which case RunMonteCarlo falls back to the same
	// 1000.0 default used elsewhere in this package.
	InitialCapital float64 `json:"initial_capital"`
	// MonteCarloJSON caches the most recently computed engine.MonteCarloResult
	// for this run (backend-03) - POST /backtest/{id}/montecarlo persists
	// here so a repeated GET doesn't re-run potentially thousands of
	// Compute() calls.
	MonteCarloJSON datatypes.JSON `json:"monte_carlo_json,omitempty" gorm:"type:jsonb"`
	CreatedAt      time.Time      `json:"created_at"`
}
