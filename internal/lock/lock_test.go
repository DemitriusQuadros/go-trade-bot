package lock

import (
	"context"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type strategyLock interface {
	Acquire(ctx context.Context, strategyID uint, holder string, ttl time.Duration) (bool, error)
	Release(ctx context.Context, strategyID uint, holder string) error
}

func exerciseLockSemantics(t *testing.T, l strategyLock, id uint) {
	ctx := context.Background()
	ok, err := l.Acquire(ctx, id, "run:1", time.Minute)
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = l.Acquire(ctx, id, "run:2", time.Minute)
	require.NoError(t, err)
	assert.False(t, ok, "second holder is refused")

	require.NoError(t, l.Release(ctx, id, "run:2"))
	ok, err = l.Acquire(ctx, id, "run:2", time.Minute)
	require.NoError(t, err)
	assert.False(t, ok, "a non-holder's release must not free the lock")

	require.NoError(t, l.Release(ctx, id, "run:1"))
	ok, err = l.Acquire(ctx, id, "run:2", time.Minute)
	require.NoError(t, err)
	assert.True(t, ok)
	require.NoError(t, l.Release(ctx, id, "run:2"))
}

func exerciseConcurrentAcquire(t *testing.T, l strategyLock, id uint) {
	var winners int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ok, err := l.Acquire(context.Background(), id, "run:"+string(rune('a'+i)), time.Minute)
			if err == nil && ok {
				atomic.AddInt32(&winners, 1)
			}
		}(i)
	}
	wg.Wait()
	assert.Equal(t, int32(1), winners)
}

func TestMemoryStrategyLock_Semantics(t *testing.T) {
	exerciseLockSemantics(t, NewMemoryStrategyLock(), 5)
}

func TestMemoryStrategyLock_ExactlyOneConcurrentWinner(t *testing.T) {
	exerciseConcurrentAcquire(t, NewMemoryStrategyLock(), 5)
}

func TestMemoryStrategyLock_TTLExpiry(t *testing.T) {
	l := NewMemoryStrategyLock()
	now := time.Now()
	l.clock = func() time.Time { return now }
	ok, _ := l.Acquire(context.Background(), 1, "a", time.Second)
	require.True(t, ok)
	now = now.Add(2 * time.Second)
	ok, _ = l.Acquire(context.Background(), 1, "b", time.Second)
	assert.True(t, ok, "an expired lease can be taken over")
}

// The Redis implementation is tested against a real Redis when
// REDIS_TEST_ADDR is set (miniredis is not a dependency of this module).
func redisLockForTest(t *testing.T) *RedisStrategyLock {
	addr := os.Getenv("REDIS_TEST_ADDR")
	if addr == "" {
		t.Skip("REDIS_TEST_ADDR not set - skipping RedisStrategyLock integration test (miniredis is not a dependency)")
	}
	client := redis.NewClient(&redis.Options{Addr: addr})
	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Skipf("redis at %s unreachable: %v", addr, err)
	}
	t.Cleanup(func() { client.Close() })
	return NewRedisStrategyLock(client)
}

func TestRedisStrategyLock_Semantics(t *testing.T) {
	l := redisLockForTest(t)
	id := uint(time.Now().UnixNano()%1_000_000 + 1_000_000)
	exerciseLockSemantics(t, l, id)
}

func TestRedisStrategyLock_ExactlyOneConcurrentWinner(t *testing.T) {
	l := redisLockForTest(t)
	id := uint(time.Now().UnixNano()%1_000_000 + 2_000_000)
	exerciseConcurrentAcquire(t, l, id)
	_ = l.client.Del(context.Background(), keyWithPrefix(KeyPrefix, id)).Err()
}

// The cycle lock and the agent writer lock are independent keys.
func TestRedisLock_PrefixesAreIndependent(t *testing.T) {
	writer := redisLockForTest(t)
	cycle := NewRedisLockWithPrefix(writer.client, CycleKeyPrefix)
	id := uint(time.Now().UnixNano()%1_000_000 + 3_000_000)
	ctx := context.Background()
	ok, err := writer.Acquire(ctx, id, "run:1", time.Minute)
	require.NoError(t, err)
	assert.True(t, ok)
	ok, err = cycle.Acquire(ctx, id, "worker", time.Minute)
	require.NoError(t, err)
	assert.True(t, ok, "strategy-cycle:<id> is a different key from agent:strategy-lock:<id>")
	exists, err := writer.client.Exists(ctx, CycleKeyPrefix+strconv.FormatUint(uint64(id), 10)).Result()
	require.NoError(t, err)
	assert.EqualValues(t, 1, exists)
	require.NoError(t, cycle.Release(ctx, id, "worker"))
	require.NoError(t, writer.Release(ctx, id, "run:1"))
}
