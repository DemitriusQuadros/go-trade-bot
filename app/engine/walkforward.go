package engine

import (
	"context"
	"fmt"
	"time"

	"go-trade-bot/internal/metrics_provider"
)

type TimeRange struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

type WalkForwardConfig struct {
	TotalRange  TimeRange
	TrainWindow time.Duration
	TestWindow  time.Duration
	StepSize    time.Duration
}

type WalkForwardWindow struct {
	Index        int                              `json:"index"`
	TrainRange   TimeRange                        `json:"train_range"`
	TestRange    TimeRange                        `json:"test_range"`
	TrainMetrics metrics_provider.BacktestMetrics `json:"train_metrics"`
	TestMetrics  metrics_provider.BacktestMetrics `json:"test_metrics"`
}

type WalkForwardResult struct {
	Windows      []WalkForwardWindow              `json:"windows"`
	AggregateOOS metrics_provider.BacktestMetrics `json:"aggregate_oos"`
}

// WindowRunner is a function that runs a backtest for a specific range and returns the trade log.
type WindowRunner func(ctx context.Context, from, to time.Time) ([]metrics_provider.TradeLogEntry, error)

func RunWalkForward(
	ctx context.Context,
	cfg WalkForwardConfig,
	runner WindowRunner,
	metricsProvider metrics_provider.MetricsProvider,
	startingBalance float64,
	periodsPerYear float64,
) (WalkForwardResult, error) {
	minDuration := cfg.TrainWindow + cfg.TestWindow
	totalDuration := cfg.TotalRange.To.Sub(cfg.TotalRange.From)
	if totalDuration < minDuration {
		return WalkForwardResult{}, fmt.Errorf("insufficient total history: total duration %v is less than train+test window %v", totalDuration, minDuration)
	}

	if cfg.StepSize <= 0 {
		cfg.StepSize = cfg.TestWindow
	}

	var windows []WalkForwardWindow
	var allOOSTrades []metrics_provider.TradeLogEntry

	start := cfg.TotalRange.From
	index := 0

	for {
		trainEnd := start.Add(cfg.TrainWindow)
		testEnd := trainEnd.Add(cfg.TestWindow)

		if testEnd.After(cfg.TotalRange.To) {
			break
		}

		trainRange := TimeRange{From: start, To: trainEnd}
		testRange := TimeRange{From: trainEnd, To: testEnd}

		// Run Train
		trainTrades, err := runner(ctx, trainRange.From, trainRange.To)
		if err != nil {
			return WalkForwardResult{}, fmt.Errorf("failed running train window %d: %w", index, err)
		}
		trainMetrics := metricsProvider.Compute(trainTrades, startingBalance, periodsPerYear)

		// Run Test (Out of sample)
		testTrades, err := runner(ctx, testRange.From, testRange.To)
		if err != nil {
			return WalkForwardResult{}, fmt.Errorf("failed running test window %d: %w", index, err)
		}
		testMetrics := metricsProvider.Compute(testTrades, startingBalance, periodsPerYear)

		windows = append(windows, WalkForwardWindow{
			Index:        index,
			TrainRange:   trainRange,
			TestRange:    testRange,
			TrainMetrics: trainMetrics,
			TestMetrics:  testMetrics,
		})

		allOOSTrades = append(allOOSTrades, testTrades...)

		start = start.Add(cfg.StepSize)
		index++
	}

	if len(windows) == 0 {
		return WalkForwardResult{}, fmt.Errorf("no walk-forward windows generated from the provided range")
	}

	aggregateOOS := metricsProvider.Compute(allOOSTrades, startingBalance, periodsPerYear)

	return WalkForwardResult{
		Windows:      windows,
		AggregateOOS: aggregateOOS,
	}, nil
}
