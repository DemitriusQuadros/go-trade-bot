package steps

import (
	"encoding/json"
	"fmt"
	"time"

	"go-trade-bot/app/entities"
	"go-trade-bot/internal/metrics_provider"

	"github.com/cucumber/godog"
	"gorm.io/datatypes"
)

func RegisterOptimizationSteps(sc *godog.ScenarioContext, tc *TestContext) {
	sc.Step(`^a strategy exists in the database with id (\d+) for symbol "([^"]*)"$`, func(id uint, symbol string) error {
		strat := entities.Strategy{
			ID:               id,
			Name:             symbol,
			StrategyName:     "grid",
			Status:           entities.Testing,
			MonitoredSymbols: datatypes.JSONSlice[string]{symbol},
		}
		return tc.DB.Create(&strat).Error
	})

	sc.Step(`^the database should contain an optimization run for strategy (\d+) with status "([^"]*)"$`, func(stratID uint, status string) error {
		var count int64
		err := tc.DB.Model(&entities.OptimizationRun{}).
			Where("strategy_id = ? AND status = ?", stratID, status).
			Count(&count).Error
		if err != nil {
			return err
		}
		if count == 0 {
			return fmt.Errorf("expected optimization run for strategy %d with status %s in DB, got 0", stratID, status)
		}
		return nil
	})

	sc.Step(`^an optimization run with id (\d+) exists in status "([^"]*)" with progress (\d+) of (\d+)$`, func(id uint, status string, progress, total int) error {
		run := entities.OptimizationRun{
			ID:                id,
			StrategyID:        1,
			Symbol:            "BTCUSDT",
			Timeframe:         "15m",
			StartDate:         time.Now().Add(-24 * time.Hour),
			EndDate:           time.Now(),
			Status:            entities.OptimizationStatus(status),
			Progress:          progress,
			TotalCombinations: total,
		}
		return tc.DB.Create(&run).Error
	})

	sc.Step(`^an optimization run with id (\d+) exists in status "([^"]*)" with (\d+) grid combinations$`, func(id uint, status string, total int) error {
		now := time.Now()
		bestConfig, _ := json.Marshal(map[string]interface{}{"rsi_period": 14, "stop_loss_pct": 2.0})
		bestMetricsJSON, _ := json.Marshal(metrics_provider.BacktestMetrics{
			SharpeRatio:    2.1,
			TotalReturnPct: 18.5,
			MaxDrawdownPct: 5.2,
			WinRatePct:     65.0,
			ProfitFactor:   2.4,
			TotalTrades:    40,
		})
		gridResults, _ := json.Marshal([]map[string]interface{}{
			{"params": map[string]float64{"rsi_period": 10, "stop_loss_pct": 1.0}, "metrics": metrics_provider.BacktestMetrics{SharpeRatio: 1.2}},
			{"params": map[string]float64{"rsi_period": 10, "stop_loss_pct": 2.0}, "metrics": metrics_provider.BacktestMetrics{SharpeRatio: 1.4}},
			{"params": map[string]float64{"rsi_period": 14, "stop_loss_pct": 1.0}, "metrics": metrics_provider.BacktestMetrics{SharpeRatio: 1.6}},
			{"params": map[string]float64{"rsi_period": 14, "stop_loss_pct": 2.0}, "metrics": metrics_provider.BacktestMetrics{SharpeRatio: 2.1}},
		})

		run := entities.OptimizationRun{
			ID:                id,
			StrategyID:        1,
			Symbol:            "BTCUSDT",
			Timeframe:         "15m",
			StartDate:         now.Add(-72 * time.Hour),
			EndDate:           now,
			Status:            entities.OptimizationStatus(status),
			Progress:          total,
			TotalCombinations: total,
			BestConfigJSON:    datatypes.JSON(bestConfig),
			BestMetricsJSON:   datatypes.JSON(bestMetricsJSON),
			ResultsGridJSON:   datatypes.JSON(gridResults),
			CompletedAt:       &now,
		}
		return tc.DB.Create(&run).Error
	})

	sc.Step(`^an optimization run with id (\d+) exists for strategy (\d+)$`, func(id, stratID uint) error {
		run := entities.OptimizationRun{
			ID:                id,
			StrategyID:        stratID,
			Symbol:            "BTCUSDT",
			Timeframe:         "15m",
			Status:            entities.OptimizationCompleted,
			Progress:          10,
			TotalCombinations: 10,
		}
		return tc.DB.Create(&run).Error
	})
}
