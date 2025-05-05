package entities

import (
	"time"

	"gorm.io/datatypes"
)

type Algorithm string

const (
	Grid      = "grid"
	Scalping  = "scalping"
	Bollinger = "bollinger"
)

type ExecutionStatus string

const (
	OK    = "ok"
	Error = "error"
)

type StrategyStatus string

const (
	Productive = "productive"
	Testing    = "testing"
	Disabled   = "disabled"
)

type Strategy struct {
	ID                    uint `gorm:"primaryKey"`
	Name                  string
	Description           string
	Algorithm             Algorithm
	Status                StrategyStatus
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
	case Grid, Bollinger, Scalping:
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
