package candledata

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync/atomic"
	"time"

	"go-trade-bot/app/entities"
	repo "go-trade-bot/app/repository/candledata"
)

// Store is the persistence the service needs (satisfied by candledata.Repository).
type Store interface {
	UpsertDataset(ctx context.Context, ds *entities.CandleDataset) error
	DeleteDataset(ctx context.Context, id uint) error
	ChunkCounts(ctx context.Context, datasetID uint) (map[entities.ChunkStatus]int64, error)
	GetDataset(ctx context.Context, id uint) (*entities.CandleDataset, error)
	SetPaused(ctx context.Context, id uint, paused bool) error
	Segments(ctx context.Context, datasetID uint) ([]entities.CandleSegment, error)
	ReplaceSegments(ctx context.Context, datasetID uint, from, to time.Time, segs []entities.CandleSegment) error
	EnqueueChunks(ctx context.Context, datasetID uint, in []repo.ChunkInput) ([]entities.CandleChunk, error)
	GetChunk(ctx context.Context, id uint) (*entities.CandleChunk, error)
	ClaimChunk(ctx context.Context, id uint, owner string, now time.Time) (*entities.CandleChunk, bool, error)
	CompleteChunk(ctx context.Context, id uint, out repo.ChunkOutcome, now time.Time) error
	FailChunk(ctx context.Context, id uint, errMsg string, dead bool, now time.Time) error
	RetryFailed(ctx context.Context, datasetID uint) ([]entities.CandleChunk, error)
	ListChunks(ctx context.Context, datasetID uint, status entities.ChunkStatus, limit int) ([]entities.CandleChunk, error)
	ListDatasets(ctx context.Context) ([]entities.CandleDataset, error)
	Heartbeat(ctx context.Context, id uint, owner string, now time.Time) (bool, error)
	ListStale(ctx context.Context, cutoff time.Time, limit int) ([]entities.CandleChunk, error)
	ListRunnable(ctx context.Context, limit int) ([]entities.CandleChunk, error)
	AllChunkCounts(ctx context.Context) (map[entities.ChunkStatus]int64, error)
}

// Metrics is the narrow metrics sink the service reports to (nil = none).
type Metrics interface {
	Inc(name string, labels map[string]string)
	Set(name string, labels map[string]string, v float64)
	Observe(name string, labels map[string]string, v float64)
}

// ListingResolver gives the first time Binance has data for (symbol, timeframe).
type ListingResolver interface {
	ListingStart(ctx context.Context, symbol, timeframe string) (time.Time, error)
}

// ChunkEnqueuer hands a chunk to the task queue (asynq, queue "candles").
type ChunkEnqueuer interface {
	EnqueueChunk(ctx context.Context, chunkID uint, priority int) error
}

// Service is the reconciler: Reconcile plans what is missing, RunChunk executes
// one planned chunk. Backfill, live top-up, gap repair and downtime recovery
// are all the same operation.
type Service struct {
	Store       Store
	Runner      *Runner
	Listing     ListingResolver
	Enqueuer    ChunkEnqueuer
	Now         func() time.Time
	Owner       string
	MaxAttempts int // chunk-level attempts before a chunk is parked as dead

	// HeartbeatEvery renews a running chunk's lease; a chunk whose lease is
	// older than StaleAfter is reaped by Sweep (its worker died).
	HeartbeatEvery time.Duration
	StaleAfter     time.Duration
	Metrics        Metrics
	// Scanner lets Create adopt candles stored before the ledger existed
	// (nil = skip adoption).
	Scanner OpenTimeScanner
}

func NewService(s Store, r *Runner, l ListingResolver, e ChunkEnqueuer, owner string) *Service {
	return &Service{Store: s, Runner: r, Listing: l, Enqueuer: e, Now: time.Now, Owner: owner, MaxAttempts: 5,
		HeartbeatEvery: 30 * time.Second, StaleAfter: 3 * time.Minute}
}

// Covered merges ledger segments (loaded and known gaps alike) into ranges.
func Covered(segs []entities.CandleSegment) []Range {
	rs := make([]Range, 0, len(segs))
	for _, s := range segs {
		rs = append(rs, Range{s.From, s.To})
	}
	return Merge(rs)
}

