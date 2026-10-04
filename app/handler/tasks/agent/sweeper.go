package agent

import (
	"context"
	"fmt"
	"log"
	"math"
	"sort"
	"time"

	"go-trade-bot/app/entities"
	agentusecase "go-trade-bot/app/usecase/agent"
	agentworker "go-trade-bot/app/workers/agent"

	"github.com/hibiken/asynq"
)

// TaskSweepEvents is the periodic sweeper task (C-01 §2.2), scheduled every
// SweepInterval by the CronProvider on the agents queue.
const TaskSweepEvents = "agent:sweep_events"

// SweepCronspec runs the sweeper every 5 minutes.
const SweepCronspec = "*/5 * * * *"

// sweepUniqueTTL stops sweeps piling up if one overruns.
const sweepUniqueTTL = 4 * time.Minute

// NewSweepTask builds the sweeper task and its options.
func NewSweepTask() (*asynq.Task, []asynq.Option) {
	return asynq.NewTask(TaskSweepEvents, []byte("{}")),
		[]asynq.Option{asynq.Queue(agentworker.Queue), asynq.MaxRetry(0), asynq.Timeout(2 * time.Minute), asynq.Unique(sweepUniqueTTL)}
}

// SweepSignals is the slice of app/repository/signal the sweeper reads.
type SweepSignals interface {
	ListClosedBetween(ctx context.Context, strategyID uint, from, to time.Time) ([]entities.Signal, error)
	LastOpenedAt(ctx context.Context, strategyID uint) (*time.Time, error)
}

// StrategyLister lists every strategy.
type StrategyLister interface {
	GetAll(ctx context.Context) ([]entities.Strategy, error)
}

// Sweeper detects drawdown / no_signal occurrences for every non-paused
// agent that has those triggers, over the strategies in its scope, and feeds
// them through the EventDispatcher's cooldown + enqueue path.
type Sweeper struct {
	agents     TriggerAgentSource
	strategies StrategyLister
	signals    SweepSignals
	dispatcher *EventDispatcher
	Now        func() time.Time
}

// NewSweeper builds a Sweeper.
func NewSweeper(agents TriggerAgentSource, strategies StrategyLister, signals SweepSignals, dispatcher *EventDispatcher) *Sweeper {
	return &Sweeper{agents: agents, strategies: strategies, signals: signals, dispatcher: dispatcher, Now: time.Now}
}

// ProcessTask handles agent:sweep_events.
func (s *Sweeper) ProcessTask(ctx context.Context, _ *asynq.Task) error {
	n, err := s.Sweep(ctx)
	if err != nil {
		log.Printf("agent:sweep_events: %v", err)
		return nil // the next sweep is 5 minutes away; never retried
	}
	if n > 0 {
		log.Printf("agent:sweep_events: %d run(s) triggered", n)
	}
	return nil
}

// Sweep runs one pass and returns how many runs it enqueued.
func (s *Sweeper) Sweep(ctx context.Context) (int, error) {
	if s.dispatcher.killSwitchOn(ctx) {
		return 0, nil
	}
	agents, err := s.agents.ListAgents(ctx)
	if err != nil {
		return 0, fmt.Errorf("list agents: %w", err)
	}
	var strategies []entities.Strategy
	loaded := false
	now := s.Now().UTC()
	fired := 0
	for _, a := range agents {
		if a.Paused {
			continue
		}
		var swept []entities.EventTrigger
		for _, e := range a.ParsedTriggers().Events {
			if entities.IsSweptEventType(e.Type) {
				swept = append(swept, e)
			}
		}
		if len(swept) == 0 {
			continue
		}
		if !loaded {
			if strategies, err = s.strategies.GetAll(ctx); err != nil {
				return fired, fmt.Errorf("list strategies: %w", err)
			}
			loaded = true
		}
		bindings, err := s.agents.ListBindingsByAgent(ctx, a.ID)
		if err != nil {
			log.Printf("agent:sweep_events: bindings of agent %d: %v", a.ID, err)
			continue
		}
		bound := make(map[uint]bool, len(bindings))
		for _, b := range bindings {
			bound[b.StrategyID] = true
		}
		for _, strat := range strategies {
			if !agentusecase.StrategyInScope(a.ID, bound, strat) {
				continue
			}
			for _, trig := range swept {
				occ, ok, err := s.evaluate(ctx, strat, trig, now)
				if err != nil {
					log.Printf("agent:sweep_events: agent %d strategy %d %s: %v", a.ID, strat.ID, trig.Type, err)
					continue
				}
				if !ok {
					continue
				}
				didFire, err := s.dispatcher.fire(ctx, a, trig, occ)
				if err != nil {
					return fired, err
				}
				if didFire {
					fired++
				}
			}
		}
	}
	return fired, nil
}

