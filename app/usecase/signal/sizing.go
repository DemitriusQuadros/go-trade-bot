package usecase

import (
	"fmt"
	"math"
)

type SizingType string

const (
	SizingFixedAmount SizingType = "fixed_amount" // Value = absolute currency amount to invest per entry
	SizingPctCapital  SizingType = "pct_capital"   // Value = percentage (0-100) of available balance
)

type PositionSizingConfig struct {
	Type  SizingType `json:"type"`
	Value float64    `json:"value"`
}

// PositionSizer computes the amount to invest for one entry, given the
// account's current available balance. Falls back to Phase 1's existing
// AccountUseCase.GetDisponibleAmout() behavior when no PositionSizingConfig
// is supplied (nil).
type PositionSizer interface {
	Size(cfg *PositionSizingConfig, available float64) (investedAmount float64, err error)
}

type DefaultPositionSizer struct{}

func NewDefaultPositionSizer() PositionSizer {
	return &DefaultPositionSizer{}
}

func (s *DefaultPositionSizer) Size(cfg *PositionSizingConfig, available float64) (float64, error) {
	if available <= 0 {
		return 0, nil
	}

	if cfg == nil {
		return available, nil
	}

	switch cfg.Type {
	case SizingFixedAmount:
		if cfg.Value < 0 {
			return 0, fmt.Errorf("fixed_amount value must be non-negative, got %f", cfg.Value)
		}
		return math.Min(cfg.Value, available), nil
	case SizingPctCapital:
		if cfg.Value < 0 || cfg.Value > 100 {
			return 0, fmt.Errorf("pct_capital value must be between 0 and 100, got %f", cfg.Value)
		}
		return available * (cfg.Value / 100.0), nil
	default:
		return 0, fmt.Errorf("unsupported position sizing type: %s", cfg.Type)
	}
}
