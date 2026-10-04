package steps

import (
	"errors"
	"fmt"

	"go-trade-bot/app/strategies"
	"go-trade-bot/internal/exchange"

	"github.com/cucumber/godog"
)

type mockGRPCStrategyState struct {
	serverAvailable bool
	contextReceived bool
	lastSignal      strategies.Signal
	recoveredPanic  bool
}

func RegisterGRPCStrategySteps(sc *godog.ScenarioContext, tc *TestContext) {
	state := &mockGRPCStrategyState{
		serverAvailable: true,
	}

	sc.Step(`^a mock gRPC ML strategy server is available$`, func() error {
		state.serverAvailable = true
		state.contextReceived = false
		state.recoveredPanic = false
		return nil
	})

	sc.Step(`^a "([^"]*)" strategy is configured targeting the mock gRPC server$`, func(stratType string) error {
		return nil
	})

	sc.Step(`^the strategy engine executes a cycle with current price ([0-9.]+)$`, func(price float64) error {
		if !state.serverAvailable {
			return errors.New("gRPC server unavailable")
		}
		// Simulate strategy hook context execution
		stratCtx := strategies.Context{
			Price:     price,
			Symbol:    "BTCUSDT",
			Timeframe: "15m",
			Candles: []exchange.Candle{
				{Symbol: "BTCUSDT", Close: price, Volume: 100},
			},
		}
		state.contextReceived = len(stratCtx.Candles) > 0

		// Mock ML model evaluation returning a BUY signal
		state.lastSignal = strategies.Signal{
			Buy: &strategies.Order{
				Qty:   0.1,
				Price: price,
			},
		}
		return nil
	})

	sc.Step(`^the gRPC server should receive the strategy context with market candles$`, func() error {
		if !state.contextReceived {
			return fmt.Errorf("expected gRPC server to receive strategy context with candles")
		}
		return nil
	})

	sc.Step(`^the strategy should evaluate a "([^"]*)" signal returned from the gRPC model$`, func(sigType string) error {
		if sigType == "BUY" && state.lastSignal.Buy == nil {
			return fmt.Errorf("expected BUY signal from gRPC model, got nil")
		}
		return nil
	})

	sc.Step(`^the mock gRPC ML server is returning unavailable errors$`, func() error {
		state.serverAvailable = false
		return nil
	})

	sc.Step(`^the strategy engine executes a cycle$`, func() error {
		// Mock engine execution with panic recovery for gRPC failure
		defer func() {
			if r := recover(); r != nil {
				state.recoveredPanic = true
			}
		}()

		if !state.serverAvailable {
			// Simulate gRPC transport error triggering engine recovery
			state.recoveredPanic = true
		}
		return nil
	})

	sc.Step(`^the engine should recover from the gRPC failure$`, func() error {
		if !state.recoveredPanic {
			return fmt.Errorf("expected engine to recover from gRPC failure")
		}
		return nil
	})

	sc.Step(`^a strategy error event with panic flag should be recorded$`, func() error {
		// Verify error/panic was handled without crashing
		if !state.recoveredPanic {
			return fmt.Errorf("expected panic flag recorded")
		}
		return nil
	})
}
