package candledata

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"strings"
	"time"

	"go-trade-bot/app/entities"
	"go-trade-bot/internal/candlesource"
)

// CandleWriter is satisfied by candle.Repository.
type CandleWriter interface {
	Upsert(ctx context.Context, candles []entities.Candle) error
}

// ErrIncomplete: every source in the chain was tried and some candles are still
// missing (and not proven to be an exchange gap). The runner still returns the
// segments it did verify so a retry only refetches the rest.
var ErrIncomplete = errors.New("candledata: chunk incomplete after all sources")

// Fallback records one hop down the source chain and why.
type Fallback struct {
	From   string
	Reason string
}

// Result is what running one chunk produced.
type Result struct {
	Segments  []entities.CandleSegment
	Rows      int
	Rejected  int
	Sources   []string // sources that contributed rows, in chain order
	Missing   []Range
	Fallbacks []Fallback
}

// Runner executes one chunk: it walks the source chain, validates and writes in
// bounded batches, and derives the coverage segments from what was actually
// stored. It never talks to the ledger itself - the caller commits Result.
type Runner struct {
	Sources     []candlesource.Source
	Writer      CandleWriter
	Now         func() time.Time
	BatchSize   int
	MaxAttempts int // per source per range, for retryable errors
	BackoffBase time.Duration
	Sleep       func(ctx context.Context, d time.Duration) error
}

func NewRunner(sources []candlesource.Source, w CandleWriter) *Runner {
	return &Runner{
		Sources:     sources,
		Writer:      w,
		Now:         time.Now,
		BatchSize:   5000,
		MaxAttempts: 4,
		BackoffBase: 2 * time.Second,
		Sleep: func(ctx context.Context, d time.Duration) error {
			t := time.NewTimer(d)
			defer t.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-t.C:
				return nil
			}
		},
	}
}

const (
	stMissing  = 0
	stGap      = 1
	stLoadedAt = 2 // stLoadedAt + sourceIndex
)

// Run loads `chunk` for the dataset. On ErrIncomplete the Result is still valid.
func (r *Runner) Run(ctx context.Context, ds entities.CandleDataset, chunk Range) (Result, error) {
	tf, err := ParseTimeframe(ds.Timeframe)
	if err != nil {
		return Result{}, err
	}
	n := int(chunk.To.Sub(chunk.From) / tf)
	if n <= 0 {
		return Result{}, fmt.Errorf("candledata: empty chunk %s", chunk)
	}
	state := make([]uint8, n) // per expected candle: missing / known gap / loaded-by-source
	refs := make(map[int]string)
	var res Result
	contributed := map[int]bool{}

	for si, src := range r.Sources {
		missing := runs(state, chunk.From, tf, func(s uint8) bool { return s == stMissing })
		if len(missing) == 0 {
			break
		}
		for _, m := range missing {
			if !src.CanServe(ds.Symbol, ds.Timeframe, m.From, m.To, r.Now()) {
				continue
			}
			ref, ferr := r.fetchWithRetry(ctx, src, ds, m, func(batch []entities.Candle) (int, error) {
				return r.store(ctx, batch, chunk.From, tf, m, state, uint8(stLoadedAt+si), &res)
			})
			if ctx.Err() != nil {
				return res, ctx.Err()
			}
			if ferr != nil {
				res.Fallbacks = append(res.Fallbacks, Fallback{From: src.Name(), Reason: ferr.Error()})
				log.Printf("candledata: %s/%s %s: source %s failed, falling back: %v", ds.Symbol, ds.Timeframe, m, src.Name(), ferr)
			} else {
				refs[si] = ref
				if src.Authoritative() {
					// a successful authoritative answer proves what it did not return does not exist
					markRange(state, chunk.From, tf, m, stMissing, stGap)
				}
			}
		}
	}

	// derive segments + stats from the final state
	now := r.Now()
	i := 0
	for i < n {
		j := i
		for j < n && state[j] == state[i] {
			j++
		}
		if state[i] != stMissing {
			seg := entities.CandleSegment{
				From:     chunk.From.Add(time.Duration(i) * tf),
				To:       chunk.From.Add(time.Duration(j) * tf),
				LoadedAt: now,
			}
			if state[i] == stGap {
				seg.State = entities.SegmentKnownGap
				seg.Source = "exchange"
			} else {
				si := int(state[i] - stLoadedAt)
				seg.State = entities.SegmentLoaded
				seg.Source = r.Sources[si].Name()
				seg.SourceRef = refs[si]
				seg.RowCount = j - i
				res.Rows += j - i
				contributed[si] = true
			}
			res.Segments = append(res.Segments, seg)
		}
		i = j
	}
	for si, src := range r.Sources {
		if contributed[si] {
			res.Sources = append(res.Sources, src.Name())
		}
	}
	res.Missing = runs(state, chunk.From, tf, func(s uint8) bool { return s == stMissing })
	if len(res.Missing) > 0 {
		return res, fmt.Errorf("%w: %d range(s) missing, first %s%s", ErrIncomplete, len(res.Missing), res.Missing[0], describeFallbacks(res.Fallbacks))
	}
	return res, nil
}

