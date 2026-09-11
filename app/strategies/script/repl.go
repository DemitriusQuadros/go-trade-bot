package script

import (
	"context"
	"fmt"

	lua "github.com/yuin/gopher-lua"
	"go-trade-bot/app/strategies"
)

// Validate compiles source without executing it, returning a parse/syntax
// error if the snippet is not loadable. Used by FastRerun (backend-08) to
// surface a genuine parse error as a response-level error, distinct from a
// per-hook runtime error (which ScriptStrategy fails closed on).
func (r *Runner) Validate(source string) error {
	L := lua.NewState(lua.Options{SkipOpenLibs: true})
	defer L.Close()
	_, err := L.LoadString(source)
	return err
}

// EvalREPL runs an arbitrary Lua snippet (a bare expression like "return 2 + 2"
// or a small script body) against a sandboxed LState with ctx/state/ind/debug
// bound BEFORE execution - this is what distinguishes it from Runner.Eval
// (which loads hook-defining source and defers execution to a named hook call).
// The snippet's top-of-stack return value (if any) is converted back to a plain
// Go value via fromLuaValue. A parse/runtime/timeout failure is returned as a
// normal error (the REPL usecase captures it into the response's Error field,
// never as a transport-level failure).
//
// trace may be nil; when non-nil, ind.* and debug.log calls are recorded into
// it exactly as they are during a hook cycle.
func (r *Runner) EvalREPL(strategyName string, cctx strategies.Context, source string, trace *TraceRecorder) (interface{}, error) {
	L := lua.NewState(lua.Options{SkipOpenLibs: true})
	openSandboxedLibs(L)

	goCtx, cancel := context.WithTimeout(context.Background(), r.timeout)
	L.SetContext(goCtx)

	L.SetGlobal("ctx", buildCtxTable(L, cctx))
	L.SetGlobal("state", buildStateTable(L, map[string]interface{}{}))
	bindIndicators(L, cctx, trace)
	bindDebugLog(L, trace)

	type replResult struct {
		value interface{}
		err   error
	}
	done := make(chan replResult, 1)

	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				done <- replResult{err: fmt.Errorf("script runtime panic: %v", rec)}
			}
		}()
		top0 := L.GetTop()
		if err := L.DoString(source); err != nil {
			done <- replResult{err: fmt.Errorf("script %q eval error: %w", strategyName, err)}
			return
		}
		var value interface{}
		if L.GetTop() > top0 {
			value = fromLuaValue(L.Get(-1))
		}
		done <- replResult{value: value}
	}()

	select {
	case res := <-done:
		cancel()
		if res.err != nil {
			r.incrementRuntimeError(strategyName, "repl_error")
			L.Close()
			return nil, res.err
		}
		L.Close()
		return res.value, nil
	case <-goCtx.Done():
		cancel()
		r.incrementRuntimeError(strategyName, "timeout")
		// L is intentionally NOT closed on the timeout path - an in-flight
		// L.DoString on the abandoned goroutine still holds it; closing here
		// would race (same documented tradeoff as Runner.Eval).
		return nil, fmt.Errorf("script %q eval exceeded %s timeout", strategyName, r.timeout)
	}
}
