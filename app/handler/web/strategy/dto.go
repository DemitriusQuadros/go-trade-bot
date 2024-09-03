package handler

import "go-trade-bot/app/entities"

type StrategyDto struct {
	Name             string   `json:"name"`
	Description      string   `json:"description"`
	MonitoredSymbols []string `json:"monitored_symbols"`
	Algorithm        string   `json:"algorithm"`
	Cycle            int      `json:"cycle"`
}

func (s StrategyDto) toModel() entities.Strategy {
	return entities.Strategy{
		Name:             s.Name,
		Description:      s.Description,
		MonitoredSymbols: s.MonitoredSymbols,
		Algorithm:        entities.Algorithm(s.Algorithm),
		StrategyConfiguration: entities.StrategyConfiguration{
			Cycle: entities.Cycle(s.Cycle),
		},
	}
}
