package entities

import (
	"time"

	"gorm.io/datatypes"
)

// ImportSource picks which fetch path an ImportRequest uses. "" (zero
// value) is treated as ImportSourceREST for backward compat with requests
// persisted/enqueued before this field existed.
type ImportSource string

const (
	// ImportSourceREST talks to the live Binance kline REST API
	// (ListKlineRange) - correct for incremental/ongoing sync, but slow
	// (rate-limited, paginated) for a deep historical range.
	ImportSourceREST ImportSource = "rest"
	// ImportSourceArchive bulk-downloads from Binance's public
	// data.binance.vision monthly kline archive - no rate limits, months of
	// history in seconds per file. The right choice for a one-time deep
	// historical backfill; granularity is whole months (a request's From/To
	// are truncated to month boundaries).
	ImportSourceArchive ImportSource = "archive"
)

type ImportRequest struct {
	Symbols    []string
	Timeframes []string
	From       time.Time
	To         time.Time
	MinHistory time.Duration
	Source     ImportSource // "" == ImportSourceREST, see above
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
