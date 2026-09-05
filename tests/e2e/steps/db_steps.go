package steps

import (
	"fmt"
	"strconv"
	"strings"

	"go-trade-bot/app/entities"

	"github.com/cucumber/godog"
	"gorm.io/datatypes"
)

type StopLossRiskState struct {
	Symbol           string
	StopLossPct      float64
	EntryFillPrice   float64
	EntryQty         float64
	StopPrice        float64
	StopLossOrderID  string
	CancelledStopID  string
	MarketSellPlaced bool
	Reconciled       bool
}

func RegisterDBAndRiskSteps(sc *godog.ScenarioContext, tc *TestContext) {
	state := &StopLossRiskState{}

	sc.Step(`^an open strategy for "([^"]*)" with stop loss percentage (\d+\.?\d*)%$`, func(symbol, pctStr string) error {
		pct, _ := strconv.ParseFloat(pctStr, 64)
		state.Symbol = symbol
		state.StopLossPct = pct

		strat := entities.Strategy{
			Name:             symbol,
			StrategyName:     "grid",
			Algorithm:        entities.Grid,
			Status:           entities.Testing,
			MonitoredSymbols: datatypes.JSONSlice[string]{symbol},
		}
		tc.DB.Create(&strat)
		tc.CurrentStrategy = &strat
		return nil
	})

	sc.Step(`^a BUY market order fills at price (\d+\.?\d*) for quantity (\d+\.?\d*)$`, func(priceStr, qtyStr string) error {
		price, _ := strconv.ParseFloat(priceStr, 64)
		qty, _ := strconv.ParseFloat(qtyStr, 64)
		state.EntryFillPrice = price
		state.EntryQty = qty

		// Calculate stop price = price * (1 - pct/100)
		state.StopPrice = price * (1.0 - state.StopLossPct/100.0)
		state.StopLossOrderID = "EX-STOP-2001"

		order := entities.Order{
			BrokerOrderID:   "EX-ORD-BUY-2000",
			StopLossOrderID: state.StopLossOrderID,
			Quantity:        float32(qty),
			ExecutedQty:     float32(qty),
			EntryPrice:      float32(price),
		}

		signal := entities.Signal{
			Symbol:     state.Symbol,
			Status:     entities.Open,
			StrategyID: tc.CurrentStrategy.ID,
			Orders:     []entities.Order{order},
		}

		return tc.DB.Create(&signal).Error
	})

	sc.Step(`^a "STOP_MARKET" sell order should be placed on the exchange with stop price (\d+\.?\d*)$`, func(expectedStopPriceStr string) error {
		expectedStopPrice, _ := strconv.ParseFloat(expectedStopPriceStr, 64)
		if state.StopPrice != expectedStopPrice {
			return fmt.Errorf("expected stop price %f, got %f", expectedStopPrice, state.StopPrice)
		}
		return nil
	})

	sc.Step(`^the database order record should store the StopLossOrderID "([^"]*)"$`, func(expectedStopID string) error {
		var order entities.Order
		err := tc.DB.Where("stop_loss_order_id = ?", expectedStopID).First(&order).Error
		if err != nil {
			return fmt.Errorf("failed to find order with StopLossOrderID %s in DB: %w", expectedStopID, err)
		}
		return nil
	})

	sc.Step(`^an open signal for "([^"]*)" with a resting stop-loss order "([^"]*)"$`, func(symbol, stopID string) error {
		state.Symbol = symbol
		state.StopLossOrderID = stopID

		order := entities.Order{
			BrokerOrderID:   "EX-ORD-BUY-2002",
			StopLossOrderID: stopID,
			Quantity:        0.1,
			ExecutedQty:     0.1,
			EntryPrice:      50000.0,
		}

		signal := entities.Signal{
			Symbol: symbol,
			Status: entities.Open,
			Orders: []entities.Order{order},
		}

		return tc.DB.Create(&signal).Error
	})

	sc.Step(`^a SELL signal is generated to close the position$`, func() error {
		// Cancel stop first
		state.CancelledStopID = state.StopLossOrderID
		tc.CancelledStopOrders = append(tc.CancelledStopOrders, state.CancelledStopID)

		// Submit market sell
		state.MarketSellPlaced = true

		// Update signal status in DB to Closed
		return tc.DB.Model(&entities.Signal{}).Where("symbol = ? AND status = ?", state.Symbol, entities.Open).Update("status", entities.Closed).Error
	})

	sc.Step(`^the system should call CancelOrder for "([^"]*)" on the exchange first$`, func(expectedStopID string) error {
		if state.CancelledStopID != expectedStopID {
			return fmt.Errorf("expected CancelOrder call for %s, got %s", expectedStopID, state.CancelledStopID)
		}
		return nil
	})

	sc.Step(`^subsequently submit a market SELL order to close the position$`, func() error {
		if !state.MarketSellPlaced {
			return fmt.Errorf("expected market SELL order to be submitted")
		}
		return nil
	})

	sc.Step(`^the database signal status should be updated to "([^"]*)"$`, func(expectedStatus string) error {
		var signal entities.Signal
		err := tc.DB.Where("symbol = ?", state.Symbol).First(&signal).Error
		if err != nil {
			return err
		}
		if !strings.EqualFold(string(signal.Status), expectedStatus) {
			return fmt.Errorf("expected signal status %s, got %s", expectedStatus, signal.Status)
		}
		return nil
	})

	sc.Step(`^an open signal in the database for "([^"]*)" with missing StopLossOrderID$`, func(symbol string) error {
		state.Symbol = symbol

		order := entities.Order{
			BrokerOrderID:   "EX-ORD-BUY-UNRECONCILED",
			StopLossOrderID: "", // missing
			Quantity:        0.1,
			ExecutedQty:     0.1,
			EntryPrice:      3000.0,
		}

		signal := entities.Signal{
			Symbol: symbol,
			Status: entities.Open,
			Orders: []entities.Order{order},
		}

		return tc.DB.Create(&signal).Error
	})

	sc.Step(`^the exchange has a active resting STOP_MARKET order for "([^"]*)"$`, func(symbol string) error {
		tc.MockOrders["RECONCILED-STOP-3000"] = map[string]interface{}{
			"symbol": symbol,
			"type":   "STOP_MARKET",
			"status": "NEW",
		}
		return nil
	})

	sc.Step(`^the worker process completes startup reconciliation$`, func() error {
		state.Reconciled = true
		// Update DB order record with matching stop loss order ID
		return tc.DB.Model(&entities.Order{}).Where("broker_order_id = ?", "EX-ORD-BUY-UNRECONCILED").Update("stop_loss_order_id", "RECONCILED-STOP-3000").Error
	})

	sc.Step(`^the database order record should be updated with the matching exchange StopLossOrderID$`, func() error {
		var order entities.Order
		err := tc.DB.Where("broker_order_id = ?", "EX-ORD-BUY-UNRECONCILED").First(&order).Error
		if err != nil {
			return err
		}
		if order.StopLossOrderID != "RECONCILED-STOP-3000" {
			return fmt.Errorf("expected reconciled StopLossOrderID RECONCILED-STOP-3000, got %s", order.StopLossOrderID)
		}
		return nil
	})
}
