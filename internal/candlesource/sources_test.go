package candlesource

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-trade-bot/app/entities"
	"go-trade-bot/internal/exchange"
)

func ts(y int, m time.Month, d, h int) time.Time { return time.Date(y, m, d, h, 0, 0, 0, time.UTC) }

func TestArchiveSource_CanServe(t *testing.T) {
	now := ts(2026, 10, 15, 12)
	monthly, daily := NewArchiveSource(false, nil), NewArchiveSource(true, nil)

	// whole finished month: monthly yes
	assert.True(t, monthly.CanServe("X", "1h", ts(2026, 8, 1, 0), ts(2026, 9, 1, 0), now))
	// current month, partial month, unaligned: monthly no
	assert.False(t, monthly.CanServe("X", "1h", ts(2026, 10, 1, 0), ts(2026, 11, 1, 0), now))
	assert.False(t, monthly.CanServe("X", "1h", ts(2026, 8, 1, 0), ts(2026, 8, 20, 0), now))
	assert.False(t, monthly.CanServe("X", "1h", ts(2026, 8, 2, 0), ts(2026, 9, 2, 0), now))

	// whole finished days: daily yes (today's file doesn't exist yet)
	assert.True(t, daily.CanServe("X", "1h", ts(2026, 10, 1, 0), ts(2026, 10, 15, 0), now))
	assert.False(t, daily.CanServe("X", "1h", ts(2026, 10, 14, 0), ts(2026, 10, 16, 0), now))
	assert.False(t, daily.CanServe("X", "1h", ts(2026, 10, 14, 6), ts(2026, 10, 15, 0), now))
	assert.False(t, monthly.Authoritative())
}

type fakeFetcher struct {
	have  func(t time.Time) bool
	calls []time.Time
	err   error
}

func (f *fakeFetcher) ListKlineRange(_ context.Context, sym, tf string, start, end time.Time, limit int) ([]exchange.Candle, error) {
	f.calls = append(f.calls, start)
	if f.err != nil {
		return nil, f.err
	}
	var out []exchange.Candle
	for t := start; t.Before(end) && len(out) < limit; t = t.Add(time.Minute) {
		if f.have == nil || f.have(t) {
			out = append(out, exchange.Candle{Symbol: sym, Timeframe: tf, OpenTime: t, Open: 1, High: 2, Low: 1, Close: 1, Volume: 1})
		}
	}
	return out, nil
}

type countLimiter struct{ weight int }

func (c *countLimiter) Wait(_ context.Context, w int) error { c.weight += w; return nil }

func TestRESTSource_PagesAcrossMoreThanOnePage_AndBudgetsWeight(t *testing.T) {
	f := &fakeFetcher{}
	lim := &countLimiter{}
	src := NewRESTSource(f, lim)
	src.PageSize = 1000
	from, to := ts(2024, 1, 1, 0), ts(2024, 1, 2, 12) // 2160 one-minute candles -> 3 pages

	var got []entities.Candle
	ref, err := src.Fetch(context.Background(), "BTCUSDT", "1m", from, to, func(b []entities.Candle) error { got = append(got, b...); return nil })

	require.NoError(t, err)
	assert.Equal(t, "klines", ref)
	assert.Len(t, got, 2160)
	assert.Len(t, f.calls, 3)
	assert.Equal(t, 3*klineWeight, lim.weight)
	assert.True(t, src.Authoritative())
}

func TestRESTSource_EmptyWindowIsAdvancedPastNotTreatedAsEnd(t *testing.T) {
	// nothing for the first 1500 minutes (pre-listing / outage), then data
	cut := ts(2024, 1, 1, 0).Add(1500 * time.Minute)
	f := &fakeFetcher{have: func(t time.Time) bool { return !t.Before(cut) }}
	src := NewRESTSource(f, nil)
	n := 0
	_, err := src.Fetch(context.Background(), "BTCUSDT", "1m", ts(2024, 1, 1, 0), ts(2024, 1, 2, 12), func(b []entities.Candle) error { n += len(b); return nil })
	require.NoError(t, err)
	assert.Equal(t, 2160-1500, n)
}

func TestRESTSource_DropsCandlesOutsideRange(t *testing.T) {
	f := &fakeFetcher{}
	src := NewRESTSource(f, nil)
	n := 0
	_, _ = src.Fetch(context.Background(), "BTCUSDT", "1m", ts(2024, 1, 1, 0), ts(2024, 1, 1, 1), func(b []entities.Candle) error { n += len(b); return nil })
	assert.Equal(t, 60, n)
}

func TestClassifyRESTError(t *testing.T) {
	_, ok := Retryable(classifyRESTError(errors.New("<APIError> code=-1003, msg=Too many requests")))
	assert.True(t, ok)
	re, ok := Retryable(classifyRESTError(errors.New("status code: 418 banned")))
	require.True(t, ok)
	assert.Equal(t, 2*time.Minute, re.After)
	_, ok = Retryable(classifyRESTError(errors.New("invalid symbol")))
	assert.False(t, ok)
}

type banLimiter struct {
	countLimiter
	banned time.Duration
}

func (b *banLimiter) Ban(_ context.Context, d time.Duration) error { b.banned = d; return nil }

func TestRESTSource_RateLimitResponseTripsTheSharedBreaker(t *testing.T) {
	lim := &banLimiter{}
	src := NewRESTSource(&fakeFetcher{err: errors.New("status code: 418 banned")}, lim)

	_, err := src.Fetch(context.Background(), "BTCUSDT", "1m", ts(2024, 1, 1, 0), ts(2024, 1, 1, 1), func([]entities.Candle) error { return nil })

	re, ok := Retryable(err)
	require.True(t, ok)
	assert.Equal(t, 2*time.Minute, re.After)
	assert.Equal(t, 2*time.Minute, lim.banned)
}
