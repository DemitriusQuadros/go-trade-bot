package backtest_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"go-trade-bot/app/entities"
	"go-trade-bot/app/usecase/backtest"
	"go-trade-bot/internal/metrics_provider"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
)

func sampleTradeLog(n int) []metrics_provider.TradeLogEntry {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	trades := make([]metrics_provider.TradeLogEntry, n)
	for i := 0; i < n; i++ {
		profit := 10.0
		if i%2 == 0 {
			profit = -5.0
		}
		trades[i] = metrics_provider.TradeLogEntry{
			Symbol:     "BTCUSDT",
			EntryTime:  base.Add(time.Duration(i) * time.Hour),
			ExitTime:   base.Add(time.Duration(i)*time.Hour + time.Minute),
			Profit:     profit,
			ExitReason: "take_profit",
		}
	}
	return trades
}

func newUseCaseWithRun(t *testing.T, run entities.BacktestRun) (*backtest.BacktestUseCase, *mockBacktestRepo) {
	t.Helper()
	backtestRepo := &mockBacktestRepo{runs: []entities.BacktestRun{run}}
	stratRepo := &mockStrategyRepo{}
	candleRepo := &mockCandleRepo{}
	provider := metrics_provider.NewCinarMetricsAdapter()

	uc := backtest.NewBacktestUseCase(candleRepo, backtestRepo, stratRepo, provider, backtest.DefaultThresholdPolicy(), t.TempDir(), 20)
	return uc, backtestRepo
}

func TestRunMonteCarlo_FlatBacktestRun_Success(t *testing.T) {
	trades := sampleTradeLog(20)
	tradeBytes, _ := json.Marshal(trades)

	run := entities.BacktestRun{
		ID:             1,
		IsWalkForward:  false,
		InitialCapital: 1000,
		TradeLogJSON:   datatypes.JSON(tradeBytes),
	}
	uc, repo := newUseCaseWithRun(t, run)

	result, err := uc.RunMonteCarlo(context.Background(), 1, 200)
	require.NoError(t, err)
	assert.Equal(t, 200, result.Iterations)

	// Verify caching: the stored run now has MonteCarloJSON populated.
	stored, err := repo.GetByID(context.Background(), 1)
	require.NoError(t, err)
	assert.NotEmpty(t, stored.MonteCarloJSON)
}

func TestRunMonteCarlo_WalkForwardRun_ParsesWrappedPayload(t *testing.T) {
	trades := sampleTradeLog(10)
	payload := backtest.WalkForwardTradeLogPayload{Trades: trades}
	payloadBytes, _ := json.Marshal(payload)

	run := entities.BacktestRun{
		ID:             2,
		IsWalkForward:  true,
		InitialCapital: 500,
		TradeLogJSON:   datatypes.JSON(payloadBytes),
	}
	uc, _ := newUseCaseWithRun(t, run)

	result, err := uc.RunMonteCarlo(context.Background(), 2, 100)
	require.NoError(t, err)
	assert.Equal(t, 100, result.Iterations)
}

func TestRunMonteCarlo_RunNotFound(t *testing.T) {
	uc, _ := newUseCaseWithRun(t, entities.BacktestRun{ID: 1})
	_, err := uc.RunMonteCarlo(context.Background(), 999, 100)
	assert.ErrorIs(t, err, backtest.ErrBacktestRunNotFound)
}

func TestRunMonteCarlo_InsufficientTrades_422(t *testing.T) {
	trades := sampleTradeLog(1)
	tradeBytes, _ := json.Marshal(trades)
	run := entities.BacktestRun{ID: 3, InitialCapital: 1000, TradeLogJSON: datatypes.JSON(tradeBytes)}
	uc, _ := newUseCaseWithRun(t, run)

	_, err := uc.RunMonteCarlo(context.Background(), 3, 100)
	assert.ErrorIs(t, err, backtest.ErrInsufficientTradesForMonteCarlo)
}

func TestRunMonteCarlo_ZeroTrades_422(t *testing.T) {
	tradeBytes, _ := json.Marshal([]metrics_provider.TradeLogEntry{})
	run := entities.BacktestRun{ID: 4, InitialCapital: 1000, TradeLogJSON: datatypes.JSON(tradeBytes)}
	uc, _ := newUseCaseWithRun(t, run)

	_, err := uc.RunMonteCarlo(context.Background(), 4, 100)
	assert.ErrorIs(t, err, backtest.ErrInsufficientTradesForMonteCarlo)
}

func TestRunMonteCarlo_IterationsExceedsCap(t *testing.T) {
	trades := sampleTradeLog(20)
	tradeBytes, _ := json.Marshal(trades)
	run := entities.BacktestRun{ID: 5, InitialCapital: 1000, TradeLogJSON: datatypes.JSON(tradeBytes)}
	uc, _ := newUseCaseWithRun(t, run)

	_, err := uc.RunMonteCarlo(context.Background(), 5, 50000)
	assert.ErrorIs(t, err, backtest.ErrInvalidMonteCarloIterations)
}

func TestRunMonteCarlo_ZeroIterations_UsesDefault(t *testing.T) {
	trades := sampleTradeLog(20)
	tradeBytes, _ := json.Marshal(trades)
	run := entities.BacktestRun{ID: 6, InitialCapital: 1000, TradeLogJSON: datatypes.JSON(tradeBytes)}
	uc, _ := newUseCaseWithRun(t, run)

	result, err := uc.RunMonteCarlo(context.Background(), 6, 0)
	require.NoError(t, err)
	assert.Equal(t, backtest.MonteCarloDefaultIterations, result.Iterations)
}

// TestGetMonteCarlo_CachedResult_NoRecompute covers AC#5: GET returns the
// cached result without calling engine.RunMonteCarlo again - verified here
// by checking the cached JSON round-trips to the same Iterations value that
// was originally requested (a fresh compute with default iterations would
// produce a different Iterations value, making a stale cache observable).
func TestGetMonteCarlo_CachedResult_NoRecompute(t *testing.T) {
	trades := sampleTradeLog(20)
	tradeBytes, _ := json.Marshal(trades)
	run := entities.BacktestRun{ID: 7, InitialCapital: 1000, TradeLogJSON: datatypes.JSON(tradeBytes)}
	uc, _ := newUseCaseWithRun(t, run)

	_, err := uc.RunMonteCarlo(context.Background(), 7, 250)
	require.NoError(t, err)

	cached, err := uc.GetMonteCarlo(context.Background(), 7)
	require.NoError(t, err)
	assert.Equal(t, 250, cached.Iterations)
}

func TestGetMonteCarlo_NeverComputed_ReturnsSentinel(t *testing.T) {
	run := entities.BacktestRun{ID: 8}
	uc, _ := newUseCaseWithRun(t, run)

	_, err := uc.GetMonteCarlo(context.Background(), 8)
	assert.ErrorIs(t, err, backtest.ErrMonteCarloNotYetComputed)
}

func TestGetMonteCarlo_RunNotFound(t *testing.T) {
	uc, _ := newUseCaseWithRun(t, entities.BacktestRun{ID: 1})
	_, err := uc.GetMonteCarlo(context.Background(), 999)
	assert.ErrorIs(t, err, backtest.ErrBacktestRunNotFound)
}
