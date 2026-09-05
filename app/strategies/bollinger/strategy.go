// Package bollinger ports app/services/algorithm/bollinger into the
// Strategy hook interface (Spec 05/08). Behavior is preserved bit for bit
// except the software stop-loss-pct branch of the original three-condition
// exit OR, which is intentionally removed - the real exchange STOP_MARKET
// order (Spec 03) is the sole stop-loss mechanism post-port (Spec 08's
// resolved judgment call, 2026-08-31). Bollinger's period (20) and both
// stdDev multipliers (2.0/2.0) stay hardcoded and EMA-smoothed, matching
// current behavior exactly (Spec 07) - not promoted to strategy config,
// per ADR-003's behavior-preservation mandate.
package bollinger

import (
	"go-trade-bot/app/strategies"
	"go-trade-bot/internal/indicators"
)

func init() {
	strategies.Register("bollinger", func() strategies.Strategy {
		return &BollingerStrategy{}
	})
}

const (
	period     = 20
	stdDevUp   = 2.0
	stdDevDown = 2.0
	minCandles = 20
)

type BollingerStrategy struct{}

func (s *BollingerStrategy) Name() string { return "bollinger" }

func (s *BollingerStrategy) Before(ctx strategies.Context) {}

func (s *BollingerStrategy) ShouldLong(ctx strategies.Context) bool {
	if len(ctx.Candles) < minCandles {
		return false
	}
	_, _, lower := ctx.Indicators.BollingerBands(ctx.Candles, period, stdDevUp, stdDevDown, indicators.MATypeEMA)
	if len(lower) == 0 {
		return false
	}
	return ctx.Price < lower[len(lower)-1]
}

func (s *BollingerStrategy) GoLong(ctx strategies.Context) strategies.Signal {
	qty := 0.0
	if ctx.Price > 0 {
		qty = ctx.Account.Available / ctx.Price
	}
	return strategies.Signal{Buy: &strategies.Order{Qty: qty, Price: ctx.Price}}
}

func (s *BollingerStrategy) ShouldShort(ctx strategies.Context) bool { return false }
func (s *BollingerStrategy) GoShort(ctx strategies.Context) strategies.Signal {
	return strategies.Signal{}
}

// UpdatePosition reproduces the surviving two conditions from the original
// three-condition exit OR (`pnl >= take_profit_pct || pnl <= -stop_loss_pct
// || current > upper[last]`) - take-profit and band-cross. The
// stop_loss_pct condition is intentionally not reproduced (Spec 08).
func (s *BollingerStrategy) UpdatePosition(ctx strategies.Context) *strategies.Signal {
	if ctx.Position == nil || len(ctx.Candles) < minCandles {
		return nil
	}

	cfg := parseConfig(ctx.Config)
	upper, _, _ := ctx.Indicators.BollingerBands(ctx.Candles, period, stdDevUp, stdDevDown, indicators.MATypeEMA)
	if len(upper) == 0 {
		return nil
	}

	entryPrice := ctx.Position.EntryPrice
	pnl := 0.0
	if entryPrice != 0 {
		pnl = (ctx.Price - entryPrice) / entryPrice * 100
	}

	if pnl >= cfg.TakeProfitPct || ctx.Price > upper[len(upper)-1] {
		return &strategies.Signal{Sell: &strategies.Order{Qty: ctx.Position.Quantity, Price: ctx.Price}}
	}
	return nil
}

func (s *BollingerStrategy) After(ctx strategies.Context)     {}
func (s *BollingerStrategy) Terminate(ctx strategies.Context) {}

type bollingerConfig struct {
	TakeProfitPct float64
}

func parseConfig(config map[string]interface{}) bollingerConfig {
	takeProfit, _ := config["take_profit_pct"].(float64)
	return bollingerConfig{TakeProfitPct: takeProfit}
}
