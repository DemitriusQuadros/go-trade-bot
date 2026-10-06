package engine

import (
	"context"
	"errors"
	"fmt"

	"go-trade-bot/app/entities"
	"go-trade-bot/app/strategies"
	usecase "go-trade-bot/app/usecase/signal"
	"go-trade-bot/internal/exchange"
)

// BacktestStopSignals is what BacktestStopEvaluator needs from the replay
// SignalUseCase.
type BacktestStopSignals interface {
	GetOpenSignal(symbol string, strategyId uint) (entities.Signal, error)
	GenerateSellSignal(e usecase.ExitSignal) error
}

// BacktestStopExchange is what BacktestStopEvaluator needs from
// SimulatedFillExchange: the state of the resting stop it simulates.
type BacktestStopExchange interface {
	GetOrder(ctx context.Context, symbol, orderID string) (exchange.OrderResult, error)
}

// BacktestStopEvaluator is the replay counterpart of a resting exchange stop.
//
// SimulatedFillExchange marks a STOP_MARKET order filled as soon as a candle's
// Low touches it, but nothing used to close the Signal: the position stayed
// "open" until a take-profit or a strategy exit happened to run, which for a
// position that dropped through its stop might be never. The strategy then saw
// ctx.position != nil forever, stopped trading, and the run ended with an
// "open_at_end" trade marked to market far below the stop.
//
// At the start of every replay cycle (before the take-profit check and any
// hook) this hook asks the exchange whether the open position's stop already
// filled and, if so, closes the position through the normal GenerateSellSignal
// path. That path's cancel -> ErrOrderNotFound branch reconciles at the stop's
// own fill price and records exit reason "stop_loss", exactly like a real
// exchange-side stop.
type BacktestStopEvaluator struct {
	Signals  BacktestStopSignals
	Exchange BacktestStopExchange
}

var _ CycleHook = (*BacktestStopEvaluator)(nil)

// NewBacktestStopEvaluator builds the evaluator.
func NewBacktestStopEvaluator(signals BacktestStopSignals, ex BacktestStopExchange) *BacktestStopEvaluator {
	return &BacktestStopEvaluator{Signals: signals, Exchange: ex}
}

// BeforeCycle closes the open position if its simulated stop already filled.
func (ev *BacktestStopEvaluator) BeforeCycle(ctx context.Context, dbStrategy entities.Strategy, symbol string, mode strategies.ExecutionMode) error {
	open, err := ev.Signals.GetOpenSignal(symbol, dbStrategy.ID)
	if err != nil {
		return fmt.Errorf("backtest stop: open signal for %s: %w", symbol, err)
	}
	if open.ID == 0 || len(open.Orders) == 0 {
		return nil
	}
	order := open.Orders[0]
	if order.StopLossOrderID == "" {
		return nil
	}

	res, err := ev.Exchange.GetOrder(ctx, symbol, order.StopLossOrderID)
	if err != nil {
		if errors.Is(err, exchange.ErrOrderNotFound) {
			return nil
		}
		return fmt.Errorf("backtest stop: stop order %s: %w", order.StopLossOrderID, err)
	}
	if res.Status != exchange.OrderStatusFilled {
		return nil
	}

	if err := ev.Signals.GenerateSellSignal(usecase.ExitSignal{
		Symbol:       symbol,
		StrategyID:   dbStrategy.ID,
		StrategyName: dbStrategy.Name,
		Mode:         mode.String(),
		ExitPrice:    float32(res.AvgFillPrice),
		ExitReason:   "stop_loss",
	}); err != nil {
		return fmt.Errorf("backtest stop-loss close for %s failed: %w", symbol, err)
	}
	return nil
}
