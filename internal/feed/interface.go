// Package feed is the architectural linchpin (PRD SS8): both the live
// trading engine and (Phase 2) the backtest engine consume candles through
// this single Feed interface, so behavior can't silently diverge between
// live and replayed data sources.
package feed

import "go-trade-bot/internal/exchange"

// Feed delivers candles one at a time. Next blocks until a candle is
// available or the feed is closed/exhausted, in which case it returns
// (zero-value, false).
type Feed interface {
	Next() (exchange.Candle, bool)
}
