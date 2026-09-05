package backtest_test

import (
	"context"
	"math"
	"testing"
	"time"

	"go-trade-bot/app/entities"
	"go-trade-bot/app/usecase/backtest"
	_ "go-trade-bot/app/strategies/bollinger"
	"go-trade-bot/internal/exchange"
	"go-trade-bot/internal/metrics_provider"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
)

type mockCandleRepo struct {
	candles []entities.Candle
}

func (m *mockCandleRepo) Upsert(ctx context.Context, candles []entities.Candle) error {
	m.candles = append(m.candles, candles...)
	return nil
}

func (m *mockCandleRepo) Range(ctx context.Context, symbol, timeframe string, from, to time.Time) ([]exchange.Candle, error) {
	var res []exchange.Candle
	for _, c := range m.candles {
		if c.Symbol == symbol && c.Timeframe == timeframe && !c.OpenTime.Before(from) && c.OpenTime.Before(to) {
			res = append(res, exchange.Candle{
				Symbol:    c.Symbol,
				Timeframe: c.Timeframe,
				OpenTime:  c.OpenTime,
				Open:      c.Open,
				High:      c.High,
				Low:       c.Low,
				Close:     c.Close,
				Volume:    c.Volume,
			})
		}
	}
	return res, nil
}

func (m *mockCandleRepo) RangeBefore(ctx context.Context, symbol, timeframe string, before time.Time, limit int) ([]exchange.Candle, error) {
	var res []exchange.Candle
	for i := len(m.candles) - 1; i >= 0; i-- {
		c := m.candles[i]
		if c.Symbol == symbol && c.Timeframe == timeframe && (c.OpenTime.Before(before) || c.OpenTime.Equal(before)) {
			res = append([]exchange.Candle{{
				Symbol:    c.Symbol,
				Timeframe: c.Timeframe,
				OpenTime:  c.OpenTime,
				Open:      c.Open,
				High:      c.High,
				Low:       c.Low,
				Close:     c.Close,
				Volume:    c.Volume,
			}}, res...)
			if len(res) == limit {
				break
			}
		}
	}
	return res, nil
}

func (m *mockCandleRepo) LatestOpenTime(ctx context.Context, symbol, timeframe string) (time.Time, error) {
	if len(m.candles) == 0 {
		return time.Time{}, nil
	}
	return m.candles[len(m.candles)-1].OpenTime, nil
}

func (m *mockCandleRepo) Count(ctx context.Context, symbol, timeframe string) (int64, error) {
	return int64(len(m.candles)), nil
}

type mockStrategyRepo struct {
	strategy entities.Strategy
}

func (m *mockStrategyRepo) GetByID(ctx context.Context, id uint) (entities.Strategy, error) {
	return m.strategy, nil
}

type mockBacktestRepo struct {
	runs []entities.BacktestRun
}

func (m *mockBacktestRepo) Create(ctx context.Context, run *entities.BacktestRun) error {
	run.ID = uint(len(m.runs) + 1)
	m.runs = append(m.runs, *run)
	return nil
}

func (m *mockBacktestRepo) GetByID(ctx context.Context, id uint) (entities.BacktestRun, error) {
	for _, r := range m.runs {
		if r.ID == id {
			return r, nil
		}
	}
	return entities.BacktestRun{}, nil
}

func (m *mockBacktestRepo) ListByStrategy(ctx context.Context, strategyID uint) ([]entities.BacktestRun, error) {
	var res []entities.BacktestRun
	for _, r := range m.runs {
		if r.StrategyID == strategyID {
			res = append(res, r)
		}
	}
	return res, nil
}

func (m *mockBacktestRepo) ListRunsWithReport(ctx context.Context, strategyID uint) ([]entities.BacktestRun, error) {
	var res []entities.BacktestRun
	for _, r := range m.runs {
		if r.StrategyID == strategyID && r.HTMLReportPath != "" {
			res = append(res, r)
		}
	}
	return res, nil
}

func (m *mockBacktestRepo) Update(ctx context.Context, run entities.BacktestRun) error {
	for i, r := range m.runs {
		if r.ID == run.ID {
			m.runs[i] = run
			return nil
		}
	}
	return nil
}

type mockMetricsProvider struct {
	metrics metrics_provider.BacktestMetrics
}

