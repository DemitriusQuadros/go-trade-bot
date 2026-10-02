package candledata

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"time"

	"go-trade-bot/app/entities"
)

var symbolRe = regexp.MustCompile(`^[A-Z0-9]{4,20}$`)

// ErrInvalid is wrapped by validation failures (HTTP 400).
type ErrInvalid struct{ Msg string }

func (e ErrInvalid) Error() string { return e.Msg }

// Create declares a dataset: validates it, stores it, adopts any candles already
// on disk and starts converging. Reconcile failures (e.g. Binance unreachable)
// are logged, not returned - the sweeper retries within a minute.
func (s *Service) Create(ctx context.Context, symbol, timeframe string, start *time.Time, keepLive bool) (*entities.CandleDataset, error) {
	if !symbolRe.MatchString(symbol) {
		return nil, ErrInvalid{fmt.Sprintf("invalid symbol %q", symbol)}
	}
	if _, err := ParseTimeframe(timeframe); err != nil {
		return nil, ErrInvalid{err.Error()}
	}
	if start != nil {
		t := start.UTC()
		start = &t
		if t.After(s.Now()) {
			return nil, ErrInvalid{"start is in the future"}
		}
	}
	ds := &entities.CandleDataset{Symbol: symbol, Timeframe: timeframe, Start: start, KeepLive: keepLive}
	if err := s.Store.UpsertDataset(ctx, ds); err != nil {
		return nil, err
	}
	if s.Scanner != nil {
		if n, err := s.Adopt(ctx, s.Scanner, ds.ID); err != nil {
			log.Printf("candledata: adopting existing %s/%s candles: %v", symbol, timeframe, err)
		} else if n > 0 {
			log.Printf("candledata: adopted %d existing segment(s) for %s/%s", n, symbol, timeframe)
		}
	}
	if _, err := s.Reconcile(ctx, ds.ID); err != nil {
		log.Printf("candledata: initial reconcile of %s/%s: %v (the sweeper will retry)", symbol, timeframe, err)
	}
	return ds, nil
}

// Delete removes the dataset, its ledger and chunks. Stored candles are kept.
func (s *Service) Delete(ctx context.Context, id uint) error {
	return s.Store.DeleteDataset(ctx, id)
}

// DatasetState summarises a dataset for the UI.
type DatasetState string

const (
	StatePaused     DatasetState = "paused"
	StateFailed     DatasetState = "failed"
	StateConverging DatasetState = "converging"
	StateLive       DatasetState = "live"
	StateIdle       DatasetState = "idle"
)

// DatasetStatus is the full picture of one dataset.
type DatasetStatus struct {
	Dataset        entities.CandleDataset
	State          DatasetState
	Desired        Range
	Loaded         []Range // merged loaded segments
	KnownGaps      []Range // merged exchange gaps
	Missing        []Range // desired minus (loaded + known gaps)
	MissingCandles int64
	DesiredCandles int64
	LagSeconds     float64
	Chunks         map[entities.ChunkStatus]int64
	LastError      string
	ListingError   string // set when the listing start could not be resolved
}

// Status computes a dataset's coverage, progress and state from the ledger and
// the chunk table; it never mutates anything.
func (s *Service) Status(ctx context.Context, id uint) (*DatasetStatus, error) {
	ds, err := s.Store.GetDataset(ctx, id)
	if err != nil {
		return nil, err
	}
	st := &DatasetStatus{Dataset: *ds}
	segs, err := s.Store.Segments(ctx, id)
	if err != nil {
		return nil, err
	}
	var loaded, gaps []Range
	for _, sg := range segs {
		if sg.State == entities.SegmentKnownGap {
			gaps = append(gaps, Range{sg.From, sg.To})
		} else {
			loaded = append(loaded, Range{sg.From, sg.To})
		}
	}
	st.Loaded, st.KnownGaps = Merge(loaded), Merge(gaps)

	if st.Chunks, err = s.Store.ChunkCounts(ctx, id); err != nil {
		return nil, err
	}
	for _, status := range []entities.ChunkStatus{entities.ChunkDead, entities.ChunkFailed} {
		if st.Chunks[status] == 0 {
			continue
		}
		if cs, err := s.Store.ListChunks(ctx, id, status, 1); err == nil && len(cs) > 0 && cs[0].LastError != "" {
			st.LastError = cs[0].LastError
			break
		}
	}

	tf, err := ParseTimeframe(ds.Timeframe)
	if err != nil {
		st.ListingError = err.Error()
	} else if listing, lerr := s.Listing.ListingStart(ctx, ds.Symbol, ds.Timeframe); lerr != nil {
		st.ListingError = lerr.Error()
	} else {
		st.Desired = DesiredRange(ds.Start, listing, tf, s.Now())
		st.DesiredCandles = int64(st.Desired.To.Sub(st.Desired.From) / tf)
		st.Missing = Subtract(st.Desired, Covered(segs))
		for _, m := range st.Missing {
			st.MissingCandles += int64(m.To.Sub(m.From) / tf)
		}
		if c := Covered(segs); len(c) > 0 {
			st.LagSeconds = s.Now().Sub(c[len(c)-1].To).Seconds()
		}
	}

	switch {
	case ds.Paused:
		st.State = StatePaused
	case st.Chunks[entities.ChunkDead]+st.Chunks[entities.ChunkFailed] > 0:
		st.State = StateFailed
	case st.Chunks[entities.ChunkPending]+st.Chunks[entities.ChunkRunning] > 0 || st.MissingCandles > 0:
		st.State = StateConverging
	case ds.KeepLive:
		st.State = StateLive
	default:
		st.State = StateIdle
	}
	return st, nil
}

// List returns the status of every dataset.
func (s *Service) List(ctx context.Context) ([]*DatasetStatus, error) {
	all, err := s.Store.ListDatasets(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*DatasetStatus, 0, len(all))
	for _, ds := range all {
		st, err := s.Status(ctx, ds.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, nil
}

// Chunks lists a dataset's chunks (read-only operational view).
func (s *Service) Chunks(ctx context.Context, id uint, status entities.ChunkStatus, limit int) ([]entities.CandleChunk, error) {
	return s.Store.ListChunks(ctx, id, status, limit)
}

// Reconcile-now is Service.Reconcile; Pause/Resume/RetryFailed are in service.go.
