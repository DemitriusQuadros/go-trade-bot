package exchange

import (
	"fmt"

	"go-trade-bot/internal/configuration"

	"github.com/adshao/go-binance/v2"
)

// BinanceTestnetAdapter implements the identical ExchangeClient interface as
// BinanceAdapter, pointed at Binance's spot testnet - used exclusively for
// ModePaper (Spec 10). It embeds a *BinanceAdapter so every method is shared;
// only construction differs (testnet credentials, testnet base URLs).
type BinanceTestnetAdapter struct {
	*BinanceAdapter
}

// NewBinanceTestnetAdapter builds a testnet-pointed adapter using
// testnet-specific credentials (never the production Broker.ApiKey/ApiSecret).
//
// go-binance's REST/WS base URLs are controlled by the package-level
// binance.UseTestnet flag (there is no per-client override in this SDK
// version). This is safe in this codebase because the exchange fx module
// (cmd/worker/modules/exchange.go) constructs exactly one ExchangeClient per
// process - either a BinanceAdapter or a BinanceTestnetAdapter, never both -
// so toggling the global at construction time cannot cross-contaminate a
// concurrently-running production adapter in the same process.
func NewBinanceTestnetAdapter(cfg *configuration.Configuration) (*BinanceTestnetAdapter, error) {
	if cfg.Broker.TestnetApiKey == "" || cfg.Broker.TestnetApiSecret == "" {
		return nil, fmt.Errorf("exchange: BROKER.TESTNET_KEY/BROKER.TESTNET_SECRET must be configured to build a BinanceTestnetAdapter")
	}
	binance.UseTestnet = true
	client := binance.NewClient(cfg.Broker.TestnetApiKey, cfg.Broker.TestnetApiSecret)
	return &BinanceTestnetAdapter{BinanceAdapter: &BinanceAdapter{client: client}}, nil
}
