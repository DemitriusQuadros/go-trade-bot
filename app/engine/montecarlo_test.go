package engine_test

import (
	"testing"
	"time"

	"go-trade-bot/app/engine"
	"go-trade-bot/internal/metrics_provider"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sampleTrades() []metrics_provider.TradeLogEntry {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	profits := []float64{50, -20, 30, -10, 40, -15, 25, -5, 60, -30, 15, -10, 20, -5, 10, -20, 45, -10, 30, -15}
	trades := make([]metrics_provider.TradeLogEntry, len(profits))
	for i, p := range profits {
		trades[i] = metrics_provider.TradeLogEntry{
			Symbol:     "BTCUSDT",
			EntryTime:  base.Add(time.Duration(i*2) * time.Hour),
			ExitTime:   base.Add(time.Duration(i*2+1) * time.Hour),
			EntryPrice: 100,
			ExitPrice:  100 + p,
			Quantity:   1,
			Profit:     p,
			ExitReason: "take_profit",
		}
	}
	return trades
}

func totalProfit(trades []metrics_provider.TradeLogEntry) float64 {
	sum := 0.0
	for _, t := range trades {
		sum += t.Profit
	}
	return sum
}

// TestRunMonteCarlo_TotalReturnCollapsesToSingleValue covers AC#1: since
// Monte Carlo only reorders (never resamples) the same trade set, every
// iteration's final equity must land at startingBalance + total profit -
// the TotalReturnDistribution should collapse to (approximately) one value.
func TestRunMonteCarlo_TotalReturnCollapsesToSingleValue(t *testing.T) {
	trades := sampleTrades()
	startingBalance := 1000.0
	expectedReturn := (totalProfit(trades) / startingBalance) * 100.0

	provider := metrics_provider.NewCinarMetricsAdapter()
	result := engine.RunMonteCarlo(trades, startingBalance, 525600, provider, engine.MonteCarloConfig{
		Iterations: 1000,
		Seed:       42,
	})

	assert.InDelta(t, expectedReturn, result.TotalReturnDistribution.Mean, 1e-6)
	assert.InDelta(t, expectedReturn, result.TotalReturnDistribution.Min, 1e-6)
	assert.InDelta(t, expectedReturn, result.TotalReturnDistribution.Max, 1e-6)
}

// TestRunMonteCarlo_WorstDrawdownAtLeastAsOriginal covers AC#2: the worst
// simulated drawdown across all reorderings must be >= the original
// chronological sequence's observed drawdown.
func TestRunMonteCarlo_WorstDrawdownAtLeastAsOriginal(t *testing.T) {
	trades := sampleTrades()
	provider := metrics_provider.NewCinarMetricsAdapter()

	original := provider.Compute(trades, 1000, 525600)
	result := engine.RunMonteCarlo(trades, 1000, 525600, provider, engine.MonteCarloConfig{
		Iterations: 2000,
		Seed:       7,
	})

	assert.GreaterOrEqual(t, result.MaxDrawdownDistribution.Max, original.MaxDrawdownPct)
}

// TestRunMonteCarlo_SeededReproducibility covers AC#3.
func TestRunMonteCarlo_SeededReproducibility(t *testing.T) {
	trades := sampleTrades()
	provider := metrics_provider.NewCinarMetricsAdapter()
	cfg := engine.MonteCarloConfig{Iterations: 500, Seed: 12345}

	r1 := engine.RunMonteCarlo(trades, 1000, 525600, provider, cfg)
	r2 := engine.RunMonteCarlo(trades, 1000, 525600, provider, cfg)

	assert.Equal(t, r1, r2, "identical seed and inputs must produce byte-identical results")
}

// TestRunMonteCarlo_ZeroSeed_VariesAcrossCalls covers AC#4: Seed=0 must
// produce fresh (time-seeded) randomness, not the same shuffle every time.
func TestRunMonteCarlo_ZeroSeed_VariesAcrossCalls(t *testing.T) {
	trades := sampleTrades()
	provider := metrics_provider.NewCinarMetricsAdapter()
	cfg := engine.MonteCarloConfig{Iterations: 500, Seed: 0}

	r1 := engine.RunMonteCarlo(trades, 1000, 525600, provider, cfg)
	time.Sleep(2 * time.Millisecond) // ensure a distinct time-derived seed
	r2 := engine.RunMonteCarlo(trades, 1000, 525600, provider, cfg)

	assert.NotEqual(t, r1.SharpeDistribution, r2.SharpeDistribution,
		"two zero-seeded runs should not be guaranteed identical (best-effort: statistically near-certain with 500 iterations over 20 trades)")
}

// TestRunMonteCarlo_DefaultIterations covers the "0 => default 1000" rule.
func TestRunMonteCarlo_DefaultIterations(t *testing.T) {
	trades := sampleTrades()
	provider := metrics_provider.NewCinarMetricsAdapter()

	result := engine.RunMonteCarlo(trades, 1000, 525600, provider, engine.MonteCarloConfig{Seed: 1})
	require.Equal(t, 1000, result.Iterations)
}

// TestRunMonteCarlo_OriginalMetricsPreserved verifies OriginalMetrics is the
// unshuffled Compute() result, useful for the caller's before/after
// comparison.
func TestRunMonteCarlo_OriginalMetricsPreserved(t *testing.T) {
	trades := sampleTrades()
	provider := metrics_provider.NewCinarMetricsAdapter()

	expected := provider.Compute(trades, 1000, 525600)
	result := engine.RunMonteCarlo(trades, 1000, 525600, provider, engine.MonteCarloConfig{Iterations: 100, Seed: 9})

	assert.Equal(t, expected, result.OriginalMetrics)
}
