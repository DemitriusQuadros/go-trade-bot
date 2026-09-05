// Package indicators is the Anti-Corruption Layer boundary for technical
// indicator computation. No package outside internal/indicators may import
// github.com/markcheno/go-talib - every indicator call flows through the
// IndicatorProvider interface defined here.
package indicators

import "go-trade-bot/internal/exchange"

// MAType selects the moving-average basis used by BollingerBands. Exposed on
// the ACL (rather than hardcoded) because the current Bollinger strategy
// uses an EMA-smoothed middle band, not the PRD's naive SMA-implied default -
// see Spec 07 for the fidelity rationale.
type MAType int

const (
	MATypeSMA MAType = iota
	MATypeEMA
)

// IndicatorProvider wraps go-talib, scoped to what the three Phase 1
// strategies actually call (RSI, BollingerBands, EMA) plus MACD/SMA/ATR,
// which are part of the frozen interface for forward compatibility (ADR-005)
// even though no Phase 1 strategy calls them yet.
type IndicatorProvider interface {
	RSI(candles []exchange.Candle, period int) []float64
	BollingerBands(candles []exchange.Candle, period int, stdDevUp, stdDevDown float64, maType MAType) (upper, mid, lower []float64)
	EMA(candles []exchange.Candle, period int) []float64
	SMA(candles []exchange.Candle, period int) []float64
	MACD(candles []exchange.Candle, fastPeriod, slowPeriod, signalPeriod int) (macd, signal, hist []float64)
	ATR(candles []exchange.Candle, period int) []float64
}
