package handler

import (
	"encoding/json"
	"go-trade-bot/app/entities"

	"gorm.io/datatypes"
)

type StrategyDto struct {
	Name             string   `json:"name"`
	Description      string   `json:"description"`
	MonitoredSymbols []string `json:"monitored_symbols"`
	Status           string   `json:"status" only:"productive,testing,disabled"`
	// Algorithm is deprecated (Spec 06) - kept accepted for exactly one
	// deprecation window so existing API clients / docs/strategy-examples/*.json
	// configs don't break. Mapped to StrategyName when StrategyName is empty.
	Algorithm     string          `json:"algorithm"`
	StrategyName  string          `json:"strategy_name"`
	Mode          string          `json:"mode"`
	Cycle         int             `json:"cycle"`
	Configuration json.RawMessage `json:"configuration"`
}

func (s StrategyDto) ToModel() entities.Strategy {
	strategyName := s.StrategyName
	if strategyName == "" && s.Algorithm != "" {
		strategyName = s.Algorithm
	}

	mode := s.Mode
	if mode == "" {
		mode = "dryrun" // safe default (Spec 10) - never defaults to "live"
	}

	return entities.Strategy{
		Name:             s.Name,
		Description:      s.Description,
		MonitoredSymbols: s.MonitoredSymbols,
		Algorithm:        entities.Algorithm(s.Algorithm),
		StrategyName:     strategyName,
		Mode:             mode,
		Status:           entities.StrategyStatus(s.Status),
		StrategyConfiguration: entities.StrategyConfiguration{
			Cycle:         entities.Cycle(s.Cycle),
			Configuration: datatypes.JSON(s.Configuration),
		},
	}
}
