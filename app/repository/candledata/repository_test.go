package candledata_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"go-trade-bot/app/entities"
	"go-trade-bot/app/repository/candledata"
)

func setup(t *testing.T) (candledata.Repository, *entities.CandleDataset) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&entities.CandleDataset{}, &entities.CandleSegment{}, &entities.CandleChunk{}))
	repo := candledata.NewRepository(db)
	ds := &entities.CandleDataset{Symbol: "BTCUSDT", Timeframe: "1h", KeepLive: true}
	require.NoError(t, repo.UpsertDataset(context.Background(), ds))
	return repo, ds
}

func day(d int) time.Time { return time.Date(2024, 1, d, 0, 0, 0, 0, time.UTC) }

func TestUpsertDataset_IsIdempotentPerSymbolTimeframe(t *testing.T) {
	repo, ds := setup(t)
	again := &entities.CandleDataset{Symbol: "BTCUSDT", Timeframe: "1h", KeepLive: false}
	require.NoError(t, repo.UpsertDataset(context.Background(), again))
	assert.Equal(t, ds.ID, again.ID)
	assert.False(t, again.KeepLive)
	all, _ := repo.ListDatasets(context.Background())
	assert.Len(t, all, 1)
}

func TestReplaceSegments_SwapsOnlyFullyContainedRows(t *testing.T) {
	repo, ds := setup(t)
	ctx := context.Background()
	seg := func(f, to int) entities.CandleSegment {
		return entities.CandleSegment{From: day(f), To: day(to), State: entities.SegmentLoaded, Source: "archive"}
	}
	require.NoError(t, repo.ReplaceSegments(ctx, ds.ID, day(1), day(10), []entities.CandleSegment{seg(1, 5), seg(5, 10)}))
	// re-run of the same chunk range replaces, never duplicates
	require.NoError(t, repo.ReplaceSegments(ctx, ds.ID, day(1), day(10), []entities.CandleSegment{seg(1, 10)}))
	got, err := repo.Segments(ctx, ds.ID)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, day(10), got[0].To)

	// a segment from another chunk range is untouched
	require.NoError(t, repo.ReplaceSegments(ctx, ds.ID, day(10), day(20), []entities.CandleSegment{seg(10, 20)}))
	require.NoError(t, repo.ReplaceSegments(ctx, ds.ID, day(10), day(20), nil))
	got, _ = repo.Segments(ctx, ds.ID)
	assert.Len(t, got, 1)
}

func TestEnqueueChunks_DedupesAndSkipsContained(t *testing.T) {
	repo, ds := setup(t)
	ctx := context.Background()
	in := []candledata.ChunkInput{{From: day(1), To: day(10), Purpose: entities.PurposeBackfill, Priority: 10}}

	created, err := repo.EnqueueChunks(ctx, ds.ID, in)
	require.NoError(t, err)
	require.Len(t, created, 1)

	again, err := repo.EnqueueChunks(ctx, ds.ID, in) // identical
	require.NoError(t, err)
	assert.Empty(t, again)

	inner, err := repo.EnqueueChunks(ctx, ds.ID, []candledata.ChunkInput{{From: day(2), To: day(3)}}) // contained by an active chunk
	require.NoError(t, err)
	assert.Empty(t, inner)
}

