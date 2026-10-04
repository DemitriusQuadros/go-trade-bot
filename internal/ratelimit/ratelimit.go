// Package ratelimit counts failures per key in a fixed window (auth-01 §4
// login rate limit): Redis INCR + PEXPIRE on the first hit (Lua), with an in-memory fallback when
// Redis is unreachable so a Redis outage never disables the limit.
package ratelimit

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// Counter counts events per key within a window.
type Counter interface {
	// Count returns the current count for key (0 when absent/expired).
	Count(ctx context.Context, key string) (int64, error)
	// Incr adds one to key, starting a new window of length window when the
	// key is new, and returns the new count.
	Incr(ctx context.Context, key string, window time.Duration) (int64, error)
	// Reset clears key.
	Reset(ctx context.Context, key string) error
}

// MemoryCounter is an in-process Counter.
type MemoryCounter struct {
	mu      sync.Mutex
	entries map[string]memEntry
	// Now is injectable for tests.
	Now func() time.Time
}

type memEntry struct {
	n       int64
	expires time.Time
}

// NewMemoryCounter builds a MemoryCounter.
func NewMemoryCounter() *MemoryCounter {
	return &MemoryCounter{entries: map[string]memEntry{}, Now: time.Now}
}

func (m *MemoryCounter) get(key string) (memEntry, bool) {
	e, ok := m.entries[key]
	if ok && !m.Now().Before(e.expires) {
		delete(m.entries, key)
		return memEntry{}, false
	}
	return e, ok
}

func (m *MemoryCounter) Count(_ context.Context, key string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, _ := m.get(key)
	return e.n, nil
}

func (m *MemoryCounter) Incr(_ context.Context, key string, window time.Duration) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.get(key)
	if !ok {
		e = memEntry{expires: m.Now().Add(window)}
	}
	e.n++
	m.entries[key] = e
	// Opportunistic cleanup keeps the map bounded.
	if len(m.entries) > 10000 {
		now := m.Now()
		for k, v := range m.entries {
			if !now.Before(v.expires) {
				delete(m.entries, k)
			}
		}
	}
	return e.n, nil
}

func (m *MemoryCounter) Reset(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.entries, key)
	return nil
}

// RedisCounter is a Counter on Redis that falls back to an in-memory
// counter on any Redis error.
type RedisCounter struct {
	client   redis.UniversalClient
	fallback *MemoryCounter
	warnOnce sync.Once
}

// NewRedisCounter builds a RedisCounter over client.
func NewRedisCounter(client redis.UniversalClient) *RedisCounter {
	return &RedisCounter{client: client, fallback: NewMemoryCounter()}
}

// NewRedisCounterFromAddr builds a RedisCounter with its own client.
func NewRedisCounterFromAddr(addr string) *RedisCounter {
	return NewRedisCounter(redis.NewClient(&redis.Options{Addr: addr, DialTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second}))
}

func (r *RedisCounter) warn(err error) {
	r.warnOnce.Do(func() {
		log.Printf("ratelimit: redis unavailable (%v) - using the in-memory fallback", err)
	})
}

func (r *RedisCounter) Count(ctx context.Context, key string) (int64, error) {
	n, err := r.client.Get(ctx, key).Int64()
	if err == redis.Nil {
		// Redis may be fine now while the fallback still holds counts from
		// an outage - take the larger.
		fb, _ := r.fallback.Count(ctx, key)
		return fb, nil
	}
	if err != nil {
		r.warn(err)
		return r.fallback.Count(ctx, key)
	}
	fb, _ := r.fallback.Count(ctx, key)
	return max(n, fb), nil
}

// incrScript increments a key and starts its window on the first hit, in
// one atomic step (works on any Redis version, unlike EXPIRE NX).
var incrScript = redis.NewScript(`
local n = redis.call("INCR", KEYS[1])
if n == 1 then redis.call("PEXPIRE", KEYS[1], ARGV[1]) end
return n`)

func (r *RedisCounter) Incr(ctx context.Context, key string, window time.Duration) (int64, error) {
	n, err := incrScript.Run(ctx, r.client, []string{key}, window.Milliseconds()).Int64()
	if err != nil {
		r.warn(err)
		return r.fallback.Incr(ctx, key, window)
	}
	return n, nil
}

func (r *RedisCounter) Reset(ctx context.Context, key string) error {
	_ = r.fallback.Reset(ctx, key)
	if err := r.client.Del(ctx, key).Err(); err != nil {
		r.warn(err)
	}
	return nil
}
