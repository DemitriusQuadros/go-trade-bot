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
	StrategyName     string   `json:"strategy_name"`
	// ScriptSource carries the Lua source for a "script" StrategyName
	// (backend-04/05). Ignored for native Go strategies.
	ScriptSource  string          `json:"script_source"`
	Mode          string          `json:"mode"`
	Cycle         int             `json:"cycle"`
	Configuration json.RawMessage `json:"configuration"`
}

func (s StrategyDto) ToModel() entities.Strategy {
	mode := s.Mode
	if mode == "" {
		mode = "dryrun" // safe default (Spec 10) - never defaults to "live"
	}

	return entities.Strategy{
		Name:             s.Name,
		Description:      s.Description,
		MonitoredSymbols: s.MonitoredSymbols,
		StrategyName:     s.StrategyName,
		ScriptSource:     s.ScriptSource,
		Mode:             mode,
		Status:           entities.StrategyStatus(s.Status),
		StrategyConfiguration: entities.StrategyConfiguration{
			Cycle:         entities.Cycle(s.Cycle),
			Configuration: datatypes.JSON(s.Configuration),
		},
	}
}
