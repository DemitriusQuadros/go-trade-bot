package exchange

import (
	"context"
	"errors"
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

var _ ExchangeClient = (*ReadOnlyClient)(nil)

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
