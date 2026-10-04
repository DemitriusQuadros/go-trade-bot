package feed_test

import (
	"testing"
	"time"

	"go-trade-bot/internal/exchange"
	"go-trade-bot/internal/feed"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func generate1mCandles(count int, startTime time.Time, basePrice float64) []exchange.Candle {
	candles := make([]exchange.Candle, count)
	for i := 0; i < count; i++ {
		t := startTime.Add(time.Duration(i) * time.Minute)
		p := basePrice + float64(i)
		candles[i] = exchange.Candle{
			Symbol:    "BTCUSDT",
			Timeframe: "1m",
			OpenTime:  t,
			Open:      p,
			High:      p + 2.0,
			Low:       p - 1.0,
			Close:     p + 1.0,
			Volume:    10.0,
		}
	}
	return candles
}

func TestAggregateCandles_15mFrom15(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	base := generate1mCandles(15, start, 100.0)

	agg, err := feed.AggregateCandles(base, "15m")
	require.NoError(t, err)
	require.Len(t, agg, 1)

	first := base[0]
	last := base[14]

	assert.Equal(t, start, agg[0].OpenTime)
	assert.Equal(t, first.Open, agg[0].Open)
	assert.Equal(t, last.Close, agg[0].Close)
	assert.Equal(t, last.High, agg[0].High) // increasing series => last high is max
	assert.Equal(t, first.Low, agg[0].Low)   // increasing series => first low is min
	assert.Equal(t, 150.0, agg[0].Volume)
}

func TestAggregateCandles_DiscardPartialTrailing(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// 32 1m candles -> 2 full 15m candles, 2 trailing discarded
	base := generate1mCandles(32, start, 100.0)

	agg, err := feed.AggregateCandles(base, "15m")
	require.NoError(t, err)
	require.Len(t, agg, 2)

	assert.Equal(t, start, agg[0].OpenTime)
	assert.Equal(t, start.Add(15*time.Minute), agg[1].OpenTime)
}

func TestAggregateCandles_SkipGaps(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	base := generate1mCandles(30, start, 100.0)

	// Introduce a gap at index 5 (simulate missing minute)
	base[5].OpenTime = base[5].OpenTime.Add(2 * time.Minute)

	agg, err := feed.AggregateCandles(base, "15m")
	require.NoError(t, err)
	// The first 15-minute window had a gap so it's skipped; the second complete 15-minute window is produced
	assert.Len(t, agg, 1)
}

func TestAggregateCandles_DownsampleError(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// Create 15m base candles
	base := []exchange.Candle{
		{OpenTime: start, Open: 100, High: 105, Low: 95, Close: 102, Volume: 100},
		{OpenTime: start.Add(15 * time.Minute), Open: 102, High: 108, Low: 101, Close: 106, Volume: 120},
	}

	// Try downsampling 15m to 1m -> should return error
	_, err := feed.AggregateCandles(base, "1m")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot aggregate to a smaller timeframe")
}