func (m *mockMetricsProvider) Compute(trades []metrics_provider.TradeLogEntry, startingBalance, periodsPerYear float64) metrics_provider.BacktestMetrics {
	return m.metrics
}

func generateCandles(count int) []entities.Candle {
	baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	candles := make([]entities.Candle, count)
	for i := 0; i < count; i++ {
		candles[i] = entities.Candle{
			Symbol:    "BTCUSDT",
			Timeframe: "1m",
			OpenTime:  baseTime.Add(time.Duration(i) * time.Minute),
			Open:      100.0,
			High:      105.0,
			Low:       95.0,
			Close:     102.0,
			Volume:    10.0,
		}
	}
	return candles
}

func TestBacktestUseCase_ThresholdPolicy_PassAndFail(t *testing.T) {
	candles := generateCandles(100)
	candleRepo := &mockCandleRepo{candles: candles}
	stratRepo := &mockStrategyRepo{
		strategy: entities.Strategy{
			ID:           1,
			Name:         "Bollinger",
			StrategyName: "bollinger",
			StrategyConfiguration: entities.StrategyConfiguration{
				Cycle:         entities.OneMinute,
				Configuration: datatypes.JSON(`{"take_profit_pct": 1.0, "stop_loss_pct": 1.0}`),
			},
		},
	}
	backtestRepo := &mockBacktestRepo{}

	// Case 1: Sharpe=1.2, DD=15.0 -> Passed=true (AC#2)
	metricsProvider1 := &mockMetricsProvider{
		metrics: metrics_provider.BacktestMetrics{
			SharpeRatio:    1.2,
			MaxDrawdownPct: 15.0,
			TotalTrades:    5,
			ProfitFactor:   2.0,
		},
	}
	uc1 := backtest.NewBacktestUseCase(
		candleRepo, backtestRepo, stratRepo, metricsProvider1,
		backtest.ThresholdPolicy{MinSharpe: 0.8, MaxDrawdownPct: 20.0},
		t.TempDir(), 20,
	)

	run1, err := uc1.Run(context.Background(), backtest.RunRequest{
		StrategyID: 1,
		Symbol:     "BTCUSDT",
		Timeframe:  "1m",
		StartDate:  candles[0].OpenTime,
		EndDate:    candles[len(candles)-1].OpenTime.Add(time.Minute),
	})
	require.NoError(t, err)
	assert.True(t, run1.Passed, "run should pass with Sharpe 1.2 and DD 15%")
	assert.NotEmpty(t, run1.HTMLReportPath)

	// Case 2: Sharpe=1.2, DD=25.0 -> Passed=false (AC#3 - drawdown violation)
	metricsProvider2 := &mockMetricsProvider{
		metrics: metrics_provider.BacktestMetrics{
			SharpeRatio:    1.2,
			MaxDrawdownPct: 25.0,
			TotalTrades:    5,
		},
	}
	uc2 := backtest.NewBacktestUseCase(
		candleRepo, backtestRepo, stratRepo, metricsProvider2,
		backtest.ThresholdPolicy{MinSharpe: 0.8, MaxDrawdownPct: 20.0},
		t.TempDir(), 20,
	)

	run2, err := uc2.Run(context.Background(), backtest.RunRequest{
		StrategyID: 1,
		Symbol:     "BTCUSDT",
		Timeframe:  "1m",
		StartDate:  candles[0].OpenTime,
		EndDate:    candles[len(candles)-1].OpenTime.Add(time.Minute),
	})
	require.NoError(t, err)
	assert.False(t, run2.Passed, "run should fail when drawdown violates threshold")
}