func firstSymbol(s entities.Strategy) string {
	if len(s.MonitoredSymbols) > 0 {
		return s.MonitoredSymbols[0]
	}
	return ""
}

func (s *Sweeper) evaluate(ctx context.Context, strat entities.Strategy, trig entities.EventTrigger, now time.Time) (Occurrence, bool, error) {
	window := time.Duration(trig.EffectiveWindowHours()) * time.Hour
	if window <= 0 {
		return Occurrence{}, false, nil
	}
	base := Occurrence{Type: trig.Type, StrategyID: strat.ID, Symbol: firstSymbol(strat), OccurredAt: now}
	switch trig.Type {
	case entities.EventTypeDrawdown:
		closed, err := s.signals.ListClosedBetween(ctx, strat.ID, now.Add(-window), now)
		if err != nil {
			return Occurrence{}, false, err
		}
		dd := RealizedDrawdown(closed)
		if dd.Trades == 0 || dd.NotionalBasis <= 0 || dd.DrawdownPct < trig.ThresholdPct {
			return Occurrence{}, false, nil
		}
		base.Data = map[string]any{
			"drawdown_pct":   round2(dd.DrawdownPct),
			"drawdown_abs":   round2(dd.DrawdownAbs),
			"notional_basis": round2(dd.NotionalBasis),
			"threshold_pct":  trig.ThresholdPct,
			"window_hours":   trig.EffectiveWindowHours(),
			"trades":         dd.Trades,
		}
		return base, true, nil
	case entities.EventTypeNoSignal:
		// A disabled strategy never trades; a backtest-mode one is refused by
		// the worker every cycle - neither can "go quiet".
		if strat.Status == entities.Disabled || strat.Mode == "backtest" {
			return Occurrence{}, false, nil
		}
		last, err := s.signals.LastOpenedAt(ctx, strat.ID)
		if err != nil {
			return Occurrence{}, false, err
		}
		ref := strat.CreatedAt
		var lastStr any
		if last != nil {
			ref = *last
			lastStr = last.UTC().Format(time.RFC3339)
		}
		if ref.IsZero() || now.Sub(ref) < window {
			return Occurrence{}, false, nil
		}
		base.Data = map[string]any{
			"last_signal_at": lastStr,
			"window_hours":   trig.EffectiveWindowHours(),
			"quiet_hours":    math.Floor(now.Sub(ref).Hours()),
		}
		return base, true, nil
	}
	return Occurrence{}, false, nil
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

// Drawdown is RealizedDrawdown's result.
type Drawdown struct {
	Trades        int
	DrawdownAbs   float64 // peak-to-trough of cumulative realized PnL (quote currency)
	NotionalBasis float64 // the strategy's starting notional: the first closed trade's invested amount
	DrawdownPct   float64 // DrawdownAbs / NotionalBasis * 100
}

// RealizedDrawdown computes the peak-to-trough of cumulative realized PnL
// over closed signals (sorted by close time), on the same basis as the
// performance snapshots: the sum of Order.Profit per closed position. The
// cumulative curve starts at 0 at the window start. The % basis is the
// strategy's starting notional in the window - the invested amount of the
// first closed position (falling back to the largest one if that is 0).
func RealizedDrawdown(closed []entities.Signal) Drawdown {
	sorted := append([]entities.Signal(nil), closed...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].UpdatedAt.Before(sorted[j].UpdatedAt) })
	var out Drawdown
	var cum, peak, maxInvested float64
	for i, s := range sorted {
		var pnl, invested float64
		for _, o := range s.Orders {
			pnl += float64(o.Profit)
			invested += float64(o.InvestedAmount)
		}
		if i == 0 {
			out.NotionalBasis = invested
		}
		if invested > maxInvested {
			maxInvested = invested
		}
		cum += pnl
		if cum > peak {
			peak = cum
		}
		if peak-cum > out.DrawdownAbs {
			out.DrawdownAbs = peak - cum
		}
		out.Trades++
	}
	if out.NotionalBasis <= 0 {
		out.NotionalBasis = maxInvested
	}
	if out.NotionalBasis > 0 {
		out.DrawdownPct = out.DrawdownAbs / out.NotionalBasis * 100
	}
	return out
}
