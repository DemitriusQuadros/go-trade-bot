package candledata

import (
	"fmt"
	"time"

	"go-trade-bot/app/entities"
)

// ChunkSpec is a planned unit of work: one range inside a single UTC month.
type ChunkSpec struct {
	Range    Range
	Purpose  entities.ChunkPurpose
	Priority int
}

// ParseTimeframe returns the interval for a timeframe whose candles align to
// the UTC epoch grid (1m..1d). 3d and 1w are rejected: Binance aligns them
// differently and the grid arithmetic here would silently mis-plan them.
func ParseTimeframe(tf string) (time.Duration, error) {
	switch tf {
	case "1m":
		return time.Minute, nil
	case "3m":
		return 3 * time.Minute, nil
	case "5m":
		return 5 * time.Minute, nil
	case "15m":
		return 15 * time.Minute, nil
	case "30m":
		return 30 * time.Minute, nil
	case "1h":
		return time.Hour, nil
	case "2h":
		return 2 * time.Hour, nil
	case "4h":
		return 4 * time.Hour, nil
	case "6h":
		return 6 * time.Hour, nil
	case "8h":
		return 8 * time.Hour, nil
	case "12h":
		return 12 * time.Hour, nil
	case "1d":
		return 24 * time.Hour, nil
	}
	return 0, fmt.Errorf("unsupported timeframe %q (supported: 1m..12h, 1d)", tf)
}

// DesiredRange is [first candle, last closed candle]: it starts at the later
// of the dataset's start and the symbol's listing time (rounded up to the
// candle grid) and ends at the close of the most recent closed candle.
func DesiredRange(start *time.Time, listing time.Time, tf time.Duration, now time.Time) Range {
	from := listing
	if start != nil && start.After(from) {
		from = *start
	}
	from = from.UTC()
	if t := from.Truncate(tf); t.Before(from) {
		from = t.Add(tf)
	}
	return Range{From: from, To: now.UTC().Truncate(tf)}
}

const (
	tailMaxSpan = 48 * time.Hour

	priorityTail     = 100
	priorityRepair   = 50
	priorityBackfill = 10
)

// Plan turns desired - covered into month-bounded chunk specs. It is pure:
// the same inputs always give the same output, so re-planning is idempotent.
func Plan(desired Range, covered []Range) []ChunkSpec {
	merged := Merge(covered)
	var first, last time.Time
	if len(merged) > 0 {
		first, last = merged[0].From, merged[len(merged)-1].To
	}
	var specs []ChunkSpec
	for _, miss := range Subtract(desired, covered) {
		for _, piece := range SplitByMonth(miss) {
			spec := ChunkSpec{Range: piece, Purpose: entities.PurposeBackfill, Priority: priorityBackfill}
			switch {
			case len(merged) > 0 && piece.From.After(first) && piece.To.Before(last):
				spec.Purpose, spec.Priority = entities.PurposeRepair, priorityRepair
			case len(merged) > 0 && piece.To.Equal(desired.To) && piece.To.Sub(piece.From) <= tailMaxSpan:
				spec.Purpose, spec.Priority = entities.PurposeTail, priorityTail
			}
			specs = append(specs, spec)
		}
	}
	return specs
}
