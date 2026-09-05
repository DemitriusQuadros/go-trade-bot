package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"go-trade-bot/app/entities"
	"go-trade-bot/app/repository/candle"
	"go-trade-bot/internal/configuration"
	"go-trade-bot/internal/db"
	"go-trade-bot/internal/exchange"
)

type ImportSummary struct {
	Symbol          string  `json:"symbol"`
	Timeframe       string  `json:"timeframe"`
	CandlesImported int     `json:"candles_imported"`
	DurationSeconds float64 `json:"duration_seconds"`
	GapsDetected    int     `json:"gaps_detected"`
}

func parseTimeframeDuration(tf string) (time.Duration, error) {
	switch tf {
	case "1m":
		return time.Minute, nil
	case "3m":
		return 3 * time.Minute, nil
	case "5m":
		return 5 * time.Minute, nil
	case "15m":
		return 15 * time.Minute, nil
	case "30m":
		return 30 * time.Minute, nil
	case "1h":
		return time.Hour, nil
	case "2h":
		return 2 * time.Hour, nil
	case "4h":
		return 4 * time.Hour, nil
	case "6h":
		return 6 * time.Hour, nil
	case "8h":
		return 8 * time.Hour, nil
	case "12h":
		return 12 * time.Hour, nil
	case "1d":
		return 24 * time.Hour, nil
	case "3d":
		return 3 * 24 * time.Hour, nil
	case "1w":
		return 7 * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("unsupported timeframe %q", tf)
	}
}

func main() {
	symbolsFlag := flag.String("symbols", "", "comma-separated list of symbols (e.g. BTCUSDT,ETHUSDT)")
	timeframesFlag := flag.String("timeframes", "", "comma-separated list of timeframes (e.g. 1m,15m,1d)")
	fromFlag := flag.String("from", "", "RFC3339 start timestamp (e.g. 2024-01-01T00:00:00Z)")
	toFlag := flag.String("to", "", "RFC3339 end timestamp (default: now)")
	minHistoryFlag := flag.Duration("min-history", 17520*time.Hour, "minimum expected history duration warning threshold")

	flag.Parse()

	if *symbolsFlag == "" || *timeframesFlag == "" || *fromFlag == "" {
		fmt.Fprintln(os.Stderr, "Usage: candleimport -symbols <symbols> -timeframes <tfs> -from <RFC3339> [-to <RFC3339>] [-min-history <duration>]")
		flag.PrintDefaults()
		os.Exit(1)
	}

	fromTime, err := time.Parse(time.RFC3339, *fromFlag)
	if err != nil {
		log.Fatalf("invalid -from format %q: %v", *fromFlag, err)
	}

	toTime := time.Now().UTC()
	if *toFlag != "" {
		toTime, err = time.Parse(time.RFC3339, *toFlag)
		if err != nil {
			log.Fatalf("invalid -to format %q: %v", *toFlag, err)
		}
	}

	if toTime.Before(fromTime) {
		log.Fatalf("-to (%v) cannot be before -from (%v)", toTime, fromTime)
	}

	if toTime.Sub(fromTime) < *minHistoryFlag {
		log.Printf("[WARN] requested range (%v) is shorter than -min-history threshold (%v)", toTime.Sub(fromTime), *minHistoryFlag)
	}

	symbols := strings.Split(*symbolsFlag, ",")
	timeframes := strings.Split(*timeframesFlag, ",")

	cfg := configuration.NewConfiguration()
	database, err := db.NewDatabase(cfg)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}

	if err := database.AutoMigrate(&entities.Candle{}); err != nil {
		log.Fatalf("failed to migrate candles table: %v", err)
	}

	repo := candle.NewCandleRepository(database)
	exchangeClient, err := exchange.NewBinanceAdapter(cfg)
	if err != nil {
		log.Fatalf("failed to initialize exchange adapter: %v", err)
	}

	ctx := context.Background()
	totalSuccess := 0
	totalJobs := len(symbols) * len(timeframes)

	for _, rawSymbol := range symbols {
		symbol := strings.TrimSpace(rawSymbol)
		if symbol == "" {
			continue
		}

		for _, rawTf := range timeframes {
			tf := strings.TrimSpace(rawTf)
			if tf == "" {
				continue
			}

			start := time.Now()
			importedCount, gapsCount, err := importSymbolTimeframe(ctx, exchangeClient, repo, symbol, tf, fromTime, toTime)
			duration := time.Since(start).Seconds()

			if err != nil {
				log.Printf("[ERROR] failed import for %s/%s: %v", symbol, tf, err)
				continue
			}

			totalSuccess++
			summary := ImportSummary{
				Symbol:          symbol,
				Timeframe:       tf,
				CandlesImported: importedCount,
				DurationSeconds: duration,
				GapsDetected:    gapsCount,
			}
			summaryJSON, _ := json.Marshal(summary)
			fmt.Println(string(summaryJSON))
		}
	}

	if totalSuccess == 0 && totalJobs > 0 {
		os.Exit(1)
	}
}

