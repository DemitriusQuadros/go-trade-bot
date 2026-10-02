package candledata

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"go-trade-bot/app/entities"
	repo "go-trade-bot/app/repository/candledata"
	"go-trade-bot/internal/candlesource"
)

type fixedListing time.Time

func (f fixedListing) ListingStart(context.Context, string, string) (time.Time, error) {
	return time.Time(f), nil
}

type recEnqueuer struct{ ids []uint }

func (r *recEnqueuer) EnqueueChunk(_ context.Context, id uint, _ int) error {
	r.ids = append(r.ids, id)
	return nil
}

type env struct {
	svc  *Service
	st   repo.Repository
	enq  *recEnqueuer
	w    *memWriter
	ds   *entities.CandleDataset
	rest *fakeSource
	arch *fakeSource
	now  *time.Time
}

var envSeq atomic.Int64

func newEnv(t *testing.T, listing, now time.Time) *env {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s-%d?mode=memory&cache=shared", t.Name(), envSeq.Add(1))), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&entities.CandleDataset{}, &entities.CandleSegment{}, &entities.CandleChunk{}))
	st := repo.NewRepository(db)
	ds := &entities.CandleDataset{Symbol: "BTCUSDT", Timeframe: "1h", KeepLive: true}
	require.NoError(t, st.UpsertDataset(context.Background(), ds))

	w := newMem()
	arch := &fakeSource{name: "archive_monthly"}
	rest := &fakeSource{name: "rest", authoritative: true}
	runner, _ := newRunner(w, arch, rest)
	cur := now
	runner.Now = func() time.Time { return cur }
	enq := &recEnqueuer{}
	svc := NewService(st, runner, fixedListing(listing), enq, "test")
	svc.Now = func() time.Time { return cur }
	return &env{svc: svc, st: st, enq: enq, w: w, ds: ds, rest: rest, arch: arch, now: &cur}
}

func (e *env) runAll(t *testing.T) {
	for len(e.enq.ids) > 0 {
		id := e.enq.ids[0]
		e.enq.ids = e.enq.ids[1:]
		require.NoError(t, e.svc.RunChunk(context.Background(), id))
	}
}

func TestService_BackfillConvergesAndIsIdempotent(t *testing.T) {
	e := newEnv(t, d(2024, 1, 1, 0), d(2024, 3, 15, 6))
	ctx := context.Background()

	n, err := e.svc.Reconcile(ctx, e.ds.ID)
	require.NoError(t, err)
	assert.Equal(t, 3, n) // Jan, Feb, Mar(partial)
	e.runAll(t)

	assert.Equal(t, 74*24+6, len(e.w.rows)) // Jan 1 .. Mar 15 05:00 inclusive
	again, err := e.svc.Reconcile(ctx, e.ds.ID)
	require.NoError(t, err)
	assert.Equal(t, 0, again, "nothing left to do")
	segs, _ := e.st.Segments(ctx, e.ds.ID)
	assert.Equal(t, []Range{{d(2024, 1, 1, 0), d(2024, 3, 15, 6)}}, Covered(segs))
}

