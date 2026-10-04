package exchange

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func assertNoRealOrders(t *testing.T, inner *mockInner) {
	t.Helper()
	inner.AssertNotCalled(t, "PlaceOrder", mock.Anything, mock.Anything)
	inner.AssertNotCalled(t, "CancelOrder", mock.Anything, mock.Anything, mock.Anything)
}

func TestDryRunExchange_MarketOrdersFillAtTickerWithSlippage_NeverReachExchange(t *testing.T) {
	inner := &mockInner{}
	inner.On("ListTickerPrices", mock.Anything, "BTCUSDT").Return([]TickerPrice{{Symbol: "BTCUSDT", Price: 100}}, nil)
	now := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	d := NewDryRunExchange(inner, 0.5, nil)
	d.Now = func() time.Time { return now }

	buy, err := d.PlaceOrder(context.Background(), PlaceOrderRequest{Symbol: "BTCUSDT", Side: SideBuy, Type: OrderTypeMarket, Quantity: 2, ClientOrderID: "c1"})
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(buy.BrokerOrderID, SimulatedOrderIDPrefix))
	assert.Equal(t, OrderStatusFilled, buy.Status)
	assert.Equal(t, 2.0, buy.ExecutedQty)
	assert.InDelta(t, 100.5, buy.AvgFillPrice, 1e-9) // worse for the buyer
	assert.Equal(t, now, buy.FilledAt)
	assert.Equal(t, "c1", buy.ClientOrderID)

	sell, err := d.PlaceOrder(context.Background(), PlaceOrderRequest{Symbol: "BTCUSDT", Side: SideSell, Type: OrderTypeMarket, Quantity: 2})
	require.NoError(t, err)
	assert.InDelta(t, 99.5, sell.AvgFillPrice, 1e-9) // worse for the seller
	assert.NotEqual(t, buy.BrokerOrderID, sell.BrokerOrderID)

	got, err := d.GetOrder(context.Background(), "BTCUSDT", buy.BrokerOrderID)
	require.NoError(t, err)
	assert.Equal(t, buy, got)

	assertNoRealOrders(t, inner)
}

func TestDryRunExchange_StopMarketRestsAndCanBeCancelledOrTriggered(t *testing.T) {
	inner := &mockInner{}
	d := NewDryRunExchange(inner, 1, nil)
	ctx := context.Background()

	stop, err := d.PlaceOrder(ctx, PlaceOrderRequest{Symbol: "BTCUSDT", Side: SideSell, Type: OrderTypeStopMarket, Quantity: 1, StopPrice: 90})
	require.NoError(t, err)
	assert.Equal(t, OrderStatusNew, stop.Status)
	assert.True(t, IsSimulatedOrderID(stop.BrokerOrderID))

	// Resting: GetOrder says NEW (works on any replica - no memory needed),
	// cancel succeeds.
	res, err := d.GetOrder(ctx, "BTCUSDT", stop.BrokerOrderID)
	require.NoError(t, err)
	assert.Equal(t, OrderStatusNew, res.Status)
	assert.NoError(t, d.CancelOrder(ctx, "BTCUSDT", stop.BrokerOrderID))

	// Triggered: cancel reports not-found (already filled), GetOrder returns
	// the fill at stop minus slippage.
	at := time.Date(2026, 1, 1, 11, 0, 0, 0, time.UTC)
	fill := d.TriggerStop(stop.BrokerOrderID, 1, 90, at)
	assert.InDelta(t, 89.1, fill.AvgFillPrice, 1e-9)
	assert.ErrorIs(t, d.CancelOrder(ctx, "BTCUSDT", stop.BrokerOrderID), ErrOrderNotFound)
	res, err = d.GetOrder(ctx, "BTCUSDT", stop.BrokerOrderID)
	require.NoError(t, err)
	assert.Equal(t, OrderStatusFilled, res.Status)
	assert.Equal(t, at, res.FilledAt)

	d.ClearTriggeredStop(stop.BrokerOrderID)
	assert.NoError(t, d.CancelOrder(ctx, "BTCUSDT", stop.BrokerOrderID))

	assertNoRealOrders(t, inner)
}

func TestDryRunExchange_NonSimulatedIDsAreReadOnly(t *testing.T) {
	inner := &mockInner{}
	inner.On("GetOrder", mock.Anything, "BTCUSDT", "12345").Return(OrderResult{BrokerOrderID: "12345", Status: OrderStatusNew}, nil)
	d := NewDryRunExchange(inner, 0, nil)

	assert.ErrorIs(t, d.CancelOrder(context.Background(), "BTCUSDT", "12345"), ErrReadOnlyClient)
	res, err := d.GetOrder(context.Background(), "BTCUSDT", "12345")
	require.NoError(t, err)
	assert.Equal(t, "12345", res.BrokerOrderID)

	_, err = d.GetOrder(context.Background(), "BTCUSDT", SimulatedOrderIDPrefix+"unknown")
	assert.ErrorIs(t, err, ErrOrderNotFound)

	assertNoRealOrders(t, inner)
}

func TestDryRunExchange_RejectsInvalidOrders(t *testing.T) {
	inner := &mockInner{}
	d := NewDryRunExchange(inner, 0, nil)
	ctx := context.Background()

	_, err := d.PlaceOrder(ctx, PlaceOrderRequest{Symbol: "BTCUSDT", Side: SideBuy, Type: OrderTypeMarket, Quantity: 0})
	assert.ErrorIs(t, err, ErrOrderRejected)
	_, err = d.PlaceOrder(ctx, PlaceOrderRequest{Symbol: "BTCUSDT", Side: SideSell, Type: OrderTypeStopMarket, Quantity: 1})
	assert.ErrorIs(t, err, ErrOrderRejected)
	_, err = d.PlaceOrder(ctx, PlaceOrderRequest{Symbol: "BTCUSDT", Side: SideBuy, Type: OrderType("LIMIT"), Quantity: 1})
	assert.ErrorIs(t, err, ErrOrderRejected)

	inner.On("ListTickerPrices", mock.Anything, "ETHUSDT").Return([]TickerPrice{}, nil)
	_, err = d.PlaceOrder(ctx, PlaceOrderRequest{Symbol: "ETHUSDT", Side: SideBuy, Type: OrderTypeMarket, Quantity: 1})
	assert.Error(t, err)

	assertNoRealOrders(t, inner)
}

func TestDryRunExchange_ReadsDelegateAndIsSimulatedClient(t *testing.T) {
	inner := &mockInner{}
	candles := []Candle{{Symbol: "BTCUSDT", Close: 1}}
	inner.On("ListKline", mock.Anything, "BTCUSDT", "1m", 10).Return(candles, nil)
	inner.On("GetAccountBalance", mock.Anything).Return(AccountBalance{Asset: "USDT", Free: 5}, nil)
	d := NewDryRunExchange(inner, 0, nil)

	got, err := d.ListKline(context.Background(), "BTCUSDT", "1m", 10)
	require.NoError(t, err)
	assert.Equal(t, candles, got)
	bal, err := d.GetAccountBalance(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 5.0, bal.Free)

	assert.True(t, IsSimulatedClient(d))
	assert.False(t, IsSimulatedClient(inner))
	assert.False(t, IsSimulatedClient(NewReadOnlyClient(inner, nil)))
}
