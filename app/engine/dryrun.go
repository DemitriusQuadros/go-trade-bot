package engine

import (
	"go-trade-bot/app/entities"
	"go-trade-bot/app/strategies"
	"go-trade-bot/internal/configuration"
	"go-trade-bot/internal/exchange"
	"go-trade-bot/internal/feed"
)

// NewDryRunDriver constructs a ReplayDriver wired for Dry-Run: Feed is a live
// WebSocket LiveFeed, reads delegate to the real exchange, and order execution
// is simulated with configurable slippage and fill delay.
func NewDryRunDriver(
	liveFeed *feed.LiveFeed,
	realExchange exchange.ExchangeClient,
	fillCfg configuration.DryRunConfig,
	eng *Engine,
	strategy strategies.Strategy,
	dbStrategy entities.Strategy,
	symbol string,
	signalRepo SignalRepositoryReader,
) *ReplayDriver {
	dataSource := NewRealExchangeMarketDataSource(realExchange)
	policy := FillPolicy{
		SlippagePct: fillCfg.SlippagePct,
		FillDelay:   fillCfg.FillDelay,
	}
	simExchange := NewSimulatedFillExchange(dataSource, policy)

	return NewReplayDriver(
		liveFeed,
		simExchange,
		eng,
		strategy,
		dbStrategy,
		symbol,
		strategies.ModeDryRun,
		signalRepo,
	)
}
