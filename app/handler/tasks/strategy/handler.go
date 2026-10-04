package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"go-trade-bot/app/entities"
	"go-trade-bot/app/strategies"
	"go-trade-bot/internal/metrics"
	"go-trade-bot/internal/notifier"
	"log"
	"sync/atomic"
	"time"

	"github.com/hibiken/asynq"
)

// drainAdmissionDelay is the short, fixed re-enqueue delay applied to a
// strategy cycle held at the admission gate while a risk-bearing settings
// swap is draining (Spec backend-05 SS3) - deliberately independent of the
// strategy's own configured Cycle so a strategy on a long cycle isn't stuck
// waiting for its normal interval to retry after a brief drain window.
const drainAdmissionDelay = 5 * time.Second

const (
	StrategyTask        = "strategy:execute:"
	total_strategy_task = "total_strategy_task"
)

type StrategyRepository interface {
	SaveExecution(ctx context.Context, execution entities.StrategyExecution) error
	GetByID(ctx context.Context, id uint) (entities.Strategy, error)
	CountOpenSignals(ctx context.Context, strategy entities.Strategy) (int64, error)
}

type StrategyWorker interface {
	EnqueueStrategyTask(strategy entities.Strategy) error
	// EnqueueStrategyTaskWithDelay is a small, additive sibling to
	// EnqueueStrategyTask (Spec backend-05 SS3), used only to re-schedule a
	// cycle held at the drain admission gate with a short fixed delay,
	// distinct from the strategy's own Cycle-based re-enqueue interval.
	EnqueueStrategyTaskWithDelay(strategy entities.Strategy, delay time.Duration) error
}

// Engine runs exactly one strategy cycle for one symbol (app/engine.Engine
// satisfies this - Spec 05/08). This replaces the pre-Phase-1
// IStrategyProcessor{Execute() error} interface and the hardcoded switch
// that used to construct grid/bollinger/scalping processors directly
// (Spec 06).
type Engine interface {
	Run(ctx context.Context, strategy strategies.Strategy, dbStrategy entities.Strategy, symbol string, mode strategies.ExecutionMode) error
}

// CycleLock is the strategy-cycle:<id> lock (agents-platform Phase B-01 §5,
// internal/lock with lock.CycleKeyPrefix). cmd/agent's agent:apply_proposal
// holds it while it swaps an approved proposal's code into a strategy; the
// worker holds it for the whole cycle, so a cycle and an apply are mutually
// exclusive. Optional: a nil lock (tests, legacy wiring) means no locking.
type CycleLock interface {
	Acquire(ctx context.Context, strategyID uint, holder string, ttl time.Duration) (bool, error)
	Release(ctx context.Context, strategyID uint, holder string) error
}

// cycleLockTTL bounds how long a crashed worker keeps a strategy's cycle
// lock (an apply waits at most this long).
const cycleLockTTL = 5 * time.Minute

// StrategyProcessor is an fx-provided singleton (Spec backend-05): its
// ceiling/testnet/inFlight/draining fields need to be readable and mutable
// from outside HandleStrategyTask (by the settings usecase's drain-then-swap
// orchestration), so they're atomics rather than plain fields, and the type
// itself is constructed once at wiring time rather than ad-hoc inside
// cmd/worker/main.go's OnStart hook the way it was before this spec.
type StrategyProcessor struct {
	collector  *metrics.MetricsCollector
	worker     StrategyWorker
	repository StrategyRepository
	// engine executes effective live/paper cycles against the process's
	// real exchange client (the testnet adapter when Testnet=true).
	engine Engine
	// dryRunEngine executes effective dryrun cycles against a simulated
	// exchange built around exchange.NewReadOnlyClient (fix-01). nil means
	// dryrun cycles are refused - never routed to engine.
	dryRunEngine Engine
	notifier     notifier.NotificationSender
	ceiling      atomic.Int32 // strategies.ExecutionMode, process-wide MODE ceiling (Spec 10)
	testnet      atomic.Bool
	inFlight     atomic.Int64 // incremented/decremented around each engine.Run call
	draining     atomic.Bool  // true while a risk-bearing settings swap is pending
	cycleLock    CycleLock    // optional; see CycleLock
}

