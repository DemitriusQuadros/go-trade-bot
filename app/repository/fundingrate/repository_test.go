package fundingrate_test

import (
	"context"
	"testing"
	"time"

	"go-trade-bot/app/entities"
	"go-trade-bot/app/repository/fundingrate"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)

	err = db.AutoMigrate(&entities.FundingRate{})
	require.NoError(t, err)

	return db
}

func TestFundingRateRepository_Upsert_Idempotency(t *testing.T) {
	db := setupTestDB(t)
	repo := fundingrate.NewFundingRateRepository(db)
	ctx := context.Background()

	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	f1 := entities.FundingRate{
		Symbol:      "BTCUSDT",
		FundingTime: t0,
		Rate:        0.0001,
		MarkPrice:   50000,
	}

	err := repo.Upsert(ctx, []entities.FundingRate{f1})
	require.NoError(t, err)

	count, err := repo.Count(ctx, "BTCUSDT")
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)

	// Second write with updated values for same (symbol, funding_time)
	f1Updated := entities.FundingRate{
		Symbol:      "BTCUSDT",
		FundingTime: t0,
		Rate:        0.00015,
		MarkPrice:   50500,
	}
	err = repo.Upsert(ctx, []entities.FundingRate{f1Updated})
	require.NoError(t, err)

	// Count should still be 1 (no duplicate row)
	count, err = repo.Count(ctx, "BTCUSDT")
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)

	// Fetch to verify values were updated
	rates, err := repo.Range(ctx, "BTCUSDT", t0, t0)
	require.NoError(t, err)
	require.Len(t, rates, 1)
	assert.Equal(t, 0.00015, rates[0].Rate)
	assert.Equal(t, 50500.0, rates[0].MarkPrice)
}

func TestFundingRateRepository_Range_And_LatestFundingTime(t *testing.T) {
	db := setupTestDB(t)
	repo := fundingrate.NewFundingRateRepository(db)
	ctx := context.Background()

	// Initial LatestFundingTime on empty DB returns zero time and no error
	latestEmpty, err := repo.LatestFundingTime(ctx, "BTCUSDT")
	require.NoError(t, err)
	assert.True(t, latestEmpty.IsZero())

	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t1 := time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 1, 1, 16, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)

	rates := []entities.FundingRate{
		{Symbol: "BTCUSDT", FundingTime: t0, Rate: 0.0001, MarkPrice: 50000},
		{Symbol: "BTCUSDT", FundingTime: t1, Rate: 0.00012, MarkPrice: 50200},
		{Symbol: "BTCUSDT", FundingTime: t2, Rate: 0.00008, MarkPrice: 49800},
		{Symbol: "BTCUSDT", FundingTime: t3, Rate: 0.00015, MarkPrice: 51000},
		{Symbol: "ETHUSDT", FundingTime: t0, Rate: 0.0002, MarkPrice: 3000},
	}

	err = repo.Upsert(ctx, rates)
	require.NoError(t, err)

	// Range for BTCUSDT from t1 to t2 inclusive
	btcRange, err := repo.Range(ctx, "BTCUSDT", t1, t2)
	require.NoError(t, err)
	require.Len(t, btcRange, 2)
	assert.True(t, btcRange[0].FundingTime.Equal(t1))
	assert.True(t, btcRange[1].FundingTime.Equal(t2))

	// LatestFundingTime for BTCUSDT
	latestBTC, err := repo.LatestFundingTime(ctx, "BTCUSDT")
	require.NoError(t, err)
	assert.True(t, latestBTC.Equal(t3))

	// Count per symbol
	btcCount, err := repo.Count(ctx, "BTCUSDT")
	require.NoError(t, err)
	assert.Equal(t, int64(4), btcCount)

	ethCount, err := repo.Count(ctx, "ETHUSDT")
	require.NoError(t, err)
	assert.Equal(t, int64(1), ethCount)

	solCount, err := repo.Count(ctx, "SOLUSDT")
	require.NoError(t, err)
	assert.Equal(t, int64(0), solCount)
}
