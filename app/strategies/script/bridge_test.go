package script

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go-trade-bot/app/strategies"
	"go-trade-bot/internal/exchange"
	"go-trade-bot/internal/indicators"
	"go-trade-bot/internal/memcache"
)

func candles(n int, base float64) []exchange.Candle {
	out := make([]exchange.Candle, 0, n)
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		c := base + float64(i)
		out = append(out, exchange.Candle{
			Symbol: "BTCUSDT", Timeframe: "1m",
			OpenTime: start.Add(time.Duration(i) * time.Minute),
			Open:     c, High: c + 1, Low: c - 1, Close: c, Volume: 10,
		})
	}
	return out
}

func baseContext() strategies.Context {
	return strategies.Context{
		Candles:    candles(100, 50000),
		Account:    strategies.Account{Available: 1000},
		Config:     map[string]interface{}{"stop_loss_pct": 2.0},
		Indicators: indicators.NewTalibAdapter(),
		Price:      50099,
		Timeframe:  "1m",
		Symbol:     "BTCUSDT",
		Mode:       strategies.ModeDryRun,
	}
}

// AC#1
func TestCallHook_ContextPriceAndConfigRoundTrip(t *testing.T) {
	r := NewRunner(time.Second, nil)
	cctx := baseContext()
	cctx.Price = 100
	src := `function should_long(c) return c.price < 60000 and c.config.stop_loss_pct == 2.0 end`
	res, _, err := r.CallHook("s", "should_long", cctx, nil, src, HookBool, nil)
	require.NoError(t, err)
	assert.True(t, res.Bool)
}

// AC#2
func TestMarshalConfig_SkipsEngineOwnedKeys(t *testing.T) {
	r := NewRunner(time.Second, nil)
	cctx := baseContext()
	cctx.Config[strategies.ConfigKeyCache] = memcache.NewInMemoryCache()
	src := `function should_long(c) return c.config._cache == nil end`
	res, _, err := r.CallHook("s", "should_long", cctx, nil, src, HookBool, nil)
	require.NoError(t, err)
	assert.True(t, res.Bool, "engine-owned config key must not leak into ctx.config")
}

// AC#3
func TestCallHook_PositionNilVsTable(t *testing.T) {
	r := NewRunner(time.Second, nil)
	src := `function should_long(c) return c.position == nil end`

	cctx := baseContext()
	cctx.Position = nil
	res, _, err := r.CallHook("s", "should_long", cctx, nil, src, HookBool, nil)
	require.NoError(t, err)
	assert.True(t, res.Bool)

	cctx.Position = &strategies.Position{Symbol: "BTCUSDT", EntryPrice: 100, Quantity: 1}
	res, _, err = r.CallHook("s", "should_long", cctx, nil, src, HookBool, nil)
	require.NoError(t, err)
	assert.False(t, res.Bool)
}

// AC#4
func TestCallHook_GoLong_SignalFieldByField(t *testing.T) {
	r := NewRunner(time.Second, nil)
	cctx := baseContext()
	src := `function go_long(c) return {buy={qty=0.01, price=c.price}, stop_loss={qty=0.01, price=c.price*0.98}} end`
	res, _, err := r.CallHook("s", "go_long", cctx, nil, src, HookSignal, nil)
	require.NoError(t, err)
	require.NotNil(t, res.Signal)
	require.NotNil(t, res.Signal.Buy)
	assert.Equal(t, 0.01, res.Signal.Buy.Qty)
	assert.Equal(t, cctx.Price, res.Signal.Buy.Price)
	require.NotNil(t, res.Signal.StopLoss)
	assert.Nil(t, res.Signal.Sell)
	assert.Nil(t, res.Signal.TakeProfit)
}

// AC#5
func TestCallHook_UpdatePosition_NilIsHold(t *testing.T) {
	r := NewRunner(time.Second, nil)
	cctx := baseContext()
	src := `function update_position(c) return nil end`
	res, _, err := r.CallHook("s", "update_position", cctx, nil, src, HookOptSignal, nil)
	require.NoError(t, err)
	assert.Nil(t, res.Signal)
}

// AC#6
func TestCallHook_GoLong_MalformedReturn_Errors(t *testing.T) {
	r := NewRunner(time.Second, nil)
	cctx := baseContext()
	src := `function go_long(c) return "oops" end`
	_, _, err := r.CallHook("s", "go_long", cctx, nil, src, HookSignal, nil)
	require.Error(t, err)
}

