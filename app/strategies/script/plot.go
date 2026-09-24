package script

import lua "github.com/yuin/gopher-lua"

// bindPlot exposes the top-level Lua global plot(name, value, color, overlay) -
// a side-effecting "draw this" call structurally matching bindDebugLog (own
// file would be one option too, but this project's existing convention
// puts single-binding-function files directly in app/strategies/script/,
// e.g. this file mirrors indicators.go's bindDebugLog placement pattern).
// color is optional; "" signals "resolve the default" to TraceRecorder.
// overlay is optional (default false): pass true to draw the line directly
// on the price pane (like TradingView draws a moving average over the
// candles) instead of the separate oscillator pane below - use it for
// anything on the same scale as price (a custom moving average, VWAP, a
// support/resistance level); leave it false for anything else (RSI-like
// oscillators, ratios, counts). No-op when trace is nil (production
// dryrun/paper/live), matching every other binding in this package.
func bindPlot(L *lua.LState, trace *TraceRecorder) {
	L.SetGlobal("plot", L.NewFunction(func(L *lua.LState) int {
		name := L.CheckString(1)
		value := L.CheckNumber(2)
		color := ""
		if L.GetTop() >= 3 {
			color = L.CheckString(3)
		}
		overlay := L.OptBool(4, false)
		if trace != nil {
			trace.LogPlot(name, float64(value), color, overlay)
		}
		return 0
	}))
}
