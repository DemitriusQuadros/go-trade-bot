package script

import (
	"fmt"

	lua "github.com/yuin/gopher-lua"
	"go-trade-bot/app/strategies"
	"go-trade-bot/internal/indicators"
)

// bindIndicators exposes internal/indicators.IndicatorProvider as the Lua
// `ind` global table: ind.rsi(period), ind.bollinger(period, stddev_up[,
// stddev_down]) -> upper, mid, lower (three return values, Lua-idiomatic
// multi-return, not a table), ind.ema(period), ind.sma(period),
// ind.macd(fast, slow, signal) -> macd, signal, hist, ind.atr(period).
// Every closure closes over cctx.Candles for the CURRENT cycle only - a
// fresh table is bound per CallHook invocation, matching the "fresh LState
// per hook" design (no cross-cycle closure reuse).
func bindIndicators(L *lua.LState, cctx strategies.Context, trace *TraceRecorder) {
	ind := L.NewTable()

	L.SetField(ind, "rsi", L.NewFunction(safeIndicatorClosure(cctx, trace, "rsi", func(L *lua.LState, cctx strategies.Context) (int, error) {
		period := L.CheckInt(1)
		values := cctx.Indicators.RSI(cctx.Candles, period)
		if len(values) == 0 {
			return 0, fmt.Errorf("rsi(%d): no data", period)
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "bollinger", L.NewFunction(safeIndicatorClosure(cctx, trace, "bollinger", func(L *lua.LState, cctx strategies.Context) (int, error) {
		period := L.CheckInt(1)
		stdDevUp := L.CheckNumber(2)
		stdDevDown := stdDevUp
		if L.GetTop() >= 3 {
			stdDevDown = L.CheckNumber(3)
		}
		upper, mid, lower := cctx.Indicators.BollingerBands(cctx.Candles, period, float64(stdDevUp), float64(stdDevDown), indicators.MATypeSMA)
		if len(upper) == 0 || len(mid) == 0 || len(lower) == 0 {
			return 0, fmt.Errorf("bollinger(%d): no data", period)
		}
		L.Push(lua.LNumber(upper[len(upper)-1]))
		L.Push(lua.LNumber(mid[len(mid)-1]))
		L.Push(lua.LNumber(lower[len(lower)-1]))
		return 3, nil
	})))

	L.SetField(ind, "ema", L.NewFunction(safeIndicatorClosure(cctx, trace, "ema", func(L *lua.LState, cctx strategies.Context) (int, error) {
		period := L.CheckInt(1)
		values := cctx.Indicators.EMA(cctx.Candles, period)
		if len(values) == 0 {
			return 0, fmt.Errorf("ema(%d): no data", period)
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "sma", L.NewFunction(safeIndicatorClosure(cctx, trace, "sma", func(L *lua.LState, cctx strategies.Context) (int, error) {
		period := L.CheckInt(1)
		values := cctx.Indicators.SMA(cctx.Candles, period)
		if len(values) == 0 {
			return 0, fmt.Errorf("sma(%d): no data", period)
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetField(ind, "macd", L.NewFunction(safeIndicatorClosure(cctx, trace, "macd", func(L *lua.LState, cctx strategies.Context) (int, error) {
		fast := L.CheckInt(1)
		slow := L.CheckInt(2)
		signal := L.CheckInt(3)
		macd, sig, hist := cctx.Indicators.MACD(cctx.Candles, fast, slow, signal)
		if len(macd) == 0 || len(sig) == 0 || len(hist) == 0 {
			return 0, fmt.Errorf("macd(%d,%d,%d): no data", fast, slow, signal)
		}
		L.Push(lua.LNumber(macd[len(macd)-1]))
		L.Push(lua.LNumber(sig[len(sig)-1]))
		L.Push(lua.LNumber(hist[len(hist)-1]))
		return 3, nil
	})))

	L.SetField(ind, "atr", L.NewFunction(safeIndicatorClosure(cctx, trace, "atr", func(L *lua.LState, cctx strategies.Context) (int, error) {
		period := L.CheckInt(1)
		values := cctx.Indicators.ATR(cctx.Candles, period)
		if len(values) == 0 {
			return 0, fmt.Errorf("atr(%d): no data", period)
		}
		L.Push(lua.LNumber(values[len(values)-1]))
		return 1, nil
	})))

	L.SetGlobal("ind", ind)
}

// safeIndicatorClosure is THE required defensive wrapper (blueprint §0.3):
// go-talib (wrapped by the real IndicatorProvider) panics on
// insufficient-candle input (confirmed against
// internal/indicators/talib_adapter_test.go's
// TestRSI_InsufficientCandles_PassesThroughUnchanged, which actually asserts
// a panic via assert.Panics). Every indicator closure gets its own
// recover()->L.RaiseError conversion so a Go-side panic reached via a
// Lua-invoked closure becomes a proper Lua-catchable error, never an
// unrecovered panic unwinding through gopher-lua's call stack (which would
// otherwise be indistinguishable from a process crash - see blueprint §5).
func safeIndicatorClosure(
	cctx strategies.Context,
	trace *TraceRecorder,
	name string,
	fn func(L *lua.LState, cctx strategies.Context) (nret int, err error),
) lua.LGFunction {
	return func(L *lua.LState) (n int) {
		defer func() {
			if r := recover(); r != nil {
				L.RaiseError("ind.%s: %v", name, r)
			}
		}()
		params := luaArgsAsParams(L)
		nret, err := fn(L, cctx)
		if err != nil {
			L.RaiseError("ind.%s: %v", name, err)
		}
		if trace != nil {
			trace.LogIndicatorCall(cctx, name, params, luaReturnAsValue(L, nret))
		}
		return nret
	}
}

// luaArgsAsParams captures the closure's positional call args (already on
// the Lua stack when the closure runs) as a params map for the trace, keyed
// positionally (arg1, arg2, ...) since indicator closures take positional
// numeric args, not named ones.
func luaArgsAsParams(L *lua.LState) map[string]any {
	top := L.GetTop()
	params := make(map[string]any, top)
	for i := 1; i <= top; i++ {
		v := L.Get(i)
		if n, ok := v.(lua.LNumber); ok {
			params[fmt.Sprintf("arg%d", i)] = float64(n)
		}
	}
	return params
}

// luaReturnAsValue captures the closure's first pushed return value (the
// primary indicator reading) as the TraceRecord's logged float64 value -
// multi-return indicators (bollinger/macd) log only their first (upper/
// macd-line) value here, which is an acceptable simplification for trace
// display purposes (the frontend chart's indicator overlay uses this single
// value per call).
func luaReturnAsValue(L *lua.LState, nret int) float64 {
	if nret == 0 {
		return 0
	}
	top := L.GetTop()
	v := L.Get(top - nret + 1)
	if n, ok := v.(lua.LNumber); ok {
		return float64(n)
	}
	return 0
}

// bindDebugLog exposes debug.log(label, value) - a project-authored table,
// NOT gopher-lua's real (unopened) debug stdlib. No-op write path when
// trace is nil (production dryrun/paper/live), per blueprint §6.4.
func bindDebugLog(L *lua.LState, trace *TraceRecorder) {
	debugTable := L.NewTable()
	L.SetField(debugTable, "log", L.NewFunction(func(L *lua.LState) int {
		label := L.CheckString(1)
		value := L.Get(2)
		if trace != nil {
			trace.LogEntry(label, fromLuaValue(value))
		}
		return 0
	}))
	L.SetGlobal("debug", debugTable)
}
