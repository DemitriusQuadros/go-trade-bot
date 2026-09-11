package entities

import (
	"time"

	"gorm.io/datatypes"
)

type ImportRequest struct {
	Symbols    []string
	Timeframes []string
	From       time.Time
	To         time.Time
	MinHistory time.Duration
}

type ImportSummary struct {
	Symbol          string  `json:"symbol"`
	Timeframe       string  `json:"timeframe"`
	CandlesImported int     `json:"candles_imported"`
	DurationSeconds float64 `json:"duration_seconds"`
	GapsDetected    int     `json:"gaps_detected"`
}

type ImportJobStatus string

const (
	ImportJobPending   ImportJobStatus = "pending"
	ImportJobRunning   ImportJobStatus = "running"
	ImportJobCompleted ImportJobStatus = "completed"
	ImportJobFailed    ImportJobStatus = "failed"
)

type ImportJob struct {
	ID           string `gorm:"primaryKey"`
	Status       ImportJobStatus
	ResultJSON   datatypes.JSON `gorm:"type:jsonb"`
	ErrorMessage string
	CreatedAt    time.Time
	CompletedAt  *time.Time
}

type ImportSchedule struct {
	ID        uint       `gorm:"primaryKey" json:"id"`
	Symbol    string     `json:"symbol"`
	Timeframe string     `json:"timeframe"`
	CronSpec  string     `json:"cron_spec"` // e.g. "0 1 * * *"
	Enabled   bool       `json:"enabled"`
	LastRunAt *time.Time `json:"last_run_at"`
	CreatedAt time.Time  `json:"created_at"`
}
