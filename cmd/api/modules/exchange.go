package modules

import (
	"fmt"
	"go-trade-bot/internal/configuration"
	"go-trade-bot/internal/exchange"

	"go.uber.org/fx"
)

// ExchangeModule provides the single ExchangeClient ACL implementation for
// the API process (Spec 01). cmd/api needs a real ExchangeClient because
// SignalUseCase.Close (POST /signal/close/{id}) now places a real market
// sell order (Spec 02), not just a DB write.
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
