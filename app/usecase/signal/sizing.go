package usecase

import (
	"fmt"
	"math"
)

type SizingType string

const (
	SizingFixedAmount SizingType = "fixed_amount" // Value = absolute currency amount to invest per entry
	SizingPctCapital  SizingType = "pct_capital"  // Value = percentage (0-100) of available balance
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

// QtyDecision is the outcome of ResolveEntryQty.
type QtyDecision struct {
	Qty float64
	// Clamped is true when the script asked for more than the ceiling allows
	// and Qty was reduced. The caller must surface this - never silently.
	Clamped bool
	// Requested is the script's own qty (0 when the script supplied none).
	Requested float64
}

// ResolveEntryQty applies the B-03 contract: a script's qty (requestedQty > 0)
// is honoured but never above ceilingAmount (quote currency) / entryPrice.
// With no script qty it falls back to fallbackAmount, the config /
// per-order-slot sizing used before B-03. entryPrice <= 0 yields qty 0.
func ResolveEntryQty(requestedQty, entryPrice, fallbackAmount, ceilingAmount float64) QtyDecision {
	if entryPrice <= 0 {
		return QtyDecision{}
	}
	if requestedQty <= 0 {
		return QtyDecision{Qty: fallbackAmount / entryPrice}
	}
	maxQty := math.Max(ceilingAmount, 0) / entryPrice
	if requestedQty > maxQty {
		return QtyDecision{Qty: maxQty, Clamped: true, Requested: requestedQty}
	}
	return QtyDecision{Qty: requestedQty, Requested: requestedQty}
}
