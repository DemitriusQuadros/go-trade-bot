package entities

import (
	"time"

	"gorm.io/datatypes"
)

type Algorithm string

const (
	Grid       = "grid"
	Heikenashi = "heikenashi"
	Volume     = "volume"
)

type ExecutionStatus string

const (
	OK    = "ok"
	Error = "error"
)

type Strategy struct {
	ID                    uint `gorm:"primaryKey"`
	Name                  string
	Description           string
	Algorithm             Algorithm
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

func IsValidAlgorithm(algo string) bool {
	switch Algorithm(algo) {
	case Grid, Heikenashi, Volume:
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
