package candle_test

import (
	"context"
	"testing"
	"time"

	"go-trade-bot/app/entities"
	"go-trade-bot/app/repository/candle"

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

func TestCandleRepository_Upsert_Idempotency(t *testing.T) {
	db := setupTestDB(t)
	repo := candle.NewCandleRepository(db)
	ctx := context.Background()

	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c1 := entities.Candle{
		Symbol:    "BTCUSDT",
		Timeframe: "1m",
		OpenTime:  t0,
		Open:      100,
		High:      110,
		Low:       95,
		Close:     105,
		Volume:    10,
	}

	err := repo.Upsert(ctx, []entities.Candle{c1})
	require.NoError(t, err)

	count, err := repo.Count(ctx, "BTCUSDT", "1m")
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)

	// Second write with updated values
	c1Updated := entities.Candle{
		Symbol:    "BTCUSDT",
		Timeframe: "1m",
		OpenTime:  t0,
		Open:      100,
		High:      115,
		Low:       90,
		Close:     112,
		Volume:    15,
	}

	err = repo.Upsert(ctx, []entities.Candle{c1Updated})
	require.NoError(t, err)

	count, err = repo.Count(ctx, "BTCUSDT", "1m")
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)

	res, err := repo.RangeBefore(ctx, "BTCUSDT", "1m", t0, 10)
	require.NoError(t, err)
	require.Len(t, res, 1)
	assert.Equal(t, 115.0, res[0].High)
	assert.Equal(t, 112.0, res[0].Close)
	assert.Equal(t, 15.0, res[0].Volume)
}

func TestCandleRepository_RangeBefore_AntiLookahead(t *testing.T) {
	db := setupTestDB(t)
	repo := candle.NewCandleRepository(db)
	ctx := context.Background()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var candles []entities.Candle
	for i := 0; i < 300; i++ {
		candles = append(candles, entities.Candle{
			Symbol:    "BTCUSDT",
			Timeframe: "1m",
			OpenTime:  base.Add(time.Duration(i) * time.Minute),
			Open:      float64(100 + i),
			High:      float64(105 + i),
			Low:       float64(95 + i),
			Close:     float64(102 + i),
			Volume:    float64(10 + i),
		})
	}
	err := repo.Upsert(ctx, candles)
	require.NoError(t, err)

	// Query asOf = 02:30 (150 minutes from base) with limit = 100
	asOf := base.Add(150 * time.Minute)
	res, err := repo.RangeBefore(ctx, "BTCUSDT", "1m", asOf, 100)
	require.NoError(t, err)
	require.Len(t, res, 100)

	// Verify ascending order
	for i := 0; i < len(res)-1; i++ {
		assert.True(t, res[i].OpenTime.Before(res[i+1].OpenTime))
	}

	// Verify no candle is after asOf
	for _, c := range res {
		assert.True(t, c.OpenTime.Before(asOf) || c.OpenTime.Equal(asOf))
	}
	assert.Equal(t, asOf, res[len(res)-1].OpenTime)
}

func TestCandleRepository_Range(t *testing.T) {
	db := setupTestDB(t)
	repo := candle.NewCandleRepository(db)
	ctx := context.Background()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var candles []entities.Candle
	for i := 0; i < 10; i++ {
		candles = append(candles, entities.Candle{
			Symbol:    "BTCUSDT",
			Timeframe: "1m",
			OpenTime:  base.Add(time.Duration(i) * time.Minute),
			Open:      100,
			High:      105,
			Low:       95,
			Close:     102,
			Volume:    10,
		})
	}
	err := repo.Upsert(ctx, candles)
	require.NoError(t, err)

	from := base.Add(2 * time.Minute)
	to := base.Add(6 * time.Minute)
	res, err := repo.Range(ctx, "BTCUSDT", "1m", from, to)
	require.NoError(t, err)
	require.Len(t, res, 4) // minutes 2, 3, 4, 5
	assert.Equal(t, from, res[0].OpenTime)
	assert.Equal(t, base.Add(5*time.Minute), res[3].OpenTime)

	// Range with no matches
	emptyRes, err := repo.Range(ctx, "BTCUSDT", "1m", base.Add(-10*time.Minute), base.Add(-5*time.Minute))
	require.NoError(t, err)
	assert.Empty(t, emptyRes)
}

func TestCandleRepository_LatestOpenTime_And_Count(t *testing.T) {
	db := setupTestDB(t)
	repo := candle.NewCandleRepository(db)
	ctx := context.Background()

	// On empty table
	latest, err := repo.LatestOpenTime(ctx, "BTCUSDT", "1m")
	require.NoError(t, err)
	assert.True(t, latest.IsZero())

	count, err := repo.Count(ctx, "BTCUSDT", "1m")
	require.NoError(t, err)
	assert.Equal(t, int64(0), count)

	// Add data
	base := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	c := entities.Candle{
		Symbol:    "BTCUSDT",
		Timeframe: "1m",
		OpenTime:  base,
		Open:      100,
		High:      105,
		Low:       95,
		Close:     102,
		Volume:    10,
	}
	err = repo.Upsert(ctx, []entities.Candle{c})
	require.NoError(t, err)

	latest, err = repo.LatestOpenTime(ctx, "BTCUSDT", "1m")
	require.NoError(t, err)
	assert.Equal(t, base, latest)

	count, err = repo.Count(ctx, "BTCUSDT", "1m")
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)
}

func TestCandleRepository_TimeframeIsolation(t *testing.T) {
	db := setupTestDB(t)
	repo := candle.NewCandleRepository(db)
	ctx := context.Background()

	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c1m := entities.Candle{
		Symbol:    "BTCUSDT",
		Timeframe: "1m",
		OpenTime:  t0,
		Open:      100,
		High:      105,
		Low:       95,
		Close:     102,
		Volume:    10,
	}
	c15m := entities.Candle{
		Symbol:    "BTCUSDT",
		Timeframe: "15m",
		OpenTime:  t0,
		Open:      100,
		High:      120,
		Low:       90,
		Close:     115,
		Volume:    150,
	}

	err := repo.Upsert(ctx, []entities.Candle{c1m, c15m})
	require.NoError(t, err)

	count1m, err := repo.Count(ctx, "BTCUSDT", "1m")
	require.NoError(t, err)
	assert.Equal(t, int64(1), count1m)

	count15m, err := repo.Count(ctx, "BTCUSDT", "15m")
	require.NoError(t, err)
	assert.Equal(t, int64(1), count15m)

	res1m, err := repo.RangeBefore(ctx, "BTCUSDT", "1m", t0, 10)
	require.NoError(t, err)
	require.Len(t, res1m, 1)
	assert.Equal(t, "1m", res1m[0].Timeframe)
	assert.Equal(t, 10.0, res1m[0].Volume)

	res15m, err := repo.RangeBefore(ctx, "BTCUSDT", "15m", t0, 10)
	require.NoError(t, err)
	require.Len(t, res15m, 1)
	assert.Equal(t, "15m", res15m[0].Timeframe)
	assert.Equal(t, 150.0, res15m[0].Volume)
}
