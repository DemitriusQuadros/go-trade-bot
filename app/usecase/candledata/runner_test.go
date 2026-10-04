package candledata

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-trade-bot/app/entities"
	"go-trade-bot/internal/candlesource"
)

// fakeSource serves candles from a generator, with injectable behaviour.
type fakeSource struct {
	name          string
	authoritative bool
	cannotServe   bool
	skip          func(t time.Time) bool // candles it does not have
	bad           func(t time.Time) bool // candles it returns corrupted
	errs          []error                // returned in order before succeeding
	calls         int
	batch         int
	delay         time.Duration
}

func (f *fakeSource) Name() string        { return f.name }
func (f *fakeSource) Authoritative() bool { return f.authoritative }
func (f *fakeSource) CanServe(_, _ string, _, _, _ time.Time) bool {
	return !f.cannotServe
}
func (f *fakeSource) Fetch(ctx context.Context, sym, tf string, from, to time.Time, emit func([]entities.Candle) error) (string, error) {
	f.calls++
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	if len(f.errs) > 0 {
		e := f.errs[0]
		f.errs = f.errs[1:]
		return "", e
	}
	dur, _ := ParseTimeframe(tf)
	var batch []entities.Candle
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		err := emit(batch)
		batch = nil
		return err
	}
	for t := from; t.Before(to); t = t.Add(dur) {
		if f.skip != nil && f.skip(t) {
			continue
		}
		c := entities.Candle{Symbol: sym, Timeframe: tf, OpenTime: t, Open: 10, High: 12, Low: 9, Close: 11, Volume: 1}
		if f.bad != nil && f.bad(t) {
			c.High, c.Low = 5, 20
		}
		batch = append(batch, c)
		if f.batch > 0 && len(batch) >= f.batch {
			if err := flush(); err != nil {
				return "", err
			}
		}
	}
	return "ref-" + f.name, flush()
}

type memWriter struct {
	rows     map[time.Time]entities.Candle
	maxBatch int
}

func newMem() *memWriter { return &memWriter{rows: map[time.Time]entities.Candle{}} }
func (m *memWriter) Upsert(_ context.Context, cs []entities.Candle) error {
	if len(cs) > m.maxBatch {
		m.maxBatch = len(cs)
	}
	for _, c := range cs {
		m.rows[c.OpenTime] = c
	}
	return nil
}

func newRunner(w CandleWriter, srcs ...candlesource.Source) (*Runner, *[]time.Duration) {
	r := NewRunner(srcs, w)
	r.Now = func() time.Time { return d(2026, 10, 1, 0) }
	var sleeps []time.Duration
	r.Sleep = func(_ context.Context, dur time.Duration) error { sleeps = append(sleeps, dur); return nil }
	return r, &sleeps
}

var ds = entities.CandleDataset{ID: 1, Symbol: "BTCUSDT", Timeframe: "1h"}

func TestRunner_HappyPath_OneSegment(t *testing.T) {
	w := newMem()
	arch := &fakeSource{name: "archive"}
	r, _ := newRunner(w, arch)
	chunk := Range{d(2024, 1, 1, 0), d(2024, 2, 1, 0)}

	res, err := r.Run(context.Background(), ds, chunk)

	require.NoError(t, err)
	assert.Len(t, w.rows, 31*24)
	require.Len(t, res.Segments, 1)
	assert.Equal(t, entities.SegmentLoaded, res.Segments[0].State)
	assert.Equal(t, "ref-archive", res.Segments[0].SourceRef)
	assert.Equal(t, chunk.From, res.Segments[0].From)
	assert.Equal(t, chunk.To, res.Segments[0].To)
	assert.Equal(t, 31*24, res.Rows)
	assert.Empty(t, res.Fallbacks)
}

func TestRunner_NotFoundFallsBackToNextSource(t *testing.T) {
	w := newMem()
	arch := &fakeSource{name: "archive", errs: []error{candlesource.ErrNotFound}}
	rest := &fakeSource{name: "rest", authoritative: true}
	r, _ := newRunner(w, arch, rest)

	res, err := r.Run(context.Background(), ds, Range{d(2024, 1, 1, 0), d(2024, 1, 3, 0)})

	require.NoError(t, err)
	assert.Equal(t, []string{"rest"}, res.Sources)
	require.Len(t, res.Fallbacks, 1)
	assert.Equal(t, "archive", res.Fallbacks[0].From)
}

func TestRunner_ShortArchiveIsCompletedByRest(t *testing.T) {
	w := newMem()
	hole := func(t time.Time) bool { return !t.Before(d(2024, 1, 1, 10)) && t.Before(d(2024, 1, 1, 20)) }
	arch := &fakeSource{name: "archive", skip: hole}
	rest := &fakeSource{name: "rest", authoritative: true}
	r, _ := newRunner(w, arch, rest)

	res, err := r.Run(context.Background(), ds, Range{d(2024, 1, 1, 0), d(2024, 1, 2, 0)})

	require.NoError(t, err)
	assert.Len(t, w.rows, 24)
	require.Len(t, res.Segments, 3) // archive, rest (the hole), archive
	assert.Equal(t, "rest", res.Segments[1].Source)
	assert.Equal(t, Range{d(2024, 1, 1, 10), d(2024, 1, 1, 20)}, Range{res.Segments[1].From, res.Segments[1].To})
	assert.Equal(t, []string{"archive", "rest"}, res.Sources)
	assert.Equal(t, 1, rest.calls)
}

