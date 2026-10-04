package engine_test

import (
	"context"
	"testing"
	"time"

	"go-trade-bot/app/engine"
	"go-trade-bot/internal/metrics_provider"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunWalkForward_AC1_And_AC2(t *testing.T) {
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := startDate.Add(365 * 24 * time.Hour) // 12 months

	cfg := engine.WalkForwardConfig{
		TotalRange:  engine.TimeRange{From: startDate, To: endDate},
		TrainWindow: 90 * 24 * time.Hour,
		TestWindow:  30 * 24 * time.Hour,
		StepSize:    30 * 24 * time.Hour,
	}

	mockRunner := func(ctx context.Context, from, to time.Time) ([]metrics_provider.TradeLogEntry, error) {
		// Return 2 trades per window
		return []metrics_provider.TradeLogEntry{
			{Symbol: "BTCUSDT", EntryTime: from, ExitTime: from.Add(time.Hour), Profit: 10, ExitReason: "take_profit"},
			{Symbol: "BTCUSDT", EntryTime: from.Add(2 * time.Hour), ExitTime: from.Add(3 * time.Hour), Profit: -5, ExitReason: "stop_loss"},
		}, nil
	}

	metricsProvider := metrics_provider.NewCinarMetricsAdapter()
	result, err := engine.RunWalkForward(context.Background(), cfg, mockRunner, metricsProvider, 1000, 525600)
	require.NoError(t, err)

	// Verify windows count (365 - 120 = 245 days / 30 = 8-9 windows)
	assert.True(t, len(result.Windows) >= 8)

	// Verify TotalTrades in Aggregate equals sum of test trades
	totalTestTrades := 0
	for _, w := range result.Windows {
		totalTestTrades += w.TestMetrics.TotalTrades
	}
	assert.Equal(t, totalTestTrades, result.AggregateOOS.TotalTrades)
	assert.Equal(t, len(result.Windows)*2, result.AggregateOOS.TotalTrades)
}

func TestRunWalkForward_AC5_InsufficientHistory(t *testing.T) {
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := startDate.Add(30 * 24 * time.Hour) // 30 days total

	cfg := engine.WalkForwardConfig{
		TotalRange:  engine.TimeRange{From: startDate, To: endDate},
		TrainWindow: 90 * 24 * time.Hour,
		TestWindow:  30 * 24 * time.Hour,
		StepSize:    30 * 24 * time.Hour,
	}

	mockRunner := func(ctx context.Context, from, to time.Time) ([]metrics_provider.TradeLogEntry, error) {
		return nil, nil
	}

	metricsProvider := metrics_provider.NewCinarMetricsAdapter()
	_, err := engine.RunWalkForward(context.Background(), cfg, mockRunner, metricsProvider, 1000, 525600)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "insufficient total history")
}
