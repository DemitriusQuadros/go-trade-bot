package entities

import (
	"time"

	"gorm.io/datatypes"
)

type OptimizationStatus string

const (
	OptimizationPending   OptimizationStatus = "pending"
	OptimizationRunning   OptimizationStatus = "running"
	OptimizationCompleted OptimizationStatus = "completed"
	OptimizationFailed    OptimizationStatus = "failed"
)

// OptimizationRun persists one hyperparameter grid-search job (backend-01/02).
//
// Deviation from the spec's literal sketch: BestMetrics is stored as a JSON
// blob (BestMetricsJSON) rather than a `gorm:"embedded"` metrics_provider.
// BacktestMetrics struct. BacktestMetrics carries an EquityCurve
// []EquityPoint field with no GORM serializer tag, which AutoMigrate cannot
// map to a column - the same reason entities.BacktestRun already stores its
// trade log as TradeLogJSON datatypes.JSON instead of embedding structs
// directly. Keeping BestMetricsJSON as a JSON blob matches that existing,
// working precedent instead of introducing a migration failure.
type OptimizationRun struct {
	ID                uint               `gorm:"primaryKey" json:"id"`
	StrategyID        uint               `json:"strategy_id"`
	Symbol            string             `json:"symbol"`
	Timeframe         string             `json:"timeframe"`
	StartDate         time.Time          `json:"start_date"`
	EndDate           time.Time          `json:"end_date"`
	InitialCapital    float64            `json:"initial_capital"`
	ParamGridJSON     datatypes.JSON     `json:"param_grid_json" gorm:"type:jsonb"`
	Status            OptimizationStatus `json:"status"`
	Progress          int                `json:"progress"`
	TotalCombinations int                `json:"total_combinations"`
	BestConfigJSON    datatypes.JSON     `json:"best_config_json" gorm:"type:jsonb"`
	BestMetricsJSON   datatypes.JSON     `json:"best_metrics_json" gorm:"type:jsonb"`
	ResultsGridJSON   datatypes.JSON     `json:"results_grid_json" gorm:"type:jsonb"`
	ErrorMessage      string             `json:"error_message"`
	CreatedAt         time.Time          `json:"created_at"`
	CompletedAt       *time.Time         `json:"completed_at"`
}
