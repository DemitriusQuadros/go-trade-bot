// Package lock provides the one-writer-per-strategy lock used by agent
// runs (agents-platform A-02 §4): while an agent run edits a strategy's
// script, no other run may.
package lock

import (
	"context"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// KeyPrefix namespaces the agent writer lock keys in Redis.
const KeyPrefix = "agent:strategy-lock:"

// CycleKeyPrefix namespaces the strategy-cycle lock (agents-platform Phase
// B-01 §5): held by cmd/worker around every strategy cycle and by cmd/agent's
// agent:apply_proposal around the code swap, so a cycle and an apply never
// overlap for the same strategy.
const CycleKeyPrefix = "strategy-cycle:"

func keyWithPrefix(prefix string, strategyID uint) string {
	return prefix + strconv.FormatUint(uint64(strategyID), 10)
}

// releaseScript deletes the key only if it is still held by holder
// (compare-and-delete), so a run whose lock already expired can never
// release a lock since taken by another run.
var releaseScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
	return redis.call("DEL", KEYS[1])
end
return 0
`)

// RedisStrategyLock implements the lock with SET NX PX + a compare-and-
// delete Lua script.
type RedisStrategyLock struct {
	client redis.UniversalClient
	prefix string
}

// NewRedisStrategyLock builds the agent writer lock (KeyPrefix) over client.
func NewRedisStrategyLock(client redis.UniversalClient) *RedisStrategyLock {
	return NewRedisLockWithPrefix(client, KeyPrefix)
}

// NewRedisLockWithPrefix builds a lock whose keys are prefix+<strategyID>.
func NewRedisLockWithPrefix(client redis.UniversalClient, prefix string) *RedisStrategyLock {
	return &RedisStrategyLock{client: client, prefix: prefix}
}

// NewRedisStrategyLockFromAddr builds a lock with its own client for addr.
func NewRedisStrategyLockFromAddr(addr string) *RedisStrategyLock {
	return NewRedisStrategyLock(redis.NewClient(&redis.Options{Addr: addr}))
}

// NewRedisCycleLockFromAddr builds the strategy-cycle lock (CycleKeyPrefix)
// with its own client for addr.
func NewRedisCycleLockFromAddr(addr string) *RedisStrategyLock {
	return NewRedisLockWithPrefix(redis.NewClient(&redis.Options{Addr: addr}), CycleKeyPrefix)
}

// Acquire takes the lock for holder with ttl; false if someone else holds it.
func (l *RedisStrategyLock) Acquire(ctx context.Context, strategyID uint, holder string, ttl time.Duration) (bool, error) {
	return l.client.SetNX(ctx, keyWithPrefix(l.prefix, strategyID), holder, ttl).Result()
}

// Release deletes the lock only if holder still owns it.
func (l *RedisStrategyLock) Release(ctx context.Context, strategyID uint, holder string) error {
	return releaseScript.Run(ctx, l.client, []string{keyWithPrefix(l.prefix, strategyID)}, holder).Err()
}

// Close closes the underlying client.
func (l *RedisStrategyLock) Close() error { return l.client.Close() }
