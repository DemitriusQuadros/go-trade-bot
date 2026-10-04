package agentplatform

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"go-trade-bot/app/entities"
	"go-trade-bot/internal/marketstatus"
)

// ChainCycleError is the 400 for a self-chain or a ChainFrom cycle
// (agents-platform C-01 §1). The REST layer renders it as
// {"error":"chain_cycle","message":"chain cycle: A → B → A"}.
type ChainCycleError struct {
	Message string
}

func (e *ChainCycleError) Error() string { return e.Message }

// IsChainCycle reports whether err is a *ChainCycleError.
func IsChainCycle(err error) bool {
	var c *ChainCycleError
	return errors.As(err, &c)
}

// Trigger validation bounds.
const (
	maxEventWindowHours  = 720   // 30 days
	maxCooldownMinutes   = 10080 // 7 days
	maxMarketRules       = 20
	maxEventTriggers     = 20
	maxChainTriggers     = 20
	maxMarketRuleIDLen   = 64
	maxPctMoveThreshold  = 1000
	maxDrawdownThreshold = 100
)

var symbolPattern = regexp.MustCompile(`^[A-Z0-9]{2,30}$`)

// validateTriggers validates and normalizes the non-cron triggers (defaults
// filled in, so the API echoes the effective values). selfID is 0 on create.
func (u *UseCase) validateTriggers(ctx context.Context, t *entities.AgentTriggers, selfID uint, selfName string) error {
	if len(t.Events) > maxEventTriggers {
		return badRequest("at most %d event triggers", maxEventTriggers)
	}
	seenEvent := map[string]bool{}
	for i := range t.Events {
		e := &t.Events[i]
		e.Type = strings.TrimSpace(e.Type)
		if !entities.IsValidEventType(e.Type) {
			return badRequest("unknown event type %q; allowed: %s", e.Type, strings.Join(entities.AllEventTypes, ", "))
		}
		if seenEvent[e.Type] {
			return badRequest("event type %q is listed twice", e.Type)
		}
		seenEvent[e.Type] = true
		if e.CooldownMinutes < 0 || e.CooldownMinutes > maxCooldownMinutes {
			return badRequest("events[%s].cooldown_minutes must be between 0 (default) and %d", e.Type, maxCooldownMinutes)
		}
		if e.CooldownMinutes == 0 {
			e.CooldownMinutes = int(e.EffectiveCooldown() / time.Minute)
		}
		switch e.Type {
		case entities.EventTypeDrawdown:
			if !(e.ThresholdPct > 0) || e.ThresholdPct > maxDrawdownThreshold {
				return badRequest("events[drawdown].threshold_pct must be > 0 and <= %d", maxDrawdownThreshold)
			}
			if e.WindowHours == 0 {
				e.WindowHours = entities.DefaultDrawdownWindowHours
			}
			if e.WindowHours < 1 || e.WindowHours > maxEventWindowHours {
				return badRequest("events[drawdown].window_hours must be between 1 and %d", maxEventWindowHours)
			}
		case entities.EventTypeNoSignal:
			e.ThresholdPct = 0
			if e.WindowHours < 1 || e.WindowHours > maxEventWindowHours {
				return badRequest("events[no_signal].window_hours is required (1-%d)", maxEventWindowHours)
			}
		default:
			e.ThresholdPct, e.WindowHours = 0, 0
		}
	}

	if len(t.Market) > maxMarketRules {
		return badRequest("at most %d market rules", maxMarketRules)
	}
	seenRule := map[string]bool{}
	for i := range t.Market {
		m := &t.Market[i]
		m.ID = strings.TrimSpace(m.ID)
		if m.ID == "" {
			m.ID = newRuleID()
		}
		if len(m.ID) > maxMarketRuleIDLen {
			return badRequest("market[%d].id must be at most %d characters", i, maxMarketRuleIDLen)
		}
		if seenRule[m.ID] {
			return badRequest("market rule id %q is used twice", m.ID)
		}
		seenRule[m.ID] = true
		m.Symbol = strings.TrimSpace(m.Symbol)
		if m.Symbol == "" || !symbolPattern.MatchString(m.Symbol) {
			return badRequest("market[%d].symbol must be a non-empty uppercase symbol like BTCUSDT (got %q)", i, m.Symbol)
		}
		if m.WindowMinutes < entities.MinMarketWindowMinutes || m.WindowMinutes > entities.MaxMarketWindowMinutes {
			return badRequest("market[%d].window_minutes must be between %d and %d", i, entities.MinMarketWindowMinutes, entities.MaxMarketWindowMinutes)
		}
		switch m.Kind {
		case entities.MarketKindPctMove:
			if !(m.ThresholdPct > 0) || m.ThresholdPct > maxPctMoveThreshold {
				return badRequest("market[%d].threshold_pct must be > 0 for pct_move", i)
			}
			m.Multiplier = 0
		case entities.MarketKindVolatilitySpike:
			if !(m.Multiplier > 1) || m.Multiplier > 1000 {
				return badRequest("market[%d].multiplier must be > 1 for volatility_spike (window ATR / prior-24h ATR)", i)
			}
			m.ThresholdPct = 0
		default:
			return badRequest("market[%d].kind must be %q or %q", i, entities.MarketKindPctMove, entities.MarketKindVolatilitySpike)
		}
		if m.CooldownMinutes == 0 {
			m.CooldownMinutes = entities.DefaultMarketCooldownMinutes
		}
		if m.CooldownMinutes < entities.MinMarketCooldownMinutes || m.CooldownMinutes > maxCooldownMinutes {
			return badRequest("market[%d].cooldown_minutes must be between %d and %d", i, entities.MinMarketCooldownMinutes, maxCooldownMinutes)
		}
	}

	if len(t.ChainFrom) == 0 {
		return nil
	}
	if len(t.ChainFrom) > maxChainTriggers {
		return badRequest("at most %d chain triggers", maxChainTriggers)
	}
	agents, err := u.repo.ListAgents(ctx)
	if err != nil {
		return err
	}
	byID := map[uint]entities.Agent{}
	for _, a := range agents {
		byID[a.ID] = a
	}
	name := func(id uint) string {
		if id == selfID && selfName != "" {
			return selfName
		}
		if a, ok := byID[id]; ok {
			return a.Name
		}
		return fmt.Sprintf("agent #%d", id)
	}
	seenChain := map[uint]bool{}
	for i := range t.ChainFrom {
		c := &t.ChainFrom[i]
		if c.On == "" {
			c.On = entities.ChainOnReport
		}
		if !entities.IsValidChainOn(c.On) {
			return badRequest("chain_from[%d].on must be report, notify or success", i)
		}
		if c.AgentID == 0 {
			return badRequest("chain_from[%d].agent_id is required", i)
		}
		if selfID != 0 && c.AgentID == selfID {
			n := name(selfID)
			return &ChainCycleError{Message: fmt.Sprintf("chain cycle: %s → %s", n, n)}
		}
		if _, ok := byID[c.AgentID]; !ok {
			return badRequest("chain_from[%d]: agent %d does not exist", i, c.AgentID)
		}
		if seenChain[c.AgentID] {
			return badRequest("chain_from lists agent %d twice", c.AgentID)
		}
		seenChain[c.AgentID] = true
	}
	if selfID == 0 {
		return nil // a new agent can't be referenced by anyone yet
	}
	// sources[x] = the agents x is chained from, with this agent's
	// proposed edges replacing its stored ones.
	sources := map[uint][]uint{}
	for _, a := range agents {
		if a.ID == selfID {
			continue
		}
		for _, c := range a.ParsedTriggers().ChainFrom {
			sources[a.ID] = append(sources[a.ID], c.AgentID)
		}
	}
	for _, c := range t.ChainFrom {
		sources[selfID] = append(sources[selfID], c.AgentID)
	}
	if path := findCycle(selfID, sources); path != nil {
		names := make([]string, len(path))
		for i, id := range path {
			names[i] = name(id)
		}
		return &ChainCycleError{Message: "chain cycle: " + strings.Join(names, " → ")}
	}
	return nil
}

