// Package engine is the single execution engine (PRD "unified engine",
// ADR-001): it owns the Feed -> Strategy hook loop. Phase 1 implements the
// live-mode loop only (Context built from a fresh REST fetch each cycle);
// backtest/dry-run variants reusing this same loop are Phase 2 (ADR-001).
package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"go-trade-bot/app/entities"
	"go-trade-bot/app/strategies"
	usecase "go-trade-bot/app/usecase/signal"
	"go-trade-bot/internal/exchange"
	"go-trade-bot/internal/indicators"
	"go-trade-bot/internal/memcache"
	"go-trade-bot/internal/notifier"
)

// candleWindow is the fixed number of most-recent candles fetched per cycle,
// sized to the largest of the three ported strategies' historical needs
// (Grid's RSI/grid-build window of 100 candles). Spec 05's frozen Context
// struct has no "requested candle count" channel back to the engine before
// Context is built, so a single generous window is used uniformly rather
// than a per-strategy-configurable count - documented as a Phase 1 judgment
// call in the final report.
const candleWindow = 100

// SignalUseCase is the local interface the engine depends on for
// translating a Strategy's declarative Signal into a real order (Spec 02/03).
type SignalUseCase interface {
	GenerateBuySignal(e usecase.EntrySignal) error
	GenerateSellSignal(e usecase.ExitSignal) error
	GetOpenSignal(symbol string, strategyId uint) (entities.Signal, error)
}

// AccountReader is the local interface the engine depends on to populate
// Context.Account.
type AccountReader interface {
	GetAccount() (entities.Account, error)
}

// Engine drives exactly one strategy cycle for one symbol via Run. All
// external interaction (exchange, indicators, DB-backed signal state,
// notifications, cross-cycle cache) is owned here, never by a Strategy
// implementation directly (PRD SS8's "no global mutable state in strategy
// code").
type Engine struct {
	Exchange      exchange.ExchangeClient
	Indicators    indicators.IndicatorProvider
	SignalUseCase SignalUseCase
	AccountReader AccountReader
	Notifier      notifier.NotificationSender
	Cache         memcache.Cache
}

func NewEngine(
	exchangeClient exchange.ExchangeClient,
	indicatorProvider indicators.IndicatorProvider,
	signalUseCase SignalUseCase,
	accountReader AccountReader,
	notifySender notifier.NotificationSender,
	cache memcache.Cache,
) *Engine {
	return &Engine{
		Exchange:      exchangeClient,
		Indicators:    indicatorProvider,
		SignalUseCase: signalUseCase,
		AccountReader: accountReader,
		Notifier:      notifySender,
		Cache:         cache,
	}
}

// Run executes exactly one strategy cycle for one symbol: build Context,
// walk the Spec 05 hook sequence, and translate any resulting Signal into a
// real order via SignalUseCase. A panic from any hook is recovered here and
// turned into an error (Spec 05 AC#9) - Terminate is deliberately not called
// on this path (that's the caller's job on an explicit Disabled transition).
func (e *Engine) Run(ctx context.Context, strategy strategies.Strategy, dbStrategy entities.Strategy, symbol string, mode strategies.ExecutionMode) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("strategy %s panicked processing %s: %v", dbStrategy.Name, symbol, r)
			e.notifyError(dbStrategy, symbol, mode, err.Error())
		}
	}()

	strategyCtx, buildErr := e.buildContext(ctx, dbStrategy, symbol, mode)
	if buildErr != nil {
		return buildErr
	}

	strategy.Before(strategyCtx)

	if strategyCtx.Position == nil {
		if strategy.ShouldLong(strategyCtx) {
			signal := strategy.GoLong(strategyCtx)
			err = e.processGoLong(dbStrategy, symbol, mode, signal)
		} else if strategy.ShouldShort(strategyCtx) {
			// Phase 1's ExchangeClient is spot-only (Spec 01) - no short
			// execution path exists. This is logged/webhooked but does NOT
			// fail the cycle (Spec 05 AC#7 says "completes without a panic",
			// not "recorded as Error" - unlike AC#4's GoLong-with-no-Buy case).
			_ = strategy.GoShort(strategyCtx)
			msg := "short positions not supported in Phase 1"
			log.Printf("[engine] %s/%s: %s", dbStrategy.Name, symbol, msg)
			e.notifyError(dbStrategy, symbol, mode, msg)
		}
	} else {
		signal := strategy.UpdatePosition(strategyCtx)
		if signal != nil {
			err = e.processUpdatePosition(dbStrategy, symbol, mode, *signal)
		}
	}

	strategy.After(strategyCtx)

	return err
}

