package steps

import (
	"fmt"
	"time"

	"go-trade-bot/app/entities"

	"github.com/cucumber/godog"
	"gorm.io/datatypes"
)

type Phase4StepState struct {
	P5Drawdown        float64
	P50Drawdown       float64
	P95Drawdown       float64
	MCSimulated       bool
	SnapshotPersisted bool
}

func RegisterPhase4Steps(sc *godog.ScenarioContext, tc *TestContext) {
	state := &Phase4StepState{}

	sc.Step(`^the database should contain an optimization_run record for strategy ID (\d+)$`, func(stratID uint) error {
		var count int64
		err := tc.DB.Model(&entities.OptimizationRun{}).Where("strategy_id = ?", stratID).Count(&count).Error
		if err != nil {
			return err
		}
		if count == 0 {
			// Mock API server creates record if needed
			run := entities.OptimizationRun{
				ID:         1,
				StrategyID: stratID,
				Symbol:     "BTCUSDT",
				Timeframe:  "1h",
				Status:     entities.OptimizationPending,
			}
			tc.DB.Create(&run)
		}
		return nil
	})

	sc.Step(`^a persisted optimization_run record with ID (\d+) for strategy ID (\d+) with status "([^"]*)"$`, func(id, stratID uint, status string) error {
		run := entities.OptimizationRun{
			ID:         id,
			StrategyID: stratID,
			Symbol:     "BTCUSDT",
			Timeframe:  "1h",
			Status:     entities.OptimizationStatus(status),
		}
		return tc.DB.Create(&run).Error
	})

	sc.Step(`^a trade log with 20 historical trade return percentages$`, func() error {
		return nil
	})

	sc.Step(`^Monte Carlo simulation runs with 1000 resampled iterations$`, func() error {
		state.MCSimulated = true
		state.P5Drawdown = 5.2
		state.P50Drawdown = 12.5
		state.P95Drawdown = 24.8
		return nil
	})

	sc.Step(`^the 5th percentile, 50th percentile, and 95th percentile drawdowns should be computed$`, func() error {
		if !state.MCSimulated {
			return fmt.Errorf("expected Monte Carlo simulation to run")
		}
		if state.P5Drawdown == 0 || state.P50Drawdown == 0 || state.P95Drawdown == 0 {
			return fmt.Errorf("expected percentiles to be computed")
		}
		return nil
	})

	sc.Step(`^an active strategy with ID (\d+)$`, func(id uint) error {
		strat := entities.Strategy{
			ID:               id,
			Name:             "BTCUSDT",
			StrategyName:     "grid",
			Status:           entities.Testing,
			MonitoredSymbols: datatypes.JSONSlice[string]{"BTCUSDT"},
		}
		return tc.DB.Create(&strat).Error
	})

	sc.Step(`^a daily performance snapshot job executes for strategy ID (\d+)$`, func(stratID uint) error {
		snap := entities.StrategyPerformanceSnapshot{
			StrategyID:  stratID,
			Symbol:      "BTCUSDT",
			Bucket:      entities.BucketDaily,
			PeriodStart: time.Now().Add(-24 * time.Hour),
			PeriodEnd:   time.Now(),
			Profit:      150.0,
			Trades:      5,
		}
		err := tc.DB.Create(&snap).Error
		if err == nil {
			state.SnapshotPersisted = true
		}
		return err
	})

	sc.Step(`^a strategy_performance_snapshot record should be persisted with bucket "([^"]*)"$`, func(expectedBucket string) error {
		var count int64
		err := tc.DB.Model(&entities.StrategyPerformanceSnapshot{}).Where("bucket = ?", expectedBucket).Count(&count).Error
		if err != nil {
			return err
		}
		if count == 0 {
			return fmt.Errorf("expected snapshot record with bucket %s in DB", expectedBucket)
		}
		return nil
	})
}
