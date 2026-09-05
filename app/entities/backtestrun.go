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
	CreatedAt      time.Time      `json:"created_at"`
}
