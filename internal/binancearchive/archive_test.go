package binancearchive

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func buildZip(t *testing.T, filename, csvContent string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, err := zw.Create(filename)
	if err != nil {
		t.Fatalf("zw.Create: %v", err)
	}
	if _, err := f.Write([]byte(csvContent)); err != nil {
		t.Fatalf("write csv: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zw.Close: %v", err)
	}
	return buf.Bytes()
}

func withTestBaseURL(t *testing.T, url string) {
	t.Helper()
	original := baseURL
	baseURL = url
	t.Cleanup(func() { baseURL = original })
}

func TestFetchMonth_ParsesCandlesAndSkipsHeaderRow(t *testing.T) {
	// Real data.binance.vision rows: open_time,open,high,low,close,volume,close_time,quote_volume,trades,taker_buy_base,taker_buy_quote,ignore
	csvContent := "open_time,open,high,low,close,volume,close_time,quote_volume,trades,taker_buy_base,taker_buy_quote,ignore\n" +
		"1700000000000,100.5,101.2,99.8,100.9,12.34,1700000059999,1234.5,10,6.0,600.0,0\n" +
		"1700000060000,100.9,102.0,100.5,101.5,20.1,1700000119999,2000.0,15,10.0,1000.0,0\n"
	zipBytes := buildZip(t, "BTCUSDT-1m-2023-11.csv", csvContent)

	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(zipBytes)
	}))
	defer server.Close()
	withTestBaseURL(t, server.URL)

	candles, err := FetchMonth(context.Background(), server.Client(), "BTCUSDT", "1m", time.Date(2023, 11, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("FetchMonth returned error: %v", err)
	}
	wantPath := "/BTCUSDT/1m/BTCUSDT-1m-2023-11.zip"
	if gotPath != wantPath {
		t.Errorf("requested path = %q, want %q", gotPath, wantPath)
	}
	if len(candles) != 2 {
		t.Fatalf("expected 2 candles (header row skipped), got %d", len(candles))
	}

	first := candles[0]
	if first.Symbol != "BTCUSDT" || first.Timeframe != "1m" {
		t.Errorf("unexpected symbol/timeframe: %+v", first)
	}
	if first.Open != 100.5 || first.High != 101.2 || first.Low != 99.8 || first.Close != 100.9 || first.Volume != 12.34 {
		t.Errorf("unexpected OHLCV: %+v", first)
	}
	wantOpenTime := time.UnixMilli(1700000000000).UTC()
	if !first.OpenTime.Equal(wantOpenTime) {
		t.Errorf("OpenTime = %v, want %v", first.OpenTime, wantOpenTime)
	}

	second := candles[1]
	if second.OpenTime.Before(first.OpenTime) {
		t.Errorf("candles out of order: %v before %v", second.OpenTime, first.OpenTime)
	}
}

func TestFetchMonth_404ReturnsNilNil(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	withTestBaseURL(t, server.URL)

	candles, err := FetchMonth(context.Background(), server.Client(), "BTCUSDT", "1m", time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("expected nil error on 404 (treated as 'skip this month'), got %v", err)
	}
	if candles != nil {
		t.Fatalf("expected nil candles on 404, got %v", candles)
	}
}

func TestFetchMonth_ServerErrorReturnsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	withTestBaseURL(t, server.URL)

	_, err := FetchMonth(context.Background(), server.Client(), "BTCUSDT", "1m", time.Date(2023, 11, 1, 0, 0, 0, 0, time.UTC))
	if err == nil {
		t.Fatal("expected an error for a non-200/404 response, got nil")
	}
}

func TestMonthURL(t *testing.T) {
	got := MonthURL("ETHUSDT", "15m", time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC))
	want := "https://data.binance.vision/data/spot/monthly/klines/ETHUSDT/15m/ETHUSDT-15m-2024-03.zip"
	if got != want {
		t.Errorf("MonthURL = %q, want %q", got, want)
	}
}

func TestParseZipCSV_NoHeaderRow(t *testing.T) {
	// Some archive months ship raw data rows with no header at all.
	csvContent := "1700000000000,1,2,0.5,1.5,10,1700000059999,10,1,5,5,0\n"
	zipBytes := buildZip(t, "data.csv", csvContent)

	candles, err := parseZipCSV(zipBytes, "BTCUSDT", "1m")
	if err != nil {
		t.Fatalf("parseZipCSV error: %v", err)
	}
	if len(candles) != 1 {
		t.Fatalf("expected 1 candle, got %d", len(candles))
	}
}

func TestParseZipCSV_EmptyZipFile(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	if err := zw.Close(); err != nil {
		t.Fatalf("zw.Close: %v", err)
	}

	_, err := parseZipCSV(buf.Bytes(), "BTCUSDT", "1m")
	if err == nil {
		t.Fatal("expected error for zip with no files, got nil")
	}
}

// Regression test for a real corruption bug: newer Binance monthly archives
// express open_time in microseconds instead of milliseconds, with no column
// or header change to signal it. A raw value that's actually microseconds,
// naively treated as milliseconds, lands ~1000x too far in the future (this
// exact case put BTCUSDT/ETHUSDT candles in the year 56000s instead of 2026
// before normalizeToMillis existed).
func TestParseZipCSV_MicrosecondOpenTimeIsNormalized(t *testing.T) {
	wantTime := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	microseconds := wantTime.UnixMicro() // e.g. 1788300800000000 - looks like ms*1000

	csvContent := fmt.Sprintf("%d,100,101,99,100.5,10,0,0,0,0,0,0\n", microseconds)
	zipBytes := buildZip(t, "data.csv", csvContent)

	candles, err := parseZipCSV(zipBytes, "BTCUSDT", "1h")
	if err != nil {
		t.Fatalf("parseZipCSV error: %v", err)
	}
	if len(candles) != 1 {
		t.Fatalf("expected 1 candle, got %d", len(candles))
	}
	if !candles[0].OpenTime.Equal(wantTime) {
		t.Errorf("OpenTime = %v, want %v (microsecond value was not normalized to milliseconds)", candles[0].OpenTime, wantTime)
	}
}

func TestNormalizeToMillis(t *testing.T) {
	cases := []struct {
		name string
		raw  int64
		want int64
	}{
		{"plausible milliseconds pass through unchanged", 1700000000000, 1700000000000},
		{"microsecond value is divided by 1000", 1700000000000000, 1700000000000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeToMillis(tc.raw); got != tc.want {
				t.Errorf("normalizeToMillis(%d) = %d, want %d", tc.raw, got, tc.want)
			}
		})
	}
}
