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
	candleimport_repo "go-trade-bot/app/repository/candleimport"
	"go-trade-bot/app/usecase/candleimport"
	"go-trade-bot/internal/configuration"
	"go-trade-bot/internal/db"
	"go-trade-bot/internal/exchange"
)

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
	importRepo := candleimport_repo.NewRepository(database)
	exchangeClient, err := exchange.NewBinanceAdapter(cfg)
	if err != nil {
		log.Fatalf("failed to initialize exchange adapter: %v", err)
	}

	uc := candleimport.NewCandleImportUseCase(repo, importRepo, nil, exchangeClient)
	req := entities.ImportRequest{
		Symbols:    symbols,
		Timeframes: timeframes,
		From:       fromTime,
		To:         toTime,
		MinHistory: *minHistoryFlag,
	}

	summaries, runErr := uc.Run(context.Background(), req)
	
	for _, summary := range summaries {
		summaryJSON, _ := json.Marshal(summary)
		fmt.Println(string(summaryJSON))
	}

	if runErr != nil {
		log.Fatalf("Import completed with errors: %v", runErr)
	}
}
