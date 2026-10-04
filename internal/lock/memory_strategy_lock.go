package lock

import (
	"context"
	"sync"
	"time"
)

// MemoryStrategyLock is an in-process StrategyLock with the same semantics
// as RedisStrategyLock (NX + TTL + holder-checked release). Useful for tests
// and single-process setups; it does NOT coordinate across processes.
type MemoryStrategyLock struct {
	mu    sync.Mutex
	held  map[uint]memoryLease
	clock func() time.Time
}

type memoryLease struct {
	holder  string
	expires time.Time
}

// NewMemoryStrategyLock builds a MemoryStrategyLock.
func NewMemoryStrategyLock() *MemoryStrategyLock {
	return &MemoryStrategyLock{held: map[uint]memoryLease{}, clock: time.Now}
}

// Acquire implements the lock.
func (l *MemoryStrategyLock) Acquire(_ context.Context, strategyID uint, holder string, ttl time.Duration) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.clock()
	if lease, ok := l.held[strategyID]; ok && now.Before(lease.expires) {
		return false, nil
	}
	l.held[strategyID] = memoryLease{holder: holder, expires: now.Add(ttl)}
	return true, nil
}

// Release implements the lock.
func (l *MemoryStrategyLock) Release(_ context.Context, strategyID uint, holder string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if lease, ok := l.held[strategyID]; ok && lease.holder == holder {
		delete(l.held, strategyID)
	}
	return nil
}
