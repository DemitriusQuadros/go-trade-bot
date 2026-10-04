package feed_test

import (
	"context"
	"testing"
	"time"

	"go-trade-bot/app/entities"
	"go-trade-bot/app/repository/candle"
	"go-trade-bot/internal/feed"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	err = db.AutoMigrate(&entities.Candle{})
	require.NoError(t, err)
	return db
}

func TestReplayFeed_SequentialDelivery(t *testing.T) {
	db := setupTestDB(t)
	repo := candle.NewCandleRepository(db)
	ctx := context.Background()

	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	var candles []entities.Candle
	for i := 0; i < 100; i++ {
		candles = append(candles, entities.Candle{
			Symbol:    "BTCUSDT",
			Timeframe: "1m",
			OpenTime:  base.Add(time.Duration(i) * time.Minute),
			Open:      float64(100 + i),
			High:      float64(105 + i),
			Low:       float64(95 + i),
			Close:     float64(102 + i),
			Volume:    10,
		})
	}
	err := repo.Upsert(ctx, candles)
	require.NoError(t, err)

	rFeed, err := feed.NewReplayFeed(ctx, repo, "BTCUSDT", "1m", base, base.Add(100*time.Minute))
	require.NoError(t, err)
	require.NotNil(t, rFeed)

	for i := 0; i < 100; i++ {
		c, ok := rFeed.Next()
		assert.True(t, ok)
		assert.Equal(t, base.Add(time.Duration(i)*time.Minute), c.OpenTime)
		assert.Equal(t, float64(100+i), c.Open)
	}

	// 101st call
	c, ok := rFeed.Next()
	assert.False(t, ok)
	assert.Empty(t, c.Symbol)

	// Idempotent exhaustion
	c2, ok2 := rFeed.Next()
	assert.False(t, ok2)
	assert.Empty(t, c2.Symbol)
}

func TestReplayFeed_EmptyRange(t *testing.T) {
	db := setupTestDB(t)
	repo := candle.NewCandleRepository(db)
	ctx := context.Background()

	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	rFeed, err := feed.NewReplayFeed(ctx, repo, "BTCUSDT", "1m", base, base.Add(10*time.Minute))
	require.NoError(t, err)
	require.NotNil(t, rFeed)

	c, ok := rFeed.Next()
	assert.False(t, ok)
	assert.Empty(t, c.Symbol)
}

func TestReplayFeed_FromEqualsTo(t *testing.T) {
	db := setupTestDB(t)
	repo := candle.NewCandleRepository(db)
	ctx := context.Background()

	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	rFeed, err := feed.NewReplayFeed(ctx, repo, "BTCUSDT", "1m", base, base)
	require.NoError(t, err)
	require.NotNil(t, rFeed)

	c, ok := rFeed.Next()
	assert.False(t, ok)
	assert.Empty(t, c.Symbol)
}

func TestReplayFeed_DataGapsPreserved(t *testing.T) {
	db := setupTestDB(t)
	repo := candle.NewCandleRepository(db)
	ctx := context.Background()

	base := time.Date(2024, 1, 1, 5, 0, 0, 0, time.UTC)
	candles := []entities.Candle{
		{
			Symbol:    "BTCUSDT",
			Timeframe: "1m",
			OpenTime:  base, // 05:00
			Open:      100,
			High:      105,
			Low:       95,
			Close:     102,
			Volume:    10,
		},
		{
			Symbol:    "BTCUSDT",
			Timeframe: "1m",
			OpenTime:  base.Add(3 * time.Minute), // 05:03 (gap of 05:01, 05:02)
			Open:      103,
			High:      108,
			Low:       101,
			Close:     107,
			Volume:    12,
		},
	}
	err := repo.Upsert(ctx, candles)
	require.NoError(t, err)

	rFeed, err := feed.NewReplayFeed(ctx, repo, "BTCUSDT", "1m", base, base.Add(10*time.Minute))
	require.NoError(t, err)

	c1, ok1 := rFeed.Next()
	require.True(t, ok1)
	assert.Equal(t, base, c1.OpenTime)

	c2, ok2 := rFeed.Next()
	require.True(t, ok2)
	assert.Equal(t, base.Add(3*time.Minute), c2.OpenTime)

	_, ok3 := rFeed.Next()
	assert.False(t, ok3)
}
