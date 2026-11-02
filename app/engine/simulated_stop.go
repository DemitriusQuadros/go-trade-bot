package engine

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"time"

	"go-trade-bot/app/entities"
	"go-trade-bot/app/strategies"
	usecase "go-trade-bot/app/usecase/signal"
	"go-trade-bot/internal/exchange"
	"go-trade-bot/internal/futuresmath"
)

// CycleHook runs at the start of Engine.Run, before the Context is built and
// before any strategy hook. Only the worker's dryrun engine sets one
// (SimulatedStopEvaluator); live/paper/backtest engines leave it nil.
type CycleHook interface {
	BeforeCycle(ctx context.Context, dbStrategy entities.Strategy, symbol string, mode strategies.ExecutionMode) error
}

// FundingSettler is implemented by SignalUseCase to settle 8h funding fees.
type FundingSettler interface {
	SettleFunding(symbol string, strategyID uint, fundingPayment float32, newLiqPrice float32, fundingTime time.Time) error
}

// SideStopTriggerer is an optional interface on dry-run exchange supporting stop direction.
type SideStopTriggerer interface {
	TriggerStopWithSide(orderID string, side exchange.OrderSide, quantity, stopPrice float64, at time.Time) exchange.OrderResult
}

// SimulatedStopSignals is what SimulatedStopEvaluator needs from the dryrun
// SignalUseCase.
type SimulatedStopSignals interface {
	GetOpenSignal(symbol string, strategyId uint) (entities.Signal, error)
	GenerateSellSignal(e usecase.ExitSignal) error
}

// SimulatedStopExchange is what SimulatedStopEvaluator needs from
// exchange.DryRunExchange.
type SimulatedStopExchange interface {
	ListKline(ctx context.Context, symbol, interval string, limit int) ([]exchange.Candle, error)
	TriggerStop(orderID string, quantity, stopPrice float64, at time.Time) exchange.OrderResult
	ClearTriggeredStop(orderID string)
}

// StopWatermarkStore persists the per-order "last evaluated candle" mark
// (app/repository/signal.SignalRepository satisfies it).
type StopWatermarkStore interface {
	UpdateSimStopEvaluatedAt(orderID uint, evaluatedAt time.Time) error
}

// SimulatedStopEvaluator is the dryrun replacement for a resting exchange
// stop-loss (fix-01). At the start of each dryrun cycle, for an open
// simulated signal with a stop, it scans closed candles not yet evaluated;
// the first whose Low <= StopLossPrice fires the stop through
// DryRunExchange.TriggerStop and closes the position via the normal
// GenerateSellSignal path (exit reason ExitReasonSimulatedStopLoss), so
// Signal/Order rows, metrics and notifications match a real stop fill.
//
// The watermark (Order.SimStopEvaluatedAt, persisted - asynq can run the next
// cycle on another replica) holds the last evaluated candle's OpenTime, so a
// candle is never evaluated twice. It is advanced only when no stop fired, so
// a failed close is retried next cycle.
//
// Only candles that opened at or after the entry fill and have fully closed
// are evaluated: the part of the entry candle before the fill is unknowable
// from OHLC, and an in-progress candle's Low can still move.
type SimulatedStopEvaluator struct {
	Signals    SimulatedStopSignals
	Exchange   SimulatedStopExchange
	Watermarks StopWatermarkStore
	// Now is the evaluation clock (time.Now by default; tests override it).
	Now func() time.Time
}

var _ CycleHook = (*SimulatedStopEvaluator)(nil)

// NewSimulatedStopEvaluator builds the evaluator.
func NewSimulatedStopEvaluator(signals SimulatedStopSignals, ex SimulatedStopExchange, watermarks StopWatermarkStore) *SimulatedStopEvaluator {
	return &SimulatedStopEvaluator{Signals: signals, Exchange: ex, Watermarks: watermarks, Now: time.Now}
}

