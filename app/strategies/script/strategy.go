package script

import (
	"context"
	"log"

	"go-trade-bot/app/entities"
	"go-trade-bot/app/strategies"
	"go-trade-bot/internal/exchange"
)

// Lua hook function names a script author defines. These are the global
// function names CallHook resolves in the loaded source - a script may omit
// any of them (CallHook returns the kind's safe default for a missing hook).
const (
	hookBefore         = "before"
	hookShouldLong     = "should_long"
	hookGoLong         = "go_long"
	hookShouldShort    = "should_short"
	hookGoShort        = "go_short"
	hookUpdatePosition = "update_position"
	hookAfter          = "after"
	hookTerminate      = "terminate"
)

// ScriptStrategy adapts a Lua script (persisted as entities.Strategy.
// ScriptSource) to the frozen strategies.Strategy interface (backend-04). One
// instance is constructed per persisted strategy row via the registry factory
// - the source/id/name are captured at construction, the shared *Runner and
// ScriptStateStore are injected. Each hook loads persistent state, calls the
// named Lua function via Runner.CallHook, then persists the returned state.
//
// A hook error (parse/runtime/timeout, or a state-load failure) is logged and
// the hook returns its safe default (false / empty Signal / nil / no-op) -
// the engine's existing per-cycle error handling records the failed cycle,
// and (per ADR-021) a Lua PCall failure never panics. State is NOT saved on a
// hook error, so a failed cycle N never clobbers cycle N-1's good state.
type ScriptStrategy struct {
	strategyID uint
	name       string
	source     string
	state      ScriptStateStore
	runner     *Runner

	// traceSink/currentTrace back backend-07's TraceableStrategy: when a sink
	// is registered (backtest/REPL/fast-rerun), a fresh per-cycle
	// TraceRecorder is built in the "before" hook, threaded through every hook
	// call in the cycle, and emitted to the sink in the "after" hook. Both are
	// nil in production (dryrun/paper/live) - CallHook then receives a nil
	// *TraceRecorder and does zero trace work.
	traceSink    func(TraceRecord)
	currentTrace *TraceRecorder
}

// NewScriptStrategy constructs a ScriptStrategy from a persisted strategy row.
func NewScriptStrategy(dbStrategy entities.Strategy, state ScriptStateStore, runner *Runner) *ScriptStrategy {
	return &ScriptStrategy{
		strategyID: dbStrategy.ID,
		name:       dbStrategy.Name,
		source:     dbStrategy.ScriptSource,
		state:      state,
		runner:     runner,
	}
}

// SetTraceSink implements TraceableStrategy (backend-07). Passing a non-nil
// sink switches the strategy into traced mode; passing nil disables tracing.
func (s *ScriptStrategy) SetTraceSink(sink func(TraceRecord)) {
	s.traceSink = sink
}

// TraceableStrategy is implemented by strategies that can emit a per-cycle
// execution trace to a caller-supplied sink (backend-07). The backtest
// usecase type-asserts against this to persist ExecutionTraceJSON, and the
// fast-rerun usecase (backend-08) uses it to accumulate the response trace.
type TraceableStrategy interface {
	SetTraceSink(sink func(TraceRecord))
}

func (s *ScriptStrategy) Name() string { return s.name }

func (s *ScriptStrategy) Before(ctx strategies.Context) {
	if s.traceSink != nil {
		var candle exchange.Candle
		if len(ctx.Candles) > 0 {
			candle = ctx.Candles[len(ctx.Candles)-1]
		}
		s.currentTrace = NewTraceRecorder(candle.OpenTime, candle)
	}
	s.callVoid(hookBefore, ctx)
}

func (s *ScriptStrategy) After(ctx strategies.Context) {
	s.callVoid(hookAfter, ctx)
	if s.traceSink != nil && s.currentTrace != nil {
		s.traceSink(s.currentTrace.Record())
		s.currentTrace = nil
	}
}

func (s *ScriptStrategy) Terminate(ctx strategies.Context) {
	s.callVoid(hookTerminate, ctx)
}

func (s *ScriptStrategy) ShouldLong(ctx strategies.Context) bool {
	return s.callBool(hookShouldLong, ctx)
}