func TestBacktestUseCase_InfProfitFactor_And_ReportFailureResilience(t *testing.T) {
	candles := generateCandles(100)
	candleRepo := &mockCandleRepo{candles: candles}
	stratRepo := &mockStrategyRepo{
		strategy: entities.Strategy{
			ID:           1,
			Name:         "Bollinger",
			StrategyName: "bollinger",
			StrategyConfiguration: entities.StrategyConfiguration{
				Cycle:         entities.OneMinute,
				Configuration: datatypes.JSON(`{"take_profit_pct": 1.0, "stop_loss_pct": 1.0}`),
			},
		},
	}
	backtestRepo := &mockBacktestRepo{}

	// +Inf Profit Factor & invalid directory to trigger report failure (AC#4 & AC#5)
	metricsProvider := &mockMetricsProvider{
		metrics: metrics_provider.BacktestMetrics{
			SharpeRatio:    1.5,
			MaxDrawdownPct: 5.0,
			ProfitFactor:   math.Inf(1),
			TotalTrades:    3,
		},
	}
	uc := backtest.NewBacktestUseCase(
		candleRepo, backtestRepo, stratRepo, metricsProvider,
		backtest.DefaultThresholdPolicy(),
		"/invalid/directory/that/cannot/be/created/\x00", 20,
	)

	run, err := uc.Run(context.Background(), backtest.RunRequest{
		StrategyID: 1,
		Symbol:     "BTCUSDT",
		Timeframe:  "1m",
		StartDate:  candles[0].OpenTime,
		EndDate:    candles[len(candles)-1].OpenTime.Add(time.Minute),
	})
	require.NoError(t, err)
	assert.Empty(t, run.HTMLReportPath, "HTMLReportPath should be empty if report generation failed")
	assert.True(t, run.Passed)
	assert.False(t, math.IsInf(run.ProfitFactor, 1), "ProfitFactor stored in DB must be finite sentinel")
	assert.Equal(t, math.MaxFloat64, run.ProfitFactor)
}

func TestBacktestUseCase_RetentionPruning(t *testing.T) {
	candles := generateCandles(100)
	candleRepo := &mockCandleRepo{candles: candles}
	stratRepo := &mockStrategyRepo{
		strategy: entities.Strategy{
			ID:           1,
			Name:         "Bollinger",
			StrategyName: "bollinger",
			StrategyConfiguration: entities.StrategyConfiguration{
				Cycle:         entities.OneMinute,
				Configuration: datatypes.JSON(`{"take_profit_pct": 1.0, "stop_loss_pct": 1.0}`),
			},
		},
	}
	backtestRepo := &mockBacktestRepo{}
	tempDir := t.TempDir()

	metricsProvider := &mockMetricsProvider{
		metrics: metrics_provider.BacktestMetrics{
			SharpeRatio:    1.0,
			MaxDrawdownPct: 10.0,
			TotalTrades:    1,
		},
	}

	// Retention limit of 2 reports
	uc := backtest.NewBacktestUseCase(
		candleRepo, backtestRepo, stratRepo, metricsProvider,
		backtest.DefaultThresholdPolicy(),
		tempDir, 2,
	)

	// Run 1
	run1, err := uc.Run(context.Background(), backtest.RunRequest{
		StrategyID: 1, Symbol: "BTCUSDT", Timeframe: "1m",
		StartDate: candles[0].OpenTime, EndDate: candles[len(candles)-1].OpenTime.Add(time.Minute),
	})
	require.NoError(t, err)
	require.FileExists(t, run1.HTMLReportPath)
	firstReportFile := run1.HTMLReportPath

	// Run 2
	run2, err := uc.Run(context.Background(), backtest.RunRequest{
		StrategyID: 1, Symbol: "BTCUSDT", Timeframe: "1m",
		StartDate: candles[0].OpenTime, EndDate: candles[len(candles)-1].OpenTime.Add(time.Minute),
	})
	require.NoError(t, err)
	require.FileExists(t, run2.HTMLReportPath)

	// Run 3 (triggers retention pruning of run1's report file)
	run3, err := uc.Run(context.Background(), backtest.RunRequest{
		StrategyID: 1, Symbol: "BTCUSDT", Timeframe: "1m",
		StartDate: candles[0].OpenTime, EndDate: candles[len(candles)-1].OpenTime.Add(time.Minute),
	})
	require.NoError(t, err)
	require.FileExists(t, run3.HTMLReportPath)

	// Verify run1 report file was deleted
	assert.NoFileExists(t, firstReportFile, "oldest report file should be deleted")
	// Verify run1 DB row still exists but has empty HTMLReportPath
	fetched1, err := backtestRepo.GetByID(context.Background(), run1.ID)
	require.NoError(t, err)
	assert.Equal(t, run1.ID, fetched1.ID)
	assert.Empty(t, fetched1.HTMLReportPath, "HTMLReportPath should be cleared on pruned DB row")
}
