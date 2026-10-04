package steps

import (
	"encoding/json"
	"time"

	"go-trade-bot/app/entities"
	"go-trade-bot/internal/metrics_provider"

	"github.com/cucumber/godog"
	"gorm.io/datatypes"
)

func RegisterMonteCarloSteps(sc *godog.ScenarioContext, tc *TestContext) {
	sc.Step(`^a completed backtest run exists with id (\d+) and (\d+) trades$`, func(id uint, tradeCount int) error {
		trades := make([]metrics_provider.TradeLogEntry, tradeCount)
		baseTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		for i := 0; i < tradeCount; i++ {
			profit := 10.0
			if i%3 == 0 {
				profit = -5.0
			}
			trades[i] = metrics_provider.TradeLogEntry{
				Symbol:     "BTCUSDT",
				EntryTime:  baseTime.Add(time.Duration(i) * time.Hour),
				EntryPrice: 50000.0,
				ExitTime:   baseTime.Add(time.Duration(i)*time.Hour + 30*time.Minute),
				ExitPrice:  50100.0,
				Quantity:   0.1,
				Profit:     profit,
				ExitReason: "take_profit",
			}
		}

		tradeLogJSON, _ := json.Marshal(trades)
		run := entities.BacktestRun{
			ID:             id,
			StrategyID:     1,
			Symbol:         "BTCUSDT",
			StartDate:      baseTime,
			EndDate:        baseTime.Add(time.Duration(tradeCount) * time.Hour),
			InitialCapital: 1000.0,
			TotalReturnPct: 12.5,
			Sharpe:         1.8,
			MaxDrawdownPct: 4.2,
			WinRatePct:     66.7,
			ProfitFactor:   2.0,
			TotalTrades:    tradeCount,
			TradeLogJSON:   datatypes.JSON(tradeLogJSON),
		}
		return tc.DB.Create(&run).Error
	})

	sc.Step(`^a completed backtest run exists with id (\d+) with cached Monte Carlo results$`, func(id uint) error {
		mcResults := map[string]interface{}{
			"iterations": 1000,
			"sharpe_distribution": map[string]float64{
				"mean": 1.75, "median": 1.74, "min": 1.1, "max": 2.3, "p5": 1.25, "p95": 2.15,
			},
			"max_drawdown_distribution": map[string]float64{
				"mean": 5.1, "median": 4.9, "min": 2.1, "max": 11.2, "p5": 2.8, "p95": 8.9,
			},
			"total_return_distribution": map[string]float64{
				"mean": 12.5, "median": 12.5, "min": 12.5, "max": 12.5, "p5": 12.5, "p95": 12.5,
			},
		}
		mcJSON, _ := json.Marshal(mcResults)

		run := entities.BacktestRun{
			ID:             id,
			StrategyID:     1,
			Symbol:         "BTCUSDT",
			TotalReturnPct: 12.5,
			Sharpe:         1.8,
			MonteCarloJSON: datatypes.JSON(mcJSON),
		}
		return tc.DB.Create(&run).Error
	})
}