// Reconcile computes desired - ledger, plans month-bounded chunks and enqueues
// the new ones. It returns the number of chunks created. Paused datasets plan
// nothing. Safe to call as often as you like.
func (s *Service) Reconcile(ctx context.Context, datasetID uint) (int, error) {
	ds, err := s.Store.GetDataset(ctx, datasetID)
	if err != nil {
		return 0, err
	}
	if ds.Paused {
		return 0, nil
	}
	tf, err := ParseTimeframe(ds.Timeframe)
	if err != nil {
		return 0, err
	}
	listing, err := s.Listing.ListingStart(ctx, ds.Symbol, ds.Timeframe)
	if err != nil {
		return 0, fmt.Errorf("resolve listing start: %w", err)
	}
	segs, err := s.Store.Segments(ctx, datasetID)
	if err != nil {
		return 0, err
	}
	desired := DesiredRange(ds.Start, listing, tf, s.Now())
	covered := Covered(segs)
	var missingDur time.Duration
	for _, m := range Subtract(desired, covered) {
		missingDur += m.To.Sub(m.From)
	}
	lbl := map[string]string{"symbol": ds.Symbol, "timeframe": ds.Timeframe}
	s.set("candle_missing_candles", lbl, float64(missingDur/tf))
	if n := len(covered); n > 0 {
		s.set("candle_sync_lag_seconds", lbl, s.Now().Sub(covered[n-1].To).Seconds())
	}
	var inputs []repo.ChunkInput
	for _, spec := range Plan(desired, covered) {
		if !ds.KeepLive && spec.Purpose == entities.PurposeTail {
			continue
		}
		inputs = append(inputs, repo.ChunkInput{From: spec.Range.From, To: spec.Range.To, Purpose: spec.Purpose, Priority: spec.Priority})
	}
	created, err := s.Store.EnqueueChunks(ctx, datasetID, inputs)
	if err != nil {
		return 0, err
	}
	var errs []error
	for _, c := range created {
		if err := s.Enqueuer.EnqueueChunk(ctx, c.ID, c.Priority); err != nil {
			errs = append(errs, fmt.Errorf("enqueue chunk %d: %w", c.ID, err))
		}
	}
	return len(created), errors.Join(errs...)
}

// RunChunk is the asynq task body. Only the part of the chunk the ledger does
// not already cover is fetched, so a retry never repeats verified work.
// A nil return means "do not retry via asynq" (done, parked, paused or lost
// the claim); a non-nil return asks asynq to retry.
func (s *Service) RunChunk(ctx context.Context, chunkID uint) error {
	chunk, err := s.Store.GetChunk(ctx, chunkID)
	if err != nil {
		return err
	}
	ds, err := s.Store.GetDataset(ctx, chunk.DatasetID)
	if err != nil {
		return err
	}
	if ds.Paused {
		return nil // stays pending; Resume re-enqueues it
	}
	claimed, ok, err := s.Store.ClaimChunk(ctx, chunkID, s.Owner, s.Now())
	if err != nil || !ok {
		return err
	}
	started := s.Now()

	// keep the lease alive; if it is lost (reaped as stale) stop working and
	// leave the chunk to whoever owns it now
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var leaseLost atomic.Bool
	hbDone := make(chan struct{})
	go func() {
		defer close(hbDone)
		t := time.NewTicker(s.HeartbeatEvery)
		defer t.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-t.C:
				alive, err := s.Store.Heartbeat(runCtx, chunkID, s.Owner, s.Now())
				if err == nil && !alive {
					leaseLost.Store(true)
					cancel()
					return
				}
			}
		}
	}()
	err = s.execute(runCtx, ds, claimed)
	cancel()
	<-hbDone
	if leaseLost.Load() {
		log.Printf("candledata: chunk %d lease lost, abandoning", chunkID)
		return nil
	}
	s.observe("candle_chunk_duration_seconds", nil, s.Now().Sub(started).Seconds())
	if err != nil {
		return s.fail(claimed, err)
	}
	return nil
}

func (s *Service) execute(ctx context.Context, ds *entities.CandleDataset, claimed *entities.CandleChunk) error {
	segs, err := s.Store.Segments(ctx, ds.ID)
	if err != nil {
		return err
	}
	var sources []string
	rows := 0
	for _, m := range Subtract(Range{claimed.From, claimed.To}, Covered(segs)) {
		res, runErr := s.Runner.Run(ctx, *ds, m)
		for _, fb := range res.Fallbacks {
			s.inc("candle_source_fallbacks_total", map[string]string{"source": fb.From})
		}
		// persist whatever was verified, even when the range is incomplete
		if len(res.Segments) > 0 {
			if err := s.Store.ReplaceSegments(ctx, ds.ID, m.From, m.To, res.Segments); err != nil {
				return err
			}
		}
		rows += res.Rows
		sources = appendUnique(sources, res.Sources...)
		if runErr != nil {
			return runErr
		}
	}
	s.inc("candle_chunk_runs_total", map[string]string{"result": "ok"})
	return s.Store.CompleteChunk(ctx, claimed.ID, repo.ChunkOutcome{SourceUsed: strings.Join(sources, ","), RowCount: rows}, s.Now())
}

// fail parks the chunk after MaxAttempts, otherwise leaves it retryable and
// asks asynq to run it again.
func (s *Service) fail(c *entities.CandleChunk, cause error) error {
	// the caller's ctx may be cancelled (worker shutdown), so don't use it here
	bg, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	dead := c.Attempts >= s.MaxAttempts
	result := "failed"
	if dead {
		result = "dead"
	}
	s.inc("candle_chunk_runs_total", map[string]string{"result": result})
	if err := s.Store.FailChunk(bg, c.ID, cause.Error(), dead, s.Now()); err != nil {
		log.Printf("candledata: marking chunk %d failed: %v", c.ID, err)
	}
	if dead {
		log.Printf("candledata: chunk %d parked as dead after %d attempts: %v", c.ID, c.Attempts, cause)
		return nil
	}
	return cause
}

