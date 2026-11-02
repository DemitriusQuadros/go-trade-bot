package backtest_test

import (
	"context"
	"testing"
	"time"

	"go-trade-bot/app/engine"
	"go-trade-bot/app/entities"
	"go-trade-bot/app/usecase/backtest"
	"go-trade-bot/internal/metrics_provider"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
)

type mockFundingRateRepo struct {
	rates        []entities.FundingRate
	calledSymbol string
	calledFrom   time.Time
	calledTo     time.Time
}

func (m *mockFundingRateRepo) ListInRange(ctx context.Context, symbol string, from, to time.Time) ([]entities.FundingRate, error) {
	m.calledSymbol = symbol
	m.calledFrom = from
	m.calledTo = to
	return m.rates, nil
}

func TestBacktestUseCase_HistoricalFundingRateLookup_Wired(t *testing.T) {
	candleRepo := &mockCandleRepo{}
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 10; i++ {
		candleRepo.candles = append(candleRepo.candles, entities.Candle{
			Symbol:    "BTCUSDT",
			Timeframe: "1h",
			OpenTime:  t0.Add(time.Duration(i) * time.Hour),
			Open:      50000,
			High:      50500,
			Low:       49500,
			Close:     50000,
			Volume:    100,
		})
	}

	strat := entities.Strategy{
		ID:           1,
		Name:         "test-script",
		StrategyName: "script",
		ScriptSource: noTradeScript,
		StrategyConfiguration: entities.StrategyConfiguration{
			Cycle: entities.OneHour,
			Configuration: datatypes.JSON(`{
				"template": {
					"entry": {"conditions": [], "combinator": "AND"},
					"exit": {"conditions": [], "combinator": "OR"}
				}
			}`),
		},
	}
	stratRepo := &mockStrategyRepo{strategy: strat}
	backtestRepo := &mockBacktestRepo{}
	mp := &mockMetricsProvider{
		metrics: metrics_provider.BacktestMetrics{
			SharpeRatio:    1.5,
			MaxDrawdownPct: 5.0,
			TotalTrades:    0,
			ProfitFactor:   1.0,
		},
	}

	uc := backtest.NewBacktestUseCase(
		candleRepo,
		backtestRepo,
		stratRepo,
		mp,
		backtest.DefaultThresholdPolicy(),
		t.TempDir(),
		10,
	)

	fundingRepo := &mockFundingRateRepo{
		rates: []entities.FundingRate{
			{
				Symbol:      "BTCUSDT",
				FundingTime: t0.Add(8 * time.Hour),
				Rate:        0.0002,
			},
		},
	}
	uc.SetFundingRateRepository(fundingRepo)

	req := backtest.RunRequest{
		StrategyID:     strat.ID,
		Symbol:         "BTCUSDT",
		Timeframe:      "1h",
		StartDate:      t0,
		EndDate:        t0.Add(10 * time.Hour),
		InitialCapital: 10000,
		FillPolicy:     engine.FillPolicy{SlippagePct: 0, FillDelay: 0},
	}

	run, err := uc.Run(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, entities.BacktestDone, run.Status)

	assert.Equal(t, "BTCUSDT", fundingRepo.calledSymbol)
	assert.Equal(t, req.StartDate, fundingRepo.calledFrom)
	assert.Equal(t, req.EndDate, fundingRepo.calledTo)
}
