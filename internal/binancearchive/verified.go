package binancearchive

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"go-trade-bot/app/entities"
)

// ErrNotFound: the archive file (or its checksum) does not exist (HTTP 404).
var ErrNotFound = errors.New("binancearchive: file not found")

// ErrChecksum: the downloaded zip does not match its published SHA-256.
var ErrChecksum = errors.New("binancearchive: checksum mismatch")

var dailyBaseURL = "https://data.binance.vision/data/spot/daily/klines"

// DayURL returns the daily klines archive URL for (symbol, interval, day).
func DayURL(symbol, interval string, day time.Time) string {
	return fmt.Sprintf("%s/%s/%s/%s-%s-%s.zip", dailyBaseURL, symbol, interval, symbol, interval, day.Format("2006-01-02"))
}

// FetchVerified downloads one monthly (daily=false) or daily (daily=true)
// kline zip together with its .CHECKSUM file, verifies the SHA-256, and only
// then parses it. It fails closed: a missing zip or checksum
// is ErrNotFound and a mismatch is ErrChecksum - nothing is returned.
// ref is "<file> sha256:<hex>", stored in the coverage ledger so a corrected
// archive can be detected and re-ingested.
func FetchVerified(ctx context.Context, httpClient *http.Client, symbol, interval string, period time.Time, daily bool) (candles []entities.Candle, ref string, err error) {
	url := MonthURL(symbol, interval, period)
	if daily {
		url = DayURL(symbol, interval, period)
	}
	zipBytes, err := get(ctx, httpClient, url)
	if err != nil {
		return nil, "", err
	}
	sumBytes, err := get(ctx, httpClient, url+".CHECKSUM")
	if err != nil {
		return nil, "", err
	}
	want := strings.ToLower(strings.Fields(string(sumBytes) + " ")[0])
	sum := sha256.Sum256(zipBytes)
	if got := hex.EncodeToString(sum[:]); got != want {
		return nil, "", fmt.Errorf("%w for %s: got %s want %s", ErrChecksum, url, got, want)
	}
	candles, err = parseZipCSV(zipBytes, symbol, interval)
	if err != nil {
		return nil, "", fmt.Errorf("parsing %s: %w", url, err)
	}
	return candles, url[strings.LastIndex(url, "/")+1:] + " sha256:" + want, nil
}

func get(ctx context.Context, c *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", url, err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, fmt.Errorf("%w: %s", ErrNotFound, url)
	case resp.StatusCode != http.StatusOK:
		return nil, &StatusError{Code: resp.StatusCode, URL: url}
	}
	return io.ReadAll(resp.Body)
}

// StatusError is an unexpected HTTP status (429/5xx are retryable upstream).
type StatusError struct {
	Code int
	URL  string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("unexpected status %d fetching %s", e.Code, e.URL)
}
