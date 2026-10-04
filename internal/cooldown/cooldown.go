// Package cooldown is the agent-trigger cooldown/dedupe store
// (agents-platform C-01 §2.3 / §3): Claim atomically takes a key for ttl
// (Redis SET NX PX), so a trigger fires at most once per cooldown even across
// several cmd/agent replicas.
package cooldown

import (
	"context"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// Store claims cooldown keys.
type Store interface {
	// Claim returns true when key was free and is now held for ttl; false
	// when it is still cooling down.
	Claim(ctx context.Context, key string, ttl time.Duration) (bool, error)
}

// RedisStore implements Store with SET NX PX.
type RedisStore struct {
	client redis.UniversalClient
}

// NewRedisStore builds a RedisStore over client.
func NewRedisStore(client redis.UniversalClient) *RedisStore { return &RedisStore{client: client} }

// NewRedisStoreFromAddr builds a RedisStore with its own client for addr.
func NewRedisStoreFromAddr(addr string) *RedisStore {
	return NewRedisStore(redis.NewClient(&redis.Options{Addr: addr}))
}

// Claim implements Store.
func (s *RedisStore) Claim(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	return s.client.SetNX(ctx, key, time.Now().UTC().Format(time.RFC3339), ttl).Result()
}

// MemoryStore is an in-process Store with an injectable clock (tests,
// single-process use).
type MemoryStore struct {
	mu   sync.Mutex
	keys map[string]time.Time
	Now  func() time.Time
}

// NewMemoryStore builds an empty MemoryStore on the wall clock.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{keys: map[string]time.Time{}, Now: time.Now}
}

// Claim implements Store.
func (s *MemoryStore) Claim(_ context.Context, key string, ttl time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.Now()
	if exp, ok := s.keys[key]; ok && now.Before(exp) {
		return false, nil
	}
	s.keys[key] = now.Add(ttl)
	return true, nil
}
