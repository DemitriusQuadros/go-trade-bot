package exchange

import "time"

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
}

// OrderResult is the ACL's response shape, returned by PlaceOrder/GetOrder.
type OrderResult struct {
	BrokerOrderID string
	ClientOrderID string
	Status        OrderStatus
	ExecutedQty   float64
	AvgFillPrice  float64 // 0 when Status == NEW (resting stop not yet triggered)
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

// AccountBalance is the ACL's balance shape for a single asset.
type AccountBalance struct {
	Asset  string
	Free   float64
	Locked float64
}
