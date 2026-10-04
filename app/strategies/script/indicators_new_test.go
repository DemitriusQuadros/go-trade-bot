package script

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go-trade-bot/internal/indicators"
)

// This file covers a representative sample of the ind.* closures added
// beyond the original six (rsi/bollinger/ema/sma/macd/atr) - one per
// return-arity shape (1-value, 2-value, 3-value) plus a zero-param call and
// an insufficient-candles error path, following the exact pattern
// bridge_test.go's AC#8/AC#9 already established for ind.rsi: compare the
// Lua closure's result against the real TalibAdapter call for the same
// candles, byte for byte.

// Single return value, extra int param (period).
func TestCallHook_IndADX_MatchesRealAdapter(t *testing.T) {
	r := NewRunner(time.Second, nil)
	cctx := baseContext()
	provider := indicators.NewTalibAdapter()
	expected := provider.ADX(cctx.Candles, 14)
	cctx.Config["expected_adx"] = expected[len(expected)-1]

	src := `function should_long(c) return math.abs(ind.adx(14) - c.config.expected_adx) < 1e-9 end`
	res, _, err := r.CallHook("s", "should_long", cctx, nil, src, HookBool, nil)
	require.NoError(t, err)
	assert.True(t, res.Bool, "ind.adx(14) must match IndicatorProvider.ADX's last element exactly")
}

// Two return values (high/low based, no close needed).
func TestCallHook_IndAroon_MatchesRealAdapter(t *testing.T) {
	r := NewRunner(time.Second, nil)
	cctx := baseContext()
	provider := indicators.NewTalibAdapter()
	down, up := provider.Aroon(cctx.Candles, 14)
	cctx.Config["expected_down"] = down[len(down)-1]
	cctx.Config["expected_up"] = up[len(up)-1]

	src := `
		function should_long(c)
			local down, up = ind.aroon(14)
			return math.abs(down - c.config.expected_down) < 1e-9 and math.abs(up - c.config.expected_up) < 1e-9
		end
	`
	res, _, err := r.CallHook("s", "should_long", cctx, nil, src, HookBool, nil)
	require.NoError(t, err)
	assert.True(t, res.Bool, "ind.aroon(14) must match IndicatorProvider.Aroon's last elements exactly")
}

// Three return values, all MAType params hardcoded to SMA on the Go side.
func TestCallHook_IndMACDExt_MatchesRealAdapter(t *testing.T) {
	r := NewRunner(time.Second, nil)
	cctx := baseContext()
	provider := indicators.NewTalibAdapter()
	macd, signal, hist := provider.MACDExt(cctx.Candles, 12, 26, 9)
	cctx.Config["expected_macd"] = macd[len(macd)-1]
	cctx.Config["expected_signal"] = signal[len(signal)-1]
	cctx.Config["expected_hist"] = hist[len(hist)-1]

	src := `
		function should_long(c)
			local macd, signal, hist = ind.macdext(12, 26, 9)
			return math.abs(macd - c.config.expected_macd) < 1e-9
				and math.abs(signal - c.config.expected_signal) < 1e-9
				and math.abs(hist - c.config.expected_hist) < 1e-9
		end
	`
	res, _, err := r.CallHook("s", "should_long", cctx, nil, src, HookBool, nil)
	require.NoError(t, err)
	assert.True(t, res.Bool, "ind.macdext(12,26,9) must match IndicatorProvider.MACDExt's last elements exactly")
}

// Zero extra params - close+volume only.
func TestCallHook_IndOBV_MatchesRealAdapter(t *testing.T) {
	r := NewRunner(time.Second, nil)
	cctx := baseContext()
	provider := indicators.NewTalibAdapter()
	expected := provider.OBV(cctx.Candles)
	cctx.Config["expected_obv"] = expected[len(expected)-1]

	src := `function should_long(c) return math.abs(ind.obv() - c.config.expected_obv) < 1e-9 end`
	res, _, err := r.CallHook("s", "should_long", cctx, nil, src, HookBool, nil)
	require.NoError(t, err)
	assert.True(t, res.Bool, "ind.obv() must match IndicatorProvider.OBV's last element exactly")
}

// Float params (not just int periods).
func TestCallHook_IndSAR_MatchesRealAdapter(t *testing.T) {
	r := NewRunner(time.Second, nil)
	cctx := baseContext()
	provider := indicators.NewTalibAdapter()
	expected := provider.SAR(cctx.Candles, 0.02, 0.2)
	cctx.Config["expected_sar"] = expected[len(expected)-1]

	src := `function should_long(c) return math.abs(ind.sar(0.02, 0.2) - c.config.expected_sar) < 1e-9 end`
	res, _, err := r.CallHook("s", "should_long", cctx, nil, src, HookBool, nil)
	require.NoError(t, err)
	assert.True(t, res.Bool, "ind.sar(0.02,0.2) must match IndicatorProvider.SAR's last element exactly")
}

// Insufficient candles must still be a catchable Lua error for a new
// indicator, not a Go panic escaping the closure - mirrors AC#9's ind.rsi
// coverage, exercising safeIndicatorClosure's recover() path for a
// multi-input (high/low/close) indicator this time.
func TestCallHook_IndADX_InsufficientCandles_CatchableLuaError(t *testing.T) {
	r := NewRunner(time.Second, nil)
	cctx := baseContext()
	cctx.Candles = candles(5, 50000) // fewer than period=14
	src := `
		function should_long(c)
			local ok, err = pcall(function() return ind.adx(14) end)
			return ok == false
		end
	`
	res, _, err := r.CallHook("s", "should_long", cctx, nil, src, HookBool, nil)
	require.NoError(t, err)
	assert.True(t, res.Bool, "ind.adx with insufficient candles must be a catchable Lua error, not a Go panic")
}
