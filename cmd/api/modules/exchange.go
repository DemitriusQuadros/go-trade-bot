package modules

import (
	"go-trade-bot/internal/configuration"
	"go-trade-bot/internal/exchange"

	"go.uber.org/fx"
)

// ExchangeModule provides the single ExchangeClient ACL implementation for
// the API process (Spec 01). cmd/api needs a real ExchangeClient because
// SignalUseCase.Close (POST /signal/close/{id}) now places a real market
// sell order (Spec 02), not just a DB write.
//
// Spec backend-05 (ADR-016): wrapped in *exchange.SwappableExchangeClient for
// the same reason as cmd/worker/modules/exchange.go - see that file's comment
// for the full rationale.
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
