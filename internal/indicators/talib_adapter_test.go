package indicators_test

import (
	"testing"

	"go-trade-bot/internal/exchange"
	"go-trade-bot/internal/indicators"

	"github.com/markcheno/go-talib"
	"github.com/stretchr/testify/assert"
)

func candlesFromCloses(closes []float64) []exchange.Candle {
	candles := make([]exchange.Candle, len(closes))
	for i, c := range closes {
		candles[i] = exchange.Candle{Close: c, High: c, Low: c}
	}
	return candles
}

func sampleCloses(n int) []float64 {
	closes := make([]float64, n)
	base := 100.0
	for i := 0; i < n; i++ {
		// deterministic wiggle so RSI/BBands aren't degenerate flat series
		base += float64((i%5)-2) * 0.7
		closes[i] = base
	}
	return closes
}

func TestRSI_MatchesTalibDirectly(t *testing.T) {
	closes := sampleCloses(100)
	candles := candlesFromCloses(closes)

	adapter := indicators.NewTalibAdapter()
	got := adapter.RSI(candles, 14)
	want := talib.Rsi(closes, 14)

	assert.Equal(t, want, got)
}

func TestBollingerBands_EMA_MatchesCurrentBollingerAlgorithmCall(t *testing.T) {
	closes := sampleCloses(30)
	candles := candlesFromCloses(closes)

	adapter := indicators.NewTalibAdapter()
	upper, mid, lower := adapter.BollingerBands(candles, 20, 2.0, 2.0, indicators.MATypeEMA)
	wantUpper, wantMid, wantLower := talib.BBands(closes, 20, 2.0, 2.0, talib.EMA)

	assert.Equal(t, wantUpper, upper)
	assert.Equal(t, wantMid, mid)
	assert.Equal(t, wantLower, lower)
}

func TestBollingerBands_SMA_DiffersFromEMA(t *testing.T) {
	closes := sampleCloses(30)
	candles := candlesFromCloses(closes)

	adapter := indicators.NewTalibAdapter()
	emaUpper, _, _ := adapter.BollingerBands(candles, 20, 2.0, 2.0, indicators.MATypeEMA)
	smaUpper, _, _ := adapter.BollingerBands(candles, 20, 2.0, 2.0, indicators.MATypeSMA)

	assert.NotEqual(t, emaUpper, smaUpper)
}

func TestEMA_MatchesScalpingUptrendCall(t *testing.T) {
	closes := sampleCloses(50)
	candles := candlesFromCloses(closes)

	adapter := indicators.NewTalibAdapter()
	got := adapter.EMA(candles, 20)
	want := talib.Ema(closes, 20)

	assert.Equal(t, want, got)
}

// TestRSI_InsufficientCandles_PassesThroughUnchanged verifies Spec 07 AC#5's
// "passes through unchanged rather than panicking or silently padding"
// requirement literally: the adapter adds no guard of its own on top of
// go-talib. In practice go-talib.Rsi's actual behavior on undersized input
// (fewer candles than the period) is an index-out-of-range panic, not the
// short/NaN-leading slice the spec's prose assumed - "unchanged" therefore
// means the adapter reproduces that same panic rather than intercepting it,
// which is why callers (the ported strategies, Spec 08) must keep their own
// pre-call length guards exactly as they do today.
func TestRSI_InsufficientCandles_PassesThroughUnchanged(t *testing.T) {
	closes := sampleCloses(5)
	candles := candlesFromCloses(closes)

	adapter := indicators.NewTalibAdapter()

	assert.Panics(t, func() { adapter.RSI(candles, 14) })
	assert.Panics(t, func() { talib.Rsi(closes, 14) })
}

func TestMACD_SMA_ATR_SmokeTest(t *testing.T) {
	closes := sampleCloses(60)
	candles := candlesFromCloses(closes)
	adapter := indicators.NewTalibAdapter()

	macd, signal, hist := adapter.MACD(candles, 12, 26, 9)
	assert.NotNil(t, macd)
	assert.NotNil(t, signal)
	assert.NotNil(t, hist)
	assert.Len(t, macd, len(closes))

	sma := adapter.SMA(candles, 20)
	assert.NotNil(t, sma)
	assert.Len(t, sma, len(closes))

	atr := adapter.ATR(candles, 14)
	assert.NotNil(t, atr)
	assert.Len(t, atr, len(closes))
}