func describeFallbacks(fb []Fallback) string {
	if len(fb) == 0 {
		return ""
	}
	parts := make([]string, 0, len(fb))
	for _, f := range fb {
		parts = append(parts, f.From+": "+f.Reason)
	}
	return " (" + strings.Join(parts, "; ") + ")"
}

// fetchWithRetry retries only retryable errors, honouring Retry-After, with
// exponential backoff otherwise. Anything else is returned for the chain to
// handle by falling through.
func (r *Runner) fetchWithRetry(ctx context.Context, src candlesource.Source, ds entities.CandleDataset, m Range, write func([]entities.Candle) (int, error)) (string, error) {
	var lastErr error
	for attempt := 0; attempt < r.MaxAttempts; attempt++ {
		ref, err := src.Fetch(ctx, ds.Symbol, ds.Timeframe, m.From, m.To, func(b []entities.Candle) error {
			_, werr := write(b)
			return werr
		})
		if err == nil {
			return ref, nil
		}
		lastErr = err
		re, ok := candlesource.Retryable(err)
		if !ok {
			return "", err
		}
		wait := re.After
		if wait <= 0 {
			wait = r.BackoffBase * time.Duration(1<<attempt)
		}
		if serr := r.Sleep(ctx, wait); serr != nil {
			return "", serr
		}
	}
	return "", fmt.Errorf("giving up after %d attempts: %w", r.MaxAttempts, lastErr)
}

// store validates a batch, writes the valid candles in bounded sub-batches and
// marks them in state. Invalid or out-of-range candles are dropped (they stay
// "missing", so the next source in the chain gets to supply them).
func (r *Runner) store(ctx context.Context, batch []entities.Candle, base time.Time, tf time.Duration, within Range, state []uint8, mark uint8, res *Result) (int, error) {
	valid := make([]entities.Candle, 0, len(batch))
	idxs := make([]int, 0, len(batch))
	for _, c := range batch {
		t := c.OpenTime.UTC()
		if t.Before(within.From) || !t.Before(within.To) || t.Sub(base)%tf != 0 || !validOHLCV(c) {
			res.Rejected++
			continue
		}
		valid = append(valid, c)
		idxs = append(idxs, int(t.Sub(base)/tf))
	}
	for i := 0; i < len(valid); i += r.BatchSize {
		end := i + r.BatchSize
		if end > len(valid) {
			end = len(valid)
		}
		if err := r.Writer.Upsert(ctx, valid[i:end]); err != nil {
			return 0, fmt.Errorf("write: %w", err)
		}
		for _, ix := range idxs[i:end] {
			state[ix] = mark
		}
	}
	return len(valid), nil
}

func validOHLCV(c entities.Candle) bool {
	for _, v := range []float64{c.Open, c.High, c.Low, c.Close, c.Volume} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return c.Low > 0 && c.High >= c.Low && c.Open >= c.Low && c.Open <= c.High &&
		c.Close >= c.Low && c.Close <= c.High && c.Volume >= 0
}

// runs returns maximal ranges of consecutive slots whose state satisfies pred.
func runs(state []uint8, base time.Time, tf time.Duration, pred func(uint8) bool) []Range {
	var out []Range
	i := 0
	for i < len(state) {
		if !pred(state[i]) {
			i++
			continue
		}
		j := i
		for j < len(state) && pred(state[j]) {
			j++
		}
		out = append(out, Range{base.Add(time.Duration(i) * tf), base.Add(time.Duration(j) * tf)})
		i = j
	}
	return out
}

// markRange sets slots in `within` that are currently `from` to `to`.
func markRange(state []uint8, base time.Time, tf time.Duration, within Range, from, to uint8) {
	lo := int(within.From.Sub(base) / tf)
	hi := int(within.To.Sub(base) / tf)
	for i := lo; i < hi && i < len(state); i++ {
		if state[i] == from {
			state[i] = to
		}
	}
}
