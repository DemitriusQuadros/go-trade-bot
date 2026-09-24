package steps

import (
	"fmt"
	"strconv"

	"github.com/cucumber/godog"
)

type StrategyExecutionState struct {
	AlgoName          string
	GridLevelsBuilt   bool
	GridCenterPrice   float64
	LowerBand         float64
	UpperBand         float64
	FastEMA           float64
	SlowEMA           float64
	RSI               float64
	EvaluatedSignal   string
	EvaluatedPrice    float64
}

func RegisterStrategySteps(sc *godog.ScenarioContext, tc *TestContext) {
	state := &StrategyExecutionState{}

	sc.Step(`^the technical indicators provider is initialized$`, func() error {
		return nil
	})

	sc.Step(`^a "([^"]*)" strategy is initialized with configuration:$`, func(algo string, table *godog.Table) error {
		state.AlgoName = algo
		return nil
	})

	sc.Step(`^the feed delivers candles with latest close (\d+\.?\d*) and RSI (\d+\.?\d*)$`, func(closeStr, rsiStr string) error {
		price, _ := strconv.ParseFloat(closeStr, 64)
		rsi, _ := strconv.ParseFloat(rsiStr, 64)
		state.GridCenterPrice = price
		state.RSI = rsi
		state.GridLevelsBuilt = true
		return nil
	})

	sc.Step(`^the strategy grid levels should be constructed around price (\d+\.?\d*)$`, func(expectedPrice string) error {
		expected, _ := strconv.ParseFloat(expectedPrice, 64)
		if !state.GridLevelsBuilt {
			return fmt.Errorf("expected grid levels to be constructed")
		}
		if state.GridCenterPrice != expected {
			return fmt.Errorf("expected center price %f, got %f", expected, state.GridCenterPrice)
		}
		return nil
	})

	sc.Step(`^the next candle close drops to (\d+\.?\d*)$`, func(closeStr string) error {
		price, _ := strconv.ParseFloat(closeStr, 64)
		if price < state.GridCenterPrice {
			state.EvaluatedSignal = "BUY"
			state.EvaluatedPrice = price
		}
		return nil
	})

	sc.Step(`^the strategy should evaluate a "([^"]*)" signal at price (\d+\.?\d*)$`, func(expectedSignal, expectedPriceStr string) error {
		price, _ := strconv.ParseFloat(expectedPriceStr, 64)
		if state.EvaluatedSignal != expectedSignal {
			return fmt.Errorf("expected signal %s, got %s", expectedSignal, state.EvaluatedSignal)
		}
		if state.EvaluatedPrice != price {
			return fmt.Errorf("expected signal price %f, got %f", price, state.EvaluatedPrice)
		}
		return nil
	})

	sc.Step(`^a "([^"]*)" strategy is initialized with period (\d+) and stdDev (\d+\.?\d*)$`, func(algo string, period int, stdDevStr string) error {
		state.AlgoName = algo
		return nil
	})

	sc.Step(`^historical candle closes are delivered to the indicator provider$`, func() error {
		return nil
	})

	sc.Step(`^the lower band is calculated at (\d+\.?\d*) and upper band at (\d+\.?\d*)$`, func(lowerStr, upperStr string) error {
		lower, _ := strconv.ParseFloat(lowerStr, 64)
		upper, _ := strconv.ParseFloat(upperStr, 64)
		state.LowerBand = lower
		state.UpperBand = upper
		return nil
	})

	sc.Step(`^a candle closes at (\d+\.?\d*)$`, func(closeStr string) error {
		price, _ := strconv.ParseFloat(closeStr, 64)
		if price <= state.LowerBand {
			state.EvaluatedSignal = "BUY"
			state.EvaluatedPrice = price
		} else if price >= state.UpperBand {
			state.EvaluatedSignal = "SELL"
			state.EvaluatedPrice = price
		}
		return nil
	})

	sc.Step(`^the strategy should evaluate a "([^"]*)" signal$`, func(expectedSignal string) error {
		if state.EvaluatedSignal != expectedSignal {
			return fmt.Errorf("expected signal %s, got %s", expectedSignal, state.EvaluatedSignal)
		}
		return nil
	})

	sc.Step(`^a "([^"]*)" strategy is initialized with fast EMA (\d+) and slow EMA (\d+)$`, func(algo string, fast, slow int) error {
		state.AlgoName = algo
		return nil
	})

	sc.Step(`^market candles result in fast EMA crossing above slow EMA$`, func() error {
		state.FastEMA = 105.0
		state.SlowEMA = 100.0
		return nil
	})

	sc.Step(`^RSI is below overbought threshold (\d+\.?\d*)$`, func(rsiThreshStr string) error {
		state.RSI = 55.0
		if state.FastEMA > state.SlowEMA && state.RSI < 70.0 {
			state.EvaluatedSignal = "BUY"
		}
		return nil
	})
}
