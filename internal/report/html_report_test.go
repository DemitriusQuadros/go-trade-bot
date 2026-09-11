package report_test

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go-trade-bot/internal/metrics_provider"
	"go-trade-bot/internal/report"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTMLReport_Generate_AC1_ValidReport(t *testing.T) {
	tempDir := t.TempDir()
	now := time.Now()

	input := report.BacktestReportInput{
		RunID:        1,
		StrategyName: "Bollinger_Strategy",
		Symbol:       "BTCUSDT",
		StartDate:    now.Add(-30 * 24 * time.Hour),
		EndDate:      now,
		Metrics: metrics_provider.BacktestMetrics{
			SharpeRatio:      1.45,
			MaxDrawdownPct:   12.3,
			WinRatePct:       65.0,
			ProfitFactor:     2.15,
			TotalTrades:      15,
			AvgTradeDuration: 45 * time.Minute,
			TotalReturnPct:   18.5,
			EquityCurve: []metrics_provider.EquityPoint{
				{Time: now.Add(-30 * 24 * time.Hour), Value: 1000},
				{Time: now.Add(-15 * 24 * time.Hour), Value: 1100},
				{Time: now, Value: 1185},
			},
		},
		Trades: []metrics_provider.TradeLogEntry{
			{
				Symbol:     "BTCUSDT",
				EntryTime:  now.Add(-20 * time.Hour),
				ExitTime:   now.Add(-19 * time.Hour),
				EntryPrice: 50000,
				ExitPrice:  51000,
				Quantity:   0.1,
				Profit:     100,
				ExitReason: "take_profit",
			},
			{
				Symbol:     "BTCUSDT",
				EntryTime:  now.Add(-10 * time.Hour),
				ExitTime:   now.Add(-9 * time.Hour),
				EntryPrice: 51000,
				ExitPrice:  50500,
				Quantity:   0.1,
				Profit:     -50,
				ExitReason: "stop_loss",
			},
		},
	}

	path, err := report.Generate(tempDir, input)
	require.NoError(t, err)
	assert.FileExists(t, path)

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	html := string(content)

	assert.Contains(t, html, "Bollinger_Strategy")
	assert.Contains(t, html, "BTCUSDT")
	assert.Contains(t, html, "1.45")
	assert.Contains(t, html, "12.30%")
	assert.Contains(t, html, "65.0%")
	assert.Contains(t, html, "2.15")
	assert.Contains(t, html, "<svg")
	assert.Contains(t, html, "take_profit")
	assert.Contains(t, html, "stop_loss")

	// Verify no external script/link tags
	assert.NotContains(t, html, "http://")
	assert.NotContains(t, html, "https://")
}

func TestHTMLReport_Generate_AC2_ZeroTrades(t *testing.T) {
	tempDir := t.TempDir()
	now := time.Now()

	input := report.BacktestReportInput{
		RunID:        2,
		StrategyName: "Empty_Strategy",
		Symbol:       "ETHUSDT",
		StartDate:    now.Add(-7 * 24 * time.Hour),
		EndDate:      now,
		Metrics: metrics_provider.BacktestMetrics{
			TotalTrades: 0,
			EquityCurve: []metrics_provider.EquityPoint{
				{Time: now, Value: 1000},
			},
		},
		Trades: []metrics_provider.TradeLogEntry{},
	}

	path, err := report.Generate(tempDir, input)
	require.NoError(t, err)
	assert.FileExists(t, path)

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	html := string(content)

	assert.Contains(t, html, "No trades executed in this backtest run.")
}

func TestHTMLReport_Generate_AC3_InfiniteProfitFactor(t *testing.T) {
	tempDir := t.TempDir()
	now := time.Now()

	input := report.BacktestReportInput{
		RunID:        3,
		StrategyName: "Perfect_Strategy",
		Symbol:       "SOLUSDT",
		StartDate:    now.Add(-7 * 24 * time.Hour),
		EndDate:      now,
		Metrics: metrics_provider.BacktestMetrics{
			ProfitFactor: math.Inf(1),
			TotalTrades:  2,
		},
		Trades: []metrics_provider.TradeLogEntry{
			{Profit: 100},
		},
	}

	path, err := report.Generate(tempDir, input)
	require.NoError(t, err)

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	html := string(content)

	assert.Contains(t, html, "∞ (no losing trades)")
	assert.False(t, strings.Contains(html, "+Inf"))
}

func TestHTMLReport_Generate_AC4_OverwriteCleanly(t *testing.T) {
	tempDir := t.TempDir()
	now := time.Now()

	input := report.BacktestReportInput{
		RunID:        4,
		StrategyName: "Test_Strategy",
		Symbol:       "ADAUSDT",
		StartDate:    now.Add(-7 * 24 * time.Hour),
		EndDate:      now,
	}

	path1, err := report.Generate(tempDir, input)
	require.NoError(t, err)

	// Second generation with same run ID
	path2, err := report.Generate(tempDir, input)
	require.NoError(t, err)

	assert.Equal(t, path1, path2)
	assert.Equal(t, filepath.Join(tempDir, "Test_Strategy_ADAUSDT_4.html"), path1)
}
