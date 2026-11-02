package futuresmath

import "time"

const (
	// DefaultFuturesMakerFeeBps is the standard maker fee rate in basis points (0.02%).
	DefaultFuturesMakerFeeBps = 2.0
	// DefaultFuturesTakerFeeBps is the standard taker fee rate in basis points (0.05%).
	DefaultFuturesTakerFeeBps = 5.0
	// DefaultLiquidationClearanceFeeBps is the clearance fee rate on liquidation (0.50%).
	DefaultLiquidationClearanceFeeBps = 50.0
	// DefaultFundingRate8h is the baseline 8-hour funding rate (+0.01%).
	DefaultFundingRate8h = 0.0001
)

// NotionalFee returns the fee in quote currency for a fill of qty at fillPrice with feeBps basis points.
func NotionalFee(qty, fillPrice, feeBps float64) float64 {
	return qty * fillPrice * (feeBps / 10000.0)
}

// FundingPayment calculates the funding payment amount for a position.
// DirectionSign(side) * (qty * markPrice) * fundingRate.
// A positive return value indicates the position holder pays funding;
// a negative return value indicates the position holder receives a funding rebate.
func FundingPayment(side Side, qty, markPrice, fundingRate float64) float64 {
	return DirectionSign(side) * (qty * markPrice) * fundingRate
}

// FundingTimestampsInWindow returns all UTC 8-hour funding timestamps
// (00:00:00, 08:00:00, 16:00:00 UTC) strictly after fromExclusive.UTC()
// and <= toInclusive.UTC(), in chronological order.
func FundingTimestampsInWindow(fromExclusive, toInclusive time.Time) []time.Time {
	from := fromExclusive.UTC()
	to := toInclusive.UTC()
	if !to.After(from) {
		return []time.Time{}
	}

	h := (from.Hour() / 8) * 8
	candidate := time.Date(from.Year(), from.Month(), from.Day(), h, 0, 0, 0, time.UTC)
	for !candidate.After(from) {
		candidate = candidate.Add(8 * time.Hour)
	}

	var timestamps []time.Time
	for !candidate.After(to) {
		timestamps = append(timestamps, candidate)
		candidate = candidate.Add(8 * time.Hour)
	}

	return timestamps
}
