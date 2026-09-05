// Package grid ports app/services/algorithm/grid's build/monitor logic into
// the Strategy hook interface (Spec 05/08). Behavior is preserved bit for
// bit except the software stop-loss check, which is intentionally removed -
// the real exchange STOP_MARKET order (Spec 03) is the sole stop-loss
// mechanism post-port (Spec 08's resolved judgment call, 2026-08-31).
package grid

import (
	"log"

	"go-trade-bot/app/strategies"
	"go-trade-bot/internal/memcache"
)

func init() {
	strategies.Register("grid", func() strategies.Strategy {
		return &GridStrategy{}
	})
}

// cacheKeyPrefix matches the original app/services/algorithm/grid/algorithm.go
// cache key format exactly ("grid-" + symbol).
const cacheKeyPrefix = "grid-"

type gridOrder struct {
	Type  string
	Price float64
}

// GridStrategy holds no state of its own - the only cross-cycle state
// (the built grid levels) lives in the engine-owned memcache.Cache passed
// through Context.Config[strategies.ConfigKeyCache] (see final report for
// why this isn't a package-level global or a struct field set at
// registration time: strategies self-register via init(), before fx builds
// the DI graph, so there is no per-instance injection point available then).
type GridStrategy struct{}

func (s *GridStrategy) Name() string { return "grid" }

// Before builds the grid exactly once per symbol, the first time it's
// evaluated with no cached grid yet - matching the current code's dispatch,
// which is keyed purely on cache-presence (not on open-position status).
// Once built, a grid is never rebuilt (Spec 08 AC#9 - a known, intentionally
// carried-forward limitation).
func (s *GridStrategy) Before(ctx strategies.Context) {
	cache, ok := gridCache(ctx)
	if !ok {
		return
	}
	if _, exists := cachedGrid(cache, ctx.Symbol); exists {
		return
	}
	s.buildGrid(ctx, cache)
}

func (s *GridStrategy) buildGrid(ctx strategies.Context, cache memcache.Cache) {
	cfg := parseConfig(ctx.Config)

	if len(ctx.Candles) == 0 {
		return
	}

	rsi := ctx.Indicators.RSI(ctx.Candles, cfg.RSIPeriod)
	if len(rsi) == 0 {
		return
	}
	currentRSI := rsi[len(rsi)-1]

	if cfg.VolumeFilter > 0 {
		if volume, ok := ctx.Config[strategies.ConfigKey24hVolume].(float64); ok && volume < cfg.VolumeFilter {
			log.Printf("[grid] volume under minimum (%.2f < %.2f), skipping %s", volume, cfg.VolumeFilter, ctx.Symbol)
			return
		}
	}

	if currentRSI > cfg.RSISellThreshold {
		log.Printf("[grid] RSI above sell threshold (%.2f > %.2f), skipping %s", currentRSI, cfg.RSISellThreshold, ctx.Symbol)
		return
	}

	latestClose := ctx.Candles[len(ctx.Candles)-1].Close
	gridSpacing := latestClose * cfg.GridSpacingPct / 100

	gridPrices := make([]float64, cfg.GridLevels)
	for i := 0; i < cfg.GridLevels; i++ {
		gridPrices[i] = latestClose + (float64(i)-float64(cfg.GridLevels/2))*gridSpacing
	}

	var gridData []gridOrder
	hasBuySignal := false
	for _, price := range gridPrices {
		if price < latestClose {
			if currentRSI < cfg.RSIBuyThreshold {
				hasBuySignal = true
				gridData = append(gridData, gridOrder{Type: "buy", Price: price})
			}
		} else {
			profit := ((price - latestClose) / latestClose) * 100
			if profit >= cfg.TakeProfitPct && hasBuySignal {
				gridData = append(gridData, gridOrder{Type: "sell", Price: price})
			}
		}
	}

	if len(gridData) > 0 {
		cache.Set(cacheKeyPrefix+ctx.Symbol, gridData)
	}
}

