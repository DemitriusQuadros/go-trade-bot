package entities

import (
	"time"

	"github.com/google/uuid"
)

type Algorithm string

const (
	Grid       = "grid"
	Heikenashi = "heikenashi"
)

type Strategy struct {
	ID                    uuid.UUID
	Name                  string
	Description           string
	Algorithm             Algorithm
	MonitoredSymbols      []string
	StrategyConfiguration StrategyConfiguration
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

type StrategyConfiguration struct {
	Cycle Cycle
}

type StrategyExecution struct {
	ID         uuid.UUID
	Strategy   Strategy
	ExecutedAt time.Time
}

type Cycle int

const (
	FiveMinutes    Cycle = 5
	TenMinutes     Cycle = 10
	FifteenMinutes Cycle = 15
	ThirtyMinutes  Cycle = 30
	OneHour        Cycle = 60
)
