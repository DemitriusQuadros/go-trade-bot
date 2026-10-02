// Package candledata is the candle-dataset reconciler: it computes what is
// missing for a dataset (desired - ledger), plans bounded chunks, and runs
// each chunk through a fallback chain of sources. See
// docs/specs/candle-data/candle-dataset-reconciler-design.md.
package candledata

import (
	"fmt"
	"sort"
	"time"
)

// Range is a half-open UTC time interval [From, To).
type Range struct {
	From time.Time
	To   time.Time
}

func (r Range) Empty() bool { return !r.To.After(r.From) }

func (r Range) String() string {
	return fmt.Sprintf("[%s, %s)", r.From.UTC().Format(time.RFC3339), r.To.UTC().Format(time.RFC3339))
}

// Merge sorts and coalesces overlapping or adjacent ranges; empty ones are dropped.
func Merge(in []Range) []Range {
	rs := make([]Range, 0, len(in))
	for _, r := range in {
		if !r.Empty() {
			rs = append(rs, r)
		}
	}
	sort.Slice(rs, func(i, j int) bool { return rs[i].From.Before(rs[j].From) })
	var out []Range
	for _, r := range rs {
		if n := len(out); n > 0 && !r.From.After(out[n-1].To) {
			if r.To.After(out[n-1].To) {
				out[n-1].To = r.To
			}
			continue
		}
		out = append(out, r)
	}
	return out
}

// Subtract returns the parts of want not covered by any range in covered.
func Subtract(want Range, covered []Range) []Range {
	if want.Empty() {
		return nil
	}
	var out []Range
	cursor := want.From
	for _, c := range Merge(covered) {
		if !c.To.After(cursor) {
			continue
		}
		if !c.From.Before(want.To) {
			break
		}
		if c.From.After(cursor) {
			out = append(out, Range{cursor, c.From})
		}
		cursor = c.To
		if !cursor.Before(want.To) {
			return out
		}
	}
	if cursor.Before(want.To) {
		out = append(out, Range{cursor, want.To})
	}
	return out
}

// SplitByMonth cuts r at UTC month boundaries so that no piece spans two
// months (the archive's own granularity and the chunk size bound).
func SplitByMonth(r Range) []Range {
	var out []Range
	cur := r.From.UTC()
	end := r.To.UTC()
	for cur.Before(end) {
		next := time.Date(cur.Year(), cur.Month()+1, 1, 0, 0, 0, 0, time.UTC)
		if next.After(end) {
			next = end
		}
		out = append(out, Range{cur, next})
		cur = next
	}
	return out
}
