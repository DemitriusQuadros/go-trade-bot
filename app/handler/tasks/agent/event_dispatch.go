package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"go-trade-bot/app/entities"
	agentusecase "go-trade-bot/app/usecase/agent"
	agentworker "go-trade-bot/app/workers/agent"
	"go-trade-bot/internal/cooldown"
	"go-trade-bot/internal/notifier"

	"github.com/hibiken/asynq"
)

// TaskAgentEvent is the worker -> agents bridge task (C-01 §2.1).
const TaskAgentEvent = notifier.TaskAgentEvent

// Trigger metric names (registered by cmd/agent/modules/metrics.go).
const (
	MetricTriggersFired       = "agent_triggers_fired_total"     // {kind}: event | market | chain
	MetricTriggersSuppressed  = "agent_trigger_suppressed_total" // {kind}: event | market | chain
	MetricMarketSubscriptions = "agent_market_subscriptions"     // gauge
)

// TriggerAgentSource is the narrow slice of app/repository/agentplatform
// the trigger components need.
type TriggerAgentSource interface {
	ListAgents(ctx context.Context) ([]entities.Agent, error)
	ListBindingsByAgent(ctx context.Context, agentID uint) ([]entities.AgentStrategyBinding, error)
}

// StrategyGetter loads one strategy.
type StrategyGetter interface {
	GetByID(ctx context.Context, id uint) (entities.Strategy, error)
}

// CounterMetrics is satisfied by *metrics.MetricsCollector.
type CounterMetrics interface {
	IncrementCounter(name string, labels map[string]string)
}

// Occurrence is one strategy event, from the worker bridge or the sweeper.
type Occurrence struct {
	Type       string // entities.EventType*
	StrategyID uint
	Symbol     string
	OccurredAt time.Time
	Data       map[string]any
}

// OccurrencesFromEvent maps a worker notifier.Event to the event trigger
// types it fires (C-01 §2): position.closed also yields stoploss.hit for a
// stop-loss exit_reason (incl. simulated_stop_loss); strategy.error is
// strategy.panic when Data["panic"] == true. Backtest-mode and unknown
// events yield nothing.
func OccurrencesFromEvent(e notifier.Event) []Occurrence {
	if e.Mode == "backtest" || e.StrategyID == 0 {
		return nil
	}
	base := Occurrence{StrategyID: e.StrategyID, Symbol: e.Symbol, OccurredAt: e.Timestamp, Data: e.Data}
	if base.OccurredAt.IsZero() {
		base.OccurredAt = time.Now().UTC()
	}
	with := func(t string) Occurrence { o := base; o.Type = t; return o }
	switch e.Type {
	case notifier.EventPositionOpened:
		return []Occurrence{with(entities.EventTypePositionOpened)}
	case notifier.EventPositionClosed:
		out := []Occurrence{with(entities.EventTypePositionClosed)}
		if reason, _ := e.Data["exit_reason"].(string); IsStopLossExit(reason) {
			out = append(out, with(entities.EventTypeStopLossHit))
		}
		return out
	case notifier.EventStrategyError:
		if panicked, _ := e.Data["panic"].(bool); panicked {
			return []Occurrence{with(entities.EventTypeStrategyPanic)}
		}
		return []Occurrence{with(entities.EventTypeStrategyError)}
	case notifier.EventDrawdownAlert:
		// Declared but never emitted by the worker today; the sweeper is the
		// drawdown source. Mapped for completeness.
		return []Occurrence{with(entities.EventTypeDrawdown)}
	}
	return nil
}

// IsStopLossExit reports whether a position.closed exit_reason is a
// stop-loss variant ("stop_loss", "simulated_stop_loss", ...).
func IsStopLossExit(reason string) bool {
	r := strings.ToLower(reason)
	return strings.Contains(r, "stop_loss") || strings.Contains(r, "stoploss")
}

// EventDispatcher turns occurrences into agent:run tasks (C-01 §2.3): only
// for agents whose triggers match, whose scope contains the strategy, that
// are not paused, and only while the kill switch is off; each (agent, type,
// strategy) at most once per cooldown (Redis SET NX PX, shared across
// cmd/agent replicas).
type EventDispatcher struct {
	agents     TriggerAgentSource
	strategies StrategyGetter
	settings   SettingsReader
	cooldowns  cooldown.Store
	enq        agentworker.TaskEnqueuer
	metrics    CounterMetrics // may be nil
}

// NewEventDispatcher builds an EventDispatcher.
func NewEventDispatcher(agents TriggerAgentSource, strategies StrategyGetter, settings SettingsReader, cooldowns cooldown.Store, enq agentworker.TaskEnqueuer, m CounterMetrics) *EventDispatcher {
	return &EventDispatcher{agents: agents, strategies: strategies, settings: settings, cooldowns: cooldowns, enq: enq, metrics: m}
}

// ProcessTask handles agent:event. An unreadable payload is dropped; an
// infrastructure error is returned so asynq retries (MaxRetry 3) - the
// cooldown keys make a retry idempotent for agents already fired.
func (d *EventDispatcher) ProcessTask(ctx context.Context, t *asynq.Task) error {
	var p notifier.AgentEventPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		log.Printf("agent:event: invalid payload: %v", err)
		return nil
	}
	for _, occ := range OccurrencesFromEvent(p.Event) {
		if _, err := d.Dispatch(ctx, occ); err != nil {
			return err
		}
	}
	return nil
}

