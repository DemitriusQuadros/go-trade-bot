package agent

import (
	"context"
	"sync"
	"time"

	"go-trade-bot/app/entities"
)

// StrategyLock is the one-writer-per-strategy lock (A-02 §4). A nil Lock
// on AgentUseCase means no locking (cmd/mcp).
type StrategyLock interface {
	Acquire(ctx context.Context, strategyID uint, holder string, ttl time.Duration) (bool, error)
	Release(ctx context.Context, strategyID uint, holder string) error
}

// StrategyLockTTL bounds how long a crashed run can keep a strategy locked.
const StrategyLockTTL = 15 * time.Minute

// maxNotificationsPerRun is the notify tool's per-run rate limit.
const maxNotificationsPerRun = 10

// runScope carries per-run state to tool executors through the context,
// so Tool.Execute's signature (shared with cmd/mcp) stays unchanged.
type runScope struct {
	agent entities.Agent
	runID uint

	mu            sync.Mutex
	locks         map[uint]bool
	notifications int
}

type runScopeKey struct{}

func withRunScope(ctx context.Context, s *runScope) context.Context {
	return context.WithValue(ctx, runScopeKey{}, s)
}

func scopeFrom(ctx context.Context) (*runScope, bool) {
	s, ok := ctx.Value(runScopeKey{}).(*runScope)
	return s, ok && s != nil
}

func (s *runScope) holder() string {
	return "run:" + uintToString(s.runID)
}

func (s *runScope) runIDPtr() *uint {
	if s.runID == 0 {
		return nil
	}
	id := s.runID
	return &id
}

func (s *runScope) takeNotificationSlot() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.notifications >= maxNotificationsPerRun {
		return false
	}
	s.notifications++
	return true
}

func (s *runScope) heldLocks() []uint {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]uint, 0, len(s.locks))
	for id := range s.locks {
		out = append(out, id)
	}
	return out
}
