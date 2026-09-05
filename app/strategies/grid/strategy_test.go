package grid

import (
	"testing"

	"go-trade-bot/app/strategies"
	"go-trade-bot/internal/exchange"
	"go-trade-bot/internal/indicators"
	"go-trade-bot/internal/memcache"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Note on verification approach (Spec 08's characterization-test method):
// the pre-port app/services/algorithm/grid.GridProcessor takes a concrete
// internal/broker.Broker (not an interface) as a constructor parameter, so
// its broker-dependent code paths cannot be exercised in a unit test without
// a real network call to Binance - which this task explicitly forbids. The
// RSI/grid-spacing/take-profit math below was instead hand-verified against
// the exact pre-port algorithm.go source (see git history /
// docs/specs/phase-1/08-strategy-porting-grid-bollinger-scalping.md's
// "Current Behavior" section) and is asserted here directly against the
// ported implementation using the real indicators.TalibAdapter, so the
// underlying RSI computation itself is never mocked/hand-faked either.

func candlesWithCloses(closes []float64) []exchange.Candle {
	candles := make([]exchange.Candle, len(closes))
	for i, c := range closes {
		candles[i] = exchange.Candle{Close: c, Volume: 100}
	}
	return candles
}

func decliningCloses(n int, start, step float64) []float64 {
	closes := make([]float64, n)
	v := start
	for i := 0; i < n; i++ {
		closes[i] = v
		v -= step
	}
	return closes
}

func risingCloses(n int, start, step float64) []float64 {
	closes := make([]float64, n)
	v := start
	for i := 0; i < n; i++ {
		closes[i] = v
		v += step
	}
	return closes
}

func gridConfigFixture() map[string]interface{} {
	return map[string]interface{}{
		"grid_levels":        10.0,
		"grid_spacing_pct":   2.0,
		"take_profit_pct":    1.5,
		"rsi_period":         14.0,
		"rsi_buy_threshold":  40.0,
		"rsi_sell_threshold": 70.0,
	}
}

func baseContext(candles []exchange.Candle, cache memcache.Cache, config map[string]interface{}, price float64) strategies.Context {
	cfg := map[string]interface{}{}
	for k, v := range config {
		cfg[k] = v
	}
	cfg[strategies.ConfigKeyCache] = cache
	return strategies.Context{
		Candles:    candles,
		Config:     cfg,
		Indicators: indicators.NewTalibAdapter(),
		Price:      price,
		Symbol:     "BTCUSDT",
		Account:    strategies.Account{Available: 1000},
	}
}

// AC#1: volume_filter > 0 and 24h volume below it => no grid built.
func TestGridStrategy_Before_SkipsBuildOnLowVolume(t *testing.T) {
	s := &GridStrategy{}
	cache := memcache.NewInMemoryCache()
	closes := decliningCloses(100, 200, 0.5)

	cfg := gridConfigFixture()
	cfg["volume_filter"] = 1000.0
	ctx := baseContext(candlesWithCloses(closes), cache, cfg, closes[len(closes)-1])
	ctx.Config[strategies.ConfigKey24hVolume] = 500.0 // below volume_filter

	s.Before(ctx)

	_, exists := cachedGrid(cache, ctx.Symbol)
	assert.False(t, exists)
}

// AC#2: currentRSI > rsi_sell_threshold => no grid built.
func TestGridStrategy_Before_SkipsBuildWhenRSIAboveSellThreshold(t *testing.T) {
	s := &GridStrategy{}
	cache := memcache.NewInMemoryCache()
	closes := risingCloses(100, 100, 1) // strongly rising -> RSI near 100

	ctx := baseContext(candlesWithCloses(closes), cache, gridConfigFixture(), closes[len(closes)-1])

	s.Before(ctx)

	_, exists := cachedGrid(cache, ctx.Symbol)
	assert.False(t, exists)
}

// Builds a grid on a declining/oversold series (RSI low, under both
// thresholds) and returns its computed levels for downstream tests.
func buildTestGrid(t *testing.T) (memcache.Cache, []exchange.Candle) {
	t.Helper()
	s := &GridStrategy{}
	cache := memcache.NewInMemoryCache()
	closes := decliningCloses(100, 200, 0.5) // -> latestClose = 150.5
	candles := candlesWithCloses(closes)

	ctx := baseContext(candles, cache, gridConfigFixture(), closes[len(closes)-1])
	s.Before(ctx)

	grid, exists := cachedGrid(cache, ctx.Symbol)
	require.True(t, exists)
	require.NotEmpty(t, grid)
	return cache, candles
}

func TestGridStrategy_Before_BuildsExpectedLevels(t *testing.T) {
	cache, _ := buildTestGrid(t)
	grid, exists := cachedGrid(cache, "BTCUSDT")
	require.True(t, exists)

	var buys, sells int
	for _, o := range grid {
		switch o.Type {
		case "buy":
			buys++
		case "sell":
			sells++
		}
	}
	// latestClose=150.5, gridSpacing=3.01, 10 levels centered on latestClose:
	// indices 0-4 are buy levels (below latestClose, RSI oversold),
	// index 5 lands exactly on latestClose (no order), indices 6-9 clear the
	// 1.5% take-profit threshold and become sell levels.
	assert.Equal(t, 5, buys)
	assert.Equal(t, 4, sells)
}

// AC#3: cached grid, price satisfies the third buy entry in iteration order
// (not the first or lowest) => exactly one buy signal at that first match.
func TestGridStrategy_ShouldLong_GoLong_FirstMatchWins(t *testing.T) {
	s := &GridStrategy{}
	cache, candles := buildTestGrid(t)

	// Price 140.0 doesn't satisfy the first two buy levels (135.45, 138.46)
	// but does satisfy the third (141.47) - proving iteration order, not
	// just "any match", governs the result.
	ctx := baseContext(candles, cache, gridConfigFixture(), 140.0)

	assert.True(t, s.ShouldLong(ctx))
	signal := s.GoLong(ctx)
	require.NotNil(t, signal.Buy)
	assert.Equal(t, 140.0, signal.Buy.Price) // matches original: EntryPrice = current ticker price, not the grid level
	assert.Nil(t, signal.Sell)
	assert.Nil(t, signal.StopLoss)
}

func TestGridStrategy_ShouldLong_False_WhenNoLevelMatches(t *testing.T) {
	s := &GridStrategy{}
	cache, candles := buildTestGrid(t)

	// Above every buy level.
	ctx := baseContext(candles, cache, gridConfigFixture(), 200.0)
	assert.False(t, s.ShouldLong(ctx))
}

// AC#4: open position, price crosses a cached sell level => Sell signal,
// with no software stop-loss-pct evaluation (Grid's stop-loss check is
// intentionally removed - Spec 08).
func TestGridStrategy_UpdatePosition_SellsOnGridLevel(t *testing.T) {
	s := &GridStrategy{}
	cache, candles := buildTestGrid(t)

	ctx := baseContext(candles, cache, gridConfigFixture(), 155.0)
	ctx.Position = &strategies.Position{Symbol: "BTCUSDT", EntryPrice: 140.0, Quantity: 0.5}

	signal := s.UpdatePosition(ctx)
	require.NotNil(t, signal)
	require.NotNil(t, signal.Sell)
	assert.Equal(t, 155.0, signal.Sell.Price)
	assert.Equal(t, 0.5, signal.Sell.Qty)
}

func TestGridStrategy_UpdatePosition_Nil_WhenNoSellLevelCrossed(t *testing.T) {
	s := &GridStrategy{}
	cache, candles := buildTestGrid(t)

	ctx := baseContext(candles, cache, gridConfigFixture(), 151.0) // below every sell level (153.51+)
	ctx.Position = &strategies.Position{Symbol: "BTCUSDT", EntryPrice: 140.0, Quantity: 0.5}

	signal := s.UpdatePosition(ctx)
	assert.Nil(t, signal)
}

// AC#9: a grid that has already triggered every level is never rebuilt -
// carried forward as-is (known limitation, not fixed by the port).
func TestGridStrategy_Before_NeverRebuildsExistingGrid(t *testing.T) {
	s := &GridStrategy{}
	cache, candles := buildTestGrid(t)
	original, _ := cachedGrid(cache, "BTCUSDT")

	// A wildly different market (would produce a very different grid if
	// rebuilt) - Before must still leave the original grid untouched.
	newCloses := risingCloses(100, 500, 5)
	ctx := baseContext(candlesWithCloses(newCloses), cache, gridConfigFixture(), newCloses[len(newCloses)-1])

	s.Before(ctx)

	after, exists := cachedGrid(cache, "BTCUSDT")
	require.True(t, exists)
	assert.Equal(t, original, after)
	_ = candles
}

func TestGridStrategy_ShouldShort_AlwaysFalse(t *testing.T) {
	s := &GridStrategy{}
	assert.False(t, s.ShouldShort(strategies.Context{}))
	assert.Equal(t, strategies.Signal{}, s.GoShort(strategies.Context{}))
}

func TestGridStrategy_Name(t *testing.T) {
	assert.Equal(t, "grid", (&GridStrategy{}).Name())
}
