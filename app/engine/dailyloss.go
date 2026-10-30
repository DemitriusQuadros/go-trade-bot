package engine

import (
	"encoding/json"
	"fmt"
	"time"

	"go-trade-bot/app/entities"
	"go-trade-bot/app/strategies"
)

// configKeyMaxDailyLossUSD is the strategy-config key for the daily loss
// limit (R-01): a positive quote-currency amount. Absent or <= 0 = no limit.
const configKeyMaxDailyLossUSD = "max_daily_loss_usd"

// strategyMaxDailyLossUSD reads the limit from the strategy's JSON config.
func strategyMaxDailyLossUSD(dbStrategy entities.Strategy) float64 {
	var config map[string]interface{}
	if err := json.Unmarshal(dbStrategy.StrategyConfiguration.Configuration, &config); err != nil {
		return 0
	}
	limit, _ := config[configKeyMaxDailyLossUSD].(float64)
	if limit < 0 {
		return 0
	}
	return limit
}

// utcDayStart is the reset boundary: 00:00 UTC of t's day. UTC matches the
// performance snapshots and the agent sweeper.
func utcDayStart(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// dailyLossHalted reports whether the strategy's realized PnL since the last
// UTC reset has reached its max_daily_loss_usd. It is stateless - derived
// from the closed-trade ledger every cycle - so it resumes by itself at the
// next reset and behaves identically in backtest and live. "Now" is the
// latest candle's open time, never the wall clock, so replays reset on
// simulated days. Only realized PnL counts; entries are blocked, exits are
// not. A strategy with no limit configured costs nothing (no query).
//
// A failed PnL read blocks entries and surfaces the error: a safety control
// must fail closed.
func (e *Engine) dailyLossHalted(strategy strategies.Strategy, dbStrategy entities.Strategy, symbol string, mode strategies.ExecutionMode, ctx strategies.Context) (bool, error) {
	limit := strategyMaxDailyLossUSD(dbStrategy)
	if limit <= 0 || len(ctx.Candles) == 0 {
		return false, nil
	}
	now := ctx.Candles[len(ctx.Candles)-1].OpenTime
	from := utcDayStart(now)

	pnl, err := e.SignalUseCase.RealizedPnL(dbStrategy.ID, from, from.Add(24*time.Hour))
	if err != nil {
		return true, fmt.Errorf("daily loss check failed for %s, entries blocked: %w", dbStrategy.Name, err)
	}
	if pnl > -limit {
		return false, nil
	}

	loss := -pnl
	e.traceLog(strategy, "daily_loss_halt", map[string]float64{"realized_loss": loss, "limit": limit})

	// Once per strategy per UTC day, so a halted strategy doesn't notify on
	// every cycle.
	key := fmt.Sprintf("daily-loss-halt:%d:%s", dbStrategy.ID, from.Format("2006-01-02"))
	if e.Cache != nil {
		if _, seen := e.Cache.Get(key); !seen {
			e.Cache.Set(key, true)
			e.notifyError(dbStrategy, symbol, mode, fmt.Sprintf(
				"daily loss limit reached for %s: realized loss %.2f >= limit %.2f; new entries blocked until %s UTC",
				dbStrategy.Name, loss, limit, from.Add(24*time.Hour).Format("2006-01-02 15:04")))
		}
	}
	return true, nil
}

// traceLog adds an engine-originated entry to a traced strategy's current
// cycle. A no-op for strategies that aren't traced.
func (e *Engine) traceLog(strategy strategies.Strategy, label string, value any) {
	if tl, ok := strategy.(interface{ TraceLog(label string, value any) }); ok {
		tl.TraceLog(label, value)
	}
}
