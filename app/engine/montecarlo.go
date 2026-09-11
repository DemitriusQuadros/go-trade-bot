package engine

import (
	"math"
	"math/rand"
	"sort"
	"time"

	"go-trade-bot/internal/metrics_provider"
)

// MonteCarloConfig configures RunMonteCarlo.
type MonteCarloConfig struct {
	Iterations int   // e.g. 1000
	Seed       int64 // 0 => time-seeded (non-reproducible); explicit non-zero => reproducible for tests
}

type DistributionStats struct {
	Mean, Median, Min, Max, P5, P95 float64
}

type MonteCarloResult struct {
	Iterations              int                              `json:"iterations"`
	OriginalMetrics         metrics_provider.BacktestMetrics `json:"original_metrics"`
	SharpeDistribution      DistributionStats                `json:"sharpe_distribution"`
	MaxDrawdownDistribution DistributionStats                `json:"max_drawdown_distribution"`
	TotalReturnDistribution DistributionStats                `json:"total_return_distribution"`
}

// RunMonteCarlo shuffles `trades` cfg.Iterations times (Fisher-Yates, via
// math/rand's Shuffle - one shuffle per iteration, the SAME set of trades
// reordered, never resampled with replacement, since this is a
// reordering-robustness test, not a bootstrap), recomputes BacktestMetrics
// per shuffle via provider.Compute, and returns the resulting distribution
// across all three headline figures.
func RunMonteCarlo(
	trades []metrics_provider.TradeLogEntry,
	startingBalance float64,
	periodsPerYear float64,
	provider metrics_provider.MetricsProvider,
	cfg MonteCarloConfig,
) MonteCarloResult {
	if cfg.Iterations <= 0 {
		cfg.Iterations = 1000
	}

	seed := cfg.Seed
	if seed == 0 {
		seed = time.Now().UnixNano()
	}
	rng := rand.New(rand.NewSource(seed))

	original := provider.Compute(trades, startingBalance, periodsPerYear)

	sharpes := make([]float64, cfg.Iterations)
	drawdowns := make([]float64, cfg.Iterations)
	returns := make([]float64, cfg.Iterations)

	shuffled := make([]metrics_provider.TradeLogEntry, len(trades))
	for i := 0; i < cfg.Iterations; i++ {
		copy(shuffled, trades)
		rng.Shuffle(len(shuffled), func(a, b int) {
			shuffled[a], shuffled[b] = shuffled[b], shuffled[a]
		})

		m := provider.Compute(shuffled, startingBalance, periodsPerYear)
		sharpes[i] = m.SharpeRatio
		drawdowns[i] = m.MaxDrawdownPct
		returns[i] = m.TotalReturnPct
	}

	return MonteCarloResult{
		Iterations:              cfg.Iterations,
		OriginalMetrics:         original,
		SharpeDistribution:      distributionOf(sharpes),
		MaxDrawdownDistribution: distributionOf(drawdowns),
		TotalReturnDistribution: distributionOf(returns),
	}
}

func distributionOf(values []float64) DistributionStats {
	if len(values) == 0 {
		return DistributionStats{}
	}

	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)

	sum := 0.0
	for _, v := range sorted {
		sum += v
	}
	mean := sum / float64(len(sorted))

	return DistributionStats{
		Mean:   mean,
		Median: percentile(sorted, 50),
		Min:    sorted[0],
		Max:    sorted[len(sorted)-1],
		P5:     percentile(sorted, 5),
		P95:    percentile(sorted, 95),
	}
}

// percentile performs linear interpolation between closest ranks - `sorted`
// must already be ascending.
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 1 {
		return sorted[0]
	}

	idx := p / 100 * float64(len(sorted)-1)
	lower := int(math.Floor(idx))
	upper := int(math.Ceil(idx))
	if lower == upper {
		return sorted[lower]
	}

	frac := idx - float64(lower)
	return sorted[lower] + (sorted[upper]-sorted[lower])*frac
}
