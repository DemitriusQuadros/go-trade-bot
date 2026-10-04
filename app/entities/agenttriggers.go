package entities

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Agent trigger kinds (agents-platform C-01). They are the values of
// AgentRun.Trigger for runs started by each mechanism, and the `kind` label
// of the agent_triggers_fired_total / agent_trigger_suppressed_total
// metrics.
const (
	TriggerKindCron   = "cron"
	TriggerKindManual = "manual"
	TriggerKindEvent  = "event"
	TriggerKindMarket = "market"
	TriggerKindChain  = "chain"
)

// Strategy event trigger types (C-01 §2).
const (
	EventTypePositionOpened = "position.opened"
	EventTypePositionClosed = "position.closed"
	EventTypeStopLossHit    = "stoploss.hit"   // derived: position.closed with a stop-loss exit_reason
	EventTypeStrategyError  = "strategy.error" // strategy.error without Data["panic"]
	EventTypeStrategyPanic  = "strategy.panic" // derived: strategy.error with Data["panic"] == true
	EventTypeDrawdown       = "drawdown"       // sweeper
	EventTypeNoSignal       = "no_signal"      // sweeper
)

// AllEventTypes is every event trigger type the API accepts, in display order.
var AllEventTypes = []string{
	EventTypePositionOpened, EventTypePositionClosed, EventTypeStopLossHit,
	EventTypeStrategyError, EventTypeStrategyPanic, EventTypeDrawdown, EventTypeNoSignal,
}

// IsValidEventType reports whether t is a known event trigger type.
func IsValidEventType(t string) bool {
	for _, known := range AllEventTypes {
		if known == t {
			return true
		}
	}
	return false
}

// IsSweptEventType reports whether t is detected by cmd/agent's periodic
// sweeper (rather than emitted by the trading worker).
func IsSweptEventType(t string) bool {
	return t == EventTypeDrawdown || t == EventTypeNoSignal
}

// Event trigger defaults (C-01 §1).
const (
	DefaultEventCooldownMinutes      = 15
	DefaultSweptEventCooldownMinutes = 240 // drawdown / no_signal
	DefaultDrawdownWindowHours       = 24
)

// EventTrigger fires an agent run when a bound strategy emits Type.
type EventTrigger struct {
	Type            string  `json:"type"`
	ThresholdPct    float64 `json:"threshold_pct,omitempty"`    // drawdown only
	WindowHours     int     `json:"window_hours,omitempty"`     // drawdown (default 24), no_signal (required)
	CooldownMinutes int     `json:"cooldown_minutes,omitempty"` // default 15 (drawdown/no_signal: 240)
}

// EffectiveCooldown is the trigger's cooldown with defaults applied.
func (e EventTrigger) EffectiveCooldown() time.Duration {
	m := e.CooldownMinutes
	if m <= 0 {
		m = DefaultEventCooldownMinutes
		if IsSweptEventType(e.Type) {
			m = DefaultSweptEventCooldownMinutes
		}
	}
	return time.Duration(m) * time.Minute
}

// EffectiveWindowHours is the trigger's window with defaults applied (0 for
// types without a window, and for a no_signal trigger missing its required
// window - the sweeper skips those).
func (e EventTrigger) EffectiveWindowHours() int {
	if e.WindowHours > 0 {
		return e.WindowHours
	}
	if e.Type == EventTypeDrawdown {
		return DefaultDrawdownWindowHours
	}
	return 0
}

// Market rule kinds (C-01 §3).
const (
	MarketKindPctMove         = "pct_move"
	MarketKindVolatilitySpike = "volatility_spike"
)

// Market rule defaults and bounds (C-01 §1).
const (
	DefaultMarketCooldownMinutes = 60
	MinMarketCooldownMinutes     = 5
	MinMarketWindowMinutes       = 1
	MaxMarketWindowMinutes       = 1440
)

// MarketRule watches one symbol's 1m candles in cmd/agent's market watcher.
type MarketRule struct {
	ID              string  `json:"id"` // stable client-generated id (uuid), used for dedupe
	Symbol          string  `json:"symbol"`
	Kind            string  `json:"kind"`                    // "pct_move" | "volatility_spike"
	WindowMinutes   int     `json:"window_minutes"`          // 1..1440
	ThresholdPct    float64 `json:"threshold_pct,omitempty"` // pct_move: abs % change over the window
	Multiplier      float64 `json:"multiplier,omitempty"`    // volatility_spike: window ATR / prior-24h ATR
	CooldownMinutes int     `json:"cooldown_minutes"`        // default 60, min 5
}

// EffectiveCooldown is the rule's cooldown with the default applied.
func (m MarketRule) EffectiveCooldown() time.Duration {
	c := m.CooldownMinutes
	if c <= 0 {
		c = DefaultMarketCooldownMinutes
	}
	if c < MinMarketCooldownMinutes {
		c = MinMarketCooldownMinutes
	}
	return time.Duration(c) * time.Minute
}

