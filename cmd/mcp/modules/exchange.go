package modules

import (
	"go-trade-bot/internal/configuration"
	"go-trade-bot/internal/exchange"

	"go.uber.org/fx"
)

// ExchangeModule mirrors cmd/worker/modules/exchange.go's wiring. cmd/mcp
// never places a real order itself (no tool in app/usecase/agent's registry
// can - Backend Spec 03 AC#9), but it still needs a real
// exchange.ExchangeClient to construct app/usecase/signal.SignalUseCase
// as-is (Backend Spec 03's get_open_positions tool wraps that use case's
// existing GetAllOpen, unchanged) - see cmd/mcp/modules/signal.go.
var ExchangeModule = fx.Module("exchange",
	fx.Provide(
		NewSwappableExchangeClient,
		asExchangeClientInterface,
	),
)

func NewSwappableExchangeClient(cfg *configuration.Configuration) (*exchange.SwappableExchangeClient, error) {
	initial, err := exchange.NewExchangeClientFromConfig(cfg)
	if err != nil {
		return nil, err
	}
	return exchange.NewSwappableExchangeClient(initial), nil
}

func asExchangeClientInterface(s *exchange.SwappableExchangeClient) exchange.ExchangeClient {
	return s
}
