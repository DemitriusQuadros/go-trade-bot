package metrics_provider

import (
	"math"
	"time"
)

type CinarMetricsAdapter struct{}

func NewCinarMetricsAdapter() *CinarMetricsAdapter {
	return &CinarMetricsAdapter{}
}

func (a *CinarMetricsAdapter) Compute(trades []TradeLogEntry, startingBalance float64, periodsPerYear float64) BacktestMetrics {
	if startingBalance <= 0 {
		startingBalance = 1000.0
	}

	if len(trades) == 0 {
		return BacktestMetrics{
			SharpeRatio:      0,
			MaxDrawdownPct:   0,
			WinRatePct:       0,
			ProfitFactor:     0,
			TotalTrades:      0,
			AvgTradeDuration: 0,
			TotalReturnPct:   0,
			EquityCurve: []EquityPoint{
				{Time: time.Now(), Value: startingBalance},
			},
		}
	}

	currentEquity := startingBalance
	equityCurve := make([]EquityPoint, 0, len(trades)+1)
	if !trades[0].EntryTime.IsZero() {
		equityCurve = append(equityCurve, EquityPoint{Time: trades[0].EntryTime, Value: startingBalance})
	} else {
		equityCurve = append(equityCurve, EquityPoint{Time: time.Now(), Value: startingBalance})
	}

	winningTrades := 0
	losingTrades := 0
	grossProfit := 0.0
	grossLoss := 0.0
	var totalDuration time.Duration
	closedTradesCount := 0

	for _, t := range trades {
		currentEquity += t.Profit
		ptTime := t.ExitTime
		if ptTime.IsZero() {
			ptTime = t.EntryTime
		}
		equityCurve = append(equityCurve, EquityPoint{Time: ptTime, Value: currentEquity})

		if t.Profit > 0 {
			winningTrades++
			grossProfit += t.Profit
		} else if t.Profit < 0 {
			losingTrades++
			grossLoss += math.Abs(t.Profit)
		}

		if t.ExitReason != "open_at_end" && !t.ExitTime.IsZero() && !t.EntryTime.IsZero() {
			dur := t.ExitTime.Sub(t.EntryTime)
			if dur > 0 {
				totalDuration += dur
				closedTradesCount++
			}
		}
	}

	// Max Drawdown %
	peak := startingBalance
	maxDrawdownPct := 0.0
	for _, pt := range equityCurve {
		if pt.Value > peak {
			peak = pt.Value
		}
		if peak > 0 {
			dd := (peak - pt.Value) / peak * 100.0
			if dd > maxDrawdownPct {
				maxDrawdownPct = dd
			}
		}
	}

	// Win Rate %
	winRatePct := (float64(winningTrades) / float64(len(trades))) * 100.0

	// Profit Factor
	var profitFactor float64
	if losingTrades == 0 {
		if winningTrades > 0 {
			profitFactor = math.Inf(1)
		} else {
			profitFactor = 0.0
		}
	} else {
		profitFactor = grossProfit / grossLoss
	}

	// Avg Trade Duration
	var avgDuration time.Duration
	if closedTradesCount > 0 {
		avgDuration = totalDuration / time.Duration(closedTradesCount)
	}

	// Total Return %
	totalReturnPct := ((currentEquity - startingBalance) / startingBalance) * 100.0

	// Sharpe Ratio
	sharpeRatio := calculateSharpeRatio(equityCurve, periodsPerYear)

	return BacktestMetrics{
		SharpeRatio:      sharpeRatio,
		MaxDrawdownPct:   maxDrawdownPct,
		WinRatePct:       winRatePct,
		ProfitFactor:     profitFactor,
		TotalTrades:      len(trades),
		AvgTradeDuration: avgDuration,
		TotalReturnPct:   totalReturnPct,
		EquityCurve:      equityCurve,
	}
}

func calculateSharpeRatio(equityCurve []EquityPoint, periodsPerYear float64) float64 {
	if len(equityCurve) < 2 {
		return 0.0
	}

	returns := make([]float64, 0, len(equityCurve)-1)
	for i := 1; i < len(equityCurve); i++ {
		prev := equityCurve[i-1].Value
		curr := equityCurve[i].Value
		if prev > 0 {
			returns = append(returns, (curr-prev)/prev)
		}
	}

	if len(returns) < 2 {
		return 0.0
	}

	// Mean
	sum := 0.0
	for _, r := range returns {
		sum += r
	}
	mean := sum / float64(len(returns))

	// Sample variance / stddev
	varSum := 0.0
	for _, r := range returns {
		varSum += (r - mean) * (r - mean)
	}
	variance := varSum / float64(len(returns)-1)
	stddev := math.Sqrt(variance)

	if stddev == 0 || math.IsNaN(stddev) {
		return 0.0
	}

	// The returns above are PER TRADE (one equity point per closed trade),
	// so they must be annualized by how many trades happen per year - not by
	// the candle timeframe's periods per year. Using the candle periods (e.g.
	// sqrt(8760) ~= 93.6 for 1h) inflated Sharpe by orders of magnitude: 3
	// consistent trades over 6 months scored ~348 in the deploy gate. The
	// span is floored at 30 days so a handful of trades clustered within
	// hours isn't extrapolated into thousands of trades a year.
	annualization := periodsPerYear
	first, last := equityCurve[0].Time, equityCurve[len(equityCurve)-1].Time
	if !first.IsZero() && !last.IsZero() && last.After(first) {
		const minSpan = 30 * 24 * time.Hour
		span := last.Sub(first)
		if span < minSpan {
			span = minSpan
		}
		years := span.Hours() / (365.25 * 24)
		annualization = float64(len(returns)) / years
	} else if annualization <= 0 {
		annualization = 525600.0 // no timestamps: legacy default (1m periods/year)
	}

	annualizedSharpe := (mean / stddev) * math.Sqrt(annualization)
	if math.IsNaN(annualizedSharpe) {
		return 0.0
	}

	return annualizedSharpe
}
