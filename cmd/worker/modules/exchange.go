package modules

import (
	"go-trade-bot/internal/configuration"
	"go-trade-bot/internal/exchange"

	"go.uber.org/fx"
)

// ExchangeModule provides the single ExchangeClient ACL implementation for
// the worker process (Spec 01). Selection between production and testnet is
// driven by configuration.Configuration.Testnet (Spec 10 ModePaper requires
// Testnet == true; ModeLive requires Testnet == false - see
// exchange.NewExchangeClientFromConfig).
//
// Spec backend-05 (ADR-016): the concrete client built once at startup is
// wrapped in a *exchange.SwappableExchangeClient, and it is the Swappable -
// not the raw adapter - that fx provides as the exchange.ExchangeClient
// interface. Every existing consumer (Engine, SignalUseCase, the strategy
// task processor) depends on the interface, never the concrete adapter type,
// so this is a zero-behavior-change wiring swap until something actually
// calls SwapFromConfig at runtime (app/usecase/settings). fx also provides
// the concrete *exchange.SwappableExchangeClient type directly so the
// settings usecase can be injected with it and call SwapFromConfig.
//
// This coexists with BrokerModule (internal/broker) until Spec 08 deletes
// the not-yet-ported app/services/algorithm/* packages, which are the last
// remaining consumers of the concrete broker.Broker type - see final report
// for why BrokerModule isn't deleted in this same change despite Spec 01's
// text describing it as superseded.
var ExchangeModule = fx.Module("exchange",
	fx.Provide(
		NewSwappableExchangeClient,
		asExchangeClientInterface,
	),
)

// NewSwappableExchangeClient builds the initial concrete adapter once, at fx
// graph-build time (exactly as the pre-Spec-backend-05 NewExchangeClient
// did), then wraps it in the Swappable indirection.
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
