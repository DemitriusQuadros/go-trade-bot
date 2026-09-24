package script

import (
	"testing"
	"time"

	"go-trade-bot/app/strategies"
	"go-trade-bot/internal/exchange"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTraceRecorder_LogIndicatorCall_AutoPlots verifies the auto-plot
// fallback: an indicator the script calls but never plot()s itself still
// shows up in the final Plots (so the chart's existing per-name checkbox
// picks it up automatically), while an indicator the script never calls
// never appears at all.
func TestTraceRecorder_LogIndicatorCall_AutoPlots(t *testing.T) {
	rec := NewTraceRecorder(time.Now(), exchange.Candle{})
	rec.LogIndicatorCall(strategies.Context{}, "rsi", map[string]any{"arg1": 14.0}, []float64{27.5})

	out := rec.Record()
	require.Len(t, out.Plots, 1)
	assert.Equal(t, "rsi", out.Plots[0].Name)
	assert.Equal(t, 27.5, out.Plots[0].Value)
	assert.NotEmpty(t, out.Plots[0].Color)

	// sma was never called - it must not appear anywhere in Plots.
	for _, p := range out.Plots {
		assert.NotEqual(t, "sma", p.Name)
	}
}

// TestTraceRecorder_LogIndicatorCall_MultiReturnAutoPlotsAllLines verifies
// the fix for a multi-return indicator (e.g. ind.bollinger) collapsing to a
// single auto-plotted line: every value the Lua closure returns must show
// up as its own named, colored, correctly-paned Plots entry.
func TestTraceRecorder_LogIndicatorCall_MultiReturnAutoPlotsAllLines(t *testing.T) {
	rec := NewTraceRecorder(time.Now(), exchange.Candle{})
	rec.LogIndicatorCall(strategies.Context{}, "bollinger", map[string]any{"arg1": 20.0, "arg2": 2.0}, []float64{110.0, 100.0, 90.0})

	out := rec.Record()
	require.Len(t, out.Plots, 3)

	byName := map[string]PlotPoint{}
	for _, p := range out.Plots {
		byName[p.Name] = p
	}

	upper, ok := byName["bollinger.upper"]
	require.True(t, ok, "expected bollinger.upper in Plots")
	assert.Equal(t, 110.0, upper.Value)
	assert.True(t, upper.Overlay, "bollinger bands share the price pane")
	assert.NotEmpty(t, upper.Color)

	mid, ok := byName["bollinger.mid"]
	require.True(t, ok, "expected bollinger.mid in Plots")
	assert.Equal(t, 100.0, mid.Value)
	assert.True(t, mid.Overlay)

	lower, ok := byName["bollinger.lower"]
	require.True(t, ok, "expected bollinger.lower in Plots")
	assert.Equal(t, 90.0, lower.Value)
	assert.True(t, lower.Overlay)

	// A different multi-return indicator's sub-lines must stay on the
	// oscillator pane (macd is not price-scale), distinguishing overlay
	// classification per sub-line, not just per top-level indicator name.
	rec2 := NewTraceRecorder(time.Now(), exchange.Candle{})
	rec2.LogIndicatorCall(strategies.Context{}, "macd", map[string]any{"arg1": 12.0, "arg2": 26.0, "arg3": 9.0}, []float64{1.5, 1.2, 0.3})
	out2 := rec2.Record()
	require.Len(t, out2.Plots, 3)
	for _, p := range out2.Plots {
		assert.False(t, p.Overlay, "macd sub-lines are oscillator-pane, not overlay: %s", p.Name)
	}
}

// TestTraceRecorder_LogPlot_OverridesAutoPlot verifies an explicit plot()
// call under the same name as an ind.* call wins - the auto-plot fallback
// is suppressed, and no duplicate same-name point is emitted (which would
// crash the frontend chart's setData, which requires strictly one point per
// series per timestamp).
func TestTraceRecorder_LogPlot_OverridesAutoPlot(t *testing.T) {
	rec := NewTraceRecorder(time.Now(), exchange.Candle{})
	rec.LogIndicatorCall(strategies.Context{}, "rsi", map[string]any{"arg1": 14.0}, []float64{27.5})
	rec.LogPlot("rsi", 99.9, "", false)

	out := rec.Record()
	require.Len(t, out.Plots, 1, "explicit plot() must replace the auto-plot, not add a second point for the same name")
	assert.Equal(t, "rsi", out.Plots[0].Name)
	assert.Equal(t, 99.9, out.Plots[0].Value, "the explicit plot() value must win over the auto-plotted indicator value")
}

// TestTraceRecorder_LogIndicatorCall_DisambiguatesDifferentParamsSameCycle
// verifies the fix for the reported bug: calling the same indicator twice
// in one cycle with DIFFERENT params (e.g. two EMAs at different periods)
// must plot both lines, distinguished by their params in the name, instead
// of the second call silently overwriting the first (the previous
// behavior - collapsing to a single "ema" point - meant only one of the two
// EMA lines ever rendered on the chart, which is what the user reported).
func TestTraceRecorder_LogIndicatorCall_DisambiguatesDifferentParamsSameCycle(t *testing.T) {
	rec := NewTraceRecorder(time.Now(), exchange.Candle{})
	rec.LogIndicatorCall(strategies.Context{}, "ema", map[string]any{"arg1": 9.0}, []float64{100.0})
	rec.LogIndicatorCall(strategies.Context{}, "ema", map[string]any{"arg1": 21.0}, []float64{95.0})

	out := rec.Record()
	require.Len(t, out.Plots, 2, "both EMA calls must produce distinct plot points")

	byName := map[string]PlotPoint{}
	for _, p := range out.Plots {
		byName[p.Name] = p
	}

	ema9, ok := byName["ema(9)"]
	require.True(t, ok, "expected ema(9) in Plots, got names: %v", plotNames(out.Plots))
	assert.Equal(t, 100.0, ema9.Value)
	assert.True(t, ema9.Overlay, "ema is a price-scale indicator")
	assert.NotEmpty(t, ema9.Color)

	ema21, ok := byName["ema(21)"]
	require.True(t, ok, "expected ema(21) in Plots, got names: %v", plotNames(out.Plots))
	assert.Equal(t, 95.0, ema21.Value)
	assert.True(t, ema21.Overlay)
	assert.NotEmpty(t, ema21.Color)
	assert.NotEqual(t, ema9.Color, ema21.Color, "two distinct lines should get distinct palette colors")
}

// TestTraceRecorder_LogIndicatorCall_SingleCallKeepsBareName verifies the
// common case (an indicator called exactly once per cycle) is unaffected by
// disambiguation - it still plots under the plain "ema" name, not "ema(9)".
func TestTraceRecorder_LogIndicatorCall_SingleCallKeepsBareName(t *testing.T) {
	rec := NewTraceRecorder(time.Now(), exchange.Candle{})
	rec.LogIndicatorCall(strategies.Context{}, "ema", map[string]any{"arg1": 9.0}, []float64{100.0})

	out := rec.Record()
	require.Len(t, out.Plots, 1)
	assert.Equal(t, "ema", out.Plots[0].Name)
	assert.Equal(t, 100.0, out.Plots[0].Value)
}

// TestTraceRecorder_LogIndicatorCall_SameParamsCollapseToOnePoint verifies a
// genuine duplicate call (same indicator, identical params, called twice in
// one cycle - e.g. a script branch that happens to call ind.ema(9) from two
// code paths in the same cycle) still collapses to one point: there is
// nothing to disambiguate, so last-value-wins remains correct there.
func TestTraceRecorder_LogIndicatorCall_SameParamsCollapseToOnePoint(t *testing.T) {
	rec := NewTraceRecorder(time.Now(), exchange.Candle{})
	rec.LogIndicatorCall(strategies.Context{}, "ema", map[string]any{"arg1": 9.0}, []float64{100.0})
	rec.LogIndicatorCall(strategies.Context{}, "ema", map[string]any{"arg1": 9.0}, []float64{101.0})

	out := rec.Record()
	require.Len(t, out.Plots, 1)
	assert.Equal(t, "ema", out.Plots[0].Name)
	assert.Equal(t, 101.0, out.Plots[0].Value)
}

func plotNames(plots []PlotPoint) []string {
	names := make([]string, len(plots))
	for i, p := range plots {
		names[i] = p.Name
	}
	return names
}

// TestTraceRecorder_PlotCap_SharedBetweenAutoAndExplicit verifies
// auto-plots and explicit plot() calls draw from the same
// maxDistinctPlotNames budget - a script that calls 12 distinct indicators
// leaves no room for a 13th, whether that 13th comes from ind.* or plot().
func TestTraceRecorder_PlotCap_SharedBetweenAutoAndExplicit(t *testing.T) {
	rec := NewTraceRecorder(time.Now(), exchange.Candle{})
	names := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l"}
	for _, n := range names {
		rec.LogIndicatorCall(strategies.Context{}, n, nil, []float64{1.0})
	}
	// A 13th distinct name, this time via explicit plot() - must be refused.
	rec.LogPlot("m", 2.0, "", false)

	out := rec.Record()
	assert.Len(t, out.Plots, 12)
	for _, p := range out.Plots {
		assert.NotEqual(t, "m", p.Name)
	}
	// One-time cap-exceeded log entry.
	found := false
	for _, l := range out.Log {
		if l.Label == "plot_limit_exceeded" {
			found = true
		}
	}
	assert.True(t, found, "expected a plot_limit_exceeded log entry")
}
