package binancearchive

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go-trade-bot/app/entities"
)

var (
	futuresMonthlyBaseURL     = "https://data.binance.vision/data/futures/um/monthly/klines"
	futuresDailyBaseURL       = "https://data.binance.vision/data/futures/um/daily/klines"
	fundingRateMonthlyBaseURL = "https://data.binance.vision/data/futures/um/monthly/fundingRate"
)

// FuturesMonthURL returns the data.binance.vision monthly USD-M futures klines archive URL.
func FuturesMonthURL(symbol, interval string, month time.Time) string {
	return fmt.Sprintf("%s/%s/%s/%s-%s-%s.zip", futuresMonthlyBaseURL, symbol, interval, symbol, interval, month.Format("2006-01"))
}

// FuturesDayURL returns the data.binance.vision daily USD-M futures klines archive URL.
func FuturesDayURL(symbol, interval string, day time.Time) string {
	return fmt.Sprintf("%s/%s/%s/%s-%s-%s.zip", futuresDailyBaseURL, symbol, interval, symbol, interval, day.Format("2006-01-02"))
}

// FundingRateMonthURL returns the data.binance.vision monthly USD-M futures funding rate archive URL.
func FundingRateMonthURL(symbol string, month time.Time) string {
	return fmt.Sprintf("%s/%s/%s-fundingRate-%s.zip", fundingRateMonthlyBaseURL, symbol, symbol, month.Format("2006-01"))
}

// FetchVerifiedFuturesKlines downloads one monthly (daily=false) or daily (daily=true)
// USD-M futures kline zip together with its .CHECKSUM file, verifies the SHA-256, and only
// then parses it.
func FetchVerifiedFuturesKlines(ctx context.Context, httpClient *http.Client, symbol, interval string, period time.Time, daily bool) (candles []entities.Candle, ref string, err error) {
	url := FuturesMonthURL(symbol, interval, period)
	if daily {
		url = FuturesDayURL(symbol, interval, period)
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

// ParseFundingRateZipCSV parses the CSV file inside a monthly funding rate zip.
// Binance's monthly funding rate CSV schema has columns:
// calc_time,funding_interval_hours,last_funding_rate (often with a header row).
// If a 2-column CSV is encountered, column index 1 is used for last_funding_rate.
func ParseFundingRateZipCSV(zipBytes []byte, symbol string) ([]entities.FundingRate, error) {
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
	reader.FieldsPerRecord = -1

	var rates []entities.FundingRate
	for {
		record, readErr := reader.Read()
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("csv read error: %w", readErr)
		}
		if len(record) < 2 {
			continue
		}

		calcTimeRaw, err := strconv.ParseInt(record[0], 10, 64)
		if err != nil {
			// Skip header rows where calc_time is not an integer
			continue
		}
		calcTimeMs := normalizeToMillis(calcTimeRaw)

		rateIdx := 2
		if len(record) == 2 {
			rateIdx = 1
		}
		rate, err := strconv.ParseFloat(record[rateIdx], 64)
		if err != nil {
			continue
		}

		rates = append(rates, entities.FundingRate{
			Symbol:      symbol,
			FundingTime: time.UnixMilli(calcTimeMs).UTC(),
			Rate:        rate,
		})
	}

	return rates, nil
}

// FetchVerifiedFundingRates downloads one monthly funding rate zip together with its
// .CHECKSUM file, verifies SHA-256, and parses the CSV into entities.FundingRate.
func FetchVerifiedFundingRates(ctx context.Context, httpClient *http.Client, symbol string, month time.Time) (rates []entities.FundingRate, ref string, err error) {
	url := FundingRateMonthURL(symbol, month)
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
	rates, err = ParseFundingRateZipCSV(zipBytes, symbol)
	if err != nil {
		return nil, "", fmt.Errorf("parsing %s: %w", url, err)
	}
	return rates, url[strings.LastIndex(url, "/")+1:] + " sha256:" + want, nil
}
