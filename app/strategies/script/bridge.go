package script

import (
	"fmt"
	"strings"

	lua "github.com/yuin/gopher-lua"
	"go-trade-bot/app/strategies"
)

// buildCtxTable marshals strategies.Context into the Lua ctx global exactly
// per blueprint §6.1. Candles are already capped at candleWindow (100) by
// the time they reach here (app/engine/engine.go's buildContext already
// applies that cap) - this function does not re-truncate.
func buildCtxTable(L *lua.LState, cctx strategies.Context) *lua.LTable {
	t := L.NewTable()
	L.SetField(t, "symbol", lua.LString(cctx.Symbol))
	L.SetField(t, "timeframe", lua.LString(cctx.Timeframe))
	L.SetField(t, "mode", lua.LString(cctx.Mode.String()))
	L.SetField(t, "price", lua.LNumber(cctx.Price))

	candles := L.NewTable()
	for _, c := range cctx.Candles {
		row := L.NewTable()
		L.SetField(row, "o", lua.LNumber(c.Open))
		L.SetField(row, "h", lua.LNumber(c.High))
		L.SetField(row, "l", lua.LNumber(c.Low))
		L.SetField(row, "c", lua.LNumber(c.Close))
		L.SetField(row, "v", lua.LNumber(c.Volume))
		L.SetField(row, "t", lua.LNumber(c.OpenTime.Unix()))
		candles.Append(row)
	}
	L.SetField(t, "candles", candles)

	if cctx.Position != nil {
		p := L.NewTable()
		L.SetField(p, "symbol", lua.LString(cctx.Position.Symbol))
		L.SetField(p, "entry_price", lua.LNumber(cctx.Position.EntryPrice))
		L.SetField(p, "quantity", lua.LNumber(cctx.Position.Quantity))
		if cctx.Position.StopLossPrice != nil {
			L.SetField(p, "stop_loss_price", lua.LNumber(*cctx.Position.StopLossPrice))
		}
		L.SetField(p, "opened_at", lua.LNumber(cctx.Position.OpenedAt.Unix()))
		L.SetField(t, "position", p)
	} else {
		L.SetField(t, "position", lua.LNil)
	}

	account := L.NewTable()
	L.SetField(account, "available", lua.LNumber(cctx.Account.Available))
	L.SetField(t, "account", account)

	L.SetField(t, "config", marshalConfig(L, cctx.Config))

	return t
}

// isEngineOwnedConfigKey reports whether a Context.Config key was injected
// by app/engine itself (never user-authored strategy JSON). Every such key
// uses a leading underscore by convention (app/strategies/config_keys.go),
// so this is a prefix check rather than an exhaustive name list - correctly
// covers strategies.ConfigKeyCache ("_cache"), ConfigKey24hVolume
// ("_24h_volume"), ConfigKeyLongTermCandles ("_long_term_candles"), and any
// ConfigKeyTimeframeCandles(tf) entry ("_timeframe_candles_<tf>").
func isEngineOwnedConfigKey(key string) bool {
	return strings.HasPrefix(key, "_")
}

// marshalConfig walks Context.Config (already map[string]interface{} from a
// JSON unmarshal per app/engine/engine.go's buildContext) into a Lua table,
// shallow copy - no schema needed, Lua tables are naturally dynamic.
// Engine-owned keys are Go-typed values that do not have an obvious/safe Lua
// representation and MUST be skipped here, not walked - scripts only ever
// see author-set JSON config (stop_loss_pct, position_sizing, custom keys),
// never these engine-internal entries.
func marshalConfig(L *lua.LState, config map[string]interface{}) *lua.LTable {
	t := L.NewTable()
	for k, v := range config {
		if isEngineOwnedConfigKey(k) {
			continue
		}
		if lv := toLuaValue(L, v); lv != nil {
			L.SetField(t, k, lv)
		}
	}
	return t
}

// toLuaValue converts a JSON-unmarshaled Go value (string, float64, bool,
// map[string]interface{}, []interface{}, nil) into its Lua equivalent.
// Returns nil (caller skips the field) for any type it doesn't recognize -
// fails closed rather than passing something a script can't safely use.
func toLuaValue(L *lua.LState, v interface{}) lua.LValue {
	switch val := v.(type) {
	case nil:
		return lua.LNil
	case bool:
		return lua.LBool(val)
	case string:
		return lua.LString(val)
	case float64:
		return lua.LNumber(val)
	case float32:
		return lua.LNumber(val)
	case int:
		return lua.LNumber(val)
	case int64:
		return lua.LNumber(val)
	case map[string]interface{}:
		nested := L.NewTable()
		for k, nv := range val {
			if lv := toLuaValue(L, nv); lv != nil {
				L.SetField(nested, k, lv)
			}
		}
		return nested
	case []interface{}:
		nested := L.NewTable()
		for _, item := range val {
			if lv := toLuaValue(L, item); lv != nil {
				nested.Append(lv)
			}
		}
		return nested
	default:
		return nil
	}
}

