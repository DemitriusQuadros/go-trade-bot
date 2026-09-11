package exchange

import (
	"context"
	"sync/atomic"

	"go-trade-bot/internal/configuration"
)

type SwappableExchangeClient struct {
	current atomic.Pointer[ExchangeClient]
}

func NewSwappableExchangeClient(initial ExchangeClient) *SwappableExchangeClient {
	s := &SwappableExchangeClient{}
	s.current.Store(&initial)
	return s
}

func (s *SwappableExchangeClient) Swap(newClient ExchangeClient) {
	s.current.Store(&newClient)
}

// SwapFromConfig is Spec backend-05's risk-bearing swap entry point: it
// builds a brand new concrete client via NewExchangeClientFromConfig (the
// same fail-fast construction cmd/worker/modules/exchange.go's NewExchangeClient
// used to run once at process start) and only stores it if construction
// succeeds. On failure, the current pointer is left completely untouched -
// a bad credential update must never leave the system with no working
// exchange client at all (Spec backend-05 AC#8).
func (s *SwappableExchangeClient) SwapFromConfig(cfg *configuration.Configuration) error {
	next, err := NewExchangeClientFromConfig(cfg)
	if err != nil {
		return err
	}
	s.Swap(next)
	return nil
}

func (s *SwappableExchangeClient) PlaceOrder(ctx context.Context, order PlaceOrderRequest) (OrderResult, error) {
	return (*s.current.Load()).PlaceOrder(ctx, order)
}

func (s *SwappableExchangeClient) CancelOrder(ctx context.Context, symbol, orderID string) error {
	return (*s.current.Load()).CancelOrder(ctx, symbol, orderID)
}

func (s *SwappableExchangeClient) GetOrder(ctx context.Context, symbol, orderID string) (OrderResult, error) {
	return (*s.current.Load()).GetOrder(ctx, symbol, orderID)
}

func (s *SwappableExchangeClient) ListKline(ctx context.Context, symbol, interval string, limit int) ([]Candle, error) {
	return (*s.current.Load()).ListKline(ctx, symbol, interval, limit)
}

func (s *SwappableExchangeClient) ListTickerPrices(ctx context.Context, symbol string) ([]TickerPrice, error) {
	return (*s.current.Load()).ListTickerPrices(ctx, symbol)
}

func (s *SwappableExchangeClient) GetAccountBalance(ctx context.Context) (AccountBalance, error) {
	return (*s.current.Load()).GetAccountBalance(ctx)
}

func (s *SwappableExchangeClient) SubscribeKline(ctx context.Context, symbol, interval string) (<-chan Candle, error) {
	return (*s.current.Load()).SubscribeKline(ctx, symbol, interval)
}