// BeforeCycle evaluates the open simulated stop for dbStrategy/symbol, if any.
func (ev *SimulatedStopEvaluator) BeforeCycle(ctx context.Context, dbStrategy entities.Strategy, symbol string, mode strategies.ExecutionMode) error {
	if mode != strategies.ModeDryRun {
		return fmt.Errorf("simulated stop evaluator invoked for %s mode; it only serves dryrun", mode)
	}

	open, err := ev.Signals.GetOpenSignal(symbol, dbStrategy.ID)
	if err != nil {
		return fmt.Errorf("simulated stop: open signal for %s: %w", symbol, err)
	}
	if open.ID == 0 || len(open.Orders) == 0 {
		return nil
	}
	order := open.Orders[0]

	isShort := open.PositionSide == entities.PositionSideShort || order.PositionSide == entities.PositionSideShort
	isFutures := order.Leverage > 1 || isShort || order.LiquidationPrice > 0

	hasSimStop := order.StopLossOrderID != "" && order.StopLossPrice > 0 && exchange.IsSimulatedOrderID(order.StopLossOrderID)
	hasSimLiq := order.LiquidationPrice > 0 && exchange.IsSimulatedOrderID(order.BrokerOrderID)

	if !hasSimStop && !hasSimLiq && !isFutures {
		return nil
	}
	if !exchange.IsSimulatedOrderID(order.BrokerOrderID) && (order.StopLossOrderID == "" || !exchange.IsSimulatedOrderID(order.StopLossOrderID)) {
		return nil
	}

	interval := dbStrategy.GetBrokerInterval()
	candleDur, err := intervalDuration(interval)
	if err != nil {
		return fmt.Errorf("simulated stop for %s: %w", symbol, err)
	}

	candles, err := ev.Exchange.ListKline(ctx, symbol, interval, candleWindow)
	if err != nil {
		return fmt.Errorf("simulated stop: candles for %s: %w", symbol, err)
	}

	now := ev.Now()
	stop := float64(order.StopLossPrice)
	liq := float64(order.LiquidationPrice)
	var lastEvaluated time.Time

	for _, c := range candles {
		closeTime := c.OpenTime.Add(candleDur)
		if closeTime.After(now) {
			continue // still forming
		}
		if c.OpenTime.Before(order.CreatedAt) {
			continue // opened before the entry fill
		}
		if order.SimStopEvaluatedAt != nil && !c.OpenTime.After(*order.SimStopEvaluatedAt) {
			continue // already evaluated
		}

		// 1. 8h Funding Settlement
		if isFutures {
			fundingWatermark := order.CreatedAt
			if order.LastFundingTime != nil {
				fundingWatermark = *order.LastFundingTime
			}
			fundingTimestamps := futuresmath.FundingTimestampsInWindow(fundingWatermark, closeTime)
			if len(fundingTimestamps) > 0 {
				if settler, ok := ev.Signals.(FundingSettler); ok {
					fundingRate := futuresmath.DefaultFundingRate8h
					if fc, okFc := ev.Exchange.(exchange.FuturesClient); okFc {
						if _, fr, _, frErr := fc.GetMarkPrice(ctx, symbol); frErr == nil && fr != 0 {
							fundingRate = fr
						}
					}
					fmSide := futuresmath.SideLong
					if isShort {
						fmSide = futuresmath.SideShort
					}
					for _, fts := range fundingTimestamps {
						markPrice := c.Close
						payment := futuresmath.FundingPayment(fmSide, float64(order.Quantity), markPrice, fundingRate)
						accFunding := float64(order.AccumulatedFunding) + payment
						newLiqPrice := futuresmath.IsolatedLiquidationPrice(symbol, fmSide, float64(order.EntryPrice), float64(order.Quantity), float64(order.Leverage), 0, accFunding)
						if sErr := settler.SettleFunding(symbol, dbStrategy.ID, float32(payment), float32(newLiqPrice), fts); sErr == nil {
							order.AccumulatedFunding = float32(accFunding)
							order.LiquidationPrice = float32(newLiqPrice)
							order.LastFundingTime = &fts
							liq = newLiqPrice
						}
					}
				}
			}
		}

		// 2. Stop-Loss vs. Liquidation Priority
		if !isShort {
			stopHit := stop > 0 && order.StopLossOrderID != "" && c.Low <= stop
			liqHit := liq > 0 && c.Low <= liq
			if stopHit && (!liqHit || stop > liq) {
				return ev.fireStop(dbStrategy, symbol, mode, open, exchange.SideSell, stop, closeTime)
			} else if liqHit {
				return ev.fireLiquidation(dbStrategy, symbol, mode, open, liq)
			}
		} else {
			stopHit := stop > 0 && order.StopLossOrderID != "" && c.High >= stop
			liqHit := liq > 0 && c.High >= liq
			if stopHit && (!liqHit || stop < liq) {
				return ev.fireStop(dbStrategy, symbol, mode, open, exchange.SideBuy, stop, closeTime)
			} else if liqHit {
				return ev.fireLiquidation(dbStrategy, symbol, mode, open, liq)
			}
		}

		if c.OpenTime.After(lastEvaluated) {
			lastEvaluated = c.OpenTime
		}
	}

	if !lastEvaluated.IsZero() {
		if err := ev.Watermarks.UpdateSimStopEvaluatedAt(order.ID, lastEvaluated); err != nil {
			return fmt.Errorf("simulated stop: persist watermark for order %d: %w", order.ID, err)
		}
	}
	return nil
}

