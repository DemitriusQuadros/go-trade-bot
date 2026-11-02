package binancearchive

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func withTestFuturesBaseURLs(t *testing.T, url string) {
	t.Helper()
	oldMonthly := futuresMonthlyBaseURL
	oldDaily := futuresDailyBaseURL
	oldFunding := fundingRateMonthlyBaseURL

	futuresMonthlyBaseURL = url
	futuresDailyBaseURL = url
	fundingRateMonthlyBaseURL = url

	t.Cleanup(func() {
		futuresMonthlyBaseURL = oldMonthly
		futuresDailyBaseURL = oldDaily
		fundingRateMonthlyBaseURL = oldFunding
	})
}

func serveFutures(t *testing.T, zipBytes []byte, checksum string, zipStatus int) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case len(r.URL.Path) > 9 && r.URL.Path[len(r.URL.Path)-9:] == ".CHECKSUM":
			if checksum == "" {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write([]byte(checksum + "  file.zip\n"))
		default:
			if zipStatus != 0 && zipStatus != http.StatusOK {
				w.WriteHeader(zipStatus)
				return
			}
			_, _ = w.Write(zipBytes)
		}
	}))
	t.Cleanup(srv.Close)
	withTestFuturesBaseURLs(t, srv.URL)
}

func TestFuturesURLBuilders(t *testing.T) {
	m := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	day := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)

	gotMonth := FuturesMonthURL("BTCUSDT", "1h", m)
	wantMonth := "https://data.binance.vision/data/futures/um/monthly/klines/BTCUSDT/1h/BTCUSDT-1h-2026-03.zip"
	assert.Equal(t, wantMonth, gotMonth)

	gotDay := FuturesDayURL("BTCUSDT", "15m", day)
	wantDay := "https://data.binance.vision/data/futures/um/daily/klines/BTCUSDT/15m/BTCUSDT-15m-2026-03-15.zip"
	assert.Equal(t, wantDay, gotDay)

	gotFunding := FundingRateMonthURL("BTCUSDT", m)
	wantFunding := "https://data.binance.vision/data/futures/um/monthly/fundingRate/BTCUSDT/BTCUSDT-fundingRate-2026-03.zip"
	assert.Equal(t, wantFunding, gotFunding)
}

func TestFetchVerifiedFuturesKlines(t *testing.T) {
	z := buildZip(t, "BTCUSDT-1h-2026-03.csv", "1704067200000,100,105,95,102,50,1704070799999,0,0,0,0,0\n")
	serveFutures(t, z, sha(z), 0)
	period := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	// Valid monthly
	candles, ref, err := FetchVerifiedFuturesKlines(context.Background(), http.DefaultClient, "BTCUSDT", "1h", period, false)
	require.NoError(t, err)
	require.Len(t, candles, 1)
	assert.Equal(t, "BTCUSDT", candles[0].Symbol)
	assert.Equal(t, 100.0, candles[0].Open)
	assert.Equal(t, 102.0, candles[0].Close)
	assert.Equal(t, "BTCUSDT-1h-2026-03.zip sha256:"+sha(z), ref)

	// Valid daily
	candlesDaily, refDaily, err := FetchVerifiedFuturesKlines(context.Background(), http.DefaultClient, "BTCUSDT", "1h", period, true)
	require.NoError(t, err)
	require.Len(t, candlesDaily, 1)
	assert.Contains(t, refDaily, "BTCUSDT-1h-2026-03-01.zip")

	// Checksum mismatch
	serveFutures(t, z, sha([]byte("tampered")), 0)
	_, _, err = FetchVerifiedFuturesKlines(context.Background(), http.DefaultClient, "BTCUSDT", "1h", period, false)
	assert.ErrorIs(t, err, ErrChecksum)
}

func TestFetchVerifiedFundingRates(t *testing.T) {
	t1 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 3, 1, 8, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 3, 1, 16, 0, 0, 0, time.UTC)

	// Include header row + 3-col row + 2-col row + microsecond timestamp row
	csvContent := "calc_time,funding_interval_hours,last_funding_rate\n" +
		fmt.Sprintf("%d,8,0.0001\n", t1.UnixMilli()) +
		fmt.Sprintf("%d,0.00015\n", t2.UnixMilli()) + // 2 columns
		fmt.Sprintf("%d,8,-0.00005\n", t3.UnixMicro()) // microsecond timestamp normalized

	z := buildZip(t, "BTCUSDT-fundingRate-2026-03.csv", csvContent)
	serveFutures(t, z, sha(z), 0)

	month := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	rates, ref, err := FetchVerifiedFundingRates(context.Background(), http.DefaultClient, "BTCUSDT", month)
	require.NoError(t, err)
	require.Len(t, rates, 3)

	assert.Equal(t, "BTCUSDT-fundingRate-2026-03.zip sha256:"+sha(z), ref)

	assert.Equal(t, "BTCUSDT", rates[0].Symbol)
	assert.Equal(t, t1, rates[0].FundingTime)
	assert.Equal(t, 0.0001, rates[0].Rate)

	assert.Equal(t, "BTCUSDT", rates[1].Symbol)
	assert.Equal(t, t2, rates[1].FundingTime)
	assert.Equal(t, 0.00015, rates[1].Rate)

	assert.Equal(t, "BTCUSDT", rates[2].Symbol)
	assert.Equal(t, t3, rates[2].FundingTime)
	assert.Equal(t, -0.00005, rates[2].Rate)

	// Checksum mismatch
	serveFutures(t, z, sha([]byte("wrong")), 0)
	_, _, err = FetchVerifiedFundingRates(context.Background(), http.DefaultClient, "BTCUSDT", month)
	assert.ErrorIs(t, err, ErrChecksum)
}
