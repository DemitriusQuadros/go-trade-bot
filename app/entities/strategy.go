package entities

import (
	"time"

	"gorm.io/datatypes"
)

type ExecutionStatus string

const (
	OK    = "ok"
	Error = "error"
)

type StrategyStatus string

const (
	Productive StrategyStatus = "productive"
	Testing    StrategyStatus = "testing"
	Disabled   StrategyStatus = "disabled"
)

type Strategy struct {
	ID          uint `gorm:"primaryKey"`
	Name        string
	Description string
	// StrategyName is the registry-validated free-form strategy key (Spec 06,
	// ADR-005) that resolves the strategy against the plugin registry. The old
	// closed Algorithm enum was fully removed in backend-05.
	StrategyName string
	Status       StrategyStatus
	// ScriptSource is the Lua source for a "script" StrategyName (backend-04).
	// Empty for every native Go strategy; required (non-empty) for script
	// strategies, enforced in the strategy usecase's validateStrategy.
	ScriptSource string `gorm:"type:text"`
	// Mode is the per-strategy persisted execution-mode tier (Spec 10,
	// ADR-002): "backtest" | "dryrun" | "paper" | "live". New AND existing
	// rows default to "dryrun" via migration - never "live".
	Mode                  string                      `gorm:"default:'dryrun'"`
	MonitoredSymbols      datatypes.JSONSlice[string] `gorm:"type:jsonb"`
	StrategyConfiguration StrategyConfiguration       `gorm:"embedded"`
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

type StrategyConfiguration struct {
	Cycle         Cycle
	Configuration datatypes.JSON `gorm:"type:jsonb"`
}

type StrategyExecution struct {
	ID         uint `gorm:"primaryKey"`
	Status     ExecutionStatus
	Message    string
	StrategyID uint
	Strategy   Strategy `gorm:"foreignKey:StrategyID"`
	ExecutedAt time.Time
}

type Cycle int

const (
	OneMinute      Cycle = 1
	FiveMinutes    Cycle = 5
	TenMinutes     Cycle = 10
	FifteenMinutes Cycle = 15
	ThirtyMinutes  Cycle = 30
	OneHour        Cycle = 60
)

func IsValidStatus(status string) bool {
	switch StrategyStatus(status) {
	case Productive, Testing, Disabled:
		return true
	default:
		return false
	}
}

func IsValidCycle(cycle int) bool {
	switch Cycle(cycle) {
	case OneMinute, FiveMinutes, TenMinutes, FifteenMinutes, ThirtyMinutes, OneHour:
		return true
	default:
		return false
	}
}

func (s Strategy) GetBrokerInterval() string {
	switch s.StrategyConfiguration.Cycle {
	case OneMinute:
		return "1m"
	case FiveMinutes:
		return "5m"
	case TenMinutes:
		return "10m"
	case FifteenMinutes:
		return "15m"
	case ThirtyMinutes:
		return "30m"
	case OneHour:
		return "1h"
	default:
		return ""
	}
}
