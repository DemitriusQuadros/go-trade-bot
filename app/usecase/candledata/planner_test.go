package candledata

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-trade-bot/app/entities"
)

func d(y int, m time.Month, day, h int) time.Time { return time.Date(y, m, day, h, 0, 0, 0, time.UTC) }

func TestMergeAndSubtract(t *testing.T) {
	a := Range{d(2024, 1, 1, 0), d(2024, 1, 5, 0)}
	b := Range{d(2024, 1, 5, 0), d(2024, 1, 8, 0)} // adjacent
	c := Range{d(2024, 1, 20, 0), d(2024, 1, 25, 0)}
	assert.Equal(t, []Range{{d(2024, 1, 1, 0), d(2024, 1, 8, 0)}, c}, Merge([]Range{c, b, a, {}}))

	want := Range{d(2024, 1, 1, 0), d(2024, 1, 31, 0)}
	got := Subtract(want, []Range{a, b, c})
	assert.Equal(t, []Range{{d(2024, 1, 8, 0), d(2024, 1, 20, 0)}, {d(2024, 1, 25, 0), d(2024, 1, 31, 0)}}, got)
	assert.Empty(t, Subtract(a, []Range{{d(2023, 12, 1, 0), d(2024, 2, 1, 0)}}))
	assert.Equal(t, []Range{a}, Subtract(a, nil))
}

func TestSplitByMonth(t *testing.T) {
	got := SplitByMonth(Range{d(2024, 1, 15, 6), d(2024, 3, 2, 0)})
	assert.Equal(t, []Range{
		{d(2024, 1, 15, 6), d(2024, 2, 1, 0)},
		{d(2024, 2, 1, 0), d(2024, 3, 1, 0)},
		{d(2024, 3, 1, 0), d(2024, 3, 2, 0)},
	}, got)
}

func TestDesiredRange(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 7, 30, 0, time.UTC)
	got := DesiredRange(nil, d(2020, 8, 11, 6), time.Hour, now)
	assert.Equal(t, Range{d(2020, 8, 11, 6), d(2026, 10, 1, 12)}, got)

	// start later than listing wins; unaligned start rounds up to the grid
	start := time.Date(2022, 7, 1, 0, 10, 0, 0, time.UTC)
	got = DesiredRange(&start, d(2020, 8, 11, 6), 15*time.Minute, now)
	assert.Equal(t, time.Date(2022, 7, 1, 0, 15, 0, 0, time.UTC), got.From)
}

func TestParseTimeframe(t *testing.T) {
	dur, err := ParseTimeframe("15m")
	require.NoError(t, err)
	assert.Equal(t, 15*time.Minute, dur)
	_, err = ParseTimeframe("1w")
	assert.Error(t, err)
}

func TestPlan_EmptyLedgerBackfillsByMonth(t *testing.T) {
	desired := Range{d(2024, 1, 20, 0), d(2024, 3, 10, 0)}
	specs := Plan(desired, nil)
	require.Len(t, specs, 3)
	for _, s := range specs {
		assert.Equal(t, entities.PurposeBackfill, s.Purpose)
	}
	assert.Equal(t, Range{d(2024, 3, 1, 0), d(2024, 3, 10, 0)}, specs[2].Range)
}

func TestPlan_FullyCoveredIsEmpty(t *testing.T) {
	desired := Range{d(2024, 1, 1, 0), d(2024, 2, 1, 0)}
	assert.Empty(t, Plan(desired, []Range{desired}))
}

func TestPlan_TailAndRepair(t *testing.T) {
	desired := Range{d(2024, 1, 1, 0), d(2024, 3, 5, 12)}
	covered := []Range{
		{d(2024, 1, 1, 0), d(2024, 1, 10, 0)},
		{d(2024, 1, 12, 0), d(2024, 3, 5, 0)}, // hole [Jan10, Jan12) and 12h tail missing
	}
	specs := Plan(desired, covered)
	require.Len(t, specs, 2)
	assert.Equal(t, entities.PurposeRepair, specs[0].Purpose)
	assert.Equal(t, Range{d(2024, 1, 10, 0), d(2024, 1, 12, 0)}, specs[0].Range)
	assert.Equal(t, entities.PurposeTail, specs[1].Purpose)
	assert.Greater(t, specs[1].Priority, specs[0].Priority)
}

func TestPlan_DowntimeOfAnyLengthSelfHeals(t *testing.T) {
	// a worker was down for 90 days: the tail is large, so it is planned as
	// month-bounded backfill chunks, not one giant tail.
	desired := Range{d(2024, 1, 1, 0), d(2024, 4, 1, 0)}
	specs := Plan(desired, []Range{{d(2024, 1, 1, 0), d(2024, 1, 1, 12)}})
	require.Len(t, specs, 3)
	for _, s := range specs {
		assert.Equal(t, entities.PurposeBackfill, s.Purpose)
	}
}
