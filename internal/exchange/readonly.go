package exchange

import (
	"context"
	"errors"
	"time"
)

// ErrReadOnlyClient is returned by ReadOnlyClient's order-mutating methods.
var ErrReadOnlyClient = errors.New("exchange: read-only client - order placement/cancellation is disabled in this process")

// ReadOnlyClient is the safety decorator cmd/agent wires in place of a real
// ExchangeClient (agents-platform A-02 §1): PlaceOrder and CancelOrder
// ALWAYS fail with ErrReadOnlyClient and never reach the inner client;
// every read method delegates. cmd/agent must never provide the
// undecorated client to its dependency graph.
type ReadOnlyClient struct {
	inner     ExchangeClient
	onBlocked func(op string)
}

// NewReadOnlyClient wraps inner. onBlocked (may be nil) is invoked with
// "place_order" or "cancel_order" on every blocked attempt - cmd/agent uses
// it to increment agent_blocked_order_attempts_total.
func NewReadOnlyClient(inner ExchangeClient, onBlocked func(op string)) *ReadOnlyClient {
	return &ReadOnlyClient{inner: inner, onBlocked: onBlocked}
}

var (
	_ ExchangeClient = (*ReadOnlyClient)(nil)
	_ FuturesClient  = (*ReadOnlyClient)(nil)
)

func (c *ReadOnlyClient) blocked(op string) {
	if c.onBlocked != nil {
		c.onBlocked(op)
	}
}

// PlaceOrder always fails; the inner client is never called.
func (c *ReadOnlyClient) PlaceOrder(context.Context, PlaceOrderRequest) (OrderResult, error) {
	c.blocked("place_order")
	return OrderResult{}, ErrReadOnlyClient
}

// CancelOrder always fails; the inner client is never called.
func (c *ReadOnlyClient) CancelOrder(context.Context, string, string) error {
	c.blocked("cancel_order")
	return ErrReadOnlyClient
}

func (c *ReadOnlyClient) GetOrder(ctx context.Context, symbol, orderID string) (OrderResult, error) {
	return c.inner.GetOrder(ctx, symbol, orderID)
}

func (c *ReadOnlyClient) ListKline(ctx context.Context, symbol, interval string, limit int) ([]Candle, error) {
	return c.inner.ListKline(ctx, symbol, interval, limit)
}

func (c *ReadOnlyClient) ListTickerPrices(ctx context.Context, symbol string) ([]TickerPrice, error) {
	return c.inner.ListTickerPrices(ctx, symbol)
}

func (c *ReadOnlyClient) GetAccountBalance(ctx context.Context) (AccountBalance, error) {
	return c.inner.GetAccountBalance(ctx)
}

func (c *ReadOnlyClient) SubscribeKline(ctx context.Context, symbol, interval string) (<-chan Candle, error) {
	return c.inner.SubscribeKline(ctx, symbol, interval)
}

func (c *ReadOnlyClient) SubscribeBookTicker(ctx context.Context, symbol string) (<-chan BookTicker, error) {
	return c.inner.SubscribeBookTicker(ctx, symbol)
}

func (c *ReadOnlyClient) SetLeverage(ctx context.Context, symbol string, leverage int) error {
	c.blocked("set_leverage")
	return ErrReadOnlyClient
}

func (c *ReadOnlyClient) SetMarginType(ctx context.Context, symbol string, marginMode MarginMode) error {
	c.blocked("set_margin_type")
	return ErrReadOnlyClient
}

func (c *ReadOnlyClient) SetPositionMode(ctx context.Context, mode PositionMode) error {
	c.blocked("set_position_mode")
	return ErrReadOnlyClient
}

func (c *ReadOnlyClient) GetPositionMode(ctx context.Context) (PositionMode, error) {
	fc, ok := c.inner.(FuturesClient)
	if !ok {
		return "", ErrFuturesNotSupported
	}
	return fc.GetPositionMode(ctx)
}

func (c *ReadOnlyClient) GetPositionRisk(ctx context.Context, symbol string, side PositionSide) (PositionRisk, error) {
	fc, ok := c.inner.(FuturesClient)
	if !ok {
		return PositionRisk{}, ErrFuturesNotSupported
	}
	return fc.GetPositionRisk(ctx, symbol, side)
}

func (c *ReadOnlyClient) GetMarkPrice(ctx context.Context, symbol string) (markPrice float64, fundingRate float64, nextFundingTime time.Time, err error) {
	fc, ok := c.inner.(FuturesClient)
	if !ok {
		return 0, 0, time.Time{}, ErrFuturesNotSupported
	}
	return fc.GetMarkPrice(ctx, symbol)
}
