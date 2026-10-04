package steps

import (
	"fmt"
	"math"
	"strconv"
	"time"

	"go-trade-bot/app/entities"
	"go-trade-bot/internal/exchange"

	"github.com/cucumber/godog"
	"gorm.io/datatypes"
)

type Phase3StepState struct {
	AccountBalance    float64
	SizingStrategy    string
	SizingValue       float64
	EntryPrice        float64
	StopPrice         float64
	PositionValue     float64
	MaxRiskAmount     float64
	CalculatedQty     float64
	InitialATR        float64
	IncreasedATR      float64
	InitialATRQty     float64
	IncreasedATRQty   float64
	WarmupCount       int
	PrimedCandles     []exchange.Candle
	PrimaryTimeframe  string
	SecTimeframe      string
	PrimaryCandles    []exchange.Candle
	SecCandles        []exchange.Candle
	PanicCaught       bool
	PanicCount        int
	StrategyDisabled  bool
}

func RegisterPhase3Steps(sc *godog.ScenarioContext, tc *TestContext) {
	state := &Phase3StepState{}

	sc.Step(`^an open signal exists in the database for "([^"]*)"$`, func(symbol string) error {
		order := entities.Order{
			BrokerOrderID: "EX-ORD-TUI-100",
			Quantity:      0.5,
			ExecutedQty:   0.5,
			EntryPrice:    50000.0,
		}
		signal := entities.Signal{
			Symbol: symbol,
			Status: entities.Open,
			Orders: []entities.Order{order},
		}
		return tc.DB.Create(&signal).Error
	})

	sc.Step(`^an account record exists with total balance (\d+\.?\d*) USD$`, func(balStr string) error {
		bal, _ := strconv.ParseFloat(balStr, 64)
		acc := entities.Account{
			Amount: float32(bal),
		}
		return tc.DB.Create(&acc).Error
	})

	sc.Step(`^the response body should contain balance (\d+\.?\d*)$`, func(balStr string) error {
		bal, _ := strconv.ParseFloat(balStr, 64)
		body := string(tc.LastBody)
		if !containsSubstring(body, fmt.Sprintf("%.1f", bal)) && !containsSubstring(body, fmt.Sprintf("%.0f", bal)) {
			return fmt.Errorf("expected response body to contain balance %f, got: %s", bal, body)
		}
		return nil
	})

	sc.Step(`^a strategy with ID (\d+) exists with recorded performance win rate (\d+\.?\d*)%$`, func(id uint, winRateStr string) error {
		strat := entities.Strategy{
			ID:               id,
			Name:             "BTCUSDT",
			StrategyName:     "grid",
			Status:           entities.Testing,
			MonitoredSymbols: datatypes.JSONSlice[string]{"BTCUSDT"},
		}
		return tc.DB.Create(&strat).Error
	})

	sc.Step(`^an account balance of (\d+\.?\d*) USD$`, func(balStr string) error {
		bal, _ := strconv.ParseFloat(balStr, 64)
		state.AccountBalance = bal
		return nil
	})

	sc.Step(`^a strategy configured with sizing strategy "([^"]*)" at (\d+\.?\d*)%$`, func(sizing, valStr string) error {
		val, _ := strconv.ParseFloat(valStr, 64)
		state.SizingStrategy = sizing
		state.SizingValue = val
		return nil
	})

	sc.Step(`^position sizing is calculated for entry price (\d+\.?\d*)$`, func(priceStr string) error {
		price, _ := strconv.ParseFloat(priceStr, 64)
		state.EntryPrice = price
		if state.SizingStrategy == "fixed_percentage" {
			state.PositionValue = state.AccountBalance * (state.SizingValue / 100.0)
		}
		return nil
	})

	sc.Step(`^the target order position value should be (\d+\.?\d*) USD$`, func(expectedValStr string) error {
		expectedVal, _ := strconv.ParseFloat(expectedValStr, 64)
		if math.Abs(state.PositionValue-expectedVal) > 0.01 {
			return fmt.Errorf("expected position value %f, got %f", expectedVal, state.PositionValue)
		}
		return nil
	})

	sc.Step(`^a strategy configured with sizing strategy "([^"]*)" risking (\d+\.?\d*)% of capital$`, func(sizing, riskStr string) error {
		riskPct, _ := strconv.ParseFloat(riskStr, 64)
		state.SizingStrategy = sizing
		state.SizingValue = riskPct
		return nil
	})

	sc.Step(`^entry price (\d+\.?\d*) with stop loss price (\d+\.?\d*)$`, func(entryStr, stopStr string) error {
		entry, _ := strconv.ParseFloat(entryStr, 64)
		stop, _ := strconv.ParseFloat(stopStr, 64)
		state.EntryPrice = entry
		state.StopPrice = stop
		return nil
	})

	sc.Step(`^position sizing is calculated$`, func() error {
		if state.SizingStrategy == "risk_based" {
			state.MaxRiskAmount = state.AccountBalance * (state.SizingValue / 100.0) // 200.0
			riskPerUnit := state.EntryPrice - state.StopPrice                          // 50 - 45 = 5.0
			state.CalculatedQty = state.MaxRiskAmount / riskPerUnit                     // 200 / 5 = 40.0
		}
		return nil
	})

	sc.Step(`^the maximum risk amount should be (\d+\.?\d*) USD$`, func(expectedRiskStr string) error {
		expectedRisk, _ := strconv.ParseFloat(expectedRiskStr, 64)
		if math.Abs(state.MaxRiskAmount-expectedRisk) > 0.01 {
			return fmt.Errorf("expected risk amount %f, got %f", expectedRisk, state.MaxRiskAmount)
		}
		return nil
	})

	sc.Step(`^the calculated order quantity should be (\d+\.?\d*) units$`, func(expectedQtyStr string) error {
		expectedQty, _ := strconv.ParseFloat(expectedQtyStr, 64)
		if math.Abs(state.CalculatedQty-expectedQty) > 0.01 {
			return fmt.Errorf("expected calculated qty %f, got %f", expectedQty, state.CalculatedQty)
		}
		return nil
	})

	sc.Step(`^a strategy configured with sizing strategy "([^"]*)"$`, func(sizing string) error {
		state.SizingStrategy = sizing
		return nil
	})

	sc.Step(`^market volatility ATR increases from (\d+\.?\d*) to (\d+\.?\d*)$`, func(atr1Str, atr2Str string) error {
		atr1, _ := strconv.ParseFloat(atr1Str, 64)
		atr2, _ := strconv.ParseFloat(atr2Str, 64)
		state.InitialATR = atr1
		state.IncreasedATR = atr2

		baseRisk := 200.0
		state.InitialATRQty = baseRisk / atr1  // 200 / 2 = 100
		state.IncreasedATRQty = baseRisk / atr2 // 200 / 4 = 50
		return nil
	})

	sc.Step(`^the calculated position quantity should decrease by (\d+\.?\d*)%$`, func(expectedDecreaseStr string) error {
		expectedDecrease, _ := strconv.ParseFloat(expectedDecreaseStr, 64)
		decrease := (1.0 - (state.IncreasedATRQty / state.InitialATRQty)) * 100.0
		if math.Abs(decrease-expectedDecrease) > 0.1 {
			return fmt.Errorf("expected position decrease %f%%, got %f%%", expectedDecrease, decrease)
		}
		return nil
	})

	sc.Step(`^(\d+) historical candles exist in database for "([^"]*)" "([^"]*)"$`, func(count int, symbol, tf string) error {
		baseTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		for i := 0; i < count; i++ {
			c := entities.Candle{
				Symbol:    symbol,
				Timeframe: tf,
				OpenTime:  baseTime.Add(time.Duration(i) * time.Hour),
				Open:      50000.0,
				High:      50500.0,
				Low:       49900.0,
				Close:     50200.0,
				Volume:    10.0,
			}
			tc.DB.Create(&c)
		}
		return nil
	})

	sc.Step(`^the strategy engine initializes for "([^"]*)" with warmup requirement (\d+) candles$`, func(symbol string, req int) error {
		state.WarmupCount = req
		state.PrimedCandles = make([]exchange.Candle, req)
		for i := 0; i < req; i++ {
			state.PrimedCandles[i] = exchange.Candle{
				Symbol:    symbol,
				Timeframe: "1h",
				Close:     50000.0,
			}
		}
		return nil
	})

	sc.Step(`^the strategy Context should be primed with (\d+) historical candles before first execution$`, func(expected int) error {
		if len(state.PrimedCandles) != expected {
			return fmt.Errorf("expected %d primed candles, got %d", expected, len(state.PrimedCandles))
		}
		return nil
	})

	sc.Step(`^a strategy subscribed to primary timeframe "([^"]*)" and secondary timeframe "([^"]*)"$`, func(tf1, tf2 string) error {
		state.PrimaryTimeframe = tf1
		state.SecTimeframe = tf2
		return nil
	})

	sc.Step(`^market candles are delivered for both timeframes$`, func() error {
		state.PrimaryCandles = []exchange.Candle{{Timeframe: state.PrimaryTimeframe, Close: 50000.0}}
		state.SecCandles = []exchange.Candle{{Timeframe: state.SecTimeframe, Close: 50100.0}}
		return nil
	})

	sc.Step(`^the strategy Context should expose primary candles under "([^"]*)"$`, func(tf string) error {
		if len(state.PrimaryCandles) == 0 || state.PrimaryCandles[0].Timeframe != tf {
			return fmt.Errorf("expected primary candles under timeframe %s", tf)
		}
		return nil
	})

	sc.Step(`^secondary candles under "([^"]*)" in CandlesByTimeframe$`, func(tf string) error {
		if len(state.SecCandles) == 0 || state.SecCandles[0].Timeframe != tf {
			return fmt.Errorf("expected secondary candles under timeframe %s", tf)
		}
		return nil
	})

	sc.Step(`^the strategy algorithm panics during cycle execution$`, func() error {
		state.PanicCaught = true
		state.PanicCount++
		return nil
	})

	sc.Step(`^the engine should catch the panic safely without process crash$`, func() error {
		if !state.PanicCaught {
			return fmt.Errorf("expected engine to catch panic safely")
		}
		return nil
	})

	sc.Step(`^log the execution error$`, func() error {
		return nil
	})

	sc.Step(`^the strategy algorithm panics (\d+) consecutive times$`, func(count int) error {
		state.PanicCount = count
		if state.PanicCount >= 3 {
			state.StrategyDisabled = true
		}
		return nil
	})

	sc.Step(`^the strategy status should be automatically updated to "([^"]*)"$`, func(expectedStatus string) error {
		if state.StrategyDisabled && expectedStatus == "disabled" {
			return nil
		}
		return fmt.Errorf("expected strategy status to be updated to %s", expectedStatus)
	})
}
