package ratelimit_test

import (
	"context"
	"testing"
	"time"

	"go-trade-bot/internal/ratelimit"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoryCounter_Window(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := ratelimit.NewMemoryCounter()
	c.Now = func() time.Time { return now }
	ctx := context.Background()

	for i := 1; i <= 3; i++ {
		n, err := c.Incr(ctx, "k", 15*time.Minute)
		require.NoError(t, err)
		assert.Equal(t, int64(i), n)
	}
	n, _ := c.Count(ctx, "k")
	assert.Equal(t, int64(3), n)

	now = now.Add(15 * time.Minute)
	n, _ = c.Count(ctx, "k")
	assert.Zero(t, n, "window expired")

	_, _ = c.Incr(ctx, "k", time.Minute)
	require.NoError(t, c.Reset(ctx, "k"))
	n, _ = c.Count(ctx, "k")
	assert.Zero(t, n)
}

func TestRedisCounter_FallsBackWhenRedisIsDown(t *testing.T) {
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", DialTimeout: 50 * time.Millisecond, MaxRetries: -1})
	defer client.Close()
	c := ratelimit.NewRedisCounter(client)
	ctx := context.Background()
	n, err := c.Incr(ctx, "login:user:x", time.Minute)
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)
	n, _ = c.Incr(ctx, "login:user:x", time.Minute)
	assert.Equal(t, int64(2), n)
	n, err = c.Count(ctx, "login:user:x")
	require.NoError(t, err)
	assert.Equal(t, int64(2), n)
}
