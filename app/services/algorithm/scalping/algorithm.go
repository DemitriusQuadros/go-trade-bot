package scalping

import (
	"context"
	"encoding/json"
	"fmt"
	"go-trade-bot/app/entities"
	usecase "go-trade-bot/app/usecase/signal"
	"go-trade-bot/internal/broker"
	"log"
	"strconv"
)

type ScalpingProcessor struct {
	strategy entities.Strategy
	broker   broker.Broker
	usecase  SignalUseCase
}

type SignalUseCase interface {
	GenerateBuySignal(e usecase.EntrySignal) error
	GenerateSellSignal(e usecase.ExitSignal) error
	GetOpenSignal(symbol string, strategyId uint) (entities.Signal, error)
}

func NewScalpingProcessor(s entities.Strategy, b broker.Broker, ss SignalUseCase) ScalpingProcessor {
	return ScalpingProcessor{
		strategy: s,
		broker:   b,
		usecase:  ss,
	}
}

func (p ScalpingProcessor) Execute() error {
	for _, symbol := range p.strategy.MonitoredSymbols {
		if err := p.RunScalpingAlgorithm(context.Background(), symbol); err != nil {
			log.Printf("Error executing grid algorithm for symbol %s: %v", symbol, err)
			continue
		}
	}
	log.Printf("Executing Scalping Strategy: %s", p.strategy.Name)
	return nil
}

func (p ScalpingProcessor) RunScalpingAlgorithm(ctx context.Context, symbol string) error {
	klines, err := p.broker.ListKline(ctx, symbol, p.strategy.GetBrokerInterval(), 3)
	if err != nil {
		return err
	}
	if len(klines) < 2 {
		return fmt.Errorf("not enough candles to analyze")
	}

	var config map[string]interface{}
	err = json.Unmarshal(p.strategy.StrategyConfiguration.Configuration, &config)
	if err != nil {
		return err
	}

	takeProfitPct, _ := config["take_profit_pct"].(float64)
	stopLossPct, _ := config["stop_loss_pct"].(float64)
	leverage, _ := config["leverage"].(float64)

	openSignal, err := p.usecase.GetOpenSignal(symbol, p.strategy.ID)
	if err != nil {
		return err
	}
	if openSignal.ID != 0 {
		tickerPrices, err := p.broker.ListTickerPrices(ctx, symbol)
		if err != nil {
			return fmt.Errorf("failed to list ticker prices for symbol %s: %v", symbol, err)
		}
		if len(tickerPrices) == 0 {
			return fmt.Errorf("no ticker prices found for symbol %s", symbol)
		}
		currentPrice, err := strconv.ParseFloat(tickerPrices[0].Price, 64)
		if err != nil {
			return fmt.Errorf("failed to parse ticker price for symbol %s: %v", symbol, err)
		}
		entryPrice := openSignal.Orders[0].EntryPrice

		pnl := (currentPrice - float64(entryPrice)) / float64(entryPrice) * 100

		if pnl >= takeProfitPct || pnl <= -stopLossPct {
			exit := usecase.ExitSignal{
				Symbol:     symbol,
				StrategyID: p.strategy.ID,
				ExitPrice:  float32(currentPrice),
			}

			err := p.usecase.GenerateSellSignal(exit)
			if err != nil {
				return err
			}
			return nil
		} else {
			return nil
		}
	}

	latestClose, err := strconv.ParseFloat(klines[len(klines)-1].Close, 64)
	if err != nil {
		return fmt.Errorf("failed to parse latest close price: %v", err)
	}
	prevClose, err := strconv.ParseFloat(klines[len(klines)-2].Close, 64)
	if err != nil {
		return fmt.Errorf("failed to parse previous close price: %v", err)
	}

	if latestClose > prevClose {
		entry := usecase.EntrySignal{
			Symbol:     symbol,
			StrategyID: p.strategy.ID,
			EntryPrice: float32(latestClose),
			Leverage:   float32(leverage),
			MarginType: entities.MarginType(entities.Isolated),
		}

		p.usecase.GenerateBuySignal(entry)
	}

	return nil
}
