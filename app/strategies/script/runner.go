// Package script implements the Lua-based strategy scripting engine
// (docs/specs/strategy-scripting). It embeds gopher-lua to let an operator
// author a trading strategy as a Lua script instead of a compiled Go
// package, while running each hook call under a sandboxed, timeout-bounded
// VM (ADR-020/ADR-021).
package script

import (
	"context"
	"fmt"
	"time"

	lua "github.com/yuin/gopher-lua"
	"go-trade-bot/internal/metrics"
)

// DefaultHookTimeout is the per-hook wall-clock deadline (ADR-020,
// operator-confirmed 2026-09-09: 500ms). Chosen as the balance point between
// false-positive-killing an indicator-heavy script and letting a runaway
// script burn CPU before being caught - production cycles run minutes apart,
// so the absolute cost of 500ms either way is negligible; the value is about
// detection speed, not steady-state overhead.
const DefaultHookTimeout = 500 * time.Millisecond

const metricScriptRuntimeErrors = "script_runtime_errors_total"

// Runner owns the sandboxed Lua execution primitive: one fresh *lua.LState
// per call (not a long-lived per-strategy VM - see the blueprint §0.1 for
// why a persistent VM is unsafe given asynq's multi-replica redelivery),
// restricted stdlib, and a goroutine+context.WithTimeout wall-clock bound
// (ADR-020 - gopher-lua's own L.SetContext is cooperative only and does not
// preempt a tight, call-free Lua loop).
type Runner struct {
	timeout time.Duration
	metrics *metrics.MetricsCollector // nil-safe; nil in tests that don't assert on metrics
}

// NewRunner constructs a Runner. timeout<=0 falls back to DefaultHookTimeout.
// collector may be nil (tests, or callers that don't care about metrics).
func NewRunner(timeout time.Duration, collector *metrics.MetricsCollector) *Runner {
	if timeout <= 0 {
		timeout = DefaultHookTimeout
	}
	return &Runner{timeout: timeout, metrics: collector}
}

// hookResult is the internal channel payload the timeout-racing goroutine
// sends back on completion.
type hookResult struct {
	L   *lua.LState
	err error
}

// Eval loads and runs source (defining every top-level function/global it
// contains) inside a fresh sandboxed LState, under the Runner's configured
// timeout. It does not call any entry point - that is CallHook's job
// (backend-02, once the data bridge exists). Eval exists as of this spec so
// the sandbox primitives (timeout, restricted stdlib, panic safety) can be
// tested standalone, without a Context to marshal.
//
// On success, the returned *lua.LState is left OPEN (the caller is
// responsible for L.Close() after inspecting globals/calling further
// functions) - this lets backend-02's CallHook reuse Eval's sandboxed-load
// step without re-implementing it, then call the resolved entry point
// against the same LState before closing it. On any error return, the
// LState is already closed by Eval itself - callers must not call L.Close()
// on the error path.
//
// The second return value is the timeout context's cancel func. On success,
// the caller MUST defer it alongside L.Close() (same timing) once it's done
// calling further functions against L - this releases the context's internal
// timer promptly instead of waiting for it to self-expire after r.timeout,
// while preserving the documented "one budget spans Eval-then-call" behavior
// (the context is NOT canceled here in Eval itself, only handed back for the
// caller to release once it has finished using L). On any error return, Eval
// has already canceled it - callers must not call it again in that case (a
// context.CancelFunc is idempotent, so a redundant call is harmless, but
// unnecessary).
func (r *Runner) Eval(strategyName, hookName, source string) (*lua.LState, context.CancelFunc, error) {
	L := lua.NewState(lua.Options{SkipOpenLibs: true})
	openSandboxedLibs(L)

	// This is a per-hook budget spanning the ENTIRE Eval-then-call sequence,
	// not just the load step, matching the spec's "500ms per-hook" framing -
	// see the returned cancel func's doc comment above for why it isn't
	// invoked here on the success path.
	goCtx, cancel := context.WithTimeout(context.Background(), r.timeout)
	L.SetContext(goCtx)

	done := make(chan hookResult, 1)

	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				done <- hookResult{err: fmt.Errorf("script runtime panic: %v", rec)}
			}
		}()
		if err := L.DoString(source); err != nil {
			done <- hookResult{err: fmt.Errorf("script %q parse/load error: %w", strategyName, err)}
			return
		}
		done <- hookResult{L: L}
	}()

	select {
	case res := <-done:
		if res.err != nil {
			cancel()
			L.Close()
			return nil, nil, res.err
		}
		return res.L, cancel, nil
	case <-goCtx.Done():
		cancel()
		r.incrementRuntimeError(strategyName, "timeout")
		// L is NOT closed here - this goroutine is not the one executing
		// L.DoString, and calling L.Close() concurrently with an in-flight
		// L.DoString on another goroutine is a data race. The abandoned
		// LState/goroutine is left to be reclaimed when (if) the leaked
		// goroutine eventually returns - this is ADR-020's stated,
		// documented tradeoff, not an oversight. See the blueprint's
		// "Abandoned goroutines" risk entry.
		return nil, nil, fmt.Errorf("script %q hook %q exceeded %s timeout", strategyName, hookName, r.timeout)
	}
}

// openSandboxedLibs re-opens only base/table/string/math from gopher-lua's
// stdlib (PRD §6, blueprint §5). Explicitly NEVER opens: os (os.execute,
// os.exit - process-level compromise), io (filesystem access), package
// (require - module resolution is explicitly Won't-have, PRD §4), debug
// (gopher-lua's real debug library is a sandbox-defeating surface - a
// project-authored debug.log() closure is added separately in backend-02,
// not this stdlib table).
func openSandboxedLibs(L *lua.LState) {
	for _, pair := range []struct {
		n string
		f lua.LGFunction
	}{
		{lua.BaseLibName, lua.OpenBase},
		{lua.TabLibName, lua.OpenTable},
		{lua.StringLibName, lua.OpenString},
		{lua.MathLibName, lua.OpenMath},
	} {
		L.Push(L.NewFunction(pair.f))
		L.Push(lua.LString(pair.n))
		L.Call(1, 0)
	}
}

func (r *Runner) incrementRuntimeError(strategyName, reason string) {
	if r.metrics == nil {
		return
	}
	r.metrics.IncrementCounter(metricScriptRuntimeErrors, map[string]string{
		"strategy": strategyName,
		"reason":   reason,
	})
}
