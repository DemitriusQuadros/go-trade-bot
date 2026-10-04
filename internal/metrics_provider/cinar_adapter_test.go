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

// Regression (deploy gate, E2E run #62): per-trade returns were annualized
// with the candle timeframe's periods per year (sqrt(8760) for 1h), so 3
// consistent trades over ~6 months scored a Sharpe of ~348. Annualization is
// now by observed trades per year.
func TestCinarMetricsAdapter_SharpeAnnualizedByTradeFrequency(t *testing.T) {
	adapter := metrics_provider.NewCinarMetricsAdapter()
	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	trades := []metrics_provider.TradeLogEntry{
		{EntryTime: start, ExitTime: start.AddDate(0, 2, 0), Profit: 100},
		{EntryTime: start.AddDate(0, 2, 0), ExitTime: start.AddDate(0, 4, 0), Profit: 110},
		{EntryTime: start.AddDate(0, 4, 0), ExitTime: start.AddDate(0, 6, 0), Profit: 90},
	}

	m := adapter.Compute(trades, 1000, 8760) // 1h candles
	// Per-trade returns: 100/1000, 110/1100, 90/1210.
	r := []float64{0.1, 0.1, 90.0 / 1210.0}
	mean := (r[0] + r[1] + r[2]) / 3
	var ss float64
	for _, x := range r {
		ss += (x - mean) * (x - mean)
	}
	sd := math.Sqrt(ss / 2)
	years := start.AddDate(0, 6, 0).Sub(start).Hours() / (365.25 * 24)
	want := mean / sd * math.Sqrt(3/years)

	assert.InDelta(t, want, m.SharpeRatio, 1e-9)
	assert.Less(t, m.SharpeRatio, 20.0, "must not be scaled by candle periods (old value ~348)")
}

// Trades clustered within hours are not extrapolated to thousands of trades
// a year: the span is floored at 30 days.
func TestCinarMetricsAdapter_SharpeShortSpanFloor(t *testing.T) {
	adapter := metrics_provider.NewCinarMetricsAdapter()
	now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	trades := []metrics_provider.TradeLogEntry{
		{EntryTime: now, ExitTime: now.Add(time.Hour), Profit: 10},
		{EntryTime: now.Add(time.Hour), ExitTime: now.Add(2 * time.Hour), Profit: 12},
		{EntryTime: now.Add(2 * time.Hour), ExitTime: now.Add(3 * time.Hour), Profit: 8},
	}
	short := adapter.Compute(trades, 1000, 525600)

	spread := make([]metrics_provider.TradeLogEntry, len(trades))
	for i, tr := range trades {
		tr.EntryTime = now.AddDate(0, 0, 10*i)
		tr.ExitTime = now.AddDate(0, 0, 10*i+10)
		spread[i] = tr
	}
	month := adapter.Compute(spread, 1000, 525600) // exactly 30 days
	assert.InDelta(t, month.SharpeRatio, short.SharpeRatio, 1e-9)
}