// findCycle returns a cycle through start in run-flow order (source first,
// e.g. [A, B, A] for "A triggers B triggers A"), or nil.
func findCycle(start uint, sources map[uint][]uint) []uint {
	visited := map[uint]bool{}
	var stack []uint // listener -> source walk
	var dfs func(id uint) bool
	dfs = func(id uint) bool {
		stack = append(stack, id)
		for _, src := range sources[id] {
			if src == start {
				stack = append(stack, start)
				return true
			}
			if !visited[src] {
				visited[src] = true
				if dfs(src) {
					return true
				}
			}
		}
		stack = stack[:len(stack)-1]
		return false
	}
	visited[start] = true
	if !dfs(start) {
		return nil
	}
	// stack is start <- s1 <- s2 ... <- start (listener to source); runs
	// flow the other way.
	out := make([]uint, len(stack))
	for i, id := range stack {
		out[len(stack)-1-i] = id
	}
	return out
}

func newRuleID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// --- Market watch status (C-01 §6) --------------------------------------------

// SetMarketStatusReader wires the Redis snapshot cmd/agent writes (nil =
// always "no runtime seen").
func (u *UseCase) SetMarketStatusReader(r marketstatus.Reader) {
	u.market = r
}

// MarketSymbolsView is GET /agents/market-symbols.
type MarketSymbolsView struct {
	Symbols       []marketstatus.SymbolStatus
	RuntimeSeenAt *time.Time
}

// MarketSymbols returns the latest market-watch snapshot. No snapshot (or
// an unreadable one) is an empty list with RuntimeSeenAt nil - the UI then
// warns that cmd/agent is not running.
func (u *UseCase) MarketSymbols(ctx context.Context) (MarketSymbolsView, error) {
	v := MarketSymbolsView{Symbols: []marketstatus.SymbolStatus{}}
	if u.market == nil {
		return v, nil
	}
	snap, err := u.market.Read(ctx)
	if err != nil {
		log.Printf("agents: cannot read market status snapshot: %v", err)
		return v, nil
	}
	if snap == nil {
		return v, nil
	}
	if snap.Symbols != nil {
		v.Symbols = snap.Symbols
	}
	if !snap.UpdatedAt.IsZero() {
		seen := snap.UpdatedAt
		v.RuntimeSeenAt = &seen
	}
	return v, nil
}
