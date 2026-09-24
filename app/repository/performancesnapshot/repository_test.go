package performancesnapshot_test

import (
	"context"
	"testing"
	"time"

	"go-trade-bot/app/entities"
	"go-trade-bot/app/repository/performancesnapshot"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&entities.StrategyPerformanceSnapshot{}))
	return db
}

// TestSnapshotRepository_Upsert_Idempotent covers backend-05 AC#2: re-running
// the snapshot job for an already-captured period updates the row in place,
// never duplicates it.
func TestSnapshotRepository_Upsert_Idempotent(t *testing.T) {
	db := setupTestDB(t)
	repo := performancesnapshot.NewSnapshotRepository(db)
	ctx := context.Background()

	periodStart := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := periodStart.AddDate(0, 0, 1)

	snap := entities.StrategyPerformanceSnapshot{
		StrategyID:  1,
		Symbol:      "BTCUSDT",
		Bucket:      entities.BucketDaily,
		PeriodStart: periodStart,
		PeriodEnd:   periodEnd,
		Profit:      45,
		Trades:      3,
	}
	require.NoError(t, repo.Upsert(ctx, snap))

	// Re-run with an updated profit - must update in place, not duplicate.
	snap.Profit = 50
	snap.Trades = 4
	require.NoError(t, repo.Upsert(ctx, snap))

	var count int64
	require.NoError(t, db.Model(&entities.StrategyPerformanceSnapshot{}).Count(&count).Error)
	assert.Equal(t, int64(1), count)

	rows, err := repo.ListDaily(ctx, 1, "BTCUSDT", periodStart, periodEnd.AddDate(0, 0, 1))
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, 50.0, rows[0].Profit)
	assert.Equal(t, 4, rows[0].Trades)
}

// TestSnapshotRepository_MultipleSymbols_IndependentRows covers backend-05
// AC#6: a strategy trading multiple symbols produces independent rows, never
// blended into one.
func TestSnapshotRepository_MultipleSymbols_IndependentRows(t *testing.T) {
	db := setupTestDB(t)
	repo := performancesnapshot.NewSnapshotRepository(db)
	ctx := context.Background()

	periodStart := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := periodStart.AddDate(0, 0, 1)

	require.NoError(t, repo.Upsert(ctx, entities.StrategyPerformanceSnapshot{
		StrategyID: 1, Symbol: "BTCUSDT", Bucket: entities.BucketDaily,
		PeriodStart: periodStart, PeriodEnd: periodEnd, Profit: 10, Trades: 1,
	}))
	require.NoError(t, repo.Upsert(ctx, entities.StrategyPerformanceSnapshot{
		StrategyID: 1, Symbol: "ETHUSDT", Bucket: entities.BucketDaily,
		PeriodStart: periodStart, PeriodEnd: periodEnd, Profit: 20, Trades: 2,
	}))

	btcRows, err := repo.ListDaily(ctx, 1, "BTCUSDT", periodStart, periodEnd.AddDate(0, 0, 1))
	require.NoError(t, err)
	require.Len(t, btcRows, 1)
	assert.Equal(t, 10.0, btcRows[0].Profit)

	ethRows, err := repo.ListDaily(ctx, 1, "ETHUSDT", periodStart, periodEnd.AddDate(0, 0, 1))
	require.NoError(t, err)
	require.Len(t, ethRows, 1)
	assert.Equal(t, 20.0, ethRows[0].Profit)
}

func TestSnapshotRepository_ListDaily_OrderedAscendingAndRangeFiltered(t *testing.T) {
	db := setupTestDB(t)
	repo := performancesnapshot.NewSnapshotRepository(db)
	ctx := context.Background()

	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		day := base.AddDate(0, 0, i)
		require.NoError(t, repo.Upsert(ctx, entities.StrategyPerformanceSnapshot{
			StrategyID: 1, Symbol: "BTCUSDT", Bucket: entities.BucketDaily,
			PeriodStart: day, PeriodEnd: day.AddDate(0, 0, 1), Profit: float64(i), Trades: 1,
		}))
	}

	rows, err := repo.ListDaily(ctx, 1, "BTCUSDT", base.AddDate(0, 0, 1), base.AddDate(0, 0, 4))
	require.NoError(t, err)
	require.Len(t, rows, 3)
	assert.Equal(t, 1.0, rows[0].Profit)
	assert.Equal(t, 3.0, rows[2].Profit)
}
