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
	rec.LogIndicatorCall(strategies.Context{}, "rsi", map[string]any{"arg1": 14.0}, 27.5)

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

// TestTraceRecorder_LogPlot_OverridesAutoPlot verifies an explicit plot()
// call under the same name as an ind.* call wins - the auto-plot fallback
// is suppressed, and no duplicate same-name point is emitted (which would
// crash the frontend chart's setData, which requires strictly one point per
// series per timestamp).
func TestTraceRecorder_LogPlot_OverridesAutoPlot(t *testing.T) {
	rec := NewTraceRecorder(time.Now(), exchange.Candle{})
	rec.LogIndicatorCall(strategies.Context{}, "rsi", map[string]any{"arg1": 14.0}, 27.5)
	rec.LogPlot("rsi", 99.9, "", false)

	out := rec.Record()
	require.Len(t, out.Plots, 1, "explicit plot() must replace the auto-plot, not add a second point for the same name")
	assert.Equal(t, "rsi", out.Plots[0].Name)
	assert.Equal(t, 99.9, out.Plots[0].Value, "the explicit plot() value must win over the auto-plotted indicator value")
}

// TestTraceRecorder_LogIndicatorCall_LastCallWinsPerCycle verifies that
// calling the same bare indicator name twice in one cycle (e.g. two
// different RSI periods) auto-plots only the latest value, not two
// same-name points for the same timestamp.
func TestTraceRecorder_LogIndicatorCall_LastCallWinsPerCycle(t *testing.T) {
	rec := NewTraceRecorder(time.Now(), exchange.Candle{})
	rec.LogIndicatorCall(strategies.Context{}, "rsi", map[string]any{"arg1": 14.0}, 20.0)
	rec.LogIndicatorCall(strategies.Context{}, "rsi", map[string]any{"arg1": 21.0}, 45.0)

	out := rec.Record()
	require.Len(t, out.Plots, 1)
	assert.Equal(t, 45.0, out.Plots[0].Value)
}

// TestTraceRecorder_PlotCap_SharedBetweenAutoAndExplicit verifies
// auto-plots and explicit plot() calls draw from the same
// maxDistinctPlotNames budget - a script that calls 12 distinct indicators
// leaves no room for a 13th, whether that 13th comes from ind.* or plot().
func TestTraceRecorder_PlotCap_SharedBetweenAutoAndExplicit(t *testing.T) {
	rec := NewTraceRecorder(time.Now(), exchange.Candle{})
	names := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l"}
	for _, n := range names {
		rec.LogIndicatorCall(strategies.Context{}, n, nil, 1.0)
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
