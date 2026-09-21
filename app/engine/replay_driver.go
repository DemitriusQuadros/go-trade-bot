package engine

import (
	"context"

	"go-trade-bot/app/entities"
	"go-trade-bot/app/strategies"
	"go-trade-bot/internal/exchange"
	"go-trade-bot/internal/feed"
	"go-trade-bot/internal/metrics_provider"
)

type SignalRepositoryReader interface {
	GetAll() ([]entities.Signal, error)
	GetOpenSignals(symbol string, strategyId uint) (entities.Signal, error)
}

// ReplayDriver drives Engine.Run once per candle delivered by a Feed.
type ReplayDriver struct {
	Feed       feed.Feed
	Exchange   *SimulatedFillExchange
	Engine     *Engine
	Strategy   strategies.Strategy
	DBStrategy entities.Strategy
	Symbol     string
	Mode       strategies.ExecutionMode
	SignalRepo SignalRepositoryReader
}

func NewReplayDriver(
	f feed.Feed,
	ex *SimulatedFillExchange,
	eng *Engine,
	strat strategies.Strategy,
	dbStrat entities.Strategy,
	symbol string,
	mode strategies.ExecutionMode,
	signalRepo SignalRepositoryReader,
) *ReplayDriver {
	return &ReplayDriver{
		Feed:       f,
		Exchange:   ex,
		Engine:     eng,
		Strategy:   strat,
		DBStrategy: dbStrat,
		Symbol:     symbol,
		Mode:       mode,
		SignalRepo: signalRepo,
	}
}

// Run consumes the Feed to exhaustion, calling Engine.Run once per candle.
func (d *ReplayDriver) Run(ctx context.Context) ([]metrics_provider.TradeLogEntry, error) {
	var lastCandle exchange.Candle

	for {
		select {
		case <-ctx.Done():
			return d.collectTradeLog(lastCandle)
		default:
		}

		c, ok := d.Feed.Next()
		if !ok {
			break
		}
		lastCandle = c

		d.Exchange.SetSimulatedTime(c.OpenTime, c)

		if err := d.Engine.Run(ctx, d.Strategy, d.DBStrategy, d.Symbol, d.Mode); err != nil {
			// Continue backtest even if a single cycle returns a strategy error
			continue
		}
	}

	return d.collectTradeLog(lastCandle)
}

func (d *ReplayDriver) collectTradeLog(lastCandle exchange.Candle) ([]metrics_provider.TradeLogEntry, error) {
	if d.SignalRepo == nil {
		return []metrics_provider.TradeLogEntry{}, nil
	}

	signals, err := d.SignalRepo.GetAll()
	if err != nil {
		return nil, err
	}

	tradeLog := make([]metrics_provider.TradeLogEntry, 0, len(signals))

	for _, s := range signals {
		if s.StrategyID != d.DBStrategy.ID || s.Symbol != d.Symbol {
			continue
		}
		if len(s.Orders) == 0 {
			continue
		}

		order := s.Orders[0]
		entryTime := order.CreatedAt
		if entryTime.IsZero() {
			entryTime = s.CreatedAt
		}

		if s.Status == entities.Closed {
			exitTime := order.UpdatedAt
			if exitTime.IsZero() {
				exitTime = s.UpdatedAt
			}
			exitReason := "manual"
			if order.Profit > 0 {
				exitReason = "take_profit"
			} else if order.Profit < 0 {
				exitReason = "stop_loss"
			}

			tradeLog = append(tradeLog, metrics_provider.TradeLogEntry{
				Symbol:     s.Symbol,
				EntryTime:  entryTime,
				EntryPrice: float64(order.EntryPrice),
				ExitTime:   exitTime,
				ExitPrice:  float64(order.ExitPrice),
				Quantity:   float64(order.Quantity),
				Profit:     float64(order.Profit),
				ExitReason: exitReason,
			})
		} else if s.Status == entities.Open {
			// Mark-to-market for open position at end
			exitPrice := float64(order.EntryPrice)
			if lastCandle.Close > 0 {
				exitPrice = lastCandle.Close
			}
			unrealizedProfit := (exitPrice - float64(order.EntryPrice)) * float64(order.Quantity)
			unrealizedProfit -= float64(order.EntryFee + order.ExitFee)

			tradeLog = append(tradeLog, metrics_provider.TradeLogEntry{
				Symbol:     s.Symbol,
				EntryTime:  entryTime,
				EntryPrice: float64(order.EntryPrice),
				ExitTime:   lastCandle.OpenTime,
				ExitPrice:  exitPrice,
				Quantity:   float64(order.Quantity),
				Profit:     unrealizedProfit,
				ExitReason: "open_at_end",
			})
		}
	}

	return tradeLog, nil
}
