package modules

import (
	"fmt"
	"go-trade-bot/internal/configuration"
	"go-trade-bot/internal/exchange"

	"go.uber.org/fx"
)

// ExchangeModule provides the single ExchangeClient ACL implementation for
// the worker process (Spec 01). Selection between production and testnet is
// driven by configuration.Configuration.Testnet (Spec 10 ModePaper requires
// Testnet == true; ModeLive requires Testnet == false - see NewExchangeClient).
//
// This coexists with BrokerModule (internal/broker) until Spec 08 deletes
// the not-yet-ported app/services/algorithm/* packages, which are the last
// remaining consumers of the concrete broker.Broker type - see final report
// for why BrokerModule isn't deleted in this same change despite Spec 01's
// text describing it as superseded.
var ExchangeModule = fx.Module("exchange",
	fx.Provide(NewExchangeClient),
)

func NewExchangeClient(cfg *configuration.Configuration) (exchange.ExchangeClient, error) {
	if cfg.Testnet {
		adapter, err := exchange.NewBinanceTestnetAdapter(cfg)
		if err != nil {
			return nil, fmt.Errorf("exchange module: %w", err)
		}
		return adapter, nil
	}
	adapter, err := exchange.NewBinanceAdapter(cfg)
	if err != nil {
		return nil, fmt.Errorf("exchange module: %w", err)
	}
	return adapter, nil
}
