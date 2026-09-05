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
	"time"

	"github.com/hibiken/asynq"
)

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
}

// Engine runs exactly one strategy cycle for one symbol (app/engine.Engine
// satisfies this - Spec 05/08). This replaces the pre-Phase-1
// IStrategyProcessor{Execute() error} interface and the hardcoded switch
// that used to construct grid/bollinger/scalping processors directly
// (Spec 06).
type Engine interface {
	Run(ctx context.Context, strategy strategies.Strategy, dbStrategy entities.Strategy, symbol string, mode strategies.ExecutionMode) error
}

type StrategyProcessor struct {
	collector      *metrics.MetricsCollector
	worker         StrategyWorker
	repository     StrategyRepository
	engine         Engine
	notifier       notifier.NotificationSender
	processCeiling strategies.ExecutionMode
	testnet        bool
}

// NewStrategyProcessor takes the process-wide MODE ceiling (Spec 10) as an
// already-parsed strategies.ExecutionMode, and testnet as a plain bool,
// rather than a raw *configuration.Configuration, so this package stays free
// of any internal/configuration dependency - the caller (cmd/worker/main.go's
// RegisterHandlers, which already receives *config.Configuration) derives
// both once at wiring time.
func NewStrategyProcessor(
	collector *metrics.MetricsCollector,
	w StrategyWorker,
	r StrategyRepository,
	e Engine,
	n notifier.NotificationSender,
	processCeiling strategies.ExecutionMode,
	testnet bool,
) *StrategyProcessor {
	return &StrategyProcessor{
		collector:      collector,
		worker:         w,
		repository:     r,
		engine:         e,
		notifier:       n,
		processCeiling: processCeiling,
		testnet:        testnet,
	}
}

func (p *StrategyProcessor) HandleStrategyTask(ctx context.Context, t *asynq.Task) error {
	var strategy entities.Strategy

	if err := json.Unmarshal(t.Payload(), &strategy); err != nil {
		return err
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
	strategyInstance, ok := strategies.Get(nStrategy.StrategyName)
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
		if err := p.engine.Run(ctx, strategyInstance, nStrategy, symbol, mode); err != nil {
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

	effective := strategyMode
	if strategyMode > p.processCeiling {
		if strategyMode == strategies.ModeLive {
			return 0, false, fmt.Sprintf("configured for live mode but process MODE ceiling is %s", p.processCeiling)
		}
		effective = p.processCeiling
	}

	if effective == strategies.ModePaper && !p.testnet {
		return 0, false, "effective mode is paper but Testnet=false; Paper Trading must never resolve to the production exchange"
	}

	return effective, true, ""
}

func (p *StrategyProcessor) terminateIfRegistered(nStrategy entities.Strategy) {
	strategyInstance, ok := strategies.Get(nStrategy.StrategyName)
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
