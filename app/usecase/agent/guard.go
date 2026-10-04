package agent

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"go-trade-bot/app/entities"
	"go-trade-bot/app/repository/agentplatform"
	"go-trade-bot/internal/notifier"
)

// RunGuard is consulted before EVERY model call of a run (not only at run
// start), so the global kill switch, a per-agent pause or an exhausted
// daily budget stop an in-flight run at its next iteration.
type RunGuard interface {
	// Check returns a non-nil error describing why the run must stop.
	Check(ctx context.Context, agent entities.Agent) error
}

// HaltError is a policy halt (kill switch, paused agent, budget) as opposed
// to an infrastructure failure while checking. Both stop the run; callers
// (e.g. the chat handler's 409) can tell them apart with errors.As.
type HaltError struct {
	Reason string
}

func (e *HaltError) Error() string { return e.Reason }

// IsHalt reports whether err is (or wraps) a *HaltError.
func IsHalt(err error) bool {
	var h *HaltError
	return errors.As(err, &h)
}

// SettingsReader is the narrow slice of app/repository/settings the guard
// needs to read the global kill switch.
type SettingsReader interface {
	Get(ctx context.Context) (*entities.Settings, error)
}

// DefaultGuard implements RunGuard over the settings row (global kill
// switch), the agent row (per-agent pause, budget) and today's AgentUsage.
// It FAILS CLOSED: if any of those reads fails, the run is stopped.
//
// Budget-exhausted notification dedupe (A-01 §4.3): at most one critical
// notification per agent per UTC day. Two layers: an in-process map keyed
// by agentID+day (cheap fast path, avoids a DB write on every blocked
// check), backed by AgentUsage.BudgetAlertSent, flipped with a conditional
// UPDATE so exactly one process wins even when cmd/api (chat) and cmd/agent
// (cron) both hit the budget on the same day.
type DefaultGuard struct {
	Settings SettingsReader
	Platform agentplatform.Repository
	Notifier notifier.AgentNotifier

	now     func() time.Time
	mu      sync.Mutex
	alerted map[string]bool
}

// NewDefaultGuard builds a DefaultGuard. notifier may be nil (no budget
// alert is sent then).
func NewDefaultGuard(settings SettingsReader, platform agentplatform.Repository, n notifier.AgentNotifier) *DefaultGuard {
	return &DefaultGuard{Settings: settings, Platform: platform, Notifier: n, now: time.Now, alerted: map[string]bool{}}
}

// Check implements RunGuard.
func (g *DefaultGuard) Check(ctx context.Context, agent entities.Agent) error {
	if g.Settings != nil {
		s, err := g.Settings.Get(ctx)
		if err != nil {
			return fmt.Errorf("could not read the agents kill switch: %v", err)
		}
		if s != nil && s.AgentsPaused {
			return &HaltError{Reason: "agents are paused by the global kill switch"}
		}
	}

	if g.Platform == nil || agent.ID == 0 {
		return nil
	}
	fresh, err := g.Platform.GetAgent(ctx, agent.ID)
	if err != nil {
		return fmt.Errorf("could not reload agent %d: %v", agent.ID, err)
	}
	if fresh.Paused {
		return &HaltError{Reason: fmt.Sprintf("agent %q is paused", fresh.Name)}
	}
	if fresh.DailyBudgetUSD > 0 {
		now := g.now()
		usage, err := g.Platform.GetUsage(ctx, fresh.ID, now)
		if err != nil {
			return fmt.Errorf("could not read agent %d usage: %v", fresh.ID, err)
		}
		if usage.CostUSD >= fresh.DailyBudgetUSD {
			g.alertBudgetExhausted(ctx, fresh, usage.CostUSD, now)
			return &HaltError{Reason: fmt.Sprintf("daily budget of $%.2f exhausted for agent %q (spent $%.2f today, UTC)", fresh.DailyBudgetUSD, fresh.Name, usage.CostUSD)}
		}
	}
	return nil
}

func (g *DefaultGuard) alertBudgetExhausted(ctx context.Context, agent entities.Agent, spent float64, now time.Time) {
	key := fmt.Sprintf("%d|%s", agent.ID, entities.UsageDay(now).Format("2006-01-02"))
	g.mu.Lock()
	if g.alerted[key] {
		g.mu.Unlock()
		return
	}
	g.alerted[key] = true
	g.mu.Unlock()

	won, err := g.Platform.MarkBudgetAlertSent(ctx, agent.ID, now)
	if err != nil {
		log.Printf("agent guard: could not record budget alert for agent %d: %v", agent.ID, err)
		return
	}
	if !won || g.Notifier == nil || len(agent.WebhookTargetIDs) == 0 {
		return
	}
	targets, err := g.Platform.ListWebhookTargetsByIDs(ctx, agent.WebhookTargetIDs)
	if err != nil {
		log.Printf("agent guard: could not load webhook targets for agent %d: %v", agent.ID, err)
		return
	}
	for _, e := range g.Notifier.SendToTargets(ctx, targets, notifier.AgentMessage{
		AgentName: agent.Name,
		Severity:  string(entities.SeverityCritical),
		Timestamp: now.UTC(),
	}.WithText(notifier.T("budget.title"), notifier.T("budget.message", agent.Name, spent, agent.DailyBudgetUSD))) {
		log.Printf("agent guard: budget alert for agent %d: %v", agent.ID, e)
	}
}
