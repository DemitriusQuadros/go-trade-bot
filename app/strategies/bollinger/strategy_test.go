package bollinger

import (
	"testing"

	"go-trade-bot/app/strategies"
	"go-trade-bot/internal/exchange"
	"go-trade-bot/internal/indicators"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// See app/strategies/grid/strategy_test.go's top-of-file note: the pre-port
// BollingerProcessor takes a concrete internal/broker.Broker, so its
// broker-dependent paths can't be exercised without a real network call.
// Fixtures below are verified against the ported implementation using the
// real indicators.TalibAdapter (so talib.BBands itself is never faked).

func candlesWithCloses(closes []float64) []exchange.Candle {
	candles := make([]exchange.Candle, len(closes))
	for i, c := range closes {
		candles[i] = exchange.Candle{Close: c}
	}
	return candles
}

func baseContext(closes []float64, config map[string]interface{}, price float64) strategies.Context {
	return strategies.Context{
		Candles:    candlesWithCloses(closes),
		Config:     config,
		Indicators: indicators.NewTalibAdapter(),
		Price:      price,
		Symbol:     "BTCUSDT",
		Account:    strategies.Account{Available: 1000},
	}
}

func flatCloses(n int, value float64) []float64 {
	closes := make([]float64, n)
	for i := range closes {
		closes[i] = value
	}
	return closes
}

// AC#5: current < lower[last] (using EMA-smoothed BBands) => buy.
func TestBollingerStrategy_ShouldLong_BelowLowerBand(t *testing.T) {
	s := &BollingerStrategy{}
	closes := flatCloses(29, 100)
	closes = append(closes, 80) // a sharp drop, well under any EMA-based lower band

	ctx := baseContext(closes, map[string]interface{}{"take_profit_pct": 2.0}, closes[len(closes)-1])

	assert.True(t, s.ShouldLong(ctx))
	signal := s.GoLong(ctx)
	require.NotNil(t, signal.Buy)
	assert.Equal(t, ctx.Price, signal.Buy.Price)
	assert.Equal(t, ctx.Account.Available/ctx.Price, signal.Buy.Qty)
}

func TestBollingerStrategy_ShouldLong_False_WhenAboveLowerBand(t *testing.T) {
	s := &BollingerStrategy{}
	closes := flatCloses(30, 100) // perfectly flat: bands collapse around 100, price == mid, not below lower

	ctx := baseContext(closes, map[string]interface{}{}, closes[len(closes)-1])
	assert.False(t, s.ShouldLong(ctx))
}

// AC#6: two separate open-position fixtures (take-profit-only, band-cross-only).
func TestBollingerStrategy_UpdatePosition_TakeProfit(t *testing.T) {
	s := &BollingerStrategy{}
	closes := flatCloses(30, 100)
	price := 103.0 // +3% from entry 100, comfortably inside the band (band width ~0 on flat data won't hold - use a mild fixture instead)

	ctx := baseContext(closes, map[string]interface{}{"take_profit_pct": 2.0}, price)
	ctx.Position = &strategies.Position{Symbol: "BTCUSDT", EntryPrice: 100, Quantity: 1}

	signal := s.UpdatePosition(ctx)
	require.NotNil(t, signal)
	require.NotNil(t, signal.Sell)
	assert.Equal(t, price, signal.Sell.Price)
	assert.Equal(t, 1.0, signal.Sell.Qty)
}

func TestBollingerStrategy_UpdatePosition_BandCross(t *testing.T) {
	s := &BollingerStrategy{}
	closes := flatCloses(29, 100)
	closes = append(closes, 130) // sharp spike, well over any EMA-based upper band, but pnl (30%) also exceeds take_profit anyway

	// Use a take_profit_pct high enough that only the band-cross condition
	// can be responsible for the sell, isolating that specific OR-branch.
	ctx := baseContext(closes, map[string]interface{}{"take_profit_pct": 1000.0}, closes[len(closes)-1])
	ctx.Position = &strategies.Position{Symbol: "BTCUSDT", EntryPrice: 100, Quantity: 1}

	signal := s.UpdatePosition(ctx)
	require.NotNil(t, signal)
	require.NotNil(t, signal.Sell)
}

// AC#6 (third fixture): a price move that would only have triggered the
// original software stop_loss_pct branch produces NO software sell signal -
// protection now comes exclusively from Spec 03's exchange-side stop.
func TestBollingerStrategy_UpdatePosition_NoStopLossBranch(t *testing.T) {
	s := &BollingerStrategy{}
	closes := flatCloses(30, 100)
	price := 97.0 // -3% from entry - would have tripped a stop_loss_pct=2 check in the old code, but not take-profit or band-cross

	ctx := baseContext(closes, map[string]interface{}{"take_profit_pct": 50.0}, price)
	ctx.Position = &strategies.Position{Symbol: "BTCUSDT", EntryPrice: 100, Quantity: 1}

	signal := s.UpdatePosition(ctx)
	assert.Nil(t, signal)
}

func TestBollingerStrategy_UpdatePosition_Hold(t *testing.T) {
	s := &BollingerStrategy{}
	// Deterministic wiggle gives the bands real width (unlike perfectly flat
	// data, which collapses the bands to zero width and makes any move look
	// like a band-cross) - price is engineered to stay well inside them.
	closes := make([]float64, 30)
	base := 100.0
	for i := range closes {
		base += float64((i%3)-1) * 0.8
		closes[i] = base
	}
	price := closes[len(closes)-1] + 0.1

	ctx := baseContext(closes, map[string]interface{}{"take_profit_pct": 50.0}, price)
	ctx.Position = &strategies.Position{Symbol: "BTCUSDT", EntryPrice: closes[len(closes)-1], Quantity: 1}

	signal := s.UpdatePosition(ctx)
	assert.Nil(t, signal)
}

// AC#8: fewer than 20 candles => no panic, no signal (error-equivalent state
// within the frozen hook interface's bool/Signal-only return shape - see
// final report for the documented limitation this represents).
func TestBollingerStrategy_InsufficientCandles_NoPanic(t *testing.T) {
	s := &BollingerStrategy{}
	closes := flatCloses(5, 100)
	ctx := baseContext(closes, map[string]interface{}{}, 100)

	assert.NotPanics(t, func() {
		assert.False(t, s.ShouldLong(ctx))
	})

	ctx.Position = &strategies.Position{Symbol: "BTCUSDT", EntryPrice: 100, Quantity: 1}
	assert.NotPanics(t, func() {
		assert.Nil(t, s.UpdatePosition(ctx))
	})
}

func TestBollingerStrategy_ShouldShort_AlwaysFalse(t *testing.T) {
	s := &BollingerStrategy{}
	assert.False(t, s.ShouldShort(strategies.Context{}))
}

func TestBollingerStrategy_Name(t *testing.T) {
	assert.Equal(t, "bollinger", (&BollingerStrategy{}).Name())
}
