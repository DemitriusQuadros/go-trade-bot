package script

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// AC#1: plot(name, value, color) called once per cycle across 3 cycles - each
// TraceRecord.Plots contains exactly one matching PlotPoint.
func TestBindPlot_ExplicitColor_RecordedAcrossCycles(t *testing.T) {
	r := NewRunner(time.Second, nil)
	cctx := baseContext()
	src := `function should_long(c) plot("ema20", 61234.5, "#38bdf8"); return false end`

	for i := 0; i < 3; i++ {
		trace := NewTraceRecorder(time.Now(), cctx.Candles[len(cctx.Candles)-1])
		_, _, err := r.CallHook("s", "should_long", cctx, nil, src, HookBool, trace)
		require.NoError(t, err)
		rec := trace.Record()
		require.Len(t, rec.Plots, 1)
		assert.Equal(t, PlotPoint{Name: "ema20", Value: 61234.5, Color: "#38bdf8"}, rec.Plots[0])
	}
}

// AC#2: plot(name, value) with no color argument resolves to the palette slot
// assigned by first-seen order, not a random/alphabetical choice.
func TestBindPlot_DefaultColor_ResolvedByFirstSeenOrder(t *testing.T) {
	r := NewRunner(time.Second, nil)
	cctx := baseContext()
	src := `function should_long(c)
		plot("ema20", 1, "#38bdf8")
		plot("ema50", 2)
		return false
	end`

	trace := NewTraceRecorder(time.Now(), cctx.Candles[len(cctx.Candles)-1])
	_, _, err := r.CallHook("s", "should_long", cctx, nil, src, HookBool, trace)
	require.NoError(t, err)
	rec := trace.Record()
	require.Len(t, rec.Plots, 2)
	assert.Equal(t, "ema50", rec.Plots[1].Name)
	assert.NotEmpty(t, rec.Plots[1].Color)
	assert.Equal(t, plotColorPalette[1], rec.Plots[1].Color)
}

// AC#3: 13 distinct names within one cycle - only the first 12 are recorded,
// and exactly one plot_limit_exceeded log entry names the 13th.
func TestBindPlot_SoftCap_DropsBeyond12DistinctNames(t *testing.T) {
	r := NewRunner(time.Second, nil)
	cctx := baseContext()

	var sb string
	for i := 1; i <= 13; i++ {
		sb += fmt.Sprintf(`plot("p%d", %d)`+"\n", i, i)
	}
	src := "function should_long(c)\n" + sb + "return false\nend"

	trace := NewTraceRecorder(time.Now(), cctx.Candles[len(cctx.Candles)-1])
	_, _, err := r.CallHook("s", "should_long", cctx, nil, src, HookBool, trace)
	require.NoError(t, err)
	rec := trace.Record()
	require.Len(t, rec.Plots, 12)

	var limitEntries []LogEntry
	for _, l := range rec.Log {
		if l.Label == "plot_limit_exceeded" {
			limitEntries = append(limitEntries, l)
		}
	}
	require.Len(t, limitEntries, 1)
	assert.Equal(t, "p13", limitEntries[0].Value)
}

// AC#4: the plotCapHit guard is per-TraceRecorder (per-cycle) - re-running the
// same over-cap script across multiple fresh TraceRecorders never produces
// more than one plot_limit_exceeded entry per cycle.
func TestBindPlot_SoftCap_GuardIsPerCycleNotGlobal(t *testing.T) {
	r := NewRunner(time.Second, nil)
	cctx := baseContext()

	var sb string
	for i := 1; i <= 13; i++ {
		sb += fmt.Sprintf(`plot("p%d", %d)`+"\n", i, i)
	}
	src := "function should_long(c)\n" + sb + "return false\nend"

	for cycle := 0; cycle < 3; cycle++ {
		trace := NewTraceRecorder(time.Now(), cctx.Candles[len(cctx.Candles)-1])
		_, _, err := r.CallHook("s", "should_long", cctx, nil, src, HookBool, trace)
		require.NoError(t, err)
		rec := trace.Record()

		count := 0
		for _, l := range rec.Log {
			if l.Label == "plot_limit_exceeded" {
				count++
			}
		}
		assert.Equal(t, 1, count, "cycle %d should have exactly one plot_limit_exceeded entry", cycle)
	}
}

// AC#5: same name plotted 3 times within one cycle - LogPlot appends every
// call, it never deduplicates/overwrites.
func TestBindPlot_SameNameMultipleCalls_AllAppended(t *testing.T) {
	r := NewRunner(time.Second, nil)
	cctx := baseContext()
	src := `function should_long(c)
		plot("x", 1)
		plot("x", 1)
		plot("x", 1)
		return false
	end`

	trace := NewTraceRecorder(time.Now(), cctx.Candles[len(cctx.Candles)-1])
	_, _, err := r.CallHook("s", "should_long", cctx, nil, src, HookBool, trace)
	require.NoError(t, err)
	rec := trace.Record()
	require.Len(t, rec.Plots, 3)
	for _, p := range rec.Plots {
		assert.Equal(t, "x", p.Name)
	}
}

// AC#6: a script that never calls plot() marshals "plots": [] (never null,
// never omitted), matching the Indicators/Log nil-to-empty-slice convention.
func TestTraceRecord_Plots_MarshalsAsEmptyArrayWhenUnset(t *testing.T) {
	rec := TraceRecord{Timestamp: time.Now()}
	data, err := rec.MarshalJSON()
	require.NoError(t, err)
	assert.Contains(t, string(data), `"plots":[]`)
}

// AC#7: production path (trace == nil) - plot() is a complete no-op, no panic.
func TestBindPlot_NilTrace_IsNoOp(t *testing.T) {
	r := NewRunner(time.Second, nil)
	cctx := baseContext()
	src := `function should_long(c) plot("x", 1, "#fff"); return true end`

	assert.NotPanics(t, func() {
		res, _, err := r.CallHook("s", "should_long", cctx, nil, src, HookBool, nil)
		require.NoError(t, err)
		assert.True(t, res.Bool)
	})
}