func TestService_LiveTopUpAfterCandleClose(t *testing.T) {
	e := newEnv(t, d(2024, 1, 1, 0), d(2024, 1, 10, 0))
	ctx := context.Background()
	_, _ = e.svc.Reconcile(ctx, e.ds.ID)
	e.runAll(t)
	calls := e.rest.calls + e.arch.calls

	*e.now = d(2024, 1, 10, 3) // 3 more candles closed
	n, err := e.svc.Reconcile(ctx, e.ds.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	e.runAll(t)

	assert.Equal(t, 9*24+3, len(e.w.rows))
	assert.Equal(t, 1, e.rest.calls+e.arch.calls-calls, "only the new candles are fetched, via one source call")
}

func TestService_DowntimeOfAnyLengthSelfHeals(t *testing.T) {
	e := newEnv(t, d(2024, 1, 1, 0), d(2024, 1, 2, 0))
	ctx := context.Background()
	_, _ = e.svc.Reconcile(ctx, e.ds.ID)
	e.runAll(t)

	*e.now = d(2024, 4, 1, 0) // the worker was down for 3 months
	n, err := e.svc.Reconcile(ctx, e.ds.ID)
	require.NoError(t, err)
	assert.Equal(t, 3, n)
	e.runAll(t)
	assert.Equal(t, 91*24, len(e.w.rows))
}

func TestService_FailureRetriesOnlyTheMissingPart_ThenParksAsDead(t *testing.T) {
	e := newEnv(t, d(2024, 1, 1, 0), d(2024, 1, 2, 0))
	ctx := context.Background()
	e.arch.cannotServe = true
	hole := func(t time.Time) bool { return !t.Before(d(2024, 1, 1, 12)) } // rest has only the first half
	e.rest.skip = hole
	e.rest.authoritative = false // so the missing half is NOT declared a gap
	_, _ = e.svc.Reconcile(ctx, e.ds.ID)
	id := e.enq.ids[0]

	err := e.svc.RunChunk(ctx, id)
	require.Error(t, err, "incomplete -> asynq retry")
	chunk, _ := e.st.GetChunk(ctx, id)
	assert.Equal(t, entities.ChunkFailed, chunk.Status)
	segs, _ := e.st.Segments(ctx, e.ds.ID)
	assert.Equal(t, []Range{{d(2024, 1, 1, 0), d(2024, 1, 1, 12)}}, Covered(segs), "verified half is kept")

	// source recovers; the retry must fetch only the missing half
	e.rest.skip, e.rest.authoritative = nil, true
	before := e.rest.calls
	require.NoError(t, e.svc.RunChunk(ctx, id))
	assert.Equal(t, 1, e.rest.calls-before)
	chunk, _ = e.st.GetChunk(ctx, id)
	assert.Equal(t, entities.ChunkDone, chunk.Status)
	assert.Equal(t, 24, len(e.w.rows))

	// a permanently failing chunk is parked after MaxAttempts
	e2 := newEnv(t, d(2024, 1, 1, 0), d(2024, 1, 2, 0))
	e2.svc.MaxAttempts = 2
	e2.arch.errs = repeatErr(candlesource.ErrNotFound, 50)
	e2.rest.errs = repeatErr(errors.New("boom"), 50)
	_, _ = e2.svc.Reconcile(ctx, e2.ds.ID)
	id2 := e2.enq.ids[0]
	require.Error(t, e2.svc.RunChunk(ctx, id2))
	require.NoError(t, e2.svc.RunChunk(ctx, id2), "second attempt hits MaxAttempts: parked, no more asynq retries")
	c2, _ := e2.st.GetChunk(ctx, id2)
	assert.Equal(t, entities.ChunkDead, c2.Status)

	// RetryFailed brings it back
	e2.arch.errs, e2.rest.errs = nil, nil
	nretry, err := e2.svc.RetryFailed(ctx, e2.ds.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, nretry)
	require.NoError(t, e2.svc.RunChunk(ctx, id2))
	c2, _ = e2.st.GetChunk(ctx, id2)
	assert.Equal(t, entities.ChunkDone, c2.Status)
}

func TestService_PauseSkipsWork_ResumeContinues(t *testing.T) {
	e := newEnv(t, d(2024, 1, 1, 0), d(2024, 2, 10, 0))
	ctx := context.Background()
	_, _ = e.svc.Reconcile(ctx, e.ds.ID)
	require.NoError(t, e.svc.Pause(ctx, e.ds.ID))

	e.runAll(t) // picked up while paused: nothing happens
	assert.Empty(t, e.w.rows)
	n, _ := e.svc.Reconcile(ctx, e.ds.ID)
	assert.Equal(t, 0, n, "paused datasets plan nothing")

	require.NoError(t, e.svc.Resume(ctx, e.ds.ID))
	e.runAll(t)
	assert.Equal(t, 40*24, len(e.w.rows))
}

func TestService_NonLiveDatasetSkipsTail(t *testing.T) {
	e := newEnv(t, d(2024, 1, 1, 0), d(2024, 1, 10, 0))
	ctx := context.Background()
	_, _ = e.svc.Reconcile(ctx, e.ds.ID)
	e.runAll(t)
	e.ds.KeepLive = false
	require.NoError(t, e.st.UpsertDataset(ctx, e.ds))

	*e.now = d(2024, 1, 10, 3)
	n, _ := e.svc.Reconcile(ctx, e.ds.ID)
	assert.Equal(t, 0, n)
}

func TestService_UnsupportedTimeframeIsAnError(t *testing.T) {
	e := newEnv(t, d(2024, 1, 1, 0), d(2024, 1, 2, 0))
	ctx := context.Background()
	w := &entities.CandleDataset{Symbol: "ETHUSDT", Timeframe: "1w"}
	require.NoError(t, e.st.UpsertDataset(ctx, w))
	_, err := e.svc.Reconcile(ctx, w.ID)
	assert.Error(t, err)
}

type fakeScanner []time.Time

func (f fakeScanner) ScanOpenTimes(_ context.Context, _, _ string, fn func(time.Time) error) error {
	for _, t := range f {
		if err := fn(t); err != nil {
			return err
		}
	}
	return nil
}

func TestService_AdoptTurnsStoredRunsIntoSegments_AndReconcileFillsTheRest(t *testing.T) {
	e := newEnv(t, d(2024, 1, 1, 0), d(2024, 1, 3, 0))
	ctx := context.Background()
	var stored fakeScanner
	for h := 0; h < 10; h++ { // 00:00-09:00
		stored = append(stored, d(2024, 1, 1, h))
	}
	for h := 14; h < 24; h++ { // 14:00-23:00, hole 10:00-13:00
		stored = append(stored, d(2024, 1, 1, h))
	}
	stored = append(stored, d(2024, 1, 2, 23)) // last closed candle
	stored = append(stored, d(2024, 1, 3, 0))  // the still-open candle: must not be adopted

	n, err := e.svc.Adopt(ctx, stored, e.ds.ID)
	require.NoError(t, err)
	assert.Equal(t, 3, n)
	segs, _ := e.st.Segments(ctx, e.ds.ID)
	assert.Equal(t, []Range{
		{d(2024, 1, 1, 0), d(2024, 1, 1, 10)},
		{d(2024, 1, 1, 14), d(2024, 1, 2, 0)},
		{d(2024, 1, 2, 23), d(2024, 1, 3, 0)},
	}, Covered(segs))

	// the planner now sees exactly the holes
	created, err := e.svc.Reconcile(ctx, e.ds.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, created) // one chunk per hole
	e.runAll(t)
	assert.Equal(t, 4+23, len(e.w.rows), "only the holes are fetched: 10:00-13:00 on Jan 1 and 00:00-22:00 on Jan 2")
	segs, _ = e.st.Segments(ctx, e.ds.ID)
	assert.Equal(t, []Range{{d(2024, 1, 1, 0), d(2024, 1, 3, 0)}}, Covered(segs))

	again, err := e.svc.Adopt(ctx, stored, e.ds.ID)
	require.NoError(t, err)
	assert.Equal(t, 0, again, "adoption is a no-op once a ledger exists")
}

// ---- step 2: leases, sweeper, metrics ----

type hbStore struct {
	Store
	beats int
	alive bool
}

func (h *hbStore) Heartbeat(ctx context.Context, id uint, owner string, now time.Time) (bool, error) {
	h.beats++
	return h.alive, nil
}

type recMetrics struct {
	counters map[string]int
	gauges   map[string]float64
}

func newRecMetrics() *recMetrics {
	return &recMetrics{counters: map[string]int{}, gauges: map[string]float64{}}
}
func (m *recMetrics) Inc(n string, l map[string]string) {
	m.counters[n+fmt.Sprint(l)]++
}
func (m *recMetrics) Set(n string, l map[string]string, v float64) { m.gauges[n+fmt.Sprint(l)] = v }
func (m *recMetrics) Observe(string, map[string]string, float64)   {}

func TestService_HeartbeatKeepsLeaseWhileRunning(t *testing.T) {
	e := newEnv(t, d(2024, 1, 1, 0), d(2024, 1, 2, 0))
	hb := &hbStore{Store: e.st, alive: true}
	e.svc.Store = hb
	e.svc.HeartbeatEvery = 5 * time.Millisecond
	e.rest.delay = 60 * time.Millisecond
	e.arch.cannotServe = true
	_, _ = e.svc.Reconcile(context.Background(), e.ds.ID)

	require.NoError(t, e.svc.RunChunk(context.Background(), e.enq.ids[0]))

	assert.GreaterOrEqual(t, hb.beats, 3)
	assert.Equal(t, 24, len(e.w.rows))
}

func TestService_LostLeaseAbortsAndDoesNotTouchTheChunk(t *testing.T) {
	e := newEnv(t, d(2024, 1, 1, 0), d(2024, 1, 2, 0))
	hb := &hbStore{Store: e.st, alive: false} // the reaper took our lease
	e.svc.Store = hb
	e.svc.HeartbeatEvery = 5 * time.Millisecond
	e.rest.delay = 500 * time.Millisecond
	e.arch.cannotServe = true
	_, _ = e.svc.Reconcile(context.Background(), e.ds.ID)
	id := e.enq.ids[0]

	start := time.Now()
	require.NoError(t, e.svc.RunChunk(context.Background(), id))

	assert.Less(t, time.Since(start), 300*time.Millisecond, "run was cancelled, not waited out")
	c, _ := e.st.GetChunk(context.Background(), id)
	assert.Equal(t, entities.ChunkRunning, c.Status, "left for its new owner; not marked failed")
}

func TestService_SweepReapsStaleChunkAndRequeuesIt(t *testing.T) {
	e := newEnv(t, d(2024, 1, 1, 0), d(2024, 1, 2, 0))
	ctx := context.Background()
	m := newRecMetrics()
	e.svc.Metrics = m
	_, _ = e.svc.Reconcile(ctx, e.ds.ID)
	id := e.enq.ids[0]
	e.enq.ids = nil
	// a worker claimed it 10 minutes ago and died
	_, ok, _ := e.st.ClaimChunk(ctx, id, "dead-worker", d(2024, 1, 2, 0).Add(-10*time.Minute))
	require.True(t, ok)

	require.NoError(t, e.svc.Sweep(ctx))

	c, _ := e.st.GetChunk(ctx, id)
	assert.Equal(t, entities.ChunkFailed, c.Status)
	assert.Contains(t, c.LastError, "heartbeat expired")
	assert.Contains(t, e.enq.ids, id, "re-enqueued")
	e.runAll(t)
	c, _ = e.st.GetChunk(ctx, id)
	assert.Equal(t, entities.ChunkDone, c.Status)
	assert.Equal(t, float64(1), m.gauges["candle_chunks"+fmt.Sprint(map[string]string{"status": "failed"})])
}

func TestService_SweepParksChunkThatKeepsDying(t *testing.T) {
	e := newEnv(t, d(2024, 1, 1, 0), d(2024, 1, 2, 0))
	ctx := context.Background()
	e.svc.MaxAttempts = 1
	_, _ = e.svc.Reconcile(ctx, e.ds.ID)
	id := e.enq.ids[0]
	_, _, _ = e.st.ClaimChunk(ctx, id, "w", d(2024, 1, 2, 0).Add(-time.Hour))

	require.NoError(t, e.svc.Sweep(ctx))

	c, _ := e.st.GetChunk(ctx, id)
	assert.Equal(t, entities.ChunkDead, c.Status)
}

func TestService_SweepIsTheContinuousLoop_LiveTailAndRecoveredEnqueues(t *testing.T) {
	e := newEnv(t, d(2024, 1, 1, 0), d(2024, 1, 2, 0))
	ctx := context.Background()
	m := newRecMetrics()
	e.svc.Metrics = m

	require.NoError(t, e.svc.Sweep(ctx)) // first pass plans the backfill
	require.NotEmpty(t, e.enq.ids)
	e.enq.ids = nil // pretend the queue lost every task (redis flush)

	require.NoError(t, e.svc.Sweep(ctx)) // pending chunks are re-enqueued
	require.NotEmpty(t, e.enq.ids)
	e.runAll(t)

	*e.now = d(2024, 1, 2, 2) // two candles close
	require.NoError(t, e.svc.Sweep(ctx))
	e.runAll(t)
	require.NoError(t, e.svc.Sweep(ctx)) // the next pass observes the converged state

	assert.Equal(t, 26, len(e.w.rows))
	lbl := fmt.Sprint(map[string]string{"symbol": "BTCUSDT", "timeframe": "1h"})
	assert.Equal(t, float64(0), m.gauges["candle_missing_candles"+lbl])
	assert.Equal(t, float64(0), m.gauges["candle_sync_lag_seconds"+lbl])
	assert.Greater(t, m.counters["candle_chunk_runs_total"+fmt.Sprint(map[string]string{"result": "ok"})], 0)
}

func TestService_SweepSkipsPausedDatasets(t *testing.T) {
	e := newEnv(t, d(2024, 1, 1, 0), d(2024, 1, 2, 0))
	ctx := context.Background()
	require.NoError(t, e.svc.Pause(ctx, e.ds.ID))
	require.NoError(t, e.svc.Sweep(ctx))
	assert.Empty(t, e.enq.ids)
}

// ---- management ----

func TestService_CreateValidatesAdoptsAndStarts(t *testing.T) {
	e := newEnv(t, d(2024, 1, 1, 0), d(2024, 1, 3, 0))
	ctx := context.Background()
	e.svc.Scanner = fakeScanner{d(2024, 1, 1, 0), d(2024, 1, 1, 1)}

	_, err := e.svc.Create(ctx, "btc/usdt", "1h", nil, true)
	var inv ErrInvalid
	require.ErrorAs(t, err, &inv)
	_, err = e.svc.Create(ctx, "BTCUSDT", "1w", nil, true)
	require.ErrorAs(t, err, &inv)
	future := d(2030, 1, 1, 0)
	_, err = e.svc.Create(ctx, "BTCUSDT", "1h", &future, true)
	require.ErrorAs(t, err, &inv)

	ds, err := e.svc.Create(ctx, "ETHUSDT", "1h", nil, true)
	require.NoError(t, err)
	segs, _ := e.st.Segments(ctx, ds.ID)
	require.Len(t, segs, 1, "existing candles adopted")
	assert.NotEmpty(t, e.enq.ids, "reconcile enqueued the rest")
}

func TestService_StatusStatesAndCoverage(t *testing.T) {
	e := newEnv(t, d(2024, 1, 1, 0), d(2024, 1, 2, 0))
	ctx := context.Background()

	st, err := e.svc.Status(ctx, e.ds.ID)
	require.NoError(t, err)
	assert.Equal(t, StateConverging, st.State, "24 candles missing")
	assert.Equal(t, int64(24), st.MissingCandles)

	_, _ = e.svc.Reconcile(ctx, e.ds.ID)
	e.runAll(t)
	st, _ = e.svc.Status(ctx, e.ds.ID)
	assert.Equal(t, StateLive, st.State)
	assert.Equal(t, []Range{{d(2024, 1, 1, 0), d(2024, 1, 2, 0)}}, st.Loaded)
	assert.Empty(t, st.Missing)

	require.NoError(t, e.svc.Pause(ctx, e.ds.ID))
	st, _ = e.svc.Status(ctx, e.ds.ID)
	assert.Equal(t, StatePaused, st.State)
	require.NoError(t, e.svc.Resume(ctx, e.ds.ID))

	// a parked chunk makes the dataset failed and exposes its error
	*e.now = d(2024, 1, 3, 0)
	e.arch.cannotServe = true
	e.rest.errs = repeatErr(errors.New("binance down"), 50)
	e.svc.MaxAttempts = 1
	_, _ = e.svc.Reconcile(ctx, e.ds.ID)
	e.runAll(t)
	st, _ = e.svc.Status(ctx, e.ds.ID)
	assert.Equal(t, StateFailed, st.State)
	assert.Contains(t, st.LastError, "binance down")
	assert.Equal(t, int64(1), st.Chunks[entities.ChunkDead])
}

func TestService_DeleteKeepsStoredCandles(t *testing.T) {
	e := newEnv(t, d(2024, 1, 1, 0), d(2024, 1, 2, 0))
	ctx := context.Background()
	_, _ = e.svc.Reconcile(ctx, e.ds.ID)
	e.runAll(t)

	require.NoError(t, e.svc.Delete(ctx, e.ds.ID))

	_, err := e.st.GetDataset(ctx, e.ds.ID)
	assert.Error(t, err)
	assert.Len(t, e.w.rows, 24)
}
