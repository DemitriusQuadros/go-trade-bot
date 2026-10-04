package exchange

import (
	"fmt"

	"go-trade-bot/internal/configuration"
)

// NewExchangeClientFromConfig selects between the production and testnet
// adapters based on cfg.Testnet (Spec 01/10). It is the single source of
// truth both cmd/{api,worker}/modules/exchange.go's NewExchangeClient (used
// once at fx graph-build time) and SwappableExchangeClient.SwapFromConfig
// (Spec backend-05, used at runtime on a risk-bearing settings change) call
// into, so the two never drift.
func NewExchangeClientFromConfig(cfg *configuration.Configuration) (ExchangeClient, error) {
	if cfg.Testnet {
		adapter, err := NewBinanceTestnetAdapter(cfg)
		if err != nil {
			return nil, fmt.Errorf("exchange: %w", err)
		}
		return adapter, nil
	}
	adapter, err := NewBinanceAdapter(cfg)
	if err != nil {
		return nil, fmt.Errorf("exchange: %w", err)
	}
	return adapter, nil
}
