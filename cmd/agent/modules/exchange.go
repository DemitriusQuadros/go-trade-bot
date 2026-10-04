package modules

import (
	"log"

	taskagent "go-trade-bot/app/handler/tasks/agent"
	"go-trade-bot/internal/configuration"
	"go-trade-bot/internal/exchange"
	"go-trade-bot/internal/metrics"

	"go.uber.org/fx"
)

// ExchangeModule is cmd/agent's SAFETY INVARIANT (agents-platform A-02 §1):
// the only exchange.ExchangeClient in this binary's graph is an
// *exchange.ReadOnlyClient, whose PlaceOrder/CancelOrder always fail with
// exchange.ErrReadOnlyClient. The real adapter is constructed inside
// NewReadOnlyExchangeClient and never provided to the graph on its own -
// there is deliberately no *SwappableExchangeClient or undecorated client
// provider here. MODE/CONFIRM_LIVE are ignored by this binary entirely.
var ExchangeModule = fx.Module("exchange",
	fx.Provide(NewReadOnlyExchangeClient),
)

// NewReadOnlyExchangeClient builds the real adapter (reads: klines, prices,
// balances) and immediately wraps it.
func NewReadOnlyExchangeClient(cfg *configuration.Configuration, collector *metrics.MetricsCollector) (exchange.ExchangeClient, error) {
	inner, err := exchange.NewExchangeClientFromConfig(cfg)
	if err != nil {
		return nil, err
	}
	log.Println("cmd/agent: read-only exchange client, order placement disabled")
	return exchange.NewReadOnlyClient(inner, func(op string) {
		log.Printf("cmd/agent: BLOCKED exchange %s attempt (read-only client)", op)
		collector.IncrementCounter(taskagent.MetricBlockedOrders, map[string]string{"op": op})
	}), nil
}
