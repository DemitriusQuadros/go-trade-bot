package scalping

import (
	"testing"

	"go-trade-bot/app/strategies"
	"go-trade-bot/internal/exchange"
	"go-trade-bot/internal/indicators"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// See app/strategies/grid/strategy_test.go's top-of-file note on why the
// pre-port ScalpingProcessor can't be exercised directly (concrete
// internal/broker.Broker dependency, no network calls allowed in tests).

func candlesWithCloseVol(closes []float64, volumes []float64) []exchange.Candle {
	candles := make([]exchange.Candle, len(closes))
	for i := range closes {
		candles[i] = exchange.Candle{Close: closes[i], Volume: volumes[i]}
	}
	return candles
}

func uniformVolumes(n int, v float64) []float64 {
	vols := make([]float64, n)
	for i := range vols {
		vols[i] = v
	}
	return vols
}

// A rising, high-volume-on-the-latest-candle, oversold-RSI, uptrending
// fixture that satisfies all four ShouldLong gates simultaneously.
func passingFixture() strategies.Context {
	// A monotonic rise drives RSI to exactly 100 (talib.Rsi with no down
	// moves at all), which fails the "<= 70" gate - mix in periodic small
	// pullbacks to keep RSI moderate, while still ending on a rising close
	// (latest > previous) to satisfy the fourth gate.
	closes := make([]float64, 60)
	base := 100.0
	for i := range closes {
		if i%2 == 0 {
			base += 0.5
		} else {
			base -= 0.4
		}
		closes[i] = base
	}
	closes[len(closes)-1] = closes[len(closes)-2] + 1 // guarantee the final move is a clean rise
	volumes := uniformVolumes(60, 10)
	volumes[len(volumes)-1] = 50 // latest candle volume well above average

	longTerm := make([]exchange.Candle, 50)
	ltBase := 90.0
	for i := range longTerm {
		ltBase += 0.5 // uptrending long-term series -> current > EMA
		longTerm[i] = exchange.Candle{Close: ltBase}
	}

	return strategies.Context{
		Candles:    candlesWithCloseVol(closes, volumes),
		Config:     map[string]interface{}{"take_profit_pct": 2.0, configLongTerm(): longTerm},
		Indicators: indicators.NewTalibAdapter(),
		Price:      closes[len(closes)-1],
		Symbol:     "BTCUSDT",
		Account:    strategies.Account{Available: 1000},
	}
}

func configLongTerm() string { return strategies.ConfigKeyLongTermCandles }

// AC#7: all four gates pass => buy.
func TestScalpingStrategy_ShouldLong_AllGatesPass(t *testing.T) {
	s := &ScalpingStrategy{}
	ctx := passingFixture()

	assert.True(t, s.ShouldLong(ctx))
	signal := s.GoLong(ctx)
	require.NotNil(t, signal.Buy)
	assert.Equal(t, ctx.Candles[len(ctx.Candles)-1].Close, signal.Buy.Price)
}

// AC#7 (edge case): same fixture, only the uptrend gate flipped to false =>
// no buy, proving each gate is independently load-bearing.
func TestScalpingStrategy_ShouldLong_False_WhenUptrendGateFails(t *testing.T) {
	s := &ScalpingStrategy{}
	ctx := passingFixture()

	// Flip the long-term series to a downtrend so isUptrend() returns false,
	// leaving every other gate (volume, RSI, rising close) untouched.
	longTerm := make([]exchange.Candle, 50)
	ltBase := 200.0
	for i := range longTerm {
		ltBase -= 0.5
		longTerm[i] = exchange.Candle{Close: ltBase}
	}
	ctx.Config[strategies.ConfigKeyLongTermCandles] = longTerm

	assert.False(t, s.ShouldLong(ctx))
}

func TestScalpingStrategy_ShouldLong_False_WhenVolumeGateFails(t *testing.T) {
	s := &ScalpingStrategy{}
	ctx := passingFixture()

	candles := make([]exchange.Candle, len(ctx.Candles))
	copy(candles, ctx.Candles)
	candles[len(candles)-1].Volume = 1 // below average
	ctx.Candles = candles

	assert.False(t, s.ShouldLong(ctx))
}

func TestScalpingStrategy_ShouldLong_False_WhenFallingClose(t *testing.T) {
	s := &ScalpingStrategy{}
	ctx := passingFixture()

	candles := make([]exchange.Candle, len(ctx.Candles))
	copy(candles, ctx.Candles)
	candles[len(candles)-1].Close = candles[len(candles)-2].Close - 1 // falling, not rising
	ctx.Candles = candles
	ctx.Price = candles[len(candles)-1].Close

	assert.False(t, s.ShouldLong(ctx))
}

func TestScalpingStrategy_UpdatePosition_TakeProfit(t *testing.T) {
	s := &ScalpingStrategy{}
	ctx := passingFixture()
	ctx.Position = &strategies.Position{Symbol: "BTCUSDT", EntryPrice: ctx.Price / 1.05, Quantity: 1}

	signal := s.UpdatePosition(ctx)
	require.NotNil(t, signal)
	require.NotNil(t, signal.Sell)
	assert.Equal(t, ctx.Price, signal.Sell.Price)
}

// AC (Spec 08 table): stop_loss_pct removed - a loss that would have
// tripped the old stop_loss_pct branch produces no software sell signal.
func TestScalpingStrategy_UpdatePosition_NoStopLossBranch(t *testing.T) {
	s := &ScalpingStrategy{}
	ctx := passingFixture()
	ctx.Config["take_profit_pct"] = 50.0
	ctx.Position = &strategies.Position{Symbol: "BTCUSDT", EntryPrice: ctx.Price * 1.05, Quantity: 1} // currently at a loss

	signal := s.UpdatePosition(ctx)
	assert.Nil(t, signal)
}

// AC#8-equivalent: fewer than 10 candles => no panic, no signal.
func TestScalpingStrategy_InsufficientCandles_NoPanic(t *testing.T) {
	s := &ScalpingStrategy{}
	ctx := passingFixture()
	ctx.Candles = ctx.Candles[:5]

	assert.NotPanics(t, func() {
		assert.False(t, s.ShouldLong(ctx))
	})
}

func TestScalpingStrategy_ShouldShort_AlwaysFalse(t *testing.T) {
	s := &ScalpingStrategy{}
	assert.False(t, s.ShouldShort(strategies.Context{}))
}

func TestScalpingStrategy_Name(t *testing.T) {
	assert.Equal(t, "scalping", (&ScalpingStrategy{}).Name())
}