// buildContext assembles the frozen Context (Spec 05) for one symbol/cycle:
// candles (REST fetch, live-mode Phase 1), the open position (if any,
// derived from Signal+Order), account balance, parsed strategy config plus
// the two engine-owned Config entries (_cache, _24h_volume), and the
// resolved IndicatorProvider/ExecutionMode.
func (e *Engine) buildContext(ctx context.Context, dbStrategy entities.Strategy, symbol string, mode strategies.ExecutionMode) (strategies.Context, error) {
	candles, err := e.Exchange.ListKline(ctx, symbol, dbStrategy.GetBrokerInterval(), candleWindow)
	if err != nil {
		return strategies.Context{}, fmt.Errorf("failed to fetch candles for %s: %w", symbol, err)
	}
	if len(candles) == 0 {
		return strategies.Context{}, fmt.Errorf("no candles returned for %s", symbol)
	}

	position, err := e.buildPosition(symbol, dbStrategy.ID)
	if err != nil {
		return strategies.Context{}, fmt.Errorf("failed to fetch open signal for %s: %w", symbol, err)
	}

	var account strategies.Account
	if e.AccountReader != nil {
		if acc, accErr := e.AccountReader.GetAccount(); accErr == nil {
			account = strategies.Account{Available: float64(acc.Amount)}
		}
	}

	config := map[string]interface{}{}
	if len(dbStrategy.StrategyConfiguration.Configuration) > 0 {
		if err := json.Unmarshal(dbStrategy.StrategyConfiguration.Configuration, &config); err != nil {
			return strategies.Context{}, fmt.Errorf("failed to parse strategy configuration for %s: %w", dbStrategy.Name, err)
		}
	}

	// Engine-owned Config entries - never user-authored, never a typed
	// Context field (ADR-005: only Grid needs either, so both are routed
	// through Config rather than promoted to the frozen struct).
	if e.Cache != nil {
		config[strategies.ConfigKeyCache] = e.Cache
	}
	if volume, volErr := e.fetch24hVolume(ctx, symbol); volErr == nil {
		config[strategies.ConfigKey24hVolume] = volume
	}
	if longTermCandles, ltErr := e.Exchange.ListKline(ctx, symbol, "15m", 50); ltErr == nil {
		config[strategies.ConfigKeyLongTermCandles] = longTermCandles
	}

	return strategies.Context{
		Candles:    candles,
		Position:   position,
		Account:    account,
		Config:     config,
		Indicators: e.Indicators,
		Price:      candles[len(candles)-1].Close,
		Timeframe:  dbStrategy.GetBrokerInterval(),
		Symbol:     symbol,
		Mode:       mode,
	}, nil
}

func (e *Engine) buildPosition(symbol string, strategyID uint) (*strategies.Position, error) {
	openSignal, err := e.SignalUseCase.GetOpenSignal(symbol, strategyID)
	if err != nil {
		return nil, err
	}
	if openSignal.ID == 0 || len(openSignal.Orders) == 0 {
		return nil, nil
	}

	order := openSignal.Orders[0]
	return &strategies.Position{
		Symbol:          symbol,
		EntryPrice:      float64(order.EntryPrice),
		Quantity:        float64(order.Quantity),
		StopLossPrice:   nil, // Phase 1 doesn't track the resting stop's price, only its order ID - no hook inspects this field yet.
		StopLossOrderID: order.StopLossOrderID,
		OpenedAt:        order.CreatedAt,
	}, nil
}

