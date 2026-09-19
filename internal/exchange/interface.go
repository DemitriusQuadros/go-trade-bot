// Package exchange is the Anti-Corruption Layer boundary for all exchange
// interaction (PRD SS5/SS6). No package outside internal/exchange may import
// github.com/adshao/go-binance/v2 - every Binance interaction flows through
// the ExchangeClient interface defined here.
package exchange

import (
	"context"
	"time"
)

// ExchangeClient is the single ACL interface every trading call site depends
// on. BinanceAdapter (production) and BinanceTestnetAdapter (Paper Trading,
// Spec 10) both implement it.
type ExchangeClient interface {
	PlaceOrder(ctx context.Context, order PlaceOrderRequest) (OrderResult, error)
	CancelOrder(ctx context.Context, symbol, orderID string) error
	// GetOrder looks up an order's current status by broker order ID. This
	// method is not in the PRD's illustrative interface, but Spec 03 (restart
	// reconciliation for the stop-loss leg, and the "stop already filled"
	// race in GenerateSellSignal) explicitly requires an order-status lookup
	// and instructs that it "should be resolved by adding GetOrder(...) to
	// ExchangeClient before Spec 01 is implemented" - added here per that
	// resolved judgment call rather than treating the interface as already
	// final without it.
	GetOrder(ctx context.Context, symbol, orderID string) (OrderResult, error)
	ListKline(ctx context.Context, symbol, interval string, limit int) ([]Candle, error)
	ListTickerPrices(ctx context.Context, symbol string) ([]TickerPrice, error)
	GetAccountBalance(ctx context.Context) (AccountBalance, error)
	SubscribeKline(ctx context.Context, symbol, interval string) (<-chan Candle, error)
}

// HistoricalKlineFetcher is a narrow capability interface, deliberately NOT
// folded into ExchangeClient above: fetching an explicit historical date
// range only ever makes sense for a one-shot/scheduled deep backfill against
// the real exchange (app/usecase/candleimport), never for the live trading
// engine or the backtest simulator (app/engine.SimulatedFillExchange, which
// only ever wants "N candles before simulated time asOf" from locally stored
// data - "a historical range fetch from Binance" is meaningless there).
// Folding this onto ExchangeClient would force a real implementation onto
// every implementer for a method almost none of them can meaningfully serve.
// BinanceAdapter implements this (and BinanceTestnetAdapter inherits it via
// embedding); callers that need it type-assert an ExchangeClient value
// against this interface rather than depending on it directly.
type HistoricalKlineFetcher interface {
	// ListKlineRange fetches klines anchored to an explicit
	// [startTime, endTime) window - unlike ListKline, which has no
	// start/end params and therefore always returns the most recent
	// `limit` candles regardless of caller intent.
	ListKlineRange(ctx context.Context, symbol, interval string, startTime, endTime time.Time, limit int) ([]Candle, error)
}
