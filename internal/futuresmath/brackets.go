package futuresmath

import (
	"math"
	"strings"
)

// Side represents the order or position direction.
type Side string

const (
	SideLong  Side = "LONG"
	SideShort Side = "SHORT"
)

// DirectionSign returns +1.0 for SideLong (or empty/default) and -1.0 for SideShort.
func DirectionSign(side Side) float64 {
	if side == SideShort {
		return -1.0
	}
	return 1.0
}

// LeverageBracket defines a Binance USD-M maintenance margin tier.
type LeverageBracket struct {
	Tier                  int
	NotionalFloor         float64
	NotionalCap           float64 // math.Inf(1) for top tier
	MaxLeverage           int
	MaintenanceMarginRate float64 // MMR (e.g. 0.004 for 0.40%)
	MaintenanceAmount     float64 // Cum (quick-deduction amount in USDT)
}

// DefaultBTCBrackets returns leverage brackets for BTCUSDT / ETHUSDT matching Binance USD-M Tiers 1–5.
func DefaultBTCBrackets() []LeverageBracket {
	return []LeverageBracket{
		{Tier: 1, NotionalFloor: 0, NotionalCap: 50_000, MaxLeverage: 125, MaintenanceMarginRate: 0.004, MaintenanceAmount: 0},
		{Tier: 2, NotionalFloor: 50_000, NotionalCap: 250_000, MaxLeverage: 100, MaintenanceMarginRate: 0.005, MaintenanceAmount: 50},
		{Tier: 3, NotionalFloor: 250_000, NotionalCap: 1_000_000, MaxLeverage: 50, MaintenanceMarginRate: 0.010, MaintenanceAmount: 1_300},
		{Tier: 4, NotionalFloor: 1_000_000, NotionalCap: 10_000_000, MaxLeverage: 20, MaintenanceMarginRate: 0.025, MaintenanceAmount: 16_300},
		{Tier: 5, NotionalFloor: 10_000_000, NotionalCap: math.Inf(1), MaxLeverage: 10, MaintenanceMarginRate: 0.050, MaintenanceAmount: 266_300},
	}
}

// DefaultAltcoinBrackets returns leverage brackets for altcoins matching Binance USD-M Tiers 1–5.
func DefaultAltcoinBrackets() []LeverageBracket {
	return []LeverageBracket{
		{Tier: 1, NotionalFloor: 0, NotionalCap: 10_000, MaxLeverage: 75, MaintenanceMarginRate: 0.005, MaintenanceAmount: 0},
		{Tier: 2, NotionalFloor: 10_000, NotionalCap: 50_000, MaxLeverage: 50, MaintenanceMarginRate: 0.010, MaintenanceAmount: 50},
		{Tier: 3, NotionalFloor: 50_000, NotionalCap: 250_000, MaxLeverage: 25, MaintenanceMarginRate: 0.020, MaintenanceAmount: 550},
		{Tier: 4, NotionalFloor: 250_000, NotionalCap: 1_000_000, MaxLeverage: 10, MaintenanceMarginRate: 0.050, MaintenanceAmount: 8_050},
		{Tier: 5, NotionalFloor: 1_000_000, NotionalCap: math.Inf(1), MaxLeverage: 5, MaintenanceMarginRate: 0.100, MaintenanceAmount: 58_050},
	}
}

// BracketsForSymbol returns DefaultBTCBrackets() for "BTCUSDT" / "ETHUSDT" and DefaultAltcoinBrackets() for others.
func BracketsForSymbol(symbol string) []LeverageBracket {
	clean := strings.ToUpper(strings.TrimSpace(symbol))
	switch clean {
	case "BTCUSDT", "ETHUSDT":
		return DefaultBTCBrackets()
	default:
		return DefaultAltcoinBrackets()
	}
}

// LookupBracket finds the bracket matching the notional value for the given symbol.
func LookupBracket(symbol string, notional float64) LeverageBracket {
	brackets := BracketsForSymbol(symbol)
	for _, b := range brackets {
		if notional <= b.NotionalCap {
			return b
		}
	}
	return brackets[len(brackets)-1]
}

// MaintenanceMargin calculates the required maintenance margin and returns the applicable bracket.
// Formula: mm = math.Max(0, notional*bracket.MaintenanceMarginRate - bracket.MaintenanceAmount).
func MaintenanceMargin(symbol string, notional float64) (float64, LeverageBracket) {
	b := LookupBracket(symbol, notional)
	mm := math.Max(0, notional*b.MaintenanceMarginRate-b.MaintenanceAmount)
	return mm, b
}

// InitialMargin returns the initial margin required for a position notional given leverage.
// If leverage <= 0, leverage is treated as 1.0.
func InitialMargin(notional float64, leverage float64) float64 {
	if leverage <= 0 {
		leverage = 1.0
	}
	return notional / leverage
}