// NewStrategyProcessor takes two engines (fix-01): realEngine for effective
// live/paper cycles and dryRunEngine for effective dryrun cycles, which must
// be built around a simulated, read-only exchange (cmd/worker/modules'
// NewDryRunEngine). A nil dryRunEngine refuses dryrun cycles. Effective
// backtest cycles are never run by the worker.
//
// It also takes the process-wide MODE ceiling (Spec 10) as an
// already-parsed strategies.ExecutionMode, and testnet as a plain bool,
// rather than a raw *configuration.Configuration, so this package stays free
// of any internal/configuration dependency - the caller (cmd/worker/main.go's
// RegisterHandlers, which already receives *config.Configuration) derives
// both once at wiring time. Both are stored as atomics so a later risk-
// bearing settings swap (Spec backend-05) can update them in place without
// requiring any consumer to hold a fresh reference.
func NewStrategyProcessor(
	collector *metrics.MetricsCollector,
	w StrategyWorker,
	r StrategyRepository,
	realEngine Engine,
	dryRunEngine Engine,
	n notifier.NotificationSender,
	processCeiling strategies.ExecutionMode,
	testnet bool,
) *StrategyProcessor {
	p := &StrategyProcessor{
		collector:    collector,
		worker:       w,
		repository:   r,
		engine:       realEngine,
		dryRunEngine: dryRunEngine,
		notifier:     n,
	}
	p.ceiling.Store(int32(processCeiling))
	p.testnet.Store(testnet)
	return p
}

// SetCycleLock installs the strategy-cycle lock (call once at wiring time,
// before the asynq server starts).
func (p *StrategyProcessor) SetCycleLock(l CycleLock) {
	p.cycleLock = l
}

// SetDraining toggles the admission gate (Spec backend-05 SS3). While true,
// HandleStrategyTask holds every newly-picked-up cycle at the door instead of
// executing it.
func (p *StrategyProcessor) SetDraining(draining bool) {
	p.draining.Store(draining)
}

// InFlightCount reports how many engine.Run calls are currently executing.
// The settings usecase polls this to zero during a risk-bearing drain.
func (p *StrategyProcessor) InFlightCount() int64 {
	return p.inFlight.Load()
}

// SetCeiling updates the process-wide MODE ceiling in place after a
// successful risk-bearing settings swap.
func (p *StrategyProcessor) SetCeiling(mode strategies.ExecutionMode) {
	p.ceiling.Store(int32(mode))
}

// SetTestnet updates the process-wide Testnet flag in place after a
// successful risk-bearing settings swap.
func (p *StrategyProcessor) SetTestnet(testnet bool) {
	p.testnet.Store(testnet)
}

func (p *StrategyProcessor) HandleStrategyTask(ctx context.Context, t *asynq.Task) error {
	var strategy entities.Strategy

	if err := json.Unmarshal(t.Payload(), &strategy); err != nil {
		return err
	}

	// Admission gate (Spec backend-05 SS3): held, not executed, while a
	// risk-bearing settings swap is draining in-flight cycles. Re-enqueued
	// with a short fixed delay distinct from the strategy's own Cycle so a
	// strategy on a long cycle isn't stuck waiting for its normal interval.
	// This cycle never actually ran, so no StrategyExecution row is created.
	if p.draining.Load() {
		if err := p.worker.EnqueueStrategyTaskWithDelay(strategy, drainAdmissionDelay); err != nil {
			log.Printf("Error re-enqueuing held strategy task during drain: %v", err)
		}
		return nil
	}

	// Strategy-cycle lock (agents-platform Phase B-01 §5): taken before the
	// strategy row is read and held until the cycle is recorded, so an
	// approved proposal's code swap (cmd/agent) never lands mid-cycle.
	if p.cycleLock != nil {
		holder := fmt.Sprintf("worker-cycle:%d:%d", strategy.ID, time.Now().UnixNano())
		acquired, lockErr := p.cycleLock.Acquire(ctx, strategy.ID, holder, cycleLockTTL)
		switch {
		case lockErr != nil:
			// Fail open: keep trading exactly as before Phase B. The apply
			// side fails closed (it never writes without the lock), so a
			// Redis error cannot let an apply overlap this cycle.
			log.Printf("strategy %d: cycle lock unavailable, running without it: %v", strategy.ID, lockErr)
		case !acquired:
			// An apply is swapping this strategy's code right now (a few
			// milliseconds): hold the cycle like the drain gate does.
			if err := p.worker.EnqueueStrategyTaskWithDelay(strategy, drainAdmissionDelay); err != nil {
				return fmt.Errorf("strategy %d: cycle lock busy and re-enqueue failed: %w", strategy.ID, err)
			}
			return nil
		default:
			defer func() {
				rctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := p.cycleLock.Release(rctx, strategy.ID, holder); err != nil {
					log.Printf("strategy %d: could not release the cycle lock (expires in %s): %v", strategy.ID, cycleLockTTL, err)
				}
			}()
		}
	}

	nStrategy, err := p.repository.GetByID(ctx, strategy.ID)
	if err != nil {
		log.Printf("Error getting strategy by ID: %v", err)
		return nil
	}

	if nStrategy.Status == entities.Disabled {
		// Spec 05 AC#8: Terminate is called once on the Disabled transition,
		// not on every subsequent skipped cycle - since a Disabled strategy
		// stops re-enqueuing itself below, this naturally fires at most once
		// per disable event (the only way this handler runs again for an
		// already-disabled strategy is a fresh manual enqueue).
		p.terminateIfRegistered(nStrategy)
		return nil
	}

	// fix-01: backtest mode is only valid through the backtest API. A cycle
	// whose effective mode is backtest is refused without running any hook
	// and is NOT re-enqueued, so it is logged and notified exactly once.
	if p.isBacktestCycle(nStrategy) {
		msg := fmt.Sprintf("strategy %q: effective mode is backtest; the worker never runs backtest-mode strategies (use the backtest API) - not re-enqueued", nStrategy.Name)
		log.Print(msg)
		p.notifyError(nStrategy, msg, "backtest mode refusal")
		p.repository.SaveExecution(ctx, entities.StrategyExecution{
			StrategyID: strategy.ID,
			Status:     entities.ExecutionStatus(entities.Error),
			Message:    msg,
			ExecutedAt: time.Now(),
			Strategy:   strategy,
		})
		return nil
	}

	err = p.processStrategy(ctx, nStrategy)

	if p.collector != nil {
		p.collector.IncrementCounter(total_strategy_task, map[string]string{
			"strategy": strategy.Name,
		})
	}

	log.Printf("Strategy: %s Executed", strategy.Name)

	p.worker.EnqueueStrategyTask(nStrategy)

	message := "Strategy executed successfully"
	status := entities.ExecutionStatus(entities.OK)
	if err != nil {
		message = "Error executing strategy: " + err.Error()
		status = entities.ExecutionStatus(entities.Error)
	}
	p.repository.SaveExecution(ctx, entities.StrategyExecution{
		StrategyID: strategy.ID,
		Status:     status,
		Message:    message,
		ExecutedAt: time.Now(),
		Strategy:   strategy,
	})

	return nil
}

