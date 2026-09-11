package indicators

import (
	"go-trade-bot/internal/exchange"

	"github.com/markcheno/go-talib"
)

// TalibAdapter is the only file in the repository permitted to import
// github.com/markcheno/go-talib (enforced by the same CI import-lint as
// Spec 01's go-binance restriction). It converts []exchange.Candle to the
// []float64 close-price (or high/low/close) slices go-talib expects
// internally - callers never touch raw float slices or know go-talib exists.
type TalibAdapter struct{}

func NewTalibAdapter() *TalibAdapter {
	return &TalibAdapter{}
}

func (a *TalibAdapter) RSI(candles []exchange.Candle, period int) []float64 {
	return talib.Rsi(closes(candles), period)
}

func (a *TalibAdapter) BollingerBands(candles []exchange.Candle, period int, stdDevUp, stdDevDown float64, maType MAType) (upper, mid, lower []float64) {
	return talib.BBands(closes(candles), period, stdDevUp, stdDevDown, toTalibMAType(maType))
}

func (a *TalibAdapter) EMA(candles []exchange.Candle, period int) []float64 {
	return talib.Ema(closes(candles), period)
}

func (a *TalibAdapter) SMA(candles []exchange.Candle, period int) []float64 {
	return talib.Sma(closes(candles), period)
}

func (a *TalibAdapter) MACD(candles []exchange.Candle, fastPeriod, slowPeriod, signalPeriod int) (macd, signal, hist []float64) {
	return talib.Macd(closes(candles), fastPeriod, slowPeriod, signalPeriod)
}

func (a *TalibAdapter) ATR(candles []exchange.Candle, period int) []float64 {
	highs := make([]float64, len(candles))
	lows := make([]float64, len(candles))
	closeVals := make([]float64, len(candles))
	for i, c := range candles {
		highs[i] = c.High
		lows[i] = c.Low
		closeVals[i] = c.Close
	}
	return talib.Atr(highs, lows, closeVals, period)
}

func closes(candles []exchange.Candle) []float64 {
	closeVals := make([]float64, len(candles))
	for i, c := range candles {
		closeVals[i] = c.Close
	}
	return closeVals
}

func toTalibMAType(m MAType) talib.MaType {
	if m == MATypeEMA {
		return talib.EMA
	}
	return talib.SMA
}