// killSwitchOn reads the global kill switch; an unreadable setting counts
// as on (fail closed).
func (d *EventDispatcher) killSwitchOn(ctx context.Context) bool {
	s, err := d.settings.Get(ctx)
	if err != nil {
		log.Printf("agent triggers: cannot read settings, dispatching nothing: %v", err)
		return true
	}
	return s != nil && s.AgentsPaused
}

// Dispatch fires occ for every matching agent and returns how many runs
// were enqueued.
func (d *EventDispatcher) Dispatch(ctx context.Context, occ Occurrence) (int, error) {
	if d.killSwitchOn(ctx) {
		return 0, nil
	}
	strat, err := d.strategies.GetByID(ctx, occ.StrategyID)
	if err != nil || strat.ID == 0 {
		log.Printf("agent:event: strategy %d not found, skipping %s", occ.StrategyID, occ.Type)
		return 0, nil
	}
	agents, err := d.agents.ListAgents(ctx)
	if err != nil {
		return 0, fmt.Errorf("agent:event: list agents: %w", err)
	}
	fired := 0
	for _, a := range agents {
		if a.Paused {
			continue
		}
		trig, ok := findEventTrigger(a.ParsedTriggers().Events, occ.Type)
		if !ok {
			continue
		}
		inScope, err := d.inScope(ctx, a, strat)
		if err != nil {
			return fired, err
		}
		if !inScope {
			continue
		}
		ok, err = d.fire(ctx, a, trig, occ)
		if err != nil {
			return fired, err
		}
		if ok {
			fired++
		}
	}
	return fired, nil
}

func findEventTrigger(events []entities.EventTrigger, typ string) (entities.EventTrigger, bool) {
	for _, e := range events {
		if e.Type == typ {
			return e, true
		}
	}
	return entities.EventTrigger{}, false
}

func (d *EventDispatcher) inScope(ctx context.Context, a entities.Agent, s entities.Strategy) (bool, error) {
	bindings, err := d.agents.ListBindingsByAgent(ctx, a.ID)
	if err != nil {
		return false, fmt.Errorf("agent triggers: bindings of agent %d: %w", a.ID, err)
	}
	bound := make(map[uint]bool, len(bindings))
	for _, b := range bindings {
		bound[b.StrategyID] = true
	}
	return agentusecase.StrategyInScope(a.ID, bound, s), nil
}

// CooldownKeyEvent is the Redis cooldown key of an event trigger.
func CooldownKeyEvent(agentID uint, typ string, strategyID uint) string {
	return fmt.Sprintf("agent-trigger:%d:%s:%d", agentID, typ, strategyID)
}

// fire applies the cooldown and enqueues the run. Callers have already
// checked the kill switch, pause and scope.
func (d *EventDispatcher) fire(ctx context.Context, a entities.Agent, trig entities.EventTrigger, occ Occurrence) (bool, error) {
	claimed, err := d.cooldowns.Claim(ctx, CooldownKeyEvent(a.ID, occ.Type, occ.StrategyID), trig.EffectiveCooldown())
	if err != nil {
		return false, fmt.Errorf("agent triggers: cooldown for agent %d: %w", a.ID, err)
	}
	if !claimed {
		d.count(MetricTriggersSuppressed, entities.TriggerKindEvent)
		return false, nil
	}
	detail, _ := json.Marshal(map[string]any{
		"event":       occ.Type,
		"strategy_id": occ.StrategyID,
		"symbol":      occ.Symbol,
		"occurred_at": occ.OccurredAt.UTC().Format(time.RFC3339),
		"data":        truncateEventData(occ.Data),
	})
	sid := occ.StrategyID
	if _, err := agentworker.EnqueueRunWith(ctx, d.enq, agentworker.RunPayload{
		AgentID: a.ID, Trigger: entities.TriggerKindEvent, Detail: detail, StrategyID: &sid,
	}); err != nil {
		// The cooldown is already taken: this occurrence is lost for the
		// agent rather than retried into a possible double run.
		log.Printf("agent triggers: enqueue %s run for agent %d failed: %v", occ.Type, a.ID, err)
		return false, nil
	}
	d.count(MetricTriggersFired, entities.TriggerKindEvent)
	log.Printf("agent triggers: %s on strategy %d -> agent %d (%s)", occ.Type, occ.StrategyID, a.ID, a.Name)
	return true, nil
}

func (d *EventDispatcher) count(metric, kind string) {
	if d.metrics != nil {
		d.metrics.IncrementCounter(metric, map[string]string{"kind": kind})
	}
}

const (
	maxEventDataBytes  = 2048
	maxEventValueBytes = 256
)

// truncateEventData bounds the event data copied into TriggerDetail: small
// maps pass through; otherwise only small values are kept and "_truncated"
// is set.
func truncateEventData(data map[string]any) map[string]any {
	if len(data) == 0 {
		return map[string]any{}
	}
	if b, err := json.Marshal(data); err == nil && len(b) <= maxEventDataBytes {
		return data
	}
	out := map[string]any{"_truncated": true}
	size := 0
	for k, v := range data {
		b, err := json.Marshal(v)
		if err != nil || len(b) > maxEventValueBytes || size+len(b) > maxEventDataBytes {
			continue
		}
		size += len(b)
		out[k] = v
	}
	return out
}