func TestRunner_AuthoritativeEmptyBecomesKnownGap(t *testing.T) {
	w := newMem()
	noTrades := func(t time.Time) bool { return !t.Before(d(2024, 1, 1, 5)) && t.Before(d(2024, 1, 1, 7)) }
	rest := &fakeSource{name: "rest", authoritative: true, skip: noTrades}
	r, _ := newRunner(w, rest)

	res, err := r.Run(context.Background(), ds, Range{d(2024, 1, 1, 0), d(2024, 1, 2, 0)})

	require.NoError(t, err)
	assert.Len(t, w.rows, 22)
	var gaps []entities.CandleSegment
	for _, s := range res.Segments {
		if s.State == entities.SegmentKnownGap {
			gaps = append(gaps, s)
		}
	}
	require.Len(t, gaps, 1)
	assert.Equal(t, d(2024, 1, 1, 5), gaps[0].From)
	assert.Equal(t, d(2024, 1, 1, 7), gaps[0].To)
}

func TestRunner_AllSourcesFailKeepsPartialProgress(t *testing.T) {
	w := newMem()
	hole := func(t time.Time) bool { return !t.Before(d(2024, 1, 1, 10)) }
	arch := &fakeSource{name: "archive", skip: hole} // not authoritative, short
	rest := &fakeSource{name: "rest", authoritative: true, errs: []error{candlesource.ErrInvalidData}}
	r, _ := newRunner(w, arch, rest)

	res, err := r.Run(context.Background(), ds, Range{d(2024, 1, 1, 0), d(2024, 1, 2, 0)})

	require.ErrorIs(t, err, ErrIncomplete)
	require.Len(t, res.Segments, 1) // the verified first 10 hours survive
	assert.Equal(t, d(2024, 1, 1, 10), res.Segments[0].To)
	assert.Equal(t, []Range{{d(2024, 1, 1, 10), d(2024, 1, 2, 0)}}, res.Missing)
	require.Len(t, res.Fallbacks, 1)
}

func TestRunner_RetryableHonoursRetryAfterThenSucceeds(t *testing.T) {
	w := newMem()
	rest := &fakeSource{name: "rest", authoritative: true, errs: []error{
		&candlesource.RetryableError{Err: errors.New("429"), After: 7 * time.Second},
		&candlesource.RetryableError{Err: errors.New("503")},
	}}
	r, sleeps := newRunner(w, rest)

	_, err := r.Run(context.Background(), ds, Range{d(2024, 1, 1, 0), d(2024, 1, 1, 6)})

	require.NoError(t, err)
	assert.Equal(t, 3, rest.calls)
	assert.Equal(t, []time.Duration{7 * time.Second, 4 * time.Second}, *sleeps) // Retry-After, then base*2^1
}

func TestRunner_RetryableExhaustedFallsThrough(t *testing.T) {
	w := newMem()
	flaky := &fakeSource{name: "rest", errs: repeatErr(&candlesource.RetryableError{Err: errors.New("503")}, 10)}
	arch := &fakeSource{name: "archive"}
	r, _ := newRunner(w, flaky, arch)

	res, err := r.Run(context.Background(), ds, Range{d(2024, 1, 1, 0), d(2024, 1, 1, 6)})

	require.NoError(t, err)
	assert.Equal(t, 4, flaky.calls) // MaxAttempts
	assert.Equal(t, []string{"archive"}, res.Sources)
}

func TestRunner_InvalidCandlesAreRejectedAndRefetched(t *testing.T) {
	w := newMem()
	bad := func(t time.Time) bool { return t.Equal(d(2024, 1, 1, 3)) }
	arch := &fakeSource{name: "archive", bad: bad}
	rest := &fakeSource{name: "rest", authoritative: true}
	r, _ := newRunner(w, arch, rest)

	res, err := r.Run(context.Background(), ds, Range{d(2024, 1, 1, 0), d(2024, 1, 1, 6)})

	require.NoError(t, err)
	assert.Equal(t, 1, res.Rejected)
	assert.Equal(t, float64(9), w.rows[d(2024, 1, 1, 3)].Low) // the good REST copy won
}

func TestRunner_LargeChunkWritesInBoundedBatches(t *testing.T) {
	w := newMem()
	src := &fakeSource{name: "archive"}
	r, _ := newRunner(w, src)
	r.BatchSize = 1000
	minute := entities.CandleDataset{ID: 2, Symbol: "BTCUSDT", Timeframe: "1m"}

	res, err := r.Run(context.Background(), minute, Range{d(2024, 1, 1, 0), d(2024, 2, 1, 0)})

	require.NoError(t, err)
	assert.Equal(t, 31*24*60, len(w.rows))
	assert.LessOrEqual(t, w.maxBatch, 1000)
	assert.Equal(t, 31*24*60, res.Rows)
}

func TestRunner_ContextCancelStops(t *testing.T) {
	w := newMem()
	ctx, cancel := context.WithCancel(context.Background())
	rest := &fakeSource{name: "rest", errs: []error{&candlesource.RetryableError{Err: errors.New("429")}}}
	r, _ := newRunner(w, rest)
	r.Sleep = func(context.Context, time.Duration) error { cancel(); return context.Canceled }

	_, err := r.Run(ctx, ds, Range{d(2024, 1, 1, 0), d(2024, 1, 1, 6)})

	assert.ErrorIs(t, err, context.Canceled)
}

func repeatErr(e error, n int) []error {
	out := make([]error, n)
	for i := range out {
		out[i] = fmt.Errorf("%w", e)
	}
	return out
}
