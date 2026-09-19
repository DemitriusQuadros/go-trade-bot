// Package binancearchive fetches and parses Binance's public historical
// kline archive (data.binance.vision) - static, pre-generated monthly CSV
// zip dumps per symbol/interval, going back to each symbol's listing date.
// This is the fast path for a one-time deep historical backfill: no REST
// rate limits, no pagination, months of 1m data in seconds per file. For
// ongoing/incremental imports (today onward), see app/usecase/candleimport
// instead, which talks to the live Binance kline REST API.
package binancearchive

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"go-trade-bot/app/entities"
)

// var, not const: archive_test.go overrides this to point at an
// httptest.Server so FetchMonth's HTTP/404 handling can be tested without a
// live network call.
var baseURL = "https://data.binance.vision/data/spot/monthly/klines"

// MonthURL returns the data.binance.vision monthly klines archive URL for
// the given symbol/interval/month. `interval` must be a Binance interval
// code (1m, 15m, 1h, ...) - the same strings this codebase already uses
// elsewhere (see app/usecase/candleimport's parseTimeframeDuration).
func MonthURL(symbol, interval string, month time.Time) string {
	monthStr := month.Format("2006-01")
	return fmt.Sprintf("%s/%s/%s/%s-%s-%s.zip", baseURL, symbol, interval, symbol, interval, monthStr)
}

// FetchMonth downloads and parses one monthly kline archive for (symbol,
// interval, month). Returns (nil, nil) - not an error - when the archive
// doesn't exist (HTTP 404): a symbol not yet listed that month, or the
// current/most recent month not archived yet. Callers should treat that as
// "skip this month", not a failure.
func FetchMonth(ctx context.Context, httpClient *http.Client, symbol, interval string, month time.Time) ([]entities.Candle, error) {
	url := MonthURL(symbol, interval, month)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d fetching %s", resp.StatusCode, url)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body for %s: %w", url, err)
	}

	candles, err := parseZipCSV(body, symbol, interval)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", url, err)
	}
	return candles, nil
}

// parseZipCSV parses the single CSV file inside a monthly kline zip. Column
// order per Binance's published archive schema: open_time, open, high, low,
// close, volume, close_time, quote_volume, trades, taker_buy_base,
// taker_buy_quote, ignore. open_time is Unix milliseconds. Only the first
// six columns are used; the rest aren't needed by entities.Candle.
func parseZipCSV(zipBytes []byte, symbol, interval string) ([]entities.Candle, error) {
	zr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return nil, fmt.Errorf("invalid zip archive: %w", err)
	}
	if len(zr.File) == 0 {
		return nil, fmt.Errorf("zip archive contains no files")
	}

	f, err := zr.File[0].Open()
	if err != nil {
		return nil, err
	}
	defer f.Close()

	reader := csv.NewReader(f)
	reader.FieldsPerRecord = -1 // some months ship a header row, most don't

	var candles []entities.Candle
	for {
		record, readErr := reader.Read()
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("csv read error: %w", readErr)
		}
		if len(record) < 6 {
			continue
		}

		openTimeRaw, err := strconv.ParseInt(record[0], 10, 64)
		if err != nil {
			// A header row's first field ("open_time") isn't an integer -
			// skip it rather than treating it as a parse failure.
			continue
		}
		openTimeMs := normalizeToMillis(openTimeRaw)
		open, err1 := strconv.ParseFloat(record[1], 64)
		high, err2 := strconv.ParseFloat(record[2], 64)
		low, err3 := strconv.ParseFloat(record[3], 64)
		closePrice, err4 := strconv.ParseFloat(record[4], 64)
		volume, err5 := strconv.ParseFloat(record[5], 64)
		if err1 != nil || err2 != nil || err3 != nil || err4 != nil || err5 != nil {
			continue
		}

		candles = append(candles, entities.Candle{
			Symbol:    symbol,
			Timeframe: interval,
			OpenTime:  time.UnixMilli(openTimeMs).UTC(),
			Open:      open,
			High:      high,
			Low:       low,
			Close:     closePrice,
			Volume:    volume,
		})
	}

	return candles, nil
}

// maxPlausibleMillis is 2100-01-01 in Unix milliseconds - comfortably past
// any real candle date, but three orders of magnitude below what the same
// moment looks like in microseconds. Binance's monthly kline archives
// switched open_time from milliseconds to microseconds for newer files
// (confirmed empirically: BTCUSDT/ETHUSDT 1h archives covering 2026 produced
// open_time values ~1000x too large, landing candles in the year 56000s
// instead of 2026, before this normalization existed) without any column or
// header change to signal it - the only way to tell them apart is magnitude.
const maxPlausibleMillis = 4_102_444_800_000

// normalizeToMillis converts a raw open_time field to Unix milliseconds
// regardless of whether the source archive expressed it in milliseconds or
// microseconds.
func normalizeToMillis(raw int64) int64 {
	if raw > maxPlausibleMillis {
		return raw / 1000
	}
	return raw
}
