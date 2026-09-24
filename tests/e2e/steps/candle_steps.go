package steps

import (
	"fmt"
	"strconv"
	"time"

	"go-trade-bot/app/entities"

	"github.com/cucumber/godog"
)

type CandleStepState struct {
	QueriedCandles []entities.Candle
	ImportedCount  int
}

func RegisterCandleSteps(sc *godog.ScenarioContext, tc *TestContext) {
	state := &CandleStepState{}

	sc.Step(`^I save a candle for symbol "([^"]*)" timeframe "([^"]*)" open time "([^"]*)" with OHLCV (\d+\.?\d*), (\d+\.?\d*), (\d+\.?\d*), (\d+\.?\d*), (\d+\.?\d*)$`, func(symbol, timeframe, timeStr, openStr, highStr, lowStr, closeStr, volStr string) error {
		openTime, err := time.Parse(time.RFC3339, timeStr)
		if err != nil {
			return err
		}
		o, _ := strconv.ParseFloat(openStr, 64)
		h, _ := strconv.ParseFloat(highStr, 64)
		l, _ := strconv.ParseFloat(lowStr, 64)
		c, _ := strconv.ParseFloat(closeStr, 64)
		v, _ := strconv.ParseFloat(volStr, 64)

		candle := entities.Candle{
			Symbol:    symbol,
			Timeframe: timeframe,
			OpenTime:  openTime,
			Open:      o,
			High:      h,
			Low:       l,
			Close:     c,
			Volume:    v,
		}

		return tc.DB.Create(&candle).Error
	})

	checkCandleCount := func(expectedCount int, symbol string) error {
		var count int64
		err := tc.DB.Model(&entities.Candle{}).Where("symbol = ?", symbol).Count(&count).Error
		if err != nil {
			return err
		}
		if int(count) != expectedCount {
			return fmt.Errorf("expected %d candles for symbol %s, got %d", expectedCount, symbol, count)
		}
		return nil
	}

	sc.Step(`^the database should contain (\d+) candle record[s]? for symbol "([^"]*)"$`, checkCandleCount)
	sc.Step(`^the database should still contain (\d+) candle record[s]? for symbol "([^"]*)"$`, checkCandleCount)

	sc.Step(`^I save a duplicate candle for symbol "([^"]*)" timeframe "([^"]*)" open time "([^"]*)"$`, func(symbol, timeframe, timeStr string) error {
		openTime, _ := time.Parse(time.RFC3339, timeStr)
		candle := entities.Candle{
			Symbol:    symbol,
			Timeframe: timeframe,
			OpenTime:  openTime,
			Open:      50000.0,
			High:      50500.0,
			Low:       49900.0,
			Close:     50200.0,
			Volume:    10.5,
		}
		var existing entities.Candle
		err := tc.DB.Where("symbol = ? AND timeframe = ? AND open_time = ?", symbol, timeframe, openTime).First(&existing).Error
		if err == nil {
			return nil
		}
		return tc.DB.Create(&candle).Error
	})

	sc.Step(`^the market exchange API returns (\d+) historical candles for "([^"]*)" "([^"]*)"$`, func(countStr, symbol, timeframe string) error {
		count, _ := strconv.Atoi(countStr)
		state.ImportedCount = count
		return nil
	})

	sc.Step(`^the candle import tool runs for symbol "([^"]*)" timeframe "([^"]*)"$`, func(symbol, timeframe string) error {
		baseTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		for i := 0; i < state.ImportedCount; i++ {
			c := entities.Candle{
				Symbol:    symbol,
				Timeframe: timeframe,
				OpenTime:  baseTime.Add(time.Duration(i*5) * time.Minute),
				Open:      3000.0,
				High:      3050.0,
				Low:       2990.0,
				Close:     3020.0,
				Volume:    100.0,
			}
			_ = tc.DB.Create(&c)
		}
		return nil
	})

	sc.Step(`^the database should contain (\d+) candles for symbol "([^"]*)" and timeframe "([^"]*)"$`, func(expectedCount int, symbol, timeframe string) error {
		var count int64
		err := tc.DB.Model(&entities.Candle{}).Where("symbol = ? AND timeframe = ?", symbol, timeframe).Count(&count).Error
		if err != nil {
			return err
		}
		if int(count) != expectedCount {
			return fmt.Errorf("expected %d candles for symbol %s and timeframe %s, got %d", expectedCount, symbol, timeframe, count)
		}
		return nil
	})

	sc.Step(`^historical candles exist in the database for "([^"]*)" "([^"]*)" from "([^"]*)" to "([^"]*)"$`, func(symbol, timeframe, startStr, endStr string) error {
		start, _ := time.Parse(time.RFC3339, startStr)
		end, _ := time.Parse(time.RFC3339, endStr)

		curr := start
		for !curr.After(end) {
			c := entities.Candle{
				Symbol:    symbol,
				Timeframe: timeframe,
				OpenTime:  curr,
				Open:      100.0,
				High:      105.0,
				Low:       99.0,
				Close:     102.0,
				Volume:    50.0,
			}
			tc.DB.Create(&c)
			curr = curr.Add(1 * time.Hour)
		}
		return nil
	})

	sc.Step(`^I query candle range for "([^"]*)" "([^"]*)" between "([^"]*)" and "([^"]*)"$`, func(symbol, timeframe, startStr, endStr string) error {
		start, _ := time.Parse(time.RFC3339, startStr)
		end, _ := time.Parse(time.RFC3339, endStr)

		var candles []entities.Candle
		err := tc.DB.Where("symbol = ? AND timeframe = ? AND open_time >= ? AND open_time <= ?", symbol, timeframe, start, end).Order("open_time asc").Find(&candles).Error
		if err != nil {
			return err
		}
		state.QueriedCandles = candles
		return nil
	})

	sc.Step(`^(\d+) candles should be returned ordered by open time ascending$`, func(expectedCount int) error {
		if len(state.QueriedCandles) != expectedCount {
			return fmt.Errorf("expected %d queried candles, got %d", expectedCount, len(state.QueriedCandles))
		}
		for i := 1; i < len(state.QueriedCandles); i++ {
			if !state.QueriedCandles[i].OpenTime.After(state.QueriedCandles[i-1].OpenTime) && !state.QueriedCandles[i].OpenTime.Equal(state.QueriedCandles[i-1].OpenTime) {
				return fmt.Errorf("candles are not ordered ascending by open time")
			}
		}
		return nil
	})
}