// Chain trigger "on" conditions (C-01 §4).
const (
	ChainOnReport  = "report"  // the source run wrote >= 1 report
	ChainOnNotify  = "notify"  // the source run sent >= 1 notification
	ChainOnSuccess = "success" // any ok run
)

// IsValidChainOn reports whether on is a known chain condition.
func IsValidChainOn(on string) bool {
	return on == ChainOnReport || on == ChainOnNotify || on == ChainOnSuccess
}

// ChainTrigger starts this agent after a run of AgentID finishes ok and On
// matches.
type ChainTrigger struct {
	AgentID uint   `json:"agent_id"`
	On      string `json:"on"`
}

// UnmarshalJSON decodes the triggers strictly (any undecodable element is
// an error - the REST path turns it into a 400), accepting both the C-01
// shape and the pre-C-01 forms: `events: ["position.closed"]` (each string
// becomes {type: s}) and `chain_from: [3]` (each id becomes {agent_id: 3,
// on: "report"}). Elements of both forms may be mixed.
func (t *AgentTriggers) UnmarshalJSON(b []byte) error {
	parsed, err := parseTriggers(b, true)
	if err != nil {
		return err
	}
	*t = parsed
	return nil
}

// ParseTriggersLenient decodes a stored triggers column, skipping elements
// that cannot be decoded instead of failing the whole object.
func ParseTriggersLenient(b []byte) AgentTriggers {
	t, _ := parseTriggers(b, false)
	return t
}

type rawTriggers struct {
	Cron      []json.RawMessage `json:"cron"`
	Events    []json.RawMessage `json:"events"`
	Market    []json.RawMessage `json:"market"`
	ChainFrom []json.RawMessage `json:"chain_from"`
}

func parseTriggers(b []byte, strict bool) (AgentTriggers, error) {
	var out AgentTriggers
	trimmed := bytes.TrimSpace(b)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return out, nil
	}
	var raw rawTriggers
	if err := json.Unmarshal(trimmed, &raw); err != nil {
		return out, fmt.Errorf("triggers: %w", err)
	}
	fail := func(field string, i int, err error) error {
		return fmt.Errorf("triggers.%s[%d]: %w", field, i, err)
	}
	for i, r := range raw.Cron {
		var s string
		if err := json.Unmarshal(r, &s); err != nil {
			if strict {
				return AgentTriggers{}, fail("cron", i, err)
			}
			continue
		}
		out.Cron = append(out.Cron, s)
	}
	for i, r := range raw.Events {
		e, err := decodeEventTrigger(r)
		if err != nil {
			if strict {
				return AgentTriggers{}, fail("events", i, err)
			}
			continue
		}
		out.Events = append(out.Events, e)
	}
	for i, r := range raw.Market {
		var m MarketRule
		if err := json.Unmarshal(r, &m); err != nil {
			if strict {
				return AgentTriggers{}, fail("market", i, err)
			}
			continue
		}
		out.Market = append(out.Market, m)
	}
	for i, r := range raw.ChainFrom {
		c, err := decodeChainTrigger(r)
		if err != nil {
			if strict {
				return AgentTriggers{}, fail("chain_from", i, err)
			}
			continue
		}
		out.ChainFrom = append(out.ChainFrom, c)
	}
	return out, nil
}

func decodeEventTrigger(r json.RawMessage) (EventTrigger, error) {
	var s string
	if err := json.Unmarshal(r, &s); err == nil {
		return EventTrigger{Type: strings.TrimSpace(s)}, nil
	}
	var e EventTrigger
	if err := json.Unmarshal(r, &e); err != nil {
		return EventTrigger{}, err
	}
	return e, nil
}

func decodeChainTrigger(r json.RawMessage) (ChainTrigger, error) {
	var id uint
	if err := json.Unmarshal(r, &id); err == nil {
		return ChainTrigger{AgentID: id, On: ChainOnReport}, nil
	}
	// chainTriggerAlias drops ChainTrigger's methods (none today) so a
	// future UnmarshalJSON on it can't recurse.
	type chainTriggerAlias ChainTrigger
	var c chainTriggerAlias
	if err := json.Unmarshal(r, &c); err != nil {
		return ChainTrigger{}, err
	}
	return ChainTrigger(c), nil
}

// TriggerSummary counts each trigger kind (AgentResponse.trigger_summary).
type TriggerSummary struct {
	Cron      int `json:"cron"`
	Events    int `json:"events"`
	Market    int `json:"market"`
	ChainFrom int `json:"chain_from"`
}

// Summary returns the per-kind trigger counts.
func (t AgentTriggers) Summary() TriggerSummary {
	return TriggerSummary{Cron: len(t.Cron), Events: len(t.Events), Market: len(t.Market), ChainFrom: len(t.ChainFrom)}
}
