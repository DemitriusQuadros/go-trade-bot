package entities

import (
	"time"

	"gorm.io/datatypes"
)

// BacktestStatus is the lifecycle of a BacktestRun. Synchronous runs are
// persisted straight as done; asynchronous runs (B-02) start queued, are
// flipped to running by the worker and end as done or failed.
type BacktestStatus string

const (
	BacktestQueued  BacktestStatus = "queued"
	BacktestRunning BacktestStatus = "running"
	BacktestDone    BacktestStatus = "done"
	BacktestFailed  BacktestStatus = "failed"
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
	// CandidateSourceHash is the sha256 (hex) of the script source a deploy
	// gate run evaluated when that source was a CANDIDATE (not the
	// strategy's saved source) - agents-platform Phase B. Empty for every
	// run on the strategy's own saved source.
	CandidateSourceHash string `json:"candidate_source_hash,omitempty"`
	// Status/ErrorMessage/Timeframe/FillPolicyJSON/StartedAt/FinishedAt
	// support asynchronous runs (B-02). The column default keeps every row
	// written before this field existed reading as done.
	Status         BacktestStatus `json:"status" gorm:"size:16;not null;default:done;index"`
	ErrorMessage   string         `json:"error_message,omitempty"`
	Timeframe      string         `json:"timeframe"`
	FillPolicyJSON datatypes.JSON `json:"-" gorm:"type:jsonb"`
	StartedAt      *time.Time     `json:"started_at,omitempty"`
	FinishedAt     *time.Time     `json:"finished_at,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
}
