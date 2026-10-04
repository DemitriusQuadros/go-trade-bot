package cooldown_test

import (
	"context"
	"os"
	"testing"
	"time"

	"go-trade-bot/internal/cooldown"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoryStore_ClaimRespectsTTL(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s := cooldown.NewMemoryStore()
	s.Now = func() time.Time { return now }
	ok, err := s.Claim(context.Background(), "k", time.Minute)
	require.NoError(t, err)
	assert.True(t, ok)
	ok, _ = s.Claim(context.Background(), "k", time.Minute)
	assert.False(t, ok)
	ok, _ = s.Claim(context.Background(), "other", time.Minute)
	assert.True(t, ok)
	now = now.Add(61 * time.Second)
	ok, _ = s.Claim(context.Background(), "k", time.Minute)
	assert.True(t, ok)
}

// Runs only with REDIS_TEST_ADDR set (same convention as internal/lock).
func TestRedisStore_Claim(t *testing.T) {
	addr := os.Getenv("REDIS_TEST_ADDR")
	if addr == "" {
		t.Skip("REDIS_TEST_ADDR not set")
	}
	client := redis.NewClient(&redis.Options{Addr: addr})
	defer client.Close()
	key := "agent-trigger:test:" + time.Now().Format(time.RFC3339Nano)
	defer client.Del(context.Background(), key)
	s := cooldown.NewRedisStore(client)
	ok, err := s.Claim(context.Background(), key, time.Minute)
	require.NoError(t, err)
	assert.True(t, ok)
	ok, err = s.Claim(context.Background(), key, time.Minute)
	require.NoError(t, err)
	assert.False(t, ok)
}
