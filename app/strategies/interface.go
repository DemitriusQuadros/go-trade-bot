// Package strategies is the pluggable strategy plugin system root (PRD SS4.2,
// SS5). Strategy/Context/Signal/ExecutionMode are frozen per ADR-005 and the
// blueprint's "Scope creep in the Strategy/Context interface" risk (SS6) -
// these signatures must not change after the first strategy is ported
// (Spec 08).
package strategies

import (
	"fmt"
	"time"

	"go-trade-bot/internal/exchange"
	"go-trade-bot/internal/indicators"
)

// ExecutionMode is the four-tier risk ladder every strategy execution runs
// under (PRD SS4.4, Spec 10). Ordinal order matters: ModeBacktest(0) <
// ModeDryRun(1) < ModePaper(2) < ModeLive(3).
type ExecutionMode int

const (
	ModeBacktest ExecutionMode = iota
	ModeDryRun
	ModePaper
	ModeLive
)

func (m ExecutionMode) String() string {
	switch m {
	case ModeBacktest:
		return "backtest"
	case ModeDryRun:
		return "dryrun"
	case ModePaper:
		return "paper"
	case ModeLive:
		return "live"
	default:
		return "unknown"
	}
}

// ParseExecutionMode is String's inverse. It returns an error on any string
// that isn't one of the four known modes - callers (Spec 10's per-cycle gate)
// must fail closed on an unparseable value, never assume a default.
func ParseExecutionMode(s string) (ExecutionMode, error) {
	switch s {
	case "backtest":
		return ModeBacktest, nil
	case "dryrun":
		return ModeDryRun, nil
	case "paper":
		return ModePaper, nil
	case "live":
		return ModeLive, nil
	default:
		return 0, fmt.Errorf("strategies: unknown execution mode %q", s)
	}
}

// Position is an in-memory, per-cycle read model built by app/engine from the
// current open Signal+Order (if any) for (strategyID, symbol). It is
// deliberately distinct from the future app/entities read-model slated for
// the TUI's Open Positions page (Phase 3) - see Spec 05's "Judgment call".
type Position struct {
	Symbol          string
	EntryPrice      float64
	Quantity        float64
	StopLossPrice   *float64 // nil if no resting stop (shouldn't happen post-Spec 03, but hooks must handle nil)
	StopLossOrderID string
	OpenedAt        time.Time
}

// Account is the slice of entities.Account a strategy hook is allowed to see.
type Account struct {
	Available float64 // mirrors entities.Account.Amount at cycle-start read time
}

// Order is a strategy's declared intent (qty/price), not the exchange's
// response. The engine translates this into an exchange.PlaceOrderRequest.
type Order struct {
	Qty   float64
	Price float64
}

// Signal is the declarative return type of GoLong/GoShort/UpdatePosition.
type Signal struct {
	Buy        *Order
	Sell       *Order
	StopLoss   *Order
	TakeProfit *Order
}

// Context is passed to every hook. No global state: everything a strategy
// needs is here (PRD SS8's "no global mutable state in strategy code").
type Context struct {
	Candles    []exchange.Candle
	Position   *Position // nil if no open position for this symbol+strategy
	Account    Account
	Config     map[string]interface{}
	Indicators indicators.IndicatorProvider
	Price      float64 // most recent candle's Close
	Timeframe  string
	Symbol     string
	Mode       ExecutionMode
}

// Strategy is the lifecycle-hook interface every trading algorithm
// implements. See docs/specs/phase-1/05-strategy-interface-and-context.md
// for the full hook-by-hook contract (when the engine calls each hook, and
// what it does with the return value).
type Strategy interface {
	Name() string
	Before(ctx Context)
	ShouldLong(ctx Context) bool
	GoLong(ctx Context) Signal
	ShouldShort(ctx Context) bool
	GoShort(ctx Context) Signal
	UpdatePosition(ctx Context) *Signal
	After(ctx Context)
	Terminate(ctx Context)
}
