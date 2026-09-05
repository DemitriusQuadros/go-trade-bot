package metrics_provider

import "time"

// TradeLogEntry records the execution details of a single trade.
type TradeLogEntry struct {
	Symbol     string    `json:"symbol"`
	EntryTime  time.Time `json:"entry_time"`
	EntryPrice float64   `json:"entry_price"`
	ExitTime   time.Time `json:"exit_time"`
	ExitPrice  float64   `json:"exit_price"`
	Quantity   float64   `json:"quantity"`
	Profit     float64   `json:"profit"` // net of fees
	ExitReason string    `json:"exit_reason"` // "take_profit" | "stop_loss" | "manual" | "band_cross" | "open_at_end"
}

// EquityPoint captures portfolio value at a point in time.
type EquityPoint struct {
	Time  time.Time `json:"time"`
	Value float64   `json:"value"`
}

// BacktestMetrics summarizes the performance of a backtest run.
type BacktestMetrics struct {
	SharpeRatio      float64       `json:"sharpe_ratio"`
	MaxDrawdownPct   float64       `json:"max_drawdown_pct"`
	WinRatePct       float64       `json:"win_rate_pct"`
	ProfitFactor     float64       `json:"profit_factor"` // +Inf if zero losing trades
	TotalTrades      int           `json:"total_trades"`
	AvgTradeDuration time.Duration `json:"avg_trade_duration"`
	TotalReturnPct   float64       `json:"total_return_pct"`
	EquityCurve      []EquityPoint `json:"equity_curve"`
}

// MetricsProvider defines the interface for calculating trading performance metrics.
type MetricsProvider interface {
	Compute(trades []TradeLogEntry, startingBalance float64, periodsPerYear float64) BacktestMetrics
}
