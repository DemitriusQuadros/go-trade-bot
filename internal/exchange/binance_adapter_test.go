package exchange

import (
	"errors"
	"testing"
	"time"

	"go-trade-bot/internal/configuration"

	"github.com/adshao/go-binance/v2"
	"github.com/adshao/go-binance/v2/common"
	"github.com/stretchr/testify/assert"
)

func TestNewBinanceAdapter_FailsFastOnMissingCredentials(t *testing.T) {
	_, err := NewBinanceAdapter(&configuration.Configuration{})
	assert.Error(t, err)
}

func TestNewBinanceAdapter_Succeeds(t *testing.T) {
	adapter, err := NewBinanceAdapter(&configuration.Configuration{
		Broker: configuration.Broker{ApiKey: "key", ApiSecret: "secret"},
	})
	assert.NoError(t, err)
	assert.NotNil(t, adapter)
}

func TestNewBinanceTestnetAdapter_FailsFastOnMissingCredentials(t *testing.T) {
	_, err := NewBinanceTestnetAdapter(&configuration.Configuration{})
	assert.Error(t, err)
}

func TestNewBinanceTestnetAdapter_Succeeds(t *testing.T) {
	adapter, err := NewBinanceTestnetAdapter(&configuration.Configuration{
		Broker: configuration.Broker{TestnetApiKey: "key", TestnetApiSecret: "secret"},
	})
	assert.NoError(t, err)
	assert.NotNil(t, adapter)
	assert.True(t, binance.UseTestnet)
}

func TestToBinanceSide(t *testing.T) {
	assert.Equal(t, binance.SideTypeBuy, toBinanceSide(SideBuy))
	assert.Equal(t, binance.SideTypeSell, toBinanceSide(SideSell))
}

func TestToBinanceOrderType(t *testing.T) {
	assert.Equal(t, binance.OrderTypeMarket, toBinanceOrderType(OrderTypeMarket))
	assert.Equal(t, binance.OrderTypeStopLoss, toBinanceOrderType(OrderTypeStopMarket))
}

func TestMapBinanceStatus(t *testing.T) {
	cases := map[binance.OrderStatusType]OrderStatus{
		binance.OrderStatusTypeNew:             OrderStatusNew,
		binance.OrderStatusTypeFilled:          OrderStatusFilled,
		binance.OrderStatusTypePartiallyFilled: OrderStatusPartiallyFilled,
		binance.OrderStatusTypeRejected:        OrderStatusRejected,
		binance.OrderStatusTypeExpired:         OrderStatusExpired,
		binance.OrderStatusTypeCanceled:        OrderStatusCanceled,
	}
	for in, want := range cases {
		assert.Equal(t, want, mapBinanceStatus(in))
	}
}

func TestAvgFillPrice(t *testing.T) {
	// market order fully filled: derived from cumulative quote / executed qty
	assert.Equal(t, 50000.0, avgFillPrice(1.0, 50000.0, "0"))
	// resting stop, not yet filled: falls back to the order's own price field
	assert.Equal(t, 48000.0, avgFillPrice(0, 0, "48000"))
	// malformed price fallback
	assert.Equal(t, 0.0, avgFillPrice(0, 0, "not-a-number"))
}

func TestOrderResultFromCreate(t *testing.T) {
	res := &binance.CreateOrderResponse{
		OrderID:                  12345,
		ClientOrderID:            "gtb-1-buy-1",
		Status:                   binance.OrderStatusTypeFilled,
		ExecutedQuantity:         "0.02",
		CummulativeQuoteQuantity: "1000",
		Price:                    "0",
	}
	got := orderResultFromCreate(res)
	assert.Equal(t, "12345", got.BrokerOrderID)
	assert.Equal(t, "gtb-1-buy-1", got.ClientOrderID)
	assert.Equal(t, OrderStatusFilled, got.Status)
	assert.Equal(t, 0.02, got.ExecutedQty)
	assert.Equal(t, 50000.0, got.AvgFillPrice)
}

func TestCandleFromKline(t *testing.T) {
	k := &binance.Kline{
		OpenTime: 1700000000000,
		Open:     "100.0",
		High:     "110.0",
		Low:      "90.0",
		Close:    "105.0",
		Volume:   "12.5",
	}
	c, err := candleFromKline("BTCUSDT", "1m", k)
	assert.NoError(t, err)
	assert.Equal(t, "BTCUSDT", c.Symbol)
	assert.Equal(t, "1m", c.Timeframe)
	assert.Equal(t, time.UnixMilli(1700000000000), c.OpenTime)
	assert.Equal(t, 100.0, c.Open)
	assert.Equal(t, 110.0, c.High)
	assert.Equal(t, 90.0, c.Low)
	assert.Equal(t, 105.0, c.Close)
	assert.Equal(t, 12.5, c.Volume)
}

func TestCandleFromKline_MalformedField(t *testing.T) {
	k := &binance.Kline{Open: "not-a-number"}
	_, err := candleFromKline("BTCUSDT", "1m", k)
	assert.Error(t, err)
}

func TestClassifyPlaceOrderError_InsufficientBalance(t *testing.T) {
	err := classifyPlaceOrderError(&common.APIError{Code: binanceErrCodeInsufficientBalance, Message: "no funds"})
	assert.ErrorIs(t, err, ErrInsufficientBalance)
}

func TestClassifyPlaceOrderError_FilterViolation(t *testing.T) {
	err := classifyPlaceOrderError(&common.APIError{Code: binanceErrCodeFilterFailure, Message: "LOT_SIZE"})
	assert.ErrorIs(t, err, ErrFilterViolation)
}

func TestClassifyPlaceOrderError_GenericRejection(t *testing.T) {
	err := classifyPlaceOrderError(&common.APIError{Code: -9999, Message: "some other rejection"})
	assert.ErrorIs(t, err, ErrOrderRejected)
}

func TestClassifyPlaceOrderError_NonAPIErrorPassesThroughUnchanged(t *testing.T) {
	original := errors.New("context deadline exceeded")
	err := classifyPlaceOrderError(original)
	assert.Equal(t, original, err)
	assert.NotErrorIs(t, err, ErrOrderRejected)
}

func TestClassifyLookupError_UnknownOrder(t *testing.T) {
	err := classifyLookupError(&common.APIError{Code: binanceErrCodeUnknownOrder, Message: "Unknown order sent."})
	assert.ErrorIs(t, err, ErrOrderNotFound)
}

func TestClassifyLookupError_OtherErrorsPassThrough(t *testing.T) {
	original := errors.New("network blip")
	err := classifyLookupError(original)
	assert.Equal(t, original, err)
}

func TestCandleFromWsKline(t *testing.T) {
	k := binance.WsKline{
		StartTime: 1700000000000,
		Open:      "100.0",
		High:      "110.0",
		Low:       "90.0",
		Close:     "105.0",
		Volume:    "12.5",
		IsFinal:   true,
	}
	c, err := candleFromWsKline("ETHUSDT", "5m", k)
	assert.NoError(t, err)
	assert.Equal(t, "ETHUSDT", c.Symbol)
	assert.Equal(t, "5m", c.Timeframe)
	assert.Equal(t, 105.0, c.Close)
}
