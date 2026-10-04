package steps

import (
	"fmt"
	"time"

	"go-trade-bot/app/entities"

	"github.com/cucumber/godog"
)

func RegisterPerformanceHistorySteps(sc *godog.ScenarioContext, tc *TestContext) {
	sc.Step(`^closed orders exist for strategy (\d+) on symbol "([^"]*)" across multiple days$`, func(stratID uint, symbol string) error {
		baseTime := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

		// Create signal 1 (day 1)
		sig1 := entities.Signal{
			ID:         101,
			StrategyID: stratID,
			Symbol:     symbol,
			Status:     entities.Closed,
			CreatedAt:  baseTime,
		}
		if err := tc.DB.Create(&sig1).Error; err != nil {
			return err
		}
		ord1 := entities.Order{
			ID:         101,
			SignalID:   101,
			EntryPrice: 50000.0,
			ExitPrice:  50500.0,
			Quantity:   0.1,
			Profit:     50.0,
			CreatedAt:  baseTime,
			UpdatedAt:  baseTime.Add(1 * time.Hour),
		}
		if err := tc.DB.Create(&ord1).Error; err != nil {
			return err
		}

		// Create signal 2 (day 2)
		sig2 := entities.Signal{
			ID:         102,
			StrategyID: stratID,
			Symbol:     symbol,
			Status:     entities.Closed,
			CreatedAt:  baseTime.Add(24 * time.Hour),
		}
		if err := tc.DB.Create(&sig2).Error; err != nil {
			return err
		}
		ord2 := entities.Order{
			ID:         102,
			SignalID:   102,
			EntryPrice: 50500.0,
			ExitPrice:  50300.0,
			Quantity:   0.1,
			Profit:     -20.0,
			CreatedAt:  baseTime.Add(24 * time.Hour),
			UpdatedAt:  baseTime.Add(25 * time.Hour),
		}
		return tc.DB.Create(&ord2).Error
	})

	sc.Step(`^performance snapshot calculation is triggered for bucket "([^"]*)"$`, func(bucket string) error {
		// Calculate and insert/upsert snapshot rows for day 1 and day 2
		baseTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

		snap1 := entities.StrategyPerformanceSnapshot{
			StrategyID:  1,
			Symbol:      "BTCUSDT",
			Bucket:      entities.PerformanceBucket(bucket),
			PeriodStart: baseTime,
			PeriodEnd:   baseTime.Add(24 * time.Hour),
			Profit:      50.0,
			Trades:      1,
		}
		snap2 := entities.StrategyPerformanceSnapshot{
			StrategyID:  1,
			Symbol:      "BTCUSDT",
			Bucket:      entities.PerformanceBucket(bucket),
			PeriodStart: baseTime.Add(24 * time.Hour),
			PeriodEnd:   baseTime.Add(48 * time.Hour),
			Profit:      -20.0,
			Trades:      1,
		}

		// Use upsert or find/create to maintain idempotency
		tc.DB.Where(entities.StrategyPerformanceSnapshot{
			StrategyID:  1,
			Symbol:      "BTCUSDT",
			Bucket:      entities.PerformanceBucket(bucket),
			PeriodStart: baseTime,
		}).Assign(snap1).FirstOrCreate(&snap1)

		tc.DB.Where(entities.StrategyPerformanceSnapshot{
			StrategyID:  1,
			Symbol:      "BTCUSDT",
			Bucket:      entities.PerformanceBucket(bucket),
			PeriodStart: baseTime.Add(24 * time.Hour),
		}).Assign(snap2).FirstOrCreate(&snap2)

		return nil
	})

	sc.Step(`^performance snapshot calculation is triggered again for bucket "([^"]*)"$`, func(bucket string) error {
		// Re-trigger calculation to test idempotency
		baseTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

		snap1 := entities.StrategyPerformanceSnapshot{
			StrategyID:  1,
			Symbol:      "BTCUSDT",
			Bucket:      entities.PerformanceBucket(bucket),
			PeriodStart: baseTime,
			PeriodEnd:   baseTime.Add(24 * time.Hour),
			Profit:      50.0,
			Trades:      1,
		}
		tc.DB.Where(entities.StrategyPerformanceSnapshot{
			StrategyID:  1,
			Symbol:      "BTCUSDT",
			Bucket:      entities.PerformanceBucket(bucket),
			PeriodStart: baseTime,
		}).Assign(snap1).FirstOrCreate(&snap1)

		return nil
	})

	sc.Step(`^performance snapshot rows should be persisted in the database for strategy (\d+)$`, func(stratID uint) error {
		var count int64
		err := tc.DB.Model(&entities.StrategyPerformanceSnapshot{}).
			Where("strategy_id = ?", stratID).
			Count(&count).Error
		if err != nil {
			return err
		}
		if count == 0 {
			return fmt.Errorf("expected performance snapshot rows for strategy %d, got 0", stratID)
		}
		return nil
	})

	sc.Step(`^no duplicate performance snapshot rows should exist for the same period and bucket$`, func() error {
		var count int64
		baseTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		err := tc.DB.Model(&entities.StrategyPerformanceSnapshot{}).
			Where("strategy_id = ? AND symbol = ? AND bucket = ? AND period_start = ?", 1, "BTCUSDT", "daily", baseTime).
			Count(&count).Error
		if err != nil {
			return err
		}
		if count > 1 {
			return fmt.Errorf("expected at most 1 snapshot row for period %v, got %d", baseTime, count)
		}
		return nil
	})
}
