package handler

import (
	"encoding/json"
	"time"

	"go-trade-bot/app/entities"
)

type StrategyResponseDTO struct {
	ID               uint            `json:"id"`
	Name             string          `json:"name"`
	Description      string          `json:"description"`
	StrategyName     string          `json:"strategy_name"`
	Status           string          `json:"status"`
	Mode             string          `json:"mode"`
	MonitoredSymbols []string        `json:"monitored_symbols"`
	Cycle            int             `json:"cycle"`
	Configuration    json.RawMessage `json:"configuration"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
}

func ToStrategyResponse(s entities.Strategy) StrategyResponseDTO {
	symbols := make([]string, len(s.MonitoredSymbols))
	copy(symbols, s.MonitoredSymbols)

	var configJSON json.RawMessage
	if len(s.StrategyConfiguration.Configuration) > 0 {
		configJSON = json.RawMessage(s.StrategyConfiguration.Configuration)
	}

	return StrategyResponseDTO{
		ID:               s.ID,
		Name:             s.Name,
		Description:      s.Description,
		StrategyName:     s.StrategyName,
		Status:           string(s.Status),
		Mode:             s.Mode,
		MonitoredSymbols: symbols,
		Cycle:            int(s.StrategyConfiguration.Cycle),
		Configuration:    configJSON,
		CreatedAt:        s.CreatedAt,
		UpdatedAt:        s.UpdatedAt,
	}
}

func ToStrategyResponseList(strategies []entities.Strategy) []StrategyResponseDTO {
	res := make([]StrategyResponseDTO, len(strategies))
	for i, s := range strategies {
		res[i] = ToStrategyResponse(s)
	}
	return res
}
