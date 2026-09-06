// Package scalping ports app/services/algorithm/scalping into the Strategy
// hook interface (Spec 05/08). Behavior is preserved bit for bit except the
// software stop-loss-pct branch of the original two-condition exit OR,
// which is intentionally removed - the real exchange STOP_MARKET order
// (Spec 03) is the sole stop-loss mechanism post-port (Spec 08's resolved
// judgment call, 2026-08-31).
//
// Scalping's uptrend filter needs a supplementary 15m/50-candle fetch,
// independent of the strategy's own configured cycle interval - the one
// deliberate exception to "strategies never touch the exchange directly"
// (Spec 07 AC#6). Rather than giving this strategy a raw ExchangeClient
// (which would reopen the ACL boundary Spec 01 exists to close), the engine
// fetches this window and injects it via
// Context.Config[strategies.ConfigKeyLongTermCandles] - resolving Spec 07's
// flagged open question with the same Config-injection pattern already used
// for Grid's cache/24h-volume needs.
package scalping

import (
	"go-trade-bot/app/strategies"
	"go-trade-bot/internal/exchange"
)

func init() {
	strategies.Register("scalping", func() strategies.Strategy {
		return &ScalpingStrategy{}
	})
}

const minCandles = 10

type ScalpingStrategy struct{}

func (s *ScalpingStrategy) Name() string { return "scalping" }

func (s *ScalpingStrategy) Before(ctx strategies.Context) {}

// ShouldLong reproduces the original entry gate exactly: volume, RSI,
// uptrend, and rising-close, all independently load-bearing (Spec 08 AC#7).
func (s *ScalpingStrategy) ShouldLong(ctx strategies.Context) bool {
	if len(ctx.Candles) < minCandles {
		return false
	}
	if !validateVolume(ctx.Candles) {
		return false
	}
	if !validateRSI(ctx.Indicators, ctx.Candles) {
		return false
	}
	if !isUptrend(ctx) {
		return false
	}
	latestClose := ctx.Candles[len(ctx.Candles)-1].Close
	prevClose := ctx.Candles[len(ctx.Candles)-2].Close
	return latestClose > prevClose
}

func (s *ScalpingStrategy) GoLong(ctx strategies.Context) strategies.Signal {
	latestClose := ctx.Candles[len(ctx.Candles)-1].Close
	qty := 0.0
	if latestClose > 0 {
		qty = ctx.Account.Available / latestClose
	}
	return strategies.Signal{Buy: &strategies.Order{Qty: qty, Price: latestClose}}
}

func (s *ScalpingStrategy) ShouldShort(ctx strategies.Context) bool { return false }
func (s *ScalpingStrategy) GoShort(ctx strategies.Context) strategies.Signal {
	return strategies.Signal{}
}

// UpdatePosition reproduces the surviving condition from the original
// two-condition exit OR (`pnl >= take_profit_pct || pnl <= -stop_loss_pct`) -
// take-profit only. The stop_loss_pct condition is intentionally not
// reproduced (Spec 08).
func (s *ScalpingStrategy) UpdatePosition(ctx strategies.Context) *strategies.Signal {
	if ctx.Position == nil {
		return nil
	}
	cfg := parseConfig(ctx.Config)

	entryPrice := ctx.Position.EntryPrice
	pnl := 0.0
	if entryPrice != 0 {
		pnl = (ctx.Price - entryPrice) / entryPrice * 100
	}

	if pnl >= cfg.TakeProfitPct {
		return &strategies.Signal{Sell: &strategies.Order{Qty: ctx.Position.Quantity, Price: ctx.Price}}
	}
	return nil
}

func (s *ScalpingStrategy) After(ctx strategies.Context)     {}
func (s *ScalpingStrategy) Terminate(ctx strategies.Context) {}

func validateVolume(candles []exchange.Candle) bool {
	avgVolume := 0.0
	for _, c := range candles {
		avgVolume += c.Volume
	}
	avgVolume /= float64(len(candles))

	latestVolume := candles[len(candles)-1].Volume
	return latestVolume >= avgVolume
}

func validateRSI(indicatorProvider indicatorProvider, candles []exchange.Candle) bool {
	rsi := indicatorProvider.RSI(candles, 14)
	if len(rsi) == 0 {
		return false
	}
	latestRSI := rsi[len(rsi)-1]
	return latestRSI <= 70
}

// isUptrend reproduces the original p.isUptrend exactly (via
// ctx.Indicators.EMA, Spec 07's ACL - not a direct go-talib call), sourcing
// its long-term candles from the engine-injected Config entry instead of a
// direct broker.ListKline call.
func isUptrend(ctx strategies.Context) bool {
	longTermCandles, ok := ctx.Config[strategies.ConfigKeyTimeframeCandles("15m")].([]exchange.Candle)
	if !ok {
		longTermCandles, ok = ctx.Config[strategies.ConfigKeyLongTermCandles].([]exchange.Candle)
	}
	if !ok || len(longTermCandles) < 20 {
		return false
	}

	ema := ctx.Indicators.EMA(longTermCandles, 20)
	if len(ema) == 0 {
		return false
	}

	currentPrice := longTermCandles[len(longTermCandles)-1].Close
	currentEMA := ema[len(ema)-1]

	return currentPrice > currentEMA
}

// indicatorProvider is the narrow slice of indicators.IndicatorProvider this
// package needs, declared locally so validateRSI is trivially testable
// without importing the full ACL interface's every method.
type indicatorProvider interface {
	RSI(candles []exchange.Candle, period int) []float64
}

type scalpingConfig struct {
	TakeProfitPct float64
}

func parseConfig(config map[string]interface{}) scalpingConfig {
	takeProfit, _ := config["take_profit_pct"].(float64)
	return scalpingConfig{TakeProfitPct: takeProfit}
}
