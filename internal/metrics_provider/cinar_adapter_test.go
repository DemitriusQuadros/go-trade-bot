package metrics_provider_test

import (
	"math"
	"testing"
	"time"

	"go-trade-bot/internal/metrics_provider"

	"github.com/stretchr/testify/assert"
)

func TestCinarMetricsAdapter_AC1_ProfitFactorAndWinRate(t *testing.T) {
	adapter := metrics_provider.NewCinarMetricsAdapter()
	now := time.Now()

	trades := []metrics_provider.TradeLogEntry{
		{EntryTime: now, ExitTime: now.Add(time.Hour), Profit: 100, ExitReason: "take_profit"},
		{EntryTime: now, ExitTime: now.Add(time.Hour), Profit: 100, ExitReason: "take_profit"},
		{EntryTime: now, ExitTime: now.Add(time.Hour), Profit: 100, ExitReason: "take_profit"},
		{EntryTime: now, ExitTime: now.Add(time.Hour), Profit: 100, ExitReason: "take_profit"},
		{EntryTime: now, ExitTime: now.Add(time.Hour), Profit: 100, ExitReason: "take_profit"},
		{EntryTime: now, ExitTime: now.Add(time.Hour), Profit: 100, ExitReason: "take_profit"},
		{EntryTime: now, ExitTime: now.Add(time.Hour), Profit: -50, ExitReason: "stop_loss"},
		{EntryTime: now, ExitTime: now.Add(time.Hour), Profit: -50, ExitReason: "stop_loss"},
		{EntryTime: now, ExitTime: now.Add(time.Hour), Profit: -50, ExitReason: "stop_loss"},
		{EntryTime: now, ExitTime: now.Add(time.Hour), Profit: -50, ExitReason: "stop_loss"},
	}

	metrics := adapter.Compute(trades, 1000, 525600)
	assert.Equal(t, 60.0, metrics.WinRatePct)
	assert.Equal(t, 3.0, metrics.ProfitFactor)
	assert.Equal(t, 10, metrics.TotalTrades)
	assert.Equal(t, 40.0, metrics.TotalReturnPct) // (+600 - 200) / 1000 * 100
}

func TestCinarMetricsAdapter_AC2_ZeroLosingTrades_PositiveInf(t *testing.T) {
	adapter := metrics_provider.NewCinarMetricsAdapter()
	now := time.Now()

	trades := []metrics_provider.TradeLogEntry{
		{EntryTime: now, ExitTime: now.Add(time.Hour), Profit: 50, ExitReason: "take_profit"},
		{EntryTime: now, ExitTime: now.Add(time.Hour), Profit: 75, ExitReason: "take_profit"},
	}

	metrics := adapter.Compute(trades, 1000, 525600)
	assert.True(t, math.IsInf(metrics.ProfitFactor, 1))
	assert.Equal(t, 100.0, metrics.WinRatePct)
}

func TestCinarMetricsAdapter_AC3_MaxDrawdown(t *testing.T) {
	adapter := metrics_provider.NewCinarMetricsAdapter()
	now := time.Now()

	// Starting balance 1000
	// Trade 1: -300 -> 700 (dd = 30%)
	// Trade 2: +400 -> 1100 (peak = 1100)
	trades := []metrics_provider.TradeLogEntry{
		{EntryTime: now, ExitTime: now.Add(1 * time.Hour), Profit: -300},
		{EntryTime: now.Add(1 * time.Hour), ExitTime: now.Add(2 * time.Hour), Profit: 400},
	}

	metrics := adapter.Compute(trades, 1000, 525600)
	assert.InDelta(t, 30.0, metrics.MaxDrawdownPct, 0.01)
}

func TestCinarMetricsAdapter_AC4_AnnualizedSharpe(t *testing.T) {
	adapter := metrics_provider.NewCinarMetricsAdapter()
	now := time.Now()

	trades := []metrics_provider.TradeLogEntry{
		{EntryTime: now, ExitTime: now.Add(1 * time.Hour), Profit: 10},
		{EntryTime: now.Add(1 * time.Hour), ExitTime: now.Add(2 * time.Hour), Profit: 12},
		{EntryTime: now.Add(2 * time.Hour), ExitTime: now.Add(3 * time.Hour), Profit: 8},
		{EntryTime: now.Add(3 * time.Hour), ExitTime: now.Add(4 * time.Hour), Profit: 11},
	}

	periodsPerYear := 525600.0
	metrics := adapter.Compute(trades, 1000, periodsPerYear)
	assert.False(t, math.IsNaN(metrics.SharpeRatio))
	assert.True(t, metrics.SharpeRatio > 0)
}

func TestCinarMetricsAdapter_AC5_OpenAtEndExclusion(t *testing.T) {
	adapter := metrics_provider.NewCinarMetricsAdapter()
	now := time.Now()

	trades := []metrics_provider.TradeLogEntry{
		{EntryTime: now, ExitTime: now.Add(2 * time.Hour), Profit: 50, ExitReason: "take_profit"},
		{EntryTime: now.Add(2 * time.Hour), ExitTime: time.Time{}, Profit: 10, ExitReason: "open_at_end"},
	}

	metrics := adapter.Compute(trades, 1000, 525600)
	assert.Equal(t, 2, metrics.TotalTrades)
	assert.Equal(t, 2*time.Hour, metrics.AvgTradeDuration) // only closed trade duration
	assert.Equal(t, 6.0, metrics.TotalReturnPct)           // 50+10 = 60 -> 6%
}

func TestCinarMetricsAdapter_AC6_EmptyTradeLog(t *testing.T) {
	adapter := metrics_provider.NewCinarMetricsAdapter()

	metrics := adapter.Compute([]metrics_provider.TradeLogEntry{}, 1000, 525600)
	assert.Equal(t, 0, metrics.TotalTrades)
	assert.Equal(t, 0.0, metrics.WinRatePct)
	assert.Equal(t, 0.0, metrics.ProfitFactor)
	assert.Equal(t, 0.0, metrics.SharpeRatio)
	assert.Equal(t, 0.0, metrics.MaxDrawdownPct)
	assert.Equal(t, 0.0, metrics.TotalReturnPct)
	assert.Equal(t, time.Duration(0), metrics.AvgTradeDuration)
	assert.False(t, math.IsNaN(metrics.ProfitFactor))
	assert.False(t, math.IsInf(metrics.ProfitFactor, 0))
}

func TestCinarMetricsAdapter_AC7_SingleTrade(t *testing.T) {
	adapter := metrics_provider.NewCinarMetricsAdapter()
	now := time.Now()

	trades := []metrics_provider.TradeLogEntry{
		{EntryTime: now, ExitTime: now.Add(1 * time.Hour), Profit: 25, ExitReason: "take_profit"},
	}

	metrics := adapter.Compute(trades, 1000, 525600)
	assert.Equal(t, 1, metrics.TotalTrades)
	assert.Equal(t, 0.0, metrics.SharpeRatio) // 1 sample returns 0 Sharpe, no NaN
	assert.False(t, math.IsNaN(metrics.SharpeRatio))
}