func (ev *SimulatedStopEvaluator) fireStop(dbStrategy entities.Strategy, symbol string, mode strategies.ExecutionMode, open entities.Signal, side exchange.OrderSide, stop float64, at time.Time) error {
	order := open.Orders[0]
	var fill exchange.OrderResult
	if st, ok := ev.Exchange.(SideStopTriggerer); ok {
		fill = st.TriggerStopWithSide(order.StopLossOrderID, side, float64(order.Quantity), stop, at)
	} else {
		fill = ev.Exchange.TriggerStop(order.StopLossOrderID, float64(order.Quantity), stop, at)
	}
	defer ev.Exchange.ClearTriggeredStop(order.StopLossOrderID)

	log.Printf("[engine] %s/%s: simulated stop-loss %s fired at %.8f (stop %.8f)",
		dbStrategy.Name, symbol, order.StopLossOrderID, fill.AvgFillPrice, stop)

	if err := ev.Signals.GenerateSellSignal(usecase.ExitSignal{
		Symbol:       symbol,
		StrategyID:   dbStrategy.ID,
		StrategyName: dbStrategy.Name,
		Mode:         mode.String(),
		ExitPrice:    float32(fill.AvgFillPrice),
		ExitReason:   usecase.ExitReasonSimulatedStopLoss,
	}); err != nil {
		return fmt.Errorf("simulated stop-loss close for %s failed: %w", symbol, err)
	}
	return nil
}

func (ev *SimulatedStopEvaluator) fireLiquidation(dbStrategy entities.Strategy, symbol string, mode strategies.ExecutionMode, open entities.Signal, liq float64) error {
	log.Printf("[engine] %s/%s: simulated liquidation triggered at %.8f",
		dbStrategy.Name, symbol, liq)

	if err := ev.Signals.GenerateSellSignal(usecase.ExitSignal{
		Symbol:       symbol,
		StrategyID:   dbStrategy.ID,
		StrategyName: dbStrategy.Name,
		Mode:         mode.String(),
		ExitPrice:    float32(liq),
		ExitReason:   entities.ExitReasonLiquidation,
	}); err != nil {
		return fmt.Errorf("simulated liquidation close for %s failed: %w", symbol, err)
	}
	return nil
}

// intervalDuration parses a kline interval such as "1m", "15m", "1h", "1d",
// "1w".
func intervalDuration(interval string) (time.Duration, error) {
	if len(interval) < 2 {
		return 0, fmt.Errorf("unsupported candle interval %q", interval)
	}
	n, err := strconv.Atoi(interval[:len(interval)-1])
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("unsupported candle interval %q", interval)
	}
	var unit time.Duration
	switch interval[len(interval)-1] {
	case 'm':
		unit = time.Minute
	case 'h':
		unit = time.Hour
	case 'd':
		unit = 24 * time.Hour
	case 'w':
		unit = 7 * 24 * time.Hour
	default:
		return 0, fmt.Errorf("unsupported candle interval %q", interval)
	}
	return time.Duration(n) * unit, nil
}
