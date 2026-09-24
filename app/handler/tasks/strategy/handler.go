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
	engine     Engine
	notifier   notifier.NotificationSender
	ceiling    atomic.Int32 // strategies.ExecutionMode, process-wide MODE ceiling (Spec 10)
	testnet    atomic.Bool
	inFlight   atomic.Int64 // incremented/decremented around each engine.Run call
	draining   atomic.Bool  // true while a risk-bearing settings swap is pending
}

// NewStrategyProcessor takes the process-wide MODE ceiling (Spec 10) as an
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
	e Engine,
	n notifier.NotificationSender,
	processCeiling strategies.ExecutionMode,
	testnet bool,
) *StrategyProcessor {
	p := &StrategyProcessor{
		collector:  collector,
		worker:     w,
		repository: r,
		engine:     e,
		notifier:   n,
	}
	p.ceiling.Store(int32(processCeiling))
	p.testnet.Store(testnet)
	return p
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

	var firstErr error
	for _, symbol := range nStrategy.MonitoredSymbols {
		p.inFlight.Add(1)
		err := p.engine.Run(ctx, strategyInstance, nStrategy, symbol, mode)
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
