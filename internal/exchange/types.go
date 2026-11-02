package exchange

import (
	"context"
	"time"
)

// OrderSide is the direction of a placed order.
type OrderSide string

const (
	SideBuy  OrderSide = "BUY"
	SideSell OrderSide = "SELL"
)

// OrderType is the ACL's own order-type vocabulary. Adapters translate these
// into whatever the concrete exchange SDK expects (see binance_adapter.go).
type OrderType string

const (
	OrderTypeMarket OrderType = "MARKET"
	// OrderTypeStopMarket is the ACL's semantic "stop that fires as a market order".
	// Binance's spot REST API has no literal STOP_MARKET order type (that's a
	// futures-only type) - the closest spot equivalent is STOP_LOSS, which
	// executes as a market order once the stop price is touched. BinanceAdapter
	// maps OrderTypeStopMarket -> binance.OrderTypeStopLoss for spot orders.
	OrderTypeStopMarket OrderType = "STOP_MARKET"
)

// OrderStatus is the ACL's own order-status vocabulary.
type OrderStatus string

const (
	OrderStatusNew             OrderStatus = "NEW" // resting (STOP_MARKET only)
	OrderStatusFilled          OrderStatus = "FILLED"
	OrderStatusPartiallyFilled OrderStatus = "PARTIALLY_FILLED"
	OrderStatusRejected        OrderStatus = "REJECTED"
	OrderStatusExpired         OrderStatus = "EXPIRED"
	OrderStatusCanceled        OrderStatus = "CANCELED"
)

// PlaceOrderRequest is the ACL's request shape for ExchangeClient.PlaceOrder.
type PlaceOrderRequest struct {
	Symbol        string
	Side          OrderSide
	Type          OrderType
	Quantity      float64
	StopPrice     float64 // required iff Type == OrderTypeStopMarket; ignored otherwise
	ClientOrderID string  // caller-supplied idempotency key, see Spec 02 Idempotency
	PositionSide  PositionSide // "LONG" or "SHORT" (mapped by Futures adapter based on One-Way vs Hedge mode; ignored on Spot)
	ReduceOnly    bool         // Futures One-Way: true for protective stops and position-closing market orders; ignored on Spot and suppressed in Hedge Mode
}

// OrderResult is the ACL's response shape, returned by PlaceOrder/GetOrder.
type OrderResult struct {
	BrokerOrderID string
	ClientOrderID string
	Status        OrderStatus
	ExecutedQty   float64
	AvgFillPrice  float64   // 0 when Status == NEW (resting stop not yet triggered)
	FilledAt      time.Time // real exchange fill/update time (live), or simulated candle time (backtest) - never wall-clock time.Now()
}

// Candle is the ACL's OHLCV shape, used everywhere a kline crosses a package
// boundary (strategies, indicators, feed).
type Candle struct {
	Symbol    string
	Timeframe string
	OpenTime  time.Time
	Open      float64
	High      float64
	Low       float64
	Close     float64
	Volume    float64
}

// TickerPrice is the ACL's latest-price shape.
type TickerPrice struct {
	Symbol string
	Price  float64
}

// BookTicker represents real-time best bid and ask order book quotes.
type BookTicker struct {
	Symbol       string
	BestBidPrice float64
	BestBidQty   float64
	BestAskPrice float64
	BestAskQty   float64
	Timestamp    time.Time
}

// AccountBalance is the ACL's balance shape for a single asset.
type AccountBalance struct {
	Asset  string
	Free   float64
	Locked float64
}

// PositionSide specifies the target position leg on Futures.
type PositionSide string

const (
	PositionSideBoth  PositionSide = "BOTH"
	PositionSideLong  PositionSide = "LONG"
	PositionSideShort PositionSide = "SHORT"
)

// MarginMode represents Isolated or Cross margin on Futures.
type MarginMode string

const (
	MarginModeIsolated MarginMode = "ISOLATED"
	MarginModeCross    MarginMode = "CROSSED"
)

// PositionMode represents One-Way ("one_way") vs Hedge ("hedge", dualSidePosition=true) mode.
type PositionMode string

const (
	PositionModeOneWay PositionMode = "one_way"
	PositionModeHedge  PositionMode = "hedge"
)

// PositionRisk represents an open Futures position from GET /fapi/v2/positionRisk.
type PositionRisk struct {
	Symbol           string
	PositionSide     PositionSide // "BOTH" (One-Way) or "LONG"/"SHORT" (Hedge)
	PositionAmt      float64      // >0 for Long, <0 for Short, 0 for Flat
	EntryPrice       float64
	MarkPrice        float64
	UnRealizedProfit float64
	LiquidationPrice float64
	Leverage         int
	MarginType       string // "isolated" or "cross"
	IsolatedMargin   float64
}

// FuturesClient is an optional capability interface implemented by
// BinanceFuturesAdapter, BinanceFuturesTestnetAdapter, SwappableExchangeClient,
// ReadOnlyClient, DryRunExchange, and SimulatedFillExchange.
type FuturesClient interface {
	SetLeverage(ctx context.Context, symbol string, leverage int) error
	SetMarginType(ctx context.Context, symbol string, marginMode MarginMode) error
	SetPositionMode(ctx context.Context, mode PositionMode) error
	GetPositionMode(ctx context.Context) (PositionMode, error)
	GetPositionRisk(ctx context.Context, symbol string, side PositionSide) (PositionRisk, error)
	GetMarkPrice(ctx context.Context, symbol string) (markPrice float64, fundingRate float64, nextFundingTime time.Time, err error)
}
