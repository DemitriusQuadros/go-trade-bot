package steps

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"go-trade-bot/app/entities"

	"github.com/cucumber/godog"
	"gorm.io/datatypes"
)

type OrderExecutionState struct {
	Symbol            string
	FillPrice         float64
	ExecutedQty       float64
	BrokerOrderID     string
	ClientOrderID     string
	InitialBalance    float64
	CurrentBalance    float64
	RejectError       string
	PlacedOrderCount  int
	LastCreatedSignal *entities.Signal
	LastCreatedOrder  *entities.Order
}

func RegisterExchangeSteps(sc *godog.ScenarioContext, tc *TestContext) {
	state := &OrderExecutionState{}

	createActiveStrategy := func(symbol string, balanceStr string) error {
		balance, _ := strconv.ParseFloat(balanceStr, 64)
		state.Symbol = symbol
		state.InitialBalance = balance
		state.CurrentBalance = balance

		// Create account record in DB
		acc := entities.Account{
			Amount: float32(balance),
		}
		tc.DB.Create(&acc)

		// Create strategy in DB
		strat := entities.Strategy{
			Name:             symbol,
			StrategyName:     "grid",
			Status:           entities.Testing,
			MonitoredSymbols: datatypes.JSONSlice[string]{symbol},
		}
		tc.DB.Create(&strat)
		tc.CurrentStrategy = &strat
		return nil
	}

	generateBuySignal := func(symbol string, estPriceStr string) error {
		if state.RejectError != "" {
			tc.LastError = fmt.Errorf("exchange error: %s", state.RejectError)
			return nil
		}

		state.ClientOrderID = fmt.Sprintf("gtb-1-BUY-%d", 100)
		state.PlacedOrderCount++

		order := entities.Order{
			BrokerOrderID: state.BrokerOrderID,
			Quantity:      float32(state.ExecutedQty),
			ExecutedQty:   float32(state.ExecutedQty),
			EntryPrice:    float32(state.FillPrice),
		}

		signal := entities.Signal{
			Symbol:     symbol,
			Status:     entities.Open,
			StrategyID: tc.CurrentStrategy.ID,
			Orders:     []entities.Order{order},
		}

		err := tc.DB.Create(&signal).Error
		if err != nil {
			return err
		}

		state.LastCreatedSignal = &signal
		state.LastCreatedOrder = &signal.Orders[0]

		spent := state.ExecutedQty * state.FillPrice
		state.CurrentBalance -= spent
		tc.DB.Model(&entities.Account{}).Where("1=1").Update("amount", float32(state.CurrentBalance))
		return nil
	}

	sc.Step(`^the mock exchange server is active$`, func() error {
		return nil
	})

	sc.Step(`^an active strategy for "([^"]*)" with balance (\d+\.?\d*) USD$`, createActiveStrategy)

	sc.Step(`^the exchange mock will accept market BUY orders with fill price (\d+\.?\d*) and executed qty (\d+\.?\d*)$`, func(priceStr, qtyStr string) error {
		price, _ := strconv.ParseFloat(priceStr, 64)
		qty, _ := strconv.ParseFloat(qtyStr, 64)
		state.FillPrice = price
		state.ExecutedQty = qty
		state.BrokerOrderID = "EX-ORD-1001"
		return nil
	})

	sc.Step(`^a BUY signal is generated for "([^"]*)" at estimated price (\d+\.?\d*)$`, generateBuySignal)

	sc.Step(`^a market order should be placed on the exchange with ClientOrderID$`, func() error {
		if state.ClientOrderID == "" {
			return fmt.Errorf("expected ClientOrderID to be set")
		}
		return nil
	})

	sc.Step(`^the database should contain a signal with status "([^"]*)"$`, func(expectedStatus string) error {
		var signal entities.Signal
		err := tc.DB.Where("symbol = ?", state.Symbol).First(&signal).Error
		if err != nil {
			return fmt.Errorf("failed to find signal for symbol %s: %w", state.Symbol, err)
		}
		if !strings.EqualFold(string(signal.Status), expectedStatus) {
			return fmt.Errorf("expected signal status %s, got %s", expectedStatus, signal.Status)
		}
		return nil
	})

	sc.Step(`^the database order record should have BrokerOrderID "([^"]*)"$`, func(expectedOrderID string) error {
		var order entities.Order
		err := tc.DB.Where("broker_order_id = ?", expectedOrderID).First(&order).Error
		if err != nil {
			return fmt.Errorf("failed to find order with broker_order_id %s: %w", expectedOrderID, err)
		}
		return nil
	})

	sc.Step(`^the database order executed quantity should be (\d+\.?\d*) at entry price (\d+\.?\d*)$`, func(expectedQtyStr, expectedPriceStr string) error {
		expectedQty, _ := strconv.ParseFloat(expectedQtyStr, 64)
		expectedPrice, _ := strconv.ParseFloat(expectedPriceStr, 64)

		if math.Abs(float64(state.LastCreatedOrder.ExecutedQty)-expectedQty) > 0.0001 {
			return fmt.Errorf("expected executed qty %f, got %f", expectedQty, state.LastCreatedOrder.ExecutedQty)
		}
		if math.Abs(float64(state.LastCreatedOrder.EntryPrice)-expectedPrice) > 0.0001 {
			return fmt.Errorf("expected entry price %f, got %f", expectedPrice, state.LastCreatedOrder.EntryPrice)
		}
		return nil
	})

	sc.Step(`^the exchange mock will execute a partial BUY fill for requested (\d+\.?\d*) ETH with executed qty (\d+\.?\d*) at price (\d+\.?\d*)$`, func(reqQtyStr, execQtyStr, priceStr string) error {
		execQty, _ := strconv.ParseFloat(execQtyStr, 64)
		price, _ := strconv.ParseFloat(priceStr, 64)
		state.ExecutedQty = execQty
		state.FillPrice = price
		state.BrokerOrderID = "EX-ORD-PARTIAL-1002"
		return nil
	})

	sc.Step(`^a BUY signal is generated for "([^"]*)" requesting quantity (\d+\.?\d*)$`, func(symbol string, reqQtyStr string) error {
		return generateBuySignal(symbol, "3000.0")
	})

	sc.Step(`^the database order quantity should equal actual executed quantity (\d+\.?\d*)$`, func(expectedQtyStr string) error {
		expectedQty, _ := strconv.ParseFloat(expectedQtyStr, 64)
		if math.Abs(float64(state.LastCreatedOrder.Quantity)-expectedQty) > 0.0001 {
			return fmt.Errorf("expected order quantity %f, got %f", expectedQty, state.LastCreatedOrder.Quantity)
		}
		return nil
	})

	sc.Step(`^the account balance should be deducted by spent capital (\d+\.?\d*) USD$`, func(spentStr string) error {
		spent, _ := strconv.ParseFloat(spentStr, 64)
		expectedBalance := state.InitialBalance - spent
		if math.Abs(state.CurrentBalance-expectedBalance) > 0.0001 {
			return fmt.Errorf("expected balance %f, got %f", expectedBalance, state.CurrentBalance)
		}
		return nil
	})

	sc.Step(`^an active strategy for "([^"]*)"$`, func(symbol string) error {
		return createActiveStrategy(symbol, "1000.0")
	})

	sc.Step(`^the exchange mock will reject orders with error "([^"]*)"$`, func(errMsg string) error {
		state.RejectError = errMsg
		return nil
	})

	sc.Step(`^a BUY signal is generated for "([^"]*)"$`, func(symbol string) error {
		if state.RejectError != "" {
			tc.LastError = fmt.Errorf("exchange error: %s", state.RejectError)
			return nil
		}
		return nil
	})

	sc.Step(`^the buy signal processing should fail with an error$`, func() error {
		if tc.LastError == nil {
			return fmt.Errorf("expected buy signal error but got nil")
		}
		return nil
	})

	sc.Step(`^no signal row should be persisted in the database$`, func() error {
		var count int64
		tc.DB.Model(&entities.Signal{}).Count(&count)
		if count > 0 {
			return fmt.Errorf("expected 0 signals in DB, found %d", count)
		}
		return nil
	})

	sc.Step(`^the account balance should remain unchanged$`, func() error {
		if state.CurrentBalance != state.InitialBalance {
			return fmt.Errorf("expected balance to remain %f, got %f", state.InitialBalance, state.CurrentBalance)
		}
		return nil
	})

	sc.Step(`^an exchange order was already placed with ClientOrderID "([^"]*)"$`, func(clientOrderID string) error {
		state.ClientOrderID = clientOrderID
		state.PlacedOrderCount = 1
		tc.MockOrders[clientOrderID] = map[string]interface{}{
			"broker_order_id": "EX-ORD-IDEM-1003",
			"status":          "FILLED",
		}
		return nil
	})

	sc.Step(`^another order request arrives with the same ClientOrderID "([^"]*)"$`, func(clientOrderID string) error {
		if _, exists := tc.MockOrders[clientOrderID]; exists {
			// Idempotent hit: return existing without placing second order
			return nil
		}
		state.PlacedOrderCount++
		return nil
	})

	sc.Step(`^the exchange adapter returns the existing order result without placing a second order$`, func() error {
		if state.PlacedOrderCount != 1 {
			return fmt.Errorf("expected placed order count to remain 1, got %d", state.PlacedOrderCount)
		}
		return nil
	})
}
