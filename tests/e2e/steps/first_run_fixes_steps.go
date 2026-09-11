package steps

import (
	"fmt"
	"time"

	"go-trade-bot/app/entities"

	"github.com/cucumber/godog"
)

func RegisterFirstRunFixesSteps(sc *godog.ScenarioContext, tc *TestContext) {
	sc.Step(`^historical candles exist for symbol "([^"]*)" and timeframe "([^"]*)"$`, func(symbol, timeframe string) error {
		c := entities.Candle{
			Symbol:    symbol,
			Timeframe: timeframe,
			OpenTime:  time.Now().Add(-1 * time.Hour),
			Open:      50000.0,
			High:      51000.0,
			Low:       49000.0,
			Close:     50500.0,
			Volume:    100.0,
		}
		return tc.DB.Create(&c).Error
	})

	sc.Step(`^historical candles exist for symbol "([^"]*)" and timeframe "([^"]*)" in date range "([^"]*)" to "([^"]*)"$`, func(symbol, timeframe, startDate, endDate string) error {
		start, err := time.Parse(time.RFC3339, startDate)
		if err != nil {
			return err
		}
		c := entities.Candle{
			Symbol:    symbol,
			Timeframe: timeframe,
			OpenTime:  start.Add(1 * time.Minute),
			Open:      50000.0,
			High:      51000.0,
			Low:       49000.0,
			Close:     50500.0,
			Volume:    100.0,
		}
		return tc.DB.Create(&c).Error
	})

	sc.Step(`^no optimization run should be created in database for symbol "([^"]*)"$`, func(symbol string) error {
		var count int64
		tc.DB.Model(&entities.OptimizationRun{}).Where("symbol = ?", symbol).Count(&count)
		if count > 0 {
			return fmt.Errorf("expected 0 optimization runs for symbol %s, found %d", symbol, count)
		}
		return nil
	})

	sc.Step(`^an import schedule exists for symbol "([^"]*)" with cron_spec "([^"]*)"$`, func(symbol, cronSpec string) error {
		sched := entities.ImportSchedule{
			ID:        1,
			Symbol:    symbol,
			Timeframe: "15m",
			CronSpec:  cronSpec,
			Enabled:   true,
			CreatedAt: time.Now(),
		}
		return tc.DB.Create(&sched).Error
	})
}