func (s *ScriptStrategy) ShouldShort(ctx strategies.Context) bool {
	return s.callBool(hookShouldShort, ctx)
}

func (s *ScriptStrategy) GoLong(ctx strategies.Context) strategies.Signal {
	return s.callSignal(hookGoLong, ctx)
}

func (s *ScriptStrategy) GoShort(ctx strategies.Context) strategies.Signal {
	return s.callSignal(hookGoShort, ctx)
}

func (s *ScriptStrategy) UpdatePosition(ctx strategies.Context) *strategies.Signal {
	return s.callOptSignal(hookUpdatePosition, ctx)
}

// callVoid runs a no-return hook (before/after/terminate). On any error it
// logs and returns without saving state.
func (s *ScriptStrategy) callVoid(hookName string, cctx strategies.Context) {
	state, err := s.state.Load(context.Background(), s.strategyID, cctx.Symbol)
	if err != nil {
		log.Printf("script %q hook %q: state load failed: %v", s.name, hookName, err)
		return
	}
	_, newState, err := s.runner.CallHook(s.name, hookName, cctx, state, s.source, HookVoid, s.currentTrace)
	if err != nil {
		log.Printf("script %q hook %q: %v", s.name, hookName, err)
		return
	}
	s.saveState(hookName, cctx.Symbol, newState)
}

// callBool runs a bool hook (should_long/should_short). Fails closed (false)
// on any error.
func (s *ScriptStrategy) callBool(hookName string, cctx strategies.Context) bool {
	state, err := s.state.Load(context.Background(), s.strategyID, cctx.Symbol)
	if err != nil {
		log.Printf("script %q hook %q: state load failed: %v", s.name, hookName, err)
		return false
	}
	res, newState, err := s.runner.CallHook(s.name, hookName, cctx, state, s.source, HookBool, s.currentTrace)
	if err != nil {
		log.Printf("script %q hook %q: %v", s.name, hookName, err)
		return false
	}
	s.saveState(hookName, cctx.Symbol, newState)
	return res.Bool
}

// callSignal runs a Signal hook (go_long/go_short). Returns an empty Signal on
// any error (the engine's "no Buy order" path treats an empty GoLong Signal as
// a hard cycle error, which is the intended surfacing).
func (s *ScriptStrategy) callSignal(hookName string, cctx strategies.Context) strategies.Signal {
	state, err := s.state.Load(context.Background(), s.strategyID, cctx.Symbol)
	if err != nil {
		log.Printf("script %q hook %q: state load failed: %v", s.name, hookName, err)
		return strategies.Signal{}
	}
	res, newState, err := s.runner.CallHook(s.name, hookName, cctx, state, s.source, HookSignal, s.currentTrace)
	if err != nil {
		log.Printf("script %q hook %q: %v", s.name, hookName, err)
		return strategies.Signal{}
	}
	// AC#10: a state-save failure must not discard the already-computed Signal.
	s.saveState(hookName, cctx.Symbol, newState)
	if res.Signal != nil {
		return *res.Signal
	}
	return strategies.Signal{}
}

// callOptSignal runs the optional-Signal hook (update_position). Returns nil
// (hold) on any error.
func (s *ScriptStrategy) callOptSignal(hookName string, cctx strategies.Context) *strategies.Signal {
	state, err := s.state.Load(context.Background(), s.strategyID, cctx.Symbol)
	if err != nil {
		log.Printf("script %q hook %q: state load failed: %v", s.name, hookName, err)
		return nil
	}
	res, newState, err := s.runner.CallHook(s.name, hookName, cctx, state, s.source, HookOptSignal, s.currentTrace)
	if err != nil {
		log.Printf("script %q hook %q: %v", s.name, hookName, err)
		return nil
	}
	s.saveState(hookName, cctx.Symbol, newState)
	return res.Signal
}

// saveState persists post-hook state, logging (but not otherwise failing) on a
// save error - the already-computed hook result must not be lost.
func (s *ScriptStrategy) saveState(hookName, symbol string, newState map[string]interface{}) {
	if err := s.state.Save(context.Background(), s.strategyID, symbol, newState); err != nil {
		log.Printf("script %q hook %q: state save failed: %v", s.name, hookName, err)
	}
}
