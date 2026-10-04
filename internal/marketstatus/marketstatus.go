// Package marketstatus is the small status snapshot cmd/agent's market
// watcher writes to Redis every sync and cmd/api reads for
// GET /api/agents/market-symbols (agents-platform C-01 §6).
package marketstatus

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

// Key is the Redis key holding the JSON snapshot.
const Key = "agent-runtime:market-status"

// TTL bounds how long a snapshot outlives a stopped cmd/agent; after it the
// API reports runtime_seen_at = null.
const TTL = 15 * time.Minute

// SymbolStatus is one watched symbol.
type SymbolStatus struct {
	Symbol       string     `json:"symbol"`
	Watchers     int        `json:"watchers"`   // distinct agents with a rule on the symbol
	LastPrice    *float64   `json:"last_price"` // last closed 1m candle's close; nil before the first candle
	LastCandleAt *time.Time `json:"last_candle_at"`
}

// Snapshot is the whole status document.
type Snapshot struct {
	UpdatedAt time.Time      `json:"updated_at"`
	Symbols   []SymbolStatus `json:"symbols"`
}

// Writer writes snapshots (the market watcher).
type Writer interface {
	Write(ctx context.Context, s Snapshot) error
}

// Reader reads the latest snapshot; nil with no error when there is none.
type Reader interface {
	Read(ctx context.Context) (*Snapshot, error)
}

// RedisStore implements Writer and Reader.
type RedisStore struct {
	client redis.UniversalClient
}

// NewRedisStore builds a store over client.
func NewRedisStore(client redis.UniversalClient) *RedisStore { return &RedisStore{client: client} }

// NewRedisStoreFromAddr builds a store with its own client for addr.
func NewRedisStoreFromAddr(addr string) *RedisStore {
	return NewRedisStore(redis.NewClient(&redis.Options{Addr: addr}))
}

// Write implements Writer.
func (s *RedisStore) Write(ctx context.Context, snap Snapshot) error {
	b, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	return s.client.Set(ctx, Key, b, TTL).Err()
}

// Read implements Reader.
func (s *RedisStore) Read(ctx context.Context) (*Snapshot, error) {
	b, err := s.client.Get(ctx, Key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var snap Snapshot
	if err := json.Unmarshal(b, &snap); err != nil {
		return nil, err
	}
	return &snap, nil
}