// buildStateTable marshals a loaded script_state row's state_json (already
// map[string]interface{} from a JSON unmarshal, see backend-03's
// ScriptStateStore.Load) into the Lua state global, using the same
// toLuaValue conversion as config.
func buildStateTable(L *lua.LState, state map[string]interface{}) *lua.LTable {
	t := L.NewTable()
	for k, v := range state {
		if lv := toLuaValue(L, v); lv != nil {
			L.SetField(t, k, lv)
		}
	}
	return t
}

// fromLuaValue is toLuaValue's inverse: walks a lua.LValue back into a plain
// Go value suitable for JSON marshaling (map[string]interface{},
// []interface{}, string, float64, bool, nil).
func fromLuaValue(v lua.LValue) interface{} {
	switch val := v.(type) {
	case lua.LBool:
		return bool(val)
	case lua.LString:
		return string(val)
	case lua.LNumber:
		return float64(val)
	case *lua.LTable:
		return fromLuaTable(val)
	default:
		return nil
	}
}

// fromLuaTable disambiguates a Lua table into either a Go slice (a table
// that is a pure, 1-indexed array with no non-integer keys) or a Go map -
// Lua itself doesn't distinguish the two, so this uses table.Len() plus a
// full-key walk to decide, matching the common Lua<->JSON bridging idiom.
func fromLuaTable(t *lua.LTable) interface{} {
	maxN := t.Len()
	isArray := true
	count := 0
	t.ForEach(func(k, _ lua.LValue) {
		count++
		if _, ok := k.(lua.LNumber); !ok {
			isArray = false
		}
	})
	if isArray && count == maxN && maxN > 0 {
		arr := make([]interface{}, 0, maxN)
		for i := 1; i <= maxN; i++ {
			arr = append(arr, fromLuaValue(t.RawGetInt(i)))
		}
		return arr
	}
	m := map[string]interface{}{}
	t.ForEach(func(k, v lua.LValue) {
		m[k.String()] = fromLuaValue(v)
	})
	return m
}

// readStateTable is buildStateTable's inverse: walks the (script-mutated)
// Lua state table back into map[string]interface{} for backend-03's
// ScriptStateStore.Save. Called after every hook invocation, unconditionally
// (blueprint §3: "upserted every cycle regardless of whether the script
// wrote to state" - no dirty-tracking).
func readStateTable(t *lua.LTable) map[string]interface{} {
	result := map[string]interface{}{}
	t.ForEach(func(k, v lua.LValue) {
		result[k.String()] = fromLuaValue(v)
	})
	return result
}

// HookKind distinguishes the four return shapes the eight Strategy hooks
// actually produce:
//   - HookVoid:   Before/After/Terminate - no return value expected.
//   - HookBool:   ShouldLong/ShouldShort - Lua `true`/`false`.
//   - HookSignal: GoLong/GoShort - a table {buy=..,sell=..,stop_loss=..,take_profit=..}.
//   - HookOptSignal: UpdatePosition - Lua `nil` (hold) or the same table shape as HookSignal.
type HookKind int

const (
	HookVoid HookKind = iota
	HookBool
	HookSignal
	HookOptSignal
)

// HookResult is CallHook's typed return value.
type HookResult struct {
	Bool   bool
	Signal *strategies.Signal // nil for HookVoid/HookBool, or for HookOptSignal's "hold" case
}

func defaultHookResult(kind HookKind) HookResult {
	switch kind {
	case HookBool:
		return HookResult{Bool: false}
	default:
		return HookResult{}
	}
}