// ShouldLong/GoLong reproduce the original monitor-phase buy-grid iteration
// ("first match wins, stop iterating") - the engine only calls these when
// ctx.Position == nil, which is exactly the condition under which the
// original combined buy/sell loop's buy branch could meaningfully fire
// (GenerateBuySignal's own dedup made an open-position buy attempt a no-op
// in the original code, so restricting evaluation to the no-position case
// here is behaviorally equivalent, not a behavior change).
func (s *GridStrategy) ShouldLong(ctx strategies.Context) bool {
	_, ok := s.firstMatchingBuy(ctx)
	return ok
}

func (s *GridStrategy) GoLong(ctx strategies.Context) strategies.Signal {
	if _, ok := s.firstMatchingBuy(ctx); !ok {
		return strategies.Signal{}
	}
	qty := 0.0
	if ctx.Price > 0 {
		qty = ctx.Account.Available / ctx.Price
	}
	// EntryPrice mirrors the original monitore's GenerateBuySignal call,
	// which used the current ticker price, not the matched grid level's price.
	return strategies.Signal{Buy: &strategies.Order{Qty: qty, Price: ctx.Price}}
}

func (s *GridStrategy) firstMatchingBuy(ctx strategies.Context) (gridOrder, bool) {
	cache, ok := gridCache(ctx)
	if !ok {
		return gridOrder{}, false
	}
	grid, exists := cachedGrid(cache, ctx.Symbol)
	if !exists {
		return gridOrder{}, false
	}
	for _, order := range grid {
		if order.Type == "buy" && ctx.Price <= order.Price {
			return order, true
		}
	}
	return gridOrder{}, false
}

// UpdatePosition reproduces the original monitor-phase sell-grid iteration.
// The software stop-loss-pct check that used to run first inside monitore is
// intentionally not reproduced here - Spec 03's real exchange STOP_MARKET
// order is the sole stop-loss mechanism post-port.
func (s *GridStrategy) UpdatePosition(ctx strategies.Context) *strategies.Signal {
	cache, ok := gridCache(ctx)
	if !ok {
		return nil
	}
	grid, exists := cachedGrid(cache, ctx.Symbol)
	if !exists {
		return nil
	}
	for _, order := range grid {
		if order.Type == "sell" && ctx.Price >= order.Price {
			qty := 0.0
			if ctx.Position != nil {
				qty = ctx.Position.Quantity
			}
			return &strategies.Signal{Sell: &strategies.Order{Qty: qty, Price: ctx.Price}}
		}
	}
	return nil
}

func (s *GridStrategy) ShouldShort(ctx strategies.Context) bool          { return false }
func (s *GridStrategy) GoShort(ctx strategies.Context) strategies.Signal { return strategies.Signal{} }
func (s *GridStrategy) After(ctx strategies.Context)                     {}
func (s *GridStrategy) Terminate(ctx strategies.Context)                 {}

type gridConfig struct {
	GridLevels       int
	GridSpacingPct   float64
	VolumeFilter     float64
	TakeProfitPct    float64
	RSIPeriod        int
	RSIBuyThreshold  float64
	RSISellThreshold float64
}

func parseConfig(config map[string]interface{}) gridConfig {
	levels, _ := config["grid_levels"].(float64)
	spacing, _ := config["grid_spacing_pct"].(float64)
	volumeFilter, _ := config["volume_filter"].(float64)
	takeProfit, _ := config["take_profit_pct"].(float64)
	rsiPeriod, _ := config["rsi_period"].(float64)
	rsiBuy, _ := config["rsi_buy_threshold"].(float64)
	rsiSell, _ := config["rsi_sell_threshold"].(float64)
	return gridConfig{
		GridLevels:       int(levels),
		GridSpacingPct:   spacing,
		VolumeFilter:     volumeFilter,
		TakeProfitPct:    takeProfit,
		RSIPeriod:        int(rsiPeriod),
		RSIBuyThreshold:  rsiBuy,
		RSISellThreshold: rsiSell,
	}
}

func gridCache(ctx strategies.Context) (memcache.Cache, bool) {
	cache, ok := ctx.Config[strategies.ConfigKeyCache].(memcache.Cache)
	return cache, ok
}

func cachedGrid(cache memcache.Cache, symbol string) ([]gridOrder, bool) {
	v, ok := cache.Get(cacheKeyPrefix + symbol)
	if !ok {
		return nil, false
	}
	grid, ok := v.([]gridOrder)
	if !ok || len(grid) == 0 {
		return nil, false
	}
	return grid, true
}