// processStrategy resolves the strategy by StrategyName via the registry
// (Spec 06) and runs one engine cycle per monitored symbol. This is what
// replaces the pre-Phase-1 hardcoded switch on entities.Algorithm.
func (p *StrategyProcessor) processStrategy(ctx context.Context, nStrategy entities.Strategy) error {
	strategyInstance, ok := strategies.Get(nStrategy.StrategyName, nStrategy)
	if !ok {
		msg := fmt.Sprintf("strategy %q not found in registry", nStrategy.StrategyName)
		log.Print(msg)
		p.notifyError(nStrategy, msg, "strategy not found in registry")
		return fmt.Errorf("%s", msg)
	}

	mode, ok, refusalReason := p.gateMode(nStrategy)
	if !ok {
		msg := fmt.Sprintf("strategy %q: %s", nStrategy.Name, refusalReason)
		log.Print(msg)
		p.notifyError(nStrategy, msg, "mode guard refusal")
		return fmt.Errorf("%s", msg)
	}

	eng, ok, refusalReason := p.engineFor(mode)
	if !ok {
		msg := fmt.Sprintf("strategy %q: %s", nStrategy.Name, refusalReason)
		log.Print(msg)
		p.notifyError(nStrategy, msg, "order routing refusal")
		return fmt.Errorf("%s", msg)
	}

	var firstErr error
	for _, symbol := range nStrategy.MonitoredSymbols {
		p.inFlight.Add(1)
		err := eng.Run(ctx, strategyInstance, nStrategy, symbol, mode)
		p.inFlight.Add(-1)
		if err != nil {
			log.Printf("Error executing %s for symbol %s: %v", nStrategy.Name, symbol, err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// gateMode implements Spec 10's dual-layer guard: the effective mode for a
// cycle is min(strategy.Mode, process ceiling) by ordinal (AC#3, cap down
// silently) - except a strategy explicitly configured for ModeLive that
// exceeds a lower process ceiling is refused outright, never downgraded
// (AC#4), since silently running a live-flagged strategy at a lower risk
// tier without telling the operator is its own kind of dangerous surprise.
// An unparseable Mode value fails closed - refused, not defaulted (AC#7).
//
// It also enforces the Testnet/Mode consistency rule from Spec 01 AC#9 /
// Spec 10 AC#6: an effective mode of ModePaper requires testnet==true, since
// Paper Trading must never resolve to the production exchange adapter. The
// mirror case (ModeLive requires testnet==false) is not re-checked here -
// effective mode can only be ModeLive if the process ceiling itself is
// ModeLive, and cmd/worker/main.go's assertModeGuard already refuses to
// start with Testnet=true and MODE=live, so by the time any cycle with an
// effective ModeLive runs, testnet==false is already guaranteed process-wide.
func (p *StrategyProcessor) gateMode(nStrategy entities.Strategy) (mode strategies.ExecutionMode, ok bool, refusalReason string) {
	strategyMode, err := strategies.ParseExecutionMode(nStrategy.Mode)
	if err != nil {
		return 0, false, fmt.Sprintf("unparseable Mode %q: %v", nStrategy.Mode, err)
	}

	ceiling := strategies.ExecutionMode(p.ceiling.Load())
	testnet := p.testnet.Load()

	effective := strategyMode
	if strategyMode > ceiling {
		if strategyMode == strategies.ModeLive {
			return 0, false, fmt.Sprintf("configured for live mode but process MODE ceiling is %s", ceiling)
		}
		effective = ceiling
	}

	if effective == strategies.ModePaper && !testnet {
		return 0, false, "effective mode is paper but Testnet=false; Paper Trading must never resolve to the production exchange"
	}

	return effective, true, ""
}

// engineFor routes a cycle by its effective mode (fix-01): live and paper run
// on the real engine (paper's real client is the testnet adapter - gateMode
// already refuses paper without Testnet), dryrun runs on the simulated
// engine, and anything else is refused. There is deliberately no fallback
// from dryrun to the real engine.
func (p *StrategyProcessor) engineFor(mode strategies.ExecutionMode) (Engine, bool, string) {
	switch mode {
	case strategies.ModeLive, strategies.ModePaper:
		if p.engine == nil {
			return nil, false, fmt.Sprintf("no real-exchange engine configured for %s mode", mode)
		}
		return p.engine, true, ""
	case strategies.ModeDryRun:
		if p.dryRunEngine == nil {
			return nil, false, "no simulated dryrun engine configured; refusing to run a dryrun cycle"
		}
		return p.dryRunEngine, true, ""
	default:
		return nil, false, fmt.Sprintf("the worker does not execute %s mode", mode)
	}
}

// isBacktestCycle reports whether nStrategy's effective mode (strategy Mode
// capped by the process ceiling, as in gateMode) is backtest. Unparseable
// modes and live-over-ceiling strategies return false and keep gateMode's
// existing refusal path.
func (p *StrategyProcessor) isBacktestCycle(nStrategy entities.Strategy) bool {
	strategyMode, err := strategies.ParseExecutionMode(nStrategy.Mode)
	if err != nil || strategyMode == strategies.ModeLive {
		return false
	}
	ceiling := strategies.ExecutionMode(p.ceiling.Load())
	effective := strategyMode
	if strategyMode > ceiling {
		effective = ceiling
	}
	return effective == strategies.ModeBacktest
}

// OrderRoutingSummary is the worker's startup log line (fix-01) describing
// where each effective mode's orders go under the current ceiling/testnet.
func (p *StrategyProcessor) OrderRoutingSummary() string {
	ceiling := strategies.ExecutionMode(p.ceiling.Load())
	testnet := p.testnet.Load()

	live := "disabled"
	if ceiling >= strategies.ModeLive && !testnet && p.engine != nil {
		live = "real"
	}
	paper := "disabled"
	if ceiling >= strategies.ModePaper && testnet && p.engine != nil {
		paper = "testnet"
	}
	dryrun := "disabled"
	if ceiling >= strategies.ModeDryRun && p.dryRunEngine != nil {
		dryrun = "simulated"
	}
	return fmt.Sprintf("worker: order routing live=%s paper=%s dryrun=%s", live, paper, dryrun)
}

func (p *StrategyProcessor) terminateIfRegistered(nStrategy entities.Strategy) {
	strategyInstance, ok := strategies.Get(nStrategy.StrategyName, nStrategy)
	if !ok {
		return
	}
	strategyInstance.Terminate(strategies.Context{
		Symbol: "",
		Mode:   resolveMode(nStrategy),
	})
}

func (p *StrategyProcessor) notifyError(nStrategy entities.Strategy, message, errContext string) {
	if p.notifier == nil {
		return
	}
	_ = p.notifier.Send(context.Background(), notifier.Event{
		Type:       notifier.EventStrategyError,
		Timestamp:  time.Now(),
		StrategyID: nStrategy.ID,
		Strategy:   nStrategy.Name,
		Mode:       resolveMode(nStrategy).String(),
		Message:    message,
		Data:       map[string]any{"error": message, "context": errContext},
	})
}

// resolveMode parses the strategy's persisted Mode, defaulting to the
// safest tier (ModeDryRun) on any unparseable/empty value. It is used only
// for informational/display purposes (webhook payloads, Terminate's Context)
// where a best-effort value is acceptable - the actual per-cycle execution
// gate is gateMode, which fails closed instead of defaulting.
func resolveMode(nStrategy entities.Strategy) strategies.ExecutionMode {
	mode, err := strategies.ParseExecutionMode(nStrategy.Mode)
	if err != nil {
		return strategies.ModeDryRun
	}
	return mode
}
