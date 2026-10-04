package indicators_test

import (
	"testing"

	"go-trade-bot/internal/exchange"
	"go-trade-bot/internal/indicators"

	"github.com/stretchr/testify/assert"
)

// candlesFromOHLCV builds a deterministic OHLCV candle series so the new
// (non-frozen) indicator adapter methods below - many of which need open,
// high, low, and/or volume, not just close - have realistic, non-degenerate
// input. Distinct from candlesFromCloses (High/Low always equal Close),
// which the frozen RSI/BollingerBands/EMA/SMA/MACD/ATR tests above rely on
// unchanged.
func candlesFromOHLCV(n int) []exchange.Candle {
	candles := make([]exchange.Candle, n)
	base := 100.0
	for i := 0; i < n; i++ {
		base += float64((i%5)-2) * 0.7
		open := base - 0.3
		close := base
		high := base + 0.8
		low := base - 0.8
		volume := 1000.0 + float64(i%7)*50.0
		candles[i] = exchange.Candle{Open: open, High: high, Low: low, Close: close, Volume: volume}
	}
	return candles
}

// TestNewIndicators_NoPanicAndNonEmpty is a table-driven smoke test covering
// every indicator method added beyond the frozen RSI/BollingerBands/EMA/SMA/
// MACD/ATR set (see IndicatorProvider's doc comment for the full list and
// the Beta/Correl/MaVp skip rationale). Each case asserts the call does not
// panic on sufficient candle data and that its primary output slice is
// non-empty and the same length as the input - go-talib pads leading
// lookback bars with zeros rather than shortening the output.
func TestNewIndicators_NoPanicAndNonEmpty(t *testing.T) {
	candles := candlesFromOHLCV(100)
	adapter := indicators.NewTalibAdapter()

	cases := []struct {
		name string
		run  func() []float64
	}{
		{name: "DEMA", run: func() []float64 { return adapter.DEMA(candles, 10) }},
		{name: "HTTrendline", run: func() []float64 { return adapter.HTTrendline(candles) }},
		{name: "KAMA", run: func() []float64 { return adapter.KAMA(candles, 10) }},
		{name: "MA", run: func() []float64 { return adapter.MA(candles, 10) }},
		{name: "MAMA", run: func() []float64 { return func() []float64 { v, _ := adapter.MAMA(candles, 0.5, 0.05); return v }() }},
		{name: "MidPoint", run: func() []float64 { return adapter.MidPoint(candles, 10) }},
		{name: "MidPrice", run: func() []float64 { return adapter.MidPrice(candles, 10) }},
		{name: "SAR", run: func() []float64 { return adapter.SAR(candles, 0.02, 0.2) }},
		{name: "SARExt", run: func() []float64 { return adapter.SARExt(candles, 0.0, 0.0, 0.02, 0.02, 0.2, 0.02, 0.02, 0.2) }},
		{name: "T3", run: func() []float64 { return adapter.T3(candles, 10, 0.7) }},
		{name: "TEMA", run: func() []float64 { return adapter.TEMA(candles, 10) }},
		{name: "TRIMA", run: func() []float64 { return adapter.TRIMA(candles, 10) }},
		{name: "WMA", run: func() []float64 { return adapter.WMA(candles, 10) }},
		{name: "ADX", run: func() []float64 { return adapter.ADX(candles, 10) }},
		{name: "ADXR", run: func() []float64 { return adapter.ADXR(candles, 10) }},
		{name: "APO", run: func() []float64 { return adapter.APO(candles, 6, 15) }},
		{name: "Aroon", run: func() []float64 { return func() []float64 { v, _ := adapter.Aroon(candles, 10); return v }() }},
		{name: "AroonOsc", run: func() []float64 { return adapter.AroonOsc(candles, 10) }},
		{name: "BOP", run: func() []float64 { return adapter.BOP(candles) }},
		{name: "CMO", run: func() []float64 { return adapter.CMO(candles, 10) }},
		{name: "CCI", run: func() []float64 { return adapter.CCI(candles, 10) }},
		{name: "DX", run: func() []float64 { return adapter.DX(candles, 10) }},
		{name: "MACDExt", run: func() []float64 {
			return func() []float64 { v, _, _ := adapter.MACDExt(candles, 6, 15, 5); return v }()
		}},
		{name: "MACDFix", run: func() []float64 { return func() []float64 { v, _, _ := adapter.MACDFix(candles, 5); return v }() }},
		{name: "MinusDI", run: func() []float64 { return adapter.MinusDI(candles, 10) }},
		{name: "MinusDM", run: func() []float64 { return adapter.MinusDM(candles, 10) }},
		{name: "MFI", run: func() []float64 { return adapter.MFI(candles, 10) }},
		{name: "Mom", run: func() []float64 { return adapter.Mom(candles, 10) }},
		{name: "PlusDI", run: func() []float64 { return adapter.PlusDI(candles, 10) }},
		{name: "PlusDM", run: func() []float64 { return adapter.PlusDM(candles, 10) }},
		{name: "PPO", run: func() []float64 { return adapter.PPO(candles, 6, 15) }},
		{name: "ROCP", run: func() []float64 { return adapter.ROCP(candles, 10) }},
		{name: "ROC", run: func() []float64 { return adapter.ROC(candles, 10) }},
		{name: "ROCR", run: func() []float64 { return adapter.ROCR(candles, 10) }},
		{name: "ROCR100", run: func() []float64 { return adapter.ROCR100(candles, 10) }},
		{name: "Stoch", run: func() []float64 { return func() []float64 { v, _ := adapter.Stoch(candles, 5, 3, 3); return v }() }},
		{name: "StochF", run: func() []float64 { return func() []float64 { v, _ := adapter.StochF(candles, 5, 3); return v }() }},
		{name: "StochRSI", run: func() []float64 { return func() []float64 { v, _ := adapter.StochRSI(candles, 10, 5, 3); return v }() }},
		{name: "Trix", run: func() []float64 { return adapter.Trix(candles, 10) }},
		{name: "UltOsc", run: func() []float64 { return adapter.UltOsc(candles, 7, 14, 28) }},
		{name: "WillR", run: func() []float64 { return adapter.WillR(candles, 10) }},
		{name: "AD", run: func() []float64 { return adapter.AD(candles) }},
		{name: "ADOsc", run: func() []float64 { return adapter.ADOsc(candles, 6, 15) }},
		{name: "OBV", run: func() []float64 { return adapter.OBV(candles) }},
		{name: "NATR", run: func() []float64 { return adapter.NATR(candles, 10) }},
		{name: "TRange", run: func() []float64 { return adapter.TRange(candles) }},
		{name: "AvgPrice", run: func() []float64 { return adapter.AvgPrice(candles) }},
		{name: "MedPrice", run: func() []float64 { return adapter.MedPrice(candles) }},
		{name: "TypPrice", run: func() []float64 { return adapter.TypPrice(candles) }},
		{name: "WclPrice", run: func() []float64 { return adapter.WclPrice(candles) }},
		{name: "HTDcPeriod", run: func() []float64 { return adapter.HTDcPeriod(candles) }},
		{name: "HTDcPhase", run: func() []float64 { return adapter.HTDcPhase(candles) }},
		{name: "HTPhasor", run: func() []float64 { return func() []float64 { v, _ := adapter.HTPhasor(candles); return v }() }},
		{name: "HTSine", run: func() []float64 { return func() []float64 { v, _ := adapter.HTSine(candles); return v }() }},
		{name: "HTTrendMode", run: func() []float64 { return adapter.HTTrendMode(candles) }},
		{name: "LinearReg", run: func() []float64 { return adapter.LinearReg(candles, 10) }},
		{name: "LinearRegAngle", run: func() []float64 { return adapter.LinearRegAngle(candles, 10) }},
		{name: "LinearRegIntercept", run: func() []float64 { return adapter.LinearRegIntercept(candles, 10) }},
		{name: "LinearRegSlope", run: func() []float64 { return adapter.LinearRegSlope(candles, 10) }},
		{name: "StdDev", run: func() []float64 { return adapter.StdDev(candles, 10, 1.0) }},
		{name: "TSF", run: func() []float64 { return adapter.TSF(candles, 10) }},
		{name: "Var", run: func() []float64 { return adapter.Var(candles, 10) }},
		{name: "Max", run: func() []float64 { return adapter.Max(candles, 10) }},
		{name: "MaxIndex", run: func() []float64 { return adapter.MaxIndex(candles, 10) }},
		{name: "Min", run: func() []float64 { return adapter.Min(candles, 10) }},
		{name: "MinIndex", run: func() []float64 { return adapter.MinIndex(candles, 10) }},
		{name: "MinMax", run: func() []float64 { return func() []float64 { v, _ := adapter.MinMax(candles, 10); return v }() }},
		{name: "MinMaxIndex", run: func() []float64 { return func() []float64 { v, _ := adapter.MinMaxIndex(candles, 10); return v }() }},
		{name: "Sum", run: func() []float64 { return adapter.Sum(candles, 10) }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got []float64
			assert.NotPanics(t, func() { got = tc.run() })
			assert.NotEmpty(t, got, "%s: expected non-empty result", tc.name)
			assert.Len(t, got, len(candles), "%s: expected output length to match input length", tc.name)
		})
	}
}
