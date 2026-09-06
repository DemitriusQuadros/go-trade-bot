package apiclient

import (
	"encoding/json"
	"time"
)

// AccountView represents account balance and margin details.
type AccountView struct {
	ID              int64     `json:"id"`
	Amount          float32   `json:"amount"`
	AvailableOrders int64     `json:"available_orders"`
	Currency        string    `json:"currency"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// StrategyConfigurationView holds embedded cycle and config blob.
type StrategyConfigurationView struct {
	Cycle         int             `json:"cycle"`
	Configuration json.RawMessage `json:"configuration"`
}

// StrategyView represents a configured strategy.
type StrategyView struct {
	ID                    uint                      `json:"id"`
	Name                  string                    `json:"name"`
	Description           string                    `json:"description"`
	Algorithm             string                    `json:"algorithm"`
	StrategyName          string                    `json:"strategy_name"`
	Status                string                    `json:"status"`
	Mode                  string                    `json:"mode"`
	MonitoredSymbols      []string                  `json:"monitored_symbols"`
	StrategyConfiguration StrategyConfigurationView `json:"strategy_configuration"`
	CreatedAt             time.Time                 `json:"created_at"`
	UpdatedAt             time.Time                 `json:"updated_at"`
}

// StrategyPerformanceView represents strategy performance aggregated by symbol.
type StrategyPerformanceView struct {
	Name   string  `json:"name"`
	Symbol string  `json:"symbol"`
	Profit float64 `json:"profit"`
	Trades int     `json:"trades"`
}

// TickerPriceView represents price info for a ticker.
type TickerPriceView struct {
	Symbol string  `json:"symbol"`
	Price  float64 `json:"price"`
}

// CandleView represents an individual OHLCV candle.
type CandleView struct {
	OpenTime  time.Time `json:"open_time"`
	Open      float64   `json:"open"`
	High      float64   `json:"high"`
	Low       float64   `json:"low"`
	Close     float64   `json:"close"`
	Volume    float64   `json:"volume"`
	CloseTime time.Time `json:"close_time"`
}

// OrderView represents a trade order belonging to a signal.
type OrderView struct {
	ID              uint      `json:"id"`
	SignalID        uint      `json:"signal_id"`
	BrokerOrderID   string    `json:"broker_order_id"`
	StopLossOrderID string    `json:"stop_loss_order_id"`
	StopLossPrice   float32   `json:"stop_loss_price"`
	EntryPrice      float32   `json:"entry_price"`
	ExitPrice       float32   `json:"exit_price"`
	Quantity        float32   `json:"quantity"`
	InvestedAmount  float32   `json:"invested_amount"`
	MarginType      string    `json:"margin_type"`
	EntryFee        float32   `json:"entry_fee"`
	ExitFee         float32   `json:"exit_fee"`
	Leverage        float32   `json:"leverage"`
	ExecutedQty     float32   `json:"executed_qty"`
	IsClosing       bool      `json:"is_closing"`
	Profit          float32   `json:"profit"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// SignalView represents a trading signal with its associated orders.
type SignalView struct {
	ID         uint         `json:"id"`
	Symbol     string       `json:"symbol"`
	StrategyID uint         `json:"strategy_id"`
	Strategy   StrategyView `json:"strategy"`
	CreatedAt  time.Time    `json:"created_at"`
	UpdatedAt  time.Time    `json:"updated_at"`
	Status     string       `json:"status"`
	Orders     []OrderView  `json:"orders"`
}

// BacktestRunView represents the outcome of a backtest run.
type BacktestRunView struct {
	ID             uint      `json:"id"`
	StrategyID     uint      `json:"strategy_id"`
	Symbol         string    `json:"symbol"`
	StartDate      time.Time `json:"start_date"`
	EndDate        time.Time `json:"end_date"`
	IsWalkForward  bool      `json:"is_walk_forward"`
	Sharpe         float64   `json:"sharpe"`
	MaxDrawdownPct float64   `json:"max_drawdown_pct"`
	WinRatePct     float64   `json:"win_rate_pct"`
	ProfitFactor   any       `json:"profit_factor"` // float64 or string "Infinity"
	TotalTrades    int       `json:"total_trades"`
	TotalReturnPct float64   `json:"total_return_pct"`
	Passed         bool      `json:"passed"`
	HTMLReportPath string    `json:"html_report_path"`
	TradeLog       any       `json:"trade_log,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

// ExecutionEventView represents an event emitted by strategy execution or system.
type ExecutionEventView struct {
	Timestamp    time.Time `json:"timestamp"`
	StrategyName string    `json:"strategy_name"`
	Symbol       string    `json:"symbol"`
	EventType    string    `json:"event_type"`
	Message      string    `json:"message"`
}

// RunBacktestRequest represents parameters to launch a single backtest.
type RunBacktestRequest struct {
	StrategyID     uint      `json:"strategy_id"`
	Symbol         string    `json:"symbol"`
	Timeframe      string    `json:"timeframe"`
	StartDate      time.Time `json:"start_date"`
	EndDate        time.Time `json:"end_date"`
	InitialCapital float64   `json:"initial_capital,omitempty"`
}

// TimeRange represents a time range for walk-forward validation.
type TimeRange struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

// WalkForwardRequest represents parameters to launch walk-forward validation.
type WalkForwardRequest struct {
	StrategyID      uint       `json:"strategy_id"`
	Symbol          string     `json:"symbol"`
	Timeframe       string     `json:"timeframe"`
	StartDate       time.Time  `json:"start_date,omitempty"`
	EndDate         time.Time  `json:"end_date,omitempty"`
	TotalRange      *TimeRange `json:"total_range,omitempty"`
	TrainWindowDays int        `json:"train_window_days,omitempty"`
	TestWindowDays  int        `json:"test_window_days,omitempty"`
	StepDays        int        `json:"step_days,omitempty"`
	TrainMonths     int        `json:"train_months,omitempty"`
	TestMonths      int        `json:"test_months,omitempty"`
	StepMonths      int        `json:"step_months,omitempty"`
	InitialCapital  float64    `json:"initial_capital,omitempty"`
}

// --- Optimization (Page 7) --------------------------------------------------

// ParamRange represents a single parameter's sweep range for a hyperparameter
// optimization grid search, per backend-01's { "min", "max", "step" } shape
// (NOT "from"/"to" — reconciled against the final backend-01 contract).
type ParamRange struct {
	Min  float64 `json:"min"`
	Max  float64 `json:"max"`
	Step float64 `json:"step"`
}

// RunOptimizationRequest represents parameters to launch a hyperparameter
// optimization grid search via POST /optimize.
type RunOptimizationRequest struct {
	StrategyID     uint                  `json:"strategy_id"`
	Symbol         string                `json:"symbol"`
	Timeframe      string                `json:"timeframe"`
	StartDate      time.Time             `json:"start_date"`
	EndDate        time.Time             `json:"end_date"`
	InitialCapital float64               `json:"initial_capital,omitempty"`
	ParamGrid      map[string]ParamRange `json:"param_grid"`
}

// BacktestMetricsView mirrors internal/metrics_provider.BacktestMetrics for a
// single grid-search combination's computed performance.
type BacktestMetricsView struct {
	SharpeRatio    float64 `json:"sharpe_ratio"`
	MaxDrawdownPct float64 `json:"max_drawdown_pct"`
	WinRatePct     float64 `json:"win_rate_pct"`
	ProfitFactor   any     `json:"profit_factor"` // float64 or string "Infinity", matching BacktestRunView's convention
	TotalTrades    int     `json:"total_trades"`
	TotalReturnPct float64 `json:"total_return_pct"`
}

// OptimizationRunView represents the status/progress of an optimization run,
// as returned by POST /optimize (202) and GET /optimize/{id}. It deliberately
// does NOT carry the full per-combination grid — per backend-01, that lives
// behind the separate GET /optimize/{id}/results endpoint (see
// OptimizationResultsView), which 409s until status == "completed".
type OptimizationRunView struct {
	ID                uint                 `json:"id"`
	StrategyID        uint                 `json:"strategy_id"`
	Status            string               `json:"status"` // "pending" | "running" | "completed" | "failed"
	Progress          int                  `json:"progress"`
	TotalCombinations int                  `json:"total_combinations"`
	BestConfig        map[string]float64   `json:"best_config"`
	BestMetrics       *BacktestMetricsView `json:"best_metrics"`
	ErrorMessage      string               `json:"error_message"`
}

// OptimizationResultItemView represents one (params, metrics) combination in
// an optimization run's full grid. Metrics is nil when this specific
// combination errored out (backend-01 AC#7 — one bad combination doesn't
// abort the whole run).
type OptimizationResultItemView struct {
	Params  map[string]float64   `json:"params"`
	Metrics *BacktestMetricsView `json:"metrics"`
}

// OptimizationResultsView represents the full per-combination grid for a
// completed optimization run, as returned by GET /optimize/{id}/results.
type OptimizationResultsView struct {
	ID          uint                         `json:"id"`
	BestConfig  map[string]float64           `json:"best_config"`
	BestMetrics BacktestMetricsView          `json:"best_metrics"`
	Grid        []OptimizationResultItemView `json:"grid"`
}

// --- Strategy performance history (Page 2 detail overlay sparkline) --------

// PerformanceHistoryPointView represents one time-bucketed P&L snapshot for a
// (strategy, symbol) pair, as returned by
// GET /strategy/{id}/performance/history. Rows are sparse — a bucket with
// zero closed trades has no row at all (backend-05 AC#5) — callers must not
// assume one row per calendar day/week/month in range.
type PerformanceHistoryPointView struct {
	PeriodStart time.Time `json:"period_start"`
	PeriodEnd   time.Time `json:"period_end"`
	Profit      float64   `json:"profit"`
	Trades      int       `json:"trades"`
}
