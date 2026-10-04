// Package ratelimit budgets Binance request weight across every worker replica
// (they share one IP) and implements a shared circuit breaker for HTTP 418/429.
package weightlimit

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// Store is the shared state: a per-window counter and a "banned until" mark.
type Store interface {
	// Incr adds by to the window counter (expiring after ttl) and returns the new total.
	Incr(ctx context.Context, key string, by int, ttl time.Duration) (int, error)
	Decr(ctx context.Context, key string, by int) error
	BannedUntil(ctx context.Context, key string) (time.Time, error)
	// BanUntil records a ban; it never shortens an existing, longer one.
	BanUntil(ctx context.Context, key string, until time.Time) error
}

// WeightLimiter is a fixed one-minute window of `Budget` weight. Keep Budget
// well under Binance's 6000/min (default use: 3000) - a fixed window can admit
// up to 2x the budget across a boundary and the trading path shares the IP.
type WeightLimiter struct {
	Store  Store
	Budget int
	Prefix string
	Now    func() time.Time
	Sleep  func(ctx context.Context, d time.Duration) error
}

func NewWeightLimiter(s Store, budget int) *WeightLimiter {
	return &WeightLimiter{Store: s, Budget: budget, Prefix: "ratelimit:binance", Now: time.Now, Sleep: sleepCtx}
}

// Wait blocks until `weight` can be spent without exceeding the budget and
// without a ban in force. A request heavier than the whole budget is let
// through alone in a fresh window rather than waiting forever.
func (l *WeightLimiter) Wait(ctx context.Context, weight int) error {
	for {
		now := l.Now()
		if until, err := l.Store.BannedUntil(ctx, l.Prefix+":ban"); err != nil {
			return err
		} else if until.After(now) {
			if err := l.Sleep(ctx, until.Sub(now)); err != nil {
				return err
			}
			continue
		}
		window := now.Unix() / 60
		key := l.Prefix + ":w:" + strconv.FormatInt(window, 10)
		total, err := l.Store.Incr(ctx, key, weight, 2*time.Minute)
		if err != nil {
			return err
		}
		if total <= l.Budget || total == weight {
			return nil
		}
		if err := l.Store.Decr(ctx, key, weight); err != nil {
			return err
		}
		next := time.Unix((window+1)*60, 0)
		if err := l.Sleep(ctx, next.Sub(now)+50*time.Millisecond); err != nil {
			return err
		}
	}
}

// Ban makes every Wait (on every replica) block for d - the circuit breaker for
// 418 (IP ban) and 429 responses.
func (l *WeightLimiter) Ban(ctx context.Context, d time.Duration) error {
	return l.Store.BanUntil(ctx, l.Prefix+":ban", l.Now().Add(d))
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// ---- in-memory store (tests, single process) ----

type MemStore struct {
	mu     sync.Mutex
	counts map[string]int
	bans   map[string]time.Time
}

func NewMemStore() *MemStore {
	return &MemStore{counts: map[string]int{}, bans: map[string]time.Time{}}
}

func (m *MemStore) Incr(_ context.Context, key string, by int, _ time.Duration) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.counts[key] += by
	return m.counts[key], nil
}
func (m *MemStore) Decr(_ context.Context, key string, by int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.counts[key] -= by
	return nil
}
func (m *MemStore) BannedUntil(_ context.Context, key string) (time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.bans[key], nil
}
func (m *MemStore) BanUntil(_ context.Context, key string, until time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if until.After(m.bans[key]) {
		m.bans[key] = until
	}
	return nil
}

// ---- Redis store (shared across replicas) ----

type RedisStore struct{ rdb redis.Cmdable }

func NewRedisStore(rdb redis.Cmdable) *RedisStore { return &RedisStore{rdb: rdb} }

func (r *RedisStore) Incr(ctx context.Context, key string, by int, ttl time.Duration) (int, error) {
	pipe := r.rdb.TxPipeline()
	incr := pipe.IncrBy(ctx, key, int64(by))
	pipe.Expire(ctx, key, ttl)
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, err
	}
	return int(incr.Val()), nil
}

func (r *RedisStore) Decr(ctx context.Context, key string, by int) error {
	return r.rdb.DecrBy(ctx, key, int64(by)).Err()
}

func (r *RedisStore) BannedUntil(ctx context.Context, key string) (time.Time, error) {
	v, err := r.rdb.Get(ctx, key).Result()
	if err == redis.Nil {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	ms, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return time.Time{}, nil
	}
	return time.UnixMilli(ms), nil
}

// banScript only ever extends a ban: SET if the new expiry is later.
var banScript = redis.NewScript(`
local cur = tonumber(redis.call('GET', KEYS[1]) or '0')
local new = tonumber(ARGV[1])
if new > cur then
  redis.call('SET', KEYS[1], ARGV[1], 'PX', ARGV[2])
  return 1
end
return 0`)

func (r *RedisStore) BanUntil(ctx context.Context, key string, until time.Time) error {
	ttl := time.Until(until)
	if ttl <= 0 {
		return nil
	}
	return banScript.Run(ctx, r.rdb, []string{key}, until.UnixMilli(), ttl.Milliseconds()+1000).Err()
}
