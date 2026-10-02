package candledata

import (
	"context"
	"time"

	"go-trade-bot/app/entities"
)

// OpenTimeScanner streams stored open times in ascending order
// (satisfied by candle.CandleRepository).
type OpenTimeScanner interface {
	ScanOpenTimes(ctx context.Context, symbol, timeframe string, fn func(time.Time) error) error
}

// Adopt turns candles that were stored before the ledger existed into loaded
// segments, so nothing already on disk is downloaded again. Each contiguous run
// of stored candles becomes one segment; anything not stored stays missing and
// is picked up by the next Reconcile. The still-open candle (which the old
// importer could store) is never adopted. It does nothing when the dataset
// already has ledger segments. Returns the number of segments written.
func (s *Service) Adopt(ctx context.Context, scanner OpenTimeScanner, datasetID uint) (int, error) {
	ds, err := s.Store.GetDataset(ctx, datasetID)
	if err != nil {
		return 0, err
	}
	tf, err := ParseTimeframe(ds.Timeframe)
	if err != nil {
		return 0, err
	}
	existing, err := s.Store.Segments(ctx, datasetID)
	if err != nil || len(existing) > 0 {
		return 0, err
	}
	closedUpTo := s.Now().UTC().Truncate(tf) // candles opening at/after this are not closed yet

	var segs []entities.CandleSegment
	var runFrom, last time.Time
	flush := func() {
		if runFrom.IsZero() {
			return
		}
		segs = append(segs, entities.CandleSegment{
			From: runFrom, To: last.Add(tf), State: entities.SegmentLoaded, Source: "adopted",
			RowCount: int(last.Add(tf).Sub(runFrom) / tf), LoadedAt: s.Now(),
		})
	}
	err = scanner.ScanOpenTimes(ctx, ds.Symbol, ds.Timeframe, func(t time.Time) error {
		if !t.Before(closedUpTo) || t.Truncate(tf) != t {
			return nil
		}
		if runFrom.IsZero() {
			runFrom, last = t, t
			return nil
		}
		if t.Sub(last) == tf {
			last = t
			return nil
		}
		flush()
		runFrom, last = t, t
		return nil
	})
	if err != nil {
		return 0, err
	}
	flush()
	if len(segs) == 0 {
		return 0, nil
	}
	if err := s.Store.ReplaceSegments(ctx, datasetID, segs[0].From, segs[len(segs)-1].To, segs); err != nil {
		return 0, err
	}
	return len(segs), nil
}
