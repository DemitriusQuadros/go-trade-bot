package engine

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go-trade-bot/app/entities"
	"go-trade-bot/app/strategies"
	usecase "go-trade-bot/app/usecase/signal"
	"go-trade-bot/internal/exchange"
	"go-trade-bot/internal/futuresmath"
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
type BacktestStopEvaluator struct {
	Signals           BacktestStopSignals
	Exchange          BacktestStopExchange
	FundingRateLookup func(symbol string, at time.Time) (float64, error)
}

var _ CycleHook = (*BacktestStopEvaluator)(nil)

// NewBacktestStopEvaluator builds the evaluator.
func NewBacktestStopEvaluator(signals BacktestStopSignals, ex BacktestStopExchange) *BacktestStopEvaluator {
	return &BacktestStopEvaluator{Signals: signals, Exchange: ex}
}

// BeforeCycle closes the open position if its simulated stop already filled or if liquidated or settles funding.
func (ev *BacktestStopEvaluator) BeforeCycle(ctx context.Context, dbStrategy entities.Strategy, symbol string, mode strategies.ExecutionMode) error {
	open, err := ev.Signals.GetOpenSignal(symbol, dbStrategy.ID)
	if err != nil {
		return fmt.Errorf("backtest stop: open signal for %s: %w", symbol, err)
	}
	if open.ID == 0 || len(open.Orders) == 0 {
		return nil
	}
	order := open.Orders[0]

	isShort := open.PositionSide == entities.PositionSideShort || order.PositionSide == entities.PositionSideShort
	isFutures := order.Leverage > 1 || isShort || order.LiquidationPrice > 0

	if order.StopLossOrderID == "" && order.LiquidationPrice <= 0 && !isFutures {
		return nil
	}

	var c exchange.Candle
	if ccProvider, ok := ev.Exchange.(interface{ CurrentCandle() exchange.Candle }); ok {
		c = ccProvider.CurrentCandle()
	}
	if c.OpenTime.IsZero() {
		if lk, ok := ev.Exchange.(interface {
			ListKline(ctx context.Context, symbol, interval string, limit int) ([]exchange.Candle, error)
		}); ok {
			candles, kErr := lk.ListKline(ctx, symbol, dbStrategy.GetBrokerInterval(), 1)
			if kErr == nil && len(candles) > 0 {
				c = candles[0]
			}
		}
	}

	if !c.OpenTime.IsZero() {
		// 1. Settle crossed 8h funding timestamps
		if isFutures {
			fundingWatermark := order.CreatedAt
			if order.LastFundingTime != nil {
				fundingWatermark = *order.LastFundingTime
			}
			fundingTimestamps := futuresmath.FundingTimestampsInWindow(fundingWatermark, c.OpenTime)
			if len(fundingTimestamps) > 0 {
				if settler, ok := ev.Signals.(FundingSettler); ok {
					fmSide := futuresmath.SideLong
					if isShort {
						fmSide = futuresmath.SideShort
					}
					for _, fts := range fundingTimestamps {
						fundingRate := futuresmath.DefaultFundingRate8h
						if ev.FundingRateLookup != nil {
							if fr, frErr := ev.FundingRateLookup(symbol, fts); frErr == nil && fr != 0 {
								fundingRate = fr
							}
						}
						markPrice := c.Close
						payment := futuresmath.FundingPayment(fmSide, float64(order.Quantity), markPrice, fundingRate)
						accFunding := float64(order.AccumulatedFunding) + payment
						newLiqPrice := futuresmath.IsolatedLiquidationPrice(symbol, fmSide, float64(order.EntryPrice), float64(order.Quantity), float64(order.Leverage), 0, accFunding)
						if sErr := settler.SettleFunding(symbol, dbStrategy.ID, float32(payment), float32(newLiqPrice), fts); sErr == nil {
							order.AccumulatedFunding = float32(accFunding)
							order.LiquidationPrice = float32(newLiqPrice)
							order.LastFundingTime = &fts
						}
					}
				}
			}
		}

		// 2. Check if liq > 0 and liquidation triggered before stop
		stop := float64(order.StopLossPrice)
		liq := float64(order.LiquidationPrice)

		liqHitBeforeStop := false
		if !isShort {
			if liq > 0 && c.Low <= liq && (stop <= 0 || liq >= stop) {
				liqHitBeforeStop = true
			}
		} else {
			if liq > 0 && c.High >= liq && (stop <= 0 || liq <= stop) {
				liqHitBeforeStop = true
			}
		}

		if liqHitBeforeStop {
			return ev.Signals.GenerateSellSignal(usecase.ExitSignal{
				Symbol:       symbol,
				StrategyID:   dbStrategy.ID,
				StrategyName: dbStrategy.Name,
				Mode:         mode.String(),
				ExitPrice:    float32(liq),
				ExitReason:   entities.ExitReasonLiquidation,
			})
		}
	}

	// 3. Check if order.StopLossOrderID != "" and GetOrder reports OrderStatusFilled
	if order.StopLossOrderID != "" {
		res, err := ev.Exchange.GetOrder(ctx, symbol, order.StopLossOrderID)
		if err != nil {
			if errors.Is(err, exchange.ErrOrderNotFound) {
				return nil
			}
			return fmt.Errorf("backtest stop: stop order %s: %w", order.StopLossOrderID, err)
		}
		if res.Status == exchange.OrderStatusFilled {
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
		}
	}
	return nil
}