// CallHook is the primary entry point every ScriptStrategy hook
// (backend-04) calls. It loads+runs source via Runner.Eval, then calls the
// named Lua function (hookName) with ctx as argument, and walks the return
// value back into a typed Result.
//
// state is passed by value (a plain map, already loaded by the caller via
// ScriptStateStore.Load - backend-03) and the RETURNED map is the post-hook
// state the caller must persist via ScriptStateStore.Save (backend-04 owns
// that load/call/save sequencing; this function is pure w.r.t. persistence -
// it never touches the DB itself).
func (r *Runner) CallHook(
	strategyName, hookName string,
	cctx strategies.Context,
	state map[string]interface{},
	source string,
	kind HookKind,
	trace *TraceRecorder, // nil in production dryrun/paper/live cycles - zero overhead
) (result HookResult, newState map[string]interface{}, err error) {
	L, cancel, err := r.Eval(strategyName, hookName, source)
	if err != nil {
		return HookResult{}, state, err
	}
	defer L.Close()
	defer cancel()

	ctxTable := buildCtxTable(L, cctx)
	stateTable := buildStateTable(L, state)
	L.SetGlobal("ctx", ctxTable)
	L.SetGlobal("state", stateTable)
	bindIndicators(L, cctx, trace)
	bindDebugLog(L, trace)

	fn := L.GetGlobal(hookName)
	if fn == lua.LNil {
		// Missing hook = safe default per hook kind, not an error - a script
		// author may legitimately omit before/after/terminate/should_short/
		// go_short if their strategy has no use for them (mirrors
		// TemplateStrategy's own "short side is a no-op" precedent).
		return defaultHookResult(kind), readStateTable(stateTable), nil
	}

	pcallErr := L.CallByParam(lua.P{Fn: fn, NRet: 1, Protect: true}, ctxTable)
	if pcallErr != nil {
		// ADR-021: a Lua PCall failure is a normal Go error, never a panic -
		// flows through ScriptStrategy (backend-04) into the engine's
		// existing "hard cycle error" path (StrategyExecution{Status: Error}).
		r.incrementRuntimeError(strategyName, "lua_error")
		return HookResult{}, readStateTable(stateTable), fmt.Errorf("script %q hook %q: %w", strategyName, hookName, pcallErr)
	}

	ret := L.Get(-1)
	L.Pop(1)
	newState = readStateTable(stateTable)

	switch kind {
	case HookVoid:
		return HookResult{}, newState, nil
	case HookBool:
		return HookResult{Bool: lua.LVAsBool(ret)}, newState, nil
	case HookSignal:
		sig, convErr := signalFromLua(ret)
		if convErr != nil {
			return HookResult{}, newState, fmt.Errorf("script %q hook %q: %w", strategyName, hookName, convErr)
		}
		trace.SetSignal(&sig)
		return HookResult{Signal: &sig}, newState, nil
	case HookOptSignal:
		if ret == lua.LNil {
			return HookResult{Signal: nil}, newState, nil
		}
		sig, convErr := signalFromLua(ret)
		if convErr != nil {
			return HookResult{}, newState, fmt.Errorf("script %q hook %q: %w", strategyName, hookName, convErr)
		}
		trace.SetSignal(&sig)
		return HookResult{Signal: &sig}, newState, nil
	}
	return HookResult{}, newState, nil
}

// signalFromLua walks a Lua table {buy={qty,price}, sell={...},
// stop_loss={...}, take_profit={...}} field-by-field into a
// strategies.Signal, per blueprint §6.2 ("no generic reflection/decoder
// needed given the shape is small and fixed"). Each of the four sub-tables
// is optional. Returns an error (not a panic/zero-value) if ret is not a
// table at all - a script returning a string/number from go_long is an
// author bug that must surface as a recorded StrategyExecution error, not
// silently produce an empty Signal.
func signalFromLua(ret lua.LValue) (strategies.Signal, error) {
	tbl, ok := ret.(*lua.LTable)
	if !ok {
		return strategies.Signal{}, fmt.Errorf("expected a table return value for a Signal, got %s", ret.Type().String())
	}

	sig := strategies.Signal{}
	sig.Buy = orderFromLuaField(tbl, "buy")
	sig.Sell = orderFromLuaField(tbl, "sell")
	sig.StopLoss = orderFromLuaField(tbl, "stop_loss")
	sig.TakeProfit = orderFromLuaField(tbl, "take_profit")
	return sig, nil
}

func orderFromLuaField(tbl *lua.LTable, field string) *strategies.Order {
	v := tbl.RawGetString(field)
	sub, ok := v.(*lua.LTable)
	if !ok {
		return nil
	}
	qty := float64(lua.LVAsNumber(sub.RawGetString("qty")))
	price := float64(lua.LVAsNumber(sub.RawGetString("price")))
	return &strategies.Order{Qty: qty, Price: price}
}