// Pause stops new work: pending chunks are skipped when picked up, running
// ones finish. Reconcile plans nothing while paused.
func (s *Service) Pause(ctx context.Context, datasetID uint) error {
	return s.Store.SetPaused(ctx, datasetID, true)
}

// Resume clears the pause, re-plans and re-enqueues chunks left pending.
func (s *Service) Resume(ctx context.Context, datasetID uint) error {
	if err := s.Store.SetPaused(ctx, datasetID, false); err != nil {
		return err
	}
	if _, err := s.Reconcile(ctx, datasetID); err != nil {
		return err
	}
	return s.enqueueAll(ctx, datasetID, entities.ChunkPending)
}

// RetryFailed puts failed and dead chunks back in the queue.
func (s *Service) RetryFailed(ctx context.Context, datasetID uint) (int, error) {
	chunks, err := s.Store.RetryFailed(ctx, datasetID)
	if err != nil {
		return 0, err
	}
	var errs []error
	for _, c := range chunks {
		if err := s.Enqueuer.EnqueueChunk(ctx, c.ID, c.Priority); err != nil {
			errs = append(errs, err)
		}
	}
	return len(chunks), errors.Join(errs...)
}

func (s *Service) enqueueAll(ctx context.Context, datasetID uint, st entities.ChunkStatus) error {
	chunks, err := s.Store.ListChunks(ctx, datasetID, st, 500)
	if err != nil {
		return err
	}
	var errs []error
	for _, c := range chunks {
		if err := s.Enqueuer.EnqueueChunk(ctx, c.ID, c.Priority); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func appendUnique(dst []string, add ...string) []string {
	for _, a := range add {
		found := false
		for _, d := range dst {
			if d == a {
				found = true
				break
			}
		}
		if !found {
			dst = append(dst, a)
		}
	}
	return dst
}

// Sweep is the periodic self-healing pass (every minute, one asynq cron entry):
//  1. reap running chunks whose worker stopped heartbeating;
//  2. reconcile every non-paused dataset (live top-up, hole repair, retries after
//     downtime - this is what makes the pipeline continuous);
//  3. re-enqueue pending chunks (the enqueuer's task id makes duplicates harmless),
//     which also recovers chunks whose enqueue failed or whose queue was lost;
//  4. publish gauges.
func (s *Service) Sweep(ctx context.Context) error {
	var errs []error
	if err := s.reap(ctx); err != nil {
		errs = append(errs, err)
	}
	datasets, err := s.Store.ListDatasets(ctx)
	if err != nil {
		return errors.Join(append(errs, err)...)
	}
	for _, ds := range datasets {
		if ds.Paused {
			continue
		}
		if _, err := s.Reconcile(ctx, ds.ID); err != nil {
			errs = append(errs, fmt.Errorf("reconcile %s/%s: %w", ds.Symbol, ds.Timeframe, err))
		}
	}
	runnable, err := s.Store.ListRunnable(ctx, 500)
	if err != nil {
		errs = append(errs, err)
	}
	for _, c := range runnable {
		if err := s.Enqueuer.EnqueueChunk(ctx, c.ID, c.Priority); err != nil {
			errs = append(errs, fmt.Errorf("enqueue chunk %d: %w", c.ID, err))
		}
	}
	if counts, err := s.Store.AllChunkCounts(ctx); err == nil {
		for _, st := range []entities.ChunkStatus{entities.ChunkPending, entities.ChunkRunning, entities.ChunkDone, entities.ChunkFailed, entities.ChunkDead} {
			s.set("candle_chunks", map[string]string{"status": string(st)}, float64(counts[st]))
		}
	}
	return errors.Join(errs...)
}

func (s *Service) reap(ctx context.Context) error {
	stale, err := s.Store.ListStale(ctx, s.Now().Add(-s.StaleAfter), 100)
	if err != nil {
		return err
	}
	var errs []error
	for _, c := range stale {
		dead := c.Attempts >= s.MaxAttempts
		if err := s.Store.FailChunk(ctx, c.ID, "worker lost: heartbeat expired", dead, s.Now()); err != nil {
			errs = append(errs, err)
			continue
		}
		log.Printf("candledata: reaped stale chunk %d (attempts %d, dead=%v)", c.ID, c.Attempts, dead)
		if !dead {
			if err := s.Enqueuer.EnqueueChunk(ctx, c.ID, c.Priority); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

func (s *Service) inc(name string, l map[string]string) {
	if s.Metrics != nil {
		s.Metrics.Inc(name, l)
	}
}
func (s *Service) set(name string, l map[string]string, v float64) {
	if s.Metrics != nil {
		s.Metrics.Set(name, l, v)
	}
}
func (s *Service) observe(name string, l map[string]string, v float64) {
	if s.Metrics != nil {
		s.Metrics.Observe(name, l, v)
	}
}
