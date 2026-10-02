package weightlimit

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeClock struct {
	now    time.Time
	sleeps []time.Duration
}

func (c *fakeClock) Now() time.Time { return c.now }
func (c *fakeClock) Sleep(_ context.Context, d time.Duration) error {
	c.sleeps = append(c.sleeps, d)
	c.now = c.now.Add(d)
	return nil
}

func newLimiter(store Store, budget int, start time.Time) (*WeightLimiter, *fakeClock) {
	clk := &fakeClock{now: start}
	l := NewWeightLimiter(store, budget)
	l.Now, l.Sleep = clk.Now, clk.Sleep
	return l, clk
}

func exercise(t *testing.T, store Store) {
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 12, 0, 20, 0, time.UTC) // 20s into the minute
	l, clk := newLimiter(store, 10, start)

	for i := 0; i < 5; i++ { // 5 x weight 2 = the whole budget
		require.NoError(t, l.Wait(ctx, 2))
	}
	assert.Empty(t, clk.sleeps)

	require.NoError(t, l.Wait(ctx, 2)) // over budget: waits for the next window
	require.Len(t, clk.sleeps, 1)
	assert.InDelta(t, 40.05, clk.sleeps[0].Seconds(), 0.01)
	assert.Equal(t, 1, clk.now.Minute(), "the wait crossed into the next minute window")

	// a heavier-than-budget request is admitted alone instead of deadlocking
	l2, _ := newLimiter(store, 1, start.Add(10*time.Minute))
	require.NoError(t, l2.Wait(ctx, 2))
}

func exerciseBan(t *testing.T, store Store) {
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	l, clk := newLimiter(store, 100, start)

	require.NoError(t, l.Ban(ctx, 2*time.Minute))
	require.NoError(t, l.Ban(ctx, time.Second)) // never shortens
	require.NoError(t, l.Wait(ctx, 2))

	require.NotEmpty(t, clk.sleeps)
	assert.GreaterOrEqual(t, clk.now.Sub(start), 2*time.Minute, "blocked for the whole ban")
}

func TestWeightLimiter_Mem(t *testing.T) {
	exercise(t, NewMemStore())
	exerciseBan(t, NewMemStore())
}

func TestWeightLimiter_Redis(t *testing.T) {
	addr := os.Getenv("REDIS_TEST_ADDR")
	if addr == "" {
		t.Skip("REDIS_TEST_ADDR not set")
	}
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { rdb.Close() })
	require.NoError(t, rdb.Del(context.Background(), "ratelimit:binance:ban").Err())
	// RedisStore keys are wall-clock windows; the fake clock above only drives sleeping, so use a unique prefix
	st := NewRedisStore(rdb)
	l, _ := newLimiter(st, 4, time.Now())
	l.Prefix = "ratelimit:test:" + time.Now().Format("150405.000")
	ctx := context.Background()
	require.NoError(t, l.Wait(ctx, 2))
	require.NoError(t, l.Wait(ctx, 2))
	require.NoError(t, l.Ban(ctx, time.Second))
	until, err := st.BannedUntil(ctx, l.Prefix+":ban")
	require.NoError(t, err)
	assert.True(t, until.After(time.Now()))
}
