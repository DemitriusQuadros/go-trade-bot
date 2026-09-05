package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"time"

	"go-trade-bot/app/entities"
	backtest_repo "go-trade-bot/app/repository/backtest"
	candle_repo "go-trade-bot/app/repository/candle"
	strategy_repo "go-trade-bot/app/repository/strategy"
	_ "go-trade-bot/app/strategies/bollinger"
	_ "go-trade-bot/app/strategies/grid"
	_ "go-trade-bot/app/strategies/scalping"
	usecase "go-trade-bot/app/usecase/backtest"
	"go-trade-bot/internal/configuration"
	"go-trade-bot/internal/db"
	"go-trade-bot/internal/metrics_provider"
)

func parseDate(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("could not parse date %q (expected RFC3339 or YYYY-MM-DD)", s)
}

func main() {
	strategyIDFlag := flag.Uint("strategy-id", 0, "Strategy ID to backtest")
	symbolFlag := flag.String("symbol", "BTCUSDT", "Trading pair symbol")
	timeframeFlag := flag.String("timeframe", "1m", "Candle timeframe (e.g. 1m, 5m, 1h)")
	fromFlag := flag.String("from", "", "Start date (RFC3339 or YYYY-MM-DD)")
	toFlag := flag.String("to", "", "End date (RFC3339 or YYYY-MM-DD)")
	capitalFlag := flag.Float64("capital", 1000.0, "Initial capital (default: 1000)")
	walkForwardFlag := flag.Bool("walkforward", false, "Run walk-forward validation")
	trainMonthsFlag := flag.Int("train-months", 3, "Train window duration in months (walk-forward only)")
	testMonthsFlag := flag.Int("test-months", 1, "Test window duration in months (walk-forward only)")
	stepMonthsFlag := flag.Int("step-months", 1, "Step duration in months (walk-forward only)")
	reportsDirFlag := flag.String("reports-dir", "reports", "Directory to output HTML reports")
	outputJSONFlag := flag.Bool("json", false, "Output results as JSON")
	flag.Parse()

	if *strategyIDFlag == 0 {
		log.Fatalf("Error: --strategy-id is required")
	}
	if *fromFlag == "" || *toFlag == "" {
		log.Fatalf("Error: --from and --to flags are required")
	}

	fromDate, err := parseDate(*fromFlag)
	if err != nil {
		log.Fatalf("Invalid --from: %v", err)
	}
	toDate, err := parseDate(*toFlag)
	if err != nil {
		log.Fatalf("Invalid --to: %v", err)
	}

	config := configuration.NewConfiguration()
	database, err := db.NewDatabase(config)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	if err := database.AutoMigrate(&entities.Candle{}, &entities.BacktestRun{}); err != nil {
		log.Fatalf("Failed to migrate database: %v", err)
	}

	candleRepo := candle_repo.NewCandleRepository(database)
	backtestRepo := backtest_repo.NewBacktestRepository(database)
	strategyRepo := strategy_repo.NewStrategyRepository(database)
	metricsAdapter := metrics_provider.NewCinarMetricsAdapter()

	policy := usecase.DefaultThresholdPolicy()
	uc := usecase.NewBacktestUseCase(
		candleRepo,
		backtestRepo,
		strategyRepo,
		metricsAdapter,
		policy,
		*reportsDirFlag,
		20,
	)

	ctx := context.Background()
	var run entities.BacktestRun

	if *walkForwardFlag {
		req := usecase.WalkForwardRequest{
			StrategyID:     *strategyIDFlag,
			Symbol:         *symbolFlag,
			Timeframe:      *timeframeFlag,
			StartDate:      fromDate,
			EndDate:        toDate,
			InitialCapital: *capitalFlag,
			TrainMonths:    *trainMonthsFlag,
			TestMonths:     *testMonthsFlag,
			StepMonths:     *stepMonthsFlag,
		}
		run, err = uc.RunWalkForward(ctx, req)
	} else {
		req := usecase.RunRequest{
			StrategyID:     *strategyIDFlag,
			Symbol:         *symbolFlag,
			Timeframe:      *timeframeFlag,
			StartDate:      fromDate,
			EndDate:        toDate,
			InitialCapital: *capitalFlag,
		}
		run, err = uc.Run(ctx, req)
	}

	if err != nil {
		log.Fatalf("Backtest execution failed: %v", err)
	}

	if *outputJSONFlag {
		var pf any = run.ProfitFactor
		if math.IsInf(run.ProfitFactor, 1) || run.ProfitFactor == math.MaxFloat64 || run.ProfitFactor >= 1e15 {
			pf = "Infinity"
		}
		outMap := map[string]any{
			"id":               run.ID,
			"strategy_id":      run.StrategyID,
			"symbol":           run.Symbol,
			"start_date":       run.StartDate,
			"end_date":         run.EndDate,
			"is_walk_forward":  run.IsWalkForward,
			"sharpe":           run.Sharpe,
			"max_drawdown_pct": run.MaxDrawdownPct,
			"win_rate_pct":     run.WinRatePct,
			"profit_factor":    pf,
			"total_trades":     run.TotalTrades,
			"total_return_pct": run.TotalReturnPct,
			"passed":           run.Passed,
			"html_report_path": run.HTMLReportPath,
		}
		b, _ := json.MarshalIndent(outMap, "", "  ")
		fmt.Println(string(b))
	} else {
		fmt.Println("=================================================")
		fmt.Printf(" BACKTEST RESULTS: Strategy #%d (%s)\n", run.StrategyID, run.Symbol)
		fmt.Println("=================================================")
		fmt.Printf(" Range:        %s to %s\n", run.StartDate.Format("2006-01-02"), run.EndDate.Format("2006-01-02"))
		fmt.Printf(" Mode:         %s\n", map[bool]string{true: "Walk-Forward OOS", false: "Single Backtest"}[run.IsWalkForward])
		fmt.Printf(" Total Return: %.2f%%\n", run.TotalReturnPct)
		fmt.Printf(" Sharpe Ratio: %.2f\n", run.Sharpe)
		fmt.Printf(" Max Drawdown: %.2f%%\n", run.MaxDrawdownPct)
		fmt.Printf(" Win Rate:     %.1f%%\n", run.WinRatePct)
		if math.IsInf(run.ProfitFactor, 1) || run.ProfitFactor == math.MaxFloat64 || run.ProfitFactor >= 1e15 {
			fmt.Println(" Profit Factor: ∞ (no losing trades)")
		} else {
			fmt.Printf(" Profit Factor: %.2f\n", run.ProfitFactor)
		}
		fmt.Printf(" Total Trades: %d\n", run.TotalTrades)
		fmt.Printf(" Thresholds:   %s\n", map[bool]string{true: "PASSED", false: "FAILED"}[run.Passed])
		if run.HTMLReportPath != "" {
			fmt.Printf(" Report Path:  %s\n", run.HTMLReportPath)
		}
		fmt.Println("=================================================")
	}

	if !run.Passed {
		os.Exit(1)
	}
}