func TestChunkLifecycle_ClaimIsExclusive_AndRetryFailed(t *testing.T) {
	repo, ds := setup(t)
	ctx := context.Background()
	now := time.Now()
	created, _ := repo.EnqueueChunks(ctx, ds.ID, []candledata.ChunkInput{{From: day(1), To: day(2), Purpose: entities.PurposeTail}})
	id := created[0].ID

	c, ok, err := repo.ClaimChunk(ctx, id, "w1", now)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, 1, c.Attempts)
	_, ok, _ = repo.ClaimChunk(ctx, id, "w2", now)
	assert.False(t, ok, "a running chunk cannot be claimed twice")

	require.NoError(t, repo.FailChunk(ctx, id, "boom", true, now))
	counts, _ := repo.ChunkCounts(ctx, ds.ID)
	assert.Equal(t, int64(1), counts[entities.ChunkDead])

	retried, err := repo.RetryFailed(ctx, ds.ID)
	require.NoError(t, err)
	require.Len(t, retried, 1)
	c, ok, _ = repo.ClaimChunk(ctx, id, "w1", now)
	require.True(t, ok)
	assert.Equal(t, 1, c.Attempts, "attempts reset by RetryFailed")

	require.NoError(t, repo.CompleteChunk(ctx, id, candledata.ChunkOutcome{SourceUsed: "rest", RowCount: 24}, now))
	got, _ := repo.GetChunk(ctx, id)
	assert.Equal(t, entities.ChunkDone, got.Status)
	assert.Equal(t, 24, got.RowCount)

	// planning the same range again after done re-opens it (ledger no longer covers it)
	re, _ := repo.EnqueueChunks(ctx, ds.ID, []candledata.ChunkInput{{From: day(1), To: day(2), Purpose: entities.PurposeRepair}})
	require.Len(t, re, 1)
	assert.Equal(t, entities.ChunkPending, re[0].Status)
}

func TestSetPausedAndDelete(t *testing.T) {
	repo, ds := setup(t)
	ctx := context.Background()
	require.NoError(t, repo.SetPaused(ctx, ds.ID, true))
	got, _ := repo.GetDataset(ctx, ds.ID)
	assert.True(t, got.Paused)
	require.Error(t, repo.SetPaused(ctx, 999, true))

	_, _ = repo.EnqueueChunks(ctx, ds.ID, []candledata.ChunkInput{{From: day(1), To: day(2)}})
	require.NoError(t, repo.DeleteDataset(ctx, ds.ID))
	_, err := repo.GetDataset(ctx, ds.ID)
	assert.Error(t, err)
	counts, _ := repo.ChunkCounts(ctx, ds.ID)
	assert.Empty(t, counts)
}

func TestHeartbeatStaleAndRunnable(t *testing.T) {
	repo, ds := setup(t)
	ctx := context.Background()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	created, _ := repo.EnqueueChunks(ctx, ds.ID, []candledata.ChunkInput{
		{From: day(1), To: day(2), Purpose: entities.PurposeBackfill, Priority: 10},
		{From: day(5), To: day(6), Purpose: entities.PurposeTail, Priority: 100},
	})
	backfill, tail := created[0].ID, created[1].ID

	run, _ := repo.ListRunnable(ctx, 10)
	require.Len(t, run, 2)
	assert.Equal(t, tail, run[0].ID, "highest priority first")

	_, ok, _ := repo.ClaimChunk(ctx, backfill, "w1", now)
	require.True(t, ok)
	stale, _ := repo.ListStale(ctx, now.Add(time.Minute), 10)
	require.Len(t, stale, 1)
	fresh, _ := repo.ListStale(ctx, now.Add(-time.Minute), 10)
	assert.Empty(t, fresh)

	alive, err := repo.Heartbeat(ctx, backfill, "w1", now.Add(2*time.Minute))
	require.NoError(t, err)
	assert.True(t, alive)
	alive, _ = repo.Heartbeat(ctx, backfill, "someone-else", now)
	assert.False(t, alive, "only the lease owner can renew")
	stale, _ = repo.ListStale(ctx, now.Add(time.Minute), 10)
	assert.Empty(t, stale, "renewed")

	require.NoError(t, repo.SetPaused(ctx, ds.ID, true))
	run, _ = repo.ListRunnable(ctx, 10)
	assert.Empty(t, run, "paused datasets are not runnable")

	counts, _ := repo.AllChunkCounts(ctx)
	assert.Equal(t, int64(1), counts[entities.ChunkRunning])
	assert.Equal(t, int64(1), counts[entities.ChunkPending])
}
