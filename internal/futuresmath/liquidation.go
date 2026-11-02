package futuresmath

import "math"

// IsolatedLiquidationPrice calculates the exact Binance USD-M liquidation price for an isolated position.
// Returns 0 if qty <= 0, entryPrice <= 0, or if pLiq <= 0 / NaN / Inf.
func IsolatedLiquidationPrice(symbol string, side Side, entryPrice, qty, leverage, addedMargin, accumulatedFunding float64) float64 {
	if qty <= 0 || entryPrice <= 0 {
		return 0
	}
	if leverage <= 0 {
		leverage = 1.0
	}
	notional := qty * entryPrice
	im := InitialMargin(notional, leverage)
	wbIso := im + addedMargin - accumulatedFunding

	bracket := LookupBracket(symbol, notional)
	s := DirectionSign(side)
	mmr := bracket.MaintenanceMarginRate
	cum := bracket.MaintenanceAmount

	pLiq := (wbIso + cum - s*qty*entryPrice) / (qty * (mmr - s))
	if pLiq <= 0 || math.IsNaN(pLiq) || math.IsInf(pLiq, 0) {
		return 0
	}
	return pLiq
}

// CrossLiquidationPrice calculates the exact Binance USD-M liquidation price for a cross position.
// Returns 0 if qty <= 0, entryPrice <= 0, or if pLiq <= 0 / NaN / Inf.
func CrossLiquidationPrice(symbol string, side Side, entryPrice, qty, leverage, availableBalance, accumulatedFunding float64) float64 {
	if qty <= 0 || entryPrice <= 0 {
		return 0
	}
	if leverage <= 0 {
		leverage = 1.0
	}
	notional := qty * entryPrice
	im := InitialMargin(notional, leverage)
	wbCross := im + availableBalance - accumulatedFunding

	bracket := LookupBracket(symbol, notional)
	s := DirectionSign(side)
	mmr := bracket.MaintenanceMarginRate
	cum := bracket.MaintenanceAmount

	pLiq := (wbCross + cum - s*qty*entryPrice) / (qty * (mmr - s))
	if pLiq <= 0 || math.IsNaN(pLiq) || math.IsInf(pLiq, 0) {
		return 0
	}
	return pLiq
}

// IsLiquidated returns true if the candle's extreme price pierced the liquidation price.
// If liqPrice <= 0, returns false.
func IsLiquidated(side Side, liqPrice, candleHigh, candleLow float64) bool {
	if liqPrice <= 0 {
		return false
	}
	if side == SideShort {
		return candleHigh >= liqPrice
	}
	return candleLow <= liqPrice
}

// UnrealizedPnL calculates the unrealized profit or loss of a position at markPrice.
func UnrealizedPnL(side Side, entryPrice, markPrice, qty float64) float64 {
	return DirectionSign(side) * (markPrice - entryPrice) * qty
}
