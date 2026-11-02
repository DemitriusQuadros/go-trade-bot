package handler_test

import (
	"encoding/json"
	"testing"
	"time"

	"go-trade-bot/app/entities"
	handler "go-trade-bot/app/handler/web/signal"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToOrderResponse_FuturesFields(t *testing.T) {
	now := time.Now().UTC()
	order := entities.Order{
		ID:                 10,
		SignalID:           20,
		PositionSide:       entities.PositionSideShort,
		BrokerOrderID:      "ORD-123",
		StopLossOrderID:    "SL-123",
		StopLossPrice:      2100.5,
		TakeProfitPrice:    1900.0,
		ExitReason:         "take_profit",
		EntryPrice:         2000.0,
		ExitPrice:          1900.0,
		Quantity:           1.5,
		InvestedAmount:     300.0,
		MarginType:         entities.Isolated,
		EntryFee:           1.2,
		ExitFee:            1.1,
		Leverage:           10.0,
		ExecutedQty:        1.5,
		InitialMargin:      300.0,
		LiquidationPrice:   2200.0,
		AccumulatedFunding: -0.45,
		IsClosing:          false,
		Profit:             150.0,
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	dto := handler.ToOrderResponse(order)

	assert.Equal(t, uint(10), dto.ID)
	assert.Equal(t, uint(20), dto.SignalID)
	assert.Equal(t, "SHORT", dto.PositionSide)
	assert.Equal(t, float32(300.0), dto.InitialMargin)
	assert.Equal(t, float32(2200.0), dto.LiquidationPrice)
	assert.Equal(t, float32(-0.45), dto.AccumulatedFunding)

	data, err := json.Marshal(dto)
	require.NoError(t, err)

	var jsonMap map[string]any
	require.NoError(t, json.Unmarshal(data, &jsonMap))

	assert.Equal(t, "SHORT", jsonMap["position_side"])
	assert.Equal(t, float64(float32(300.0)), jsonMap["initial_margin"])
	assert.Equal(t, float64(float32(2200.0)), jsonMap["liquidation_price"])
	assert.InDelta(t, float64(-0.45), jsonMap["accumulated_funding"], 0.001)
}

func TestToOrderResponse_ZeroFuturesFields(t *testing.T) {
	order := entities.Order{
		ID:       1,
		SignalID: 2,
	}

	dto := handler.ToOrderResponse(order)

	assert.Empty(t, dto.PositionSide)
	assert.Equal(t, float32(0), dto.InitialMargin)
	assert.Equal(t, float32(0), dto.LiquidationPrice)
	assert.Equal(t, float32(0), dto.AccumulatedFunding)

	data, err := json.Marshal(dto)
	require.NoError(t, err)

	var jsonMap map[string]any
	require.NoError(t, json.Unmarshal(data, &jsonMap))

	// position_side has omitempty, so it should not appear when empty
	assert.NotContains(t, jsonMap, "position_side")

	// initial_margin, liquidation_price, accumulated_funding do NOT have omitempty
	assert.Contains(t, jsonMap, "initial_margin")
	assert.Equal(t, float64(0), jsonMap["initial_margin"])
	assert.Contains(t, jsonMap, "liquidation_price")
	assert.Equal(t, float64(0), jsonMap["liquidation_price"])
	assert.Contains(t, jsonMap, "accumulated_funding")
	assert.Equal(t, float64(0), jsonMap["accumulated_funding"])
}

func TestToSignalResponse_FuturesFields(t *testing.T) {
	now := time.Now().UTC()
	signal := entities.Signal{
		ID:           5,
		Symbol:       "ETHUSDT",
		StrategyID:   12,
		Status:       entities.Open,
		Mode:         "paper",
		PositionSide: entities.PositionSideShort,
		Orders: []entities.Order{
			{
				ID:                 1,
				SignalID:           5,
				PositionSide:       entities.PositionSideShort,
				InitialMargin:      50.0,
				LiquidationPrice:   2500.0,
				AccumulatedFunding: 0.15,
			},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	dto := handler.ToSignalResponse(signal)

	assert.Equal(t, uint(5), dto.ID)
	assert.Equal(t, "SHORT", dto.PositionSide)
	require.Len(t, dto.Orders, 1)
	assert.Equal(t, "SHORT", dto.Orders[0].PositionSide)
	assert.Equal(t, float32(50.0), dto.Orders[0].InitialMargin)
	assert.Equal(t, float32(2500.0), dto.Orders[0].LiquidationPrice)
	assert.Equal(t, float32(0.15), dto.Orders[0].AccumulatedFunding)

	data, err := json.Marshal(dto)
	require.NoError(t, err)

	var jsonMap map[string]any
	require.NoError(t, json.Unmarshal(data, &jsonMap))

	assert.Equal(t, "SHORT", jsonMap["position_side"])

	orders, ok := jsonMap["orders"].([]any)
	require.True(t, ok)
	require.Len(t, orders, 1)
	orderMap := orders[0].(map[string]any)
	assert.Equal(t, "SHORT", orderMap["position_side"])
	assert.Equal(t, float64(50.0), orderMap["initial_margin"])
	assert.Equal(t, float64(2500.0), orderMap["liquidation_price"])
	assert.InDelta(t, float64(0.15), orderMap["accumulated_funding"], 0.001)
}

func TestToSignalResponse_EmptyPositionSideOmitted(t *testing.T) {
	signal := entities.Signal{
		ID:     1,
		Symbol: "BTCUSDT",
		Status: entities.Open,
	}

	dto := handler.ToSignalResponse(signal)
	assert.Empty(t, dto.PositionSide)

	data, err := json.Marshal(dto)
	require.NoError(t, err)

	var jsonMap map[string]any
	require.NoError(t, json.Unmarshal(data, &jsonMap))

	assert.NotContains(t, jsonMap, "position_side")
}

func TestToSignalResponseList(t *testing.T) {
	signals := []entities.Signal{
		{
			ID:           1,
			Symbol:       "BTCUSDT",
			PositionSide: entities.PositionSideLong,
		},
		{
			ID:           2,
			Symbol:       "ETHUSDT",
			PositionSide: entities.PositionSideShort,
		},
	}

	list := handler.ToSignalResponseList(signals)
	require.Len(t, list, 2)
	assert.Equal(t, "LONG", list[0].PositionSide)
	assert.Equal(t, "SHORT", list[1].PositionSide)
}
