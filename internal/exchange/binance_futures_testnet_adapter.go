package exchange

import (
	"fmt"

	"go-trade-bot/internal/configuration"

	"github.com/adshao/go-binance/v2/futures"
)

// BinanceFuturesTestnetAdapter implements the identical ExchangeClient, HistoricalKlineFetcher,
// and FuturesClient interfaces as BinanceFuturesAdapter, pointed at Binance's USD-M Futures testnet.
// It embeds a *BinanceFuturesAdapter so all methods are shared; only construction differs.
type BinanceFuturesTestnetAdapter struct {
	*BinanceFuturesAdapter
}

var (
	_ ExchangeClient         = (*BinanceFuturesTestnetAdapter)(nil)
	_ HistoricalKlineFetcher = (*BinanceFuturesTestnetAdapter)(nil)
	_ FuturesClient          = (*BinanceFuturesTestnetAdapter)(nil)
)

// NewBinanceFuturesTestnetAdapter builds a testnet-pointed Futures adapter using testnet credentials.
func NewBinanceFuturesTestnetAdapter(cfg *configuration.Configuration) (*BinanceFuturesTestnetAdapter, error) {
	if cfg.Broker.TestnetApiKey == "" || cfg.Broker.TestnetApiSecret == "" {
		return nil, fmt.Errorf("exchange: BROKER.TESTNET_KEY/BROKER.TESTNET_SECRET must be configured to build a BinanceFuturesTestnetAdapter")
	}
	futures.UseTestnet = true
	client := futures.NewClient(cfg.Broker.TestnetApiKey, cfg.Broker.TestnetApiSecret)
	posMode := PositionMode(cfg.PositionMode)
	if posMode == "" {
		posMode = PositionMode(cfg.Broker.PositionMode)
	}
	if posMode != PositionModeHedge {
		posMode = PositionModeOneWay
	}
	base := &BinanceFuturesAdapter{
		client:        client,
		positionMode:  posMode,
		leverageCache: make(map[string]int),
		marginCache:   make(map[string]MarginMode),
		filtersCache:  make(map[string]symbolFilters),
	}
	return &BinanceFuturesTestnetAdapter{BinanceFuturesAdapter: base}, nil
}
