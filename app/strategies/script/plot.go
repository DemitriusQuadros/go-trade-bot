package script

import lua "github.com/yuin/gopher-lua"

// bindPlot exposes the top-level Lua global plot(name, value, color) - a
// side-effecting "draw this" call structurally matching bindDebugLog (own
// file would be one option too, but this project's existing convention
// puts single-binding-function files directly in app/strategies/script/,
// e.g. this file mirrors indicators.go's bindDebugLog placement pattern).
// color is optional; "" signals "resolve the default" to TraceRecorder.
// No-op when trace is nil (production dryrun/paper/live), matching every
// other binding in this package.
func bindPlot(L *lua.LState, trace *TraceRecorder) {
	L.SetGlobal("plot", L.NewFunction(func(L *lua.LState) int {
		name := L.CheckString(1)
		value := L.CheckNumber(2)
		color := ""
		if L.GetTop() >= 3 {
			color = L.CheckString(3)
		}
		if trace != nil {
			trace.LogPlot(name, float64(value), color)
		}
		return 0
	}))
}