// AC#7
func TestCallHook_StateRoundTrips(t *testing.T) {
	r := NewRunner(time.Second, nil)
	cctx := baseContext()
	src := `function should_long(c) state.ladder_level = state.ladder_level + 1; return state.ladder_level == 3 end`
	res, newState, err := r.CallHook("s", "should_long", cctx, map[string]interface{}{"ladder_level": 2.0}, src, HookBool, nil)
	require.NoError(t, err)
	assert.True(t, res.Bool)
	assert.Equal(t, 3.0, newState["ladder_level"])
}

// AC#8
func TestCallHook_IndRSI_MatchesRealAdapter(t *testing.T) {
	r := NewRunner(time.Second, nil)
	cctx := baseContext()
	provider := indicators.NewTalibAdapter()
	expected := provider.RSI(cctx.Candles, 14)
	cctx.Config["expected_rsi"] = expected[len(expected)-1]

	src := `function should_long(c) return math.abs(ind.rsi(14) - c.config.expected_rsi) < 1e-9 end`
	res, _, err := r.CallHook("s", "should_long", cctx, nil, src, HookBool, nil)
	require.NoError(t, err)
	assert.True(t, res.Bool, "ind.rsi(14) must match IndicatorProvider.RSI's last element exactly")
}

// AC#9
func TestCallHook_IndRSI_InsufficientCandles_CatchableLuaError(t *testing.T) {
	r := NewRunner(time.Second, nil)
	cctx := baseContext()
	cctx.Candles = candles(5, 50000) // fewer than period=14
	src := `
		function should_long(c)
			local ok, err = pcall(function() return ind.rsi(14) end)
			return ok == false
		end
	`
	res, _, err := r.CallHook("s", "should_long", cctx, nil, src, HookBool, nil)
	require.NoError(t, err)
	assert.True(t, res.Bool, "ind.rsi with insufficient candles must be a catchable Lua error, not a Go panic")
}

// AC#10
func TestCallHook_TraceRecorder_CapturesIndicatorAndLog(t *testing.T) {
	r := NewRunner(time.Second, nil)
	cctx := baseContext()
	trace := NewTraceRecorder(time.Now(), cctx.Candles[len(cctx.Candles)-1])
	src := `function should_long(c) ind.rsi(14); debug.log("armed", true); return true end`
	_, _, err := r.CallHook("s", "should_long", cctx, nil, src, HookBool, trace)
	require.NoError(t, err)
	rec := trace.Record()
	require.Len(t, rec.Indicators, 1)
	assert.Equal(t, "rsi", rec.Indicators[0].Name)
	require.Len(t, rec.Log, 1)
	assert.Equal(t, "armed", rec.Log[0].Label)
	assert.Equal(t, true, rec.Log[0].Value)
}

// AC#11
func TestCallHook_NilTraceRecorder_NoPanic(t *testing.T) {
	r := NewRunner(time.Second, nil)
	cctx := baseContext()
	src := `function should_long(c) ind.rsi(14); debug.log("armed", true); return true end`
	assert.NotPanics(t, func() {
		_, _, err := r.CallHook("s", "should_long", cctx, nil, src, HookBool, nil)
		require.NoError(t, err)
	})
}

// AC#12
func TestCallHook_MissingOptionalHooks_SafeDefault(t *testing.T) {
	r := NewRunner(time.Second, nil)
	cctx := baseContext()
	src := `function should_long(c) return true end` // no should_short/before/after/terminate

	res, _, err := r.CallHook("s", "should_short", cctx, nil, src, HookBool, nil)
	require.NoError(t, err)
	assert.False(t, res.Bool)

	_, _, err = r.CallHook("s", "before", cctx, nil, src, HookVoid, nil)
	require.NoError(t, err)
}

// AC#13
func TestCallHook_LuaError_IncrementsMetricNotPanicCounter(t *testing.T) {
	collector := newTestCollector()
	r := NewRunner(time.Second, collector)
	cctx := baseContext()
	src := `function should_long(c) error("boom") end`
	_, _, err := r.CallHook("s", "should_long", cctx, nil, src, HookBool, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom")
}
