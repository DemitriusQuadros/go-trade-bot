package candlesource

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-trade-bot/internal/exchange"
)

const s3XML = `<?xml version="1.0"?><ListBucketResult>
<Contents><Key>data/spot/monthly/klines/SOLUSDT/1h/SOLUSDT-1h-2020-09.zip</Key></Contents>
<Contents><Key>data/spot/monthly/klines/SOLUSDT/1h/SOLUSDT-1h-2020-09.zip.CHECKSUM</Key></Contents>
<Contents><Key>data/spot/monthly/klines/SOLUSDT/1h/SOLUSDT-1h-2020-08.zip</Key></Contents>
</ListBucketResult>`

func TestListingResolver_ArchiveMonthThenRESTRefinement(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		assert.Contains(t, r.URL.RawQuery, "prefix=data/spot/monthly/klines/SOLUSDT/1h/")
		_, _ = w.Write([]byte(s3XML))
	}))
	defer srv.Close()

	l := NewListingResolver(nil, nil)
	l.BaseURL = srv.URL
	got, err := l.ListingStart(context.Background(), "SOLUSDT", "1h")
	require.NoError(t, err)
	assert.Equal(t, time.Date(2020, 8, 1, 0, 0, 0, 0, time.UTC), got)

	_, _ = l.ListingStart(context.Background(), "SOLUSDT", "1h")
	assert.Equal(t, 1, hits, "cached")

	// with a REST fetcher the exact first candle wins
	f := &fakeFetcher{have: func(t time.Time) bool { return !t.Before(time.Date(2020, 8, 11, 6, 0, 0, 0, time.UTC)) }}
	l2 := NewListingResolver(nil, f)
	l2.BaseURL = srv.URL
	f2 := &exactFirst{first: time.Date(2020, 8, 11, 6, 0, 0, 0, time.UTC)}
	l2.Fetcher = f2
	got, err = l2.ListingStart(context.Background(), "SOLUSDT", "1h")
	require.NoError(t, err)
	assert.Equal(t, f2.first, got)
}

func TestListingResolver_FallsBackToRESTWhenArchiveListingFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer srv.Close()
	l := NewListingResolver(nil, &exactFirst{first: time.Date(2021, 3, 5, 0, 0, 0, 0, time.UTC)})
	l.BaseURL = srv.URL
	got, err := l.ListingStart(context.Background(), "XYZUSDT", "1h")
	require.NoError(t, err)
	assert.Equal(t, 2021, got.Year())

	l3 := NewListingResolver(nil, nil)
	l3.BaseURL = srv.URL
	_, err = l3.ListingStart(context.Background(), "XYZUSDT", "1h")
	assert.Error(t, err)
}

type exactFirst struct{ first time.Time }

func (e *exactFirst) ListKlineRange(_ context.Context, sym, tf string, _, _ time.Time, _ int) ([]exchange.Candle, error) {
	return []exchange.Candle{{Symbol: sym, Timeframe: tf, OpenTime: e.first}}, nil
}