// fetch24hVolume replicates the original internal/broker.Broker.Get24hVolume
// computation (a 1-day/1-candle kline fetch) so Grid's volume_filter gate
// keeps its exact current semantics without the strategy touching the
// exchange directly (Spec 05's ACL boundary) - see final report for this
// judgment call.
func (e *Engine) fetch24hVolume(ctx context.Context, symbol string) (float64, error) {
	candles, err := e.Exchange.ListKline(ctx, symbol, "1d", 1)
	if err != nil {
		return 0, err
	}
	if len(candles) == 0 {
		return 0, fmt.Errorf("no 24h volume data for %s", symbol)
	}
	return candles[0].Volume, nil
}

// processGoLong translates a GoLong Signal into a real buy order. Per Spec
// 05 AC#4, a strategy bug (ShouldLong=true but GoLong returns no Buy order)
// is treated as a hard cycle error - no order is placed, a strategy.error
// webhook fires, and the returned error causes the caller (handler.go) to
// record StrategyExecution{Status: Error} for this cycle.
func (e *Engine) processGoLong(dbStrategy entities.Strategy, symbol string, mode strategies.ExecutionMode, signal strategies.Signal) error {
	if signal.Buy == nil {
		msg := fmt.Sprintf("strategy %s: GoLong returned no Buy order despite ShouldLong=true", dbStrategy.Name)
		log.Print(msg)
		e.notifyError(dbStrategy, symbol, mode, msg)
		return fmt.Errorf("%s", msg)
	}

	entry := usecase.EntrySignal{
		Symbol:       symbol,
		StrategyID:   dbStrategy.ID,
		StrategyName: dbStrategy.Name,
		Mode:         mode.String(),
		EntryPrice:   float32(signal.Buy.Price),
		MarginType:   entities.Isolated,
		StopLossPct:  strategyStopLossPct(dbStrategy),
	}
	if signal.StopLoss != nil {
		price := signal.StopLoss.Price
		entry.StopLossPrice = &price
	}

	if err := e.SignalUseCase.GenerateBuySignal(entry); err != nil {
		log.Printf("[engine] %s/%s: buy failed: %v", dbStrategy.Name, symbol, err)
		return err
	}
	return nil
}

func (e *Engine) processUpdatePosition(dbStrategy entities.Strategy, symbol string, mode strategies.ExecutionMode, signal strategies.Signal) error {
	if signal.Sell == nil {
		return nil // hold, or a trailing-stop-only adjustment (Out of Scope, Spec 03/05)
	}

	exit := usecase.ExitSignal{
		Symbol:       symbol,
		StrategyID:   dbStrategy.ID,
		StrategyName: dbStrategy.Name,
		Mode:         mode.String(),
		ExitPrice:    float32(signal.Sell.Price),
	}

	if err := e.SignalUseCase.GenerateSellSignal(exit); err != nil {
		log.Printf("[engine] %s/%s: sell failed: %v", dbStrategy.Name, symbol, err)
		return err
	}
	return nil
}

func (e *Engine) notifyError(dbStrategy entities.Strategy, symbol string, mode strategies.ExecutionMode, message string) {
	if e.Notifier == nil {
		return
	}
	_ = e.Notifier.Send(context.Background(), notifier.Event{
		Type:       notifier.EventStrategyError,
		Timestamp:  time.Now(),
		StrategyID: dbStrategy.ID,
		Strategy:   dbStrategy.Name,
		Symbol:     symbol,
		Mode:       mode.String(),
		Message:    message,
		Data:       map[string]any{"error": message, "context": "engine.Run"},
	})
}

func strategyStopLossPct(dbStrategy entities.Strategy) float64 {
	var config map[string]interface{}
	if err := json.Unmarshal(dbStrategy.StrategyConfiguration.Configuration, &config); err != nil {
		return 0
	}
	pct, _ := config["stop_loss_pct"].(float64)
	return pct
}
