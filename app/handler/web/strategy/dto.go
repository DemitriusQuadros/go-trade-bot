package handler

import (
	"encoding/json"
	"go-trade-bot/app/entities"

	"gorm.io/datatypes"
)

type StrategyDto struct {
	Name             string          `json:"name"`
	Description      string          `json:"description"`
	MonitoredSymbols []string        `json:"monitored_symbols"`
	Algorithm        string          `json:"algorithm"`
	Cycle            int             `json:"cycle"`
	Configuration    json.RawMessage `json:"configuration"`
}

func (s StrategyDto) ToModel() entities.Strategy {
	return entities.Strategy{
		Name:             s.Name,
		Description:      s.Description,
		MonitoredSymbols: s.MonitoredSymbols,
		Algorithm:        entities.Algorithm(s.Algorithm),
		StrategyConfiguration: entities.StrategyConfiguration{
			Cycle:         entities.Cycle(s.Cycle),
			Configuration: datatypes.JSON(s.Configuration),
		},
	}
}