func importSymbolTimeframe(
	ctx context.Context,
	client exchange.ExchangeClient,
	repo candle.CandleRepository,
	symbol, timeframe string,
	from, to time.Time,
) (int, int, error) {
	tfDuration, err := parseTimeframeDuration(timeframe)
	if err != nil {
		return 0, 0, err
	}

	latestTime, err := repo.LatestOpenTime(ctx, symbol, timeframe)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to check latest open time: %w", err)
	}

	actualFrom := from
	if !latestTime.IsZero() && latestTime.After(from) {
		// Resume with 10-candle overlap
		overlap := latestTime.Add(-10 * tfDuration)
		if overlap.After(from) {
			actualFrom = overlap
		}
	}

	totalImported := 0
	currentStart := actualFrom
	const batchLimit = 1000

	for currentStart.Before(to) {
		var klines []exchange.Candle
		var fetchErr error

		// Retry with backoff for rate limits
		for retries := 0; retries < 3; retries++ {
			klines, fetchErr = client.ListKline(ctx, symbol, timeframe, batchLimit)
			if fetchErr != nil {
				if strings.Contains(fetchErr.Error(), "429") || strings.Contains(fetchErr.Error(), "Rate limit") {
					log.Printf("[WARN] rate limited for %s/%s, backing off 5s...", symbol, timeframe)
					time.Sleep(5 * time.Second)
					continue
				}
				break
			}
			break
		}

		if fetchErr != nil {
			return totalImported, 0, fmt.Errorf("ListKline failed for %s/%s: %w", symbol, timeframe, fetchErr)
		}

		if len(klines) == 0 {
			break
		}

		// Filter and convert candles within target range
		var entityCandles []entities.Candle
		var maxOpenTime time.Time

		for _, k := range klines {
			if k.OpenTime.Before(currentStart) {
				continue
			}
			if k.OpenTime.After(to) || k.OpenTime.Equal(to) {
				continue
			}

			entityCandles = append(entityCandles, entities.Candle{
				Symbol:    symbol,
				Timeframe: timeframe,
				OpenTime:  k.OpenTime,
				Open:      k.Open,
				High:      k.High,
				Low:       k.Low,
				Close:     k.Close,
				Volume:    k.Volume,
			})

			if k.OpenTime.After(maxOpenTime) {
				maxOpenTime = k.OpenTime
			}
		}

		if len(entityCandles) > 0 {
			if err := repo.Upsert(ctx, entityCandles); err != nil {
				return totalImported, 0, fmt.Errorf("upsert failed for %s/%s: %w", symbol, timeframe, err)
			}
			totalImported += len(entityCandles)
		}

		if maxOpenTime.IsZero() || maxOpenTime.Before(currentStart) {
			// Advance to prevent infinite loop
			currentStart = currentStart.Add(time.Duration(batchLimit) * tfDuration)
		} else {
			currentStart = maxOpenTime.Add(tfDuration)
		}
	}

	// Gap detection
	storedCandles, err := repo.Range(ctx, symbol, timeframe, from, to)
	if err != nil {
		return totalImported, 0, nil
	}

	gapsDetected := 0
	for i := 0; i < len(storedCandles)-1; i++ {
		diff := storedCandles[i+1].OpenTime.Sub(storedCandles[i].OpenTime)
		if diff > tfDuration {
			gapsDetected++
			log.Printf("[WARN] Gap detected for %s/%s between %v and %v (gap: %v)",
				symbol, timeframe, storedCandles[i].OpenTime, storedCandles[i+1].OpenTime, diff)
		}
	}

	return totalImported, gapsDetected, nil
}
