package bollinger

import (
	"context"
	"encoding/json"
	"fmt"
	"go-trade-bot/app/entities"
	usecase "go-trade-bot/app/usecase/signal"
	"go-trade-bot/internal/broker"
	"log"
	"strconv"

	talib "github.com/markcheno/go-talib"
)

type BollingerProcessor struct {
	strategy entities.Strategy
	broker   broker.Broker
	usecase  SignalUseCase
}

type SignalUseCase interface {
	GenerateBuySignal(e usecase.EntrySignal) error
	GenerateSellSignal(e usecase.ExitSignal) error
	GetOpenSignal(symbol string, strategyId uint) (entities.Signal, error)
}

func NewBollingerProcessor(s entities.Strategy, b broker.Broker, ss SignalUseCase) BollingerProcessor {
	return BollingerProcessor{
		strategy: s,
		broker:   b,
		usecase:  ss,
	}
}

func (p BollingerProcessor) Execute() error {
	for _, symbol := range p.strategy.MonitoredSymbols {
		if err := p.RunBollingerAlgorithm(context.Background(), symbol); err != nil {
			log.Printf("Error executing bollinger algorithm for symbol %s: %v", symbol, err)
			continue
		}
	}
	log.Printf("Executing Bollinger Strategy: %s", p.strategy.Name)
	return nil
}

func (p BollingerProcessor) RunBollingerAlgorithm(ctx context.Context, symbol string) error {
	klines, err := p.broker.ListKline(ctx, symbol, p.strategy.GetBrokerInterval(), 30)
	if err != nil {
		return err
	}
	if len(klines) < 20 {
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

	closes := make([]float64, len(klines))
	for i, k := range klines {
		closes[i], _ = strconv.ParseFloat(k.Close, 64)
	}

	upper, _, lower := talib.BBands(closes, 20, 2.0, 2.0, talib.EMA)

	current := closes[len(closes)-1]

	openSignal, err := p.usecase.GetOpenSignal(symbol, p.strategy.ID)
	if err != nil {
		return err
	}
	// If has a open signal, check if we need to close it
	if openSignal.ID != 0 {
		return p.generateSell(ctx, openSignal, takeProfitPct, stopLossPct, upper)
	}

	// if we don't have an open signal, check if we need to open one
	if current < lower[len(lower)-1] {
		entry := usecase.EntrySignal{
			Symbol:     symbol,
			StrategyID: p.strategy.ID,
			EntryPrice: float32(current),
			Leverage:   float32(leverage),
			MarginType: entities.MarginType(entities.Isolated),
		}
		return p.usecase.GenerateBuySignal(entry)
	}

	return nil
}

func (p BollingerProcessor) generateSell(ctx context.Context, openSignal entities.Signal, takeProfitPct float64, stopLossPct float64, upper []float64) error {
	ticker, err := p.broker.ListTickerPrices(ctx, openSignal.Symbol)
	if err != nil {
		return fmt.Errorf("Can't get current price for symbol %s when closing open order", openSignal.Symbol)
	}
	current, _ := strconv.ParseFloat(ticker[0].Price, 32)
	entryPrice := openSignal.Orders[0].EntryPrice
	leverage := float64(openSignal.Orders[0].Leverage)
	pnl := ((current - float64(entryPrice)) / float64(entryPrice)) * leverage * 100

	if pnl >= takeProfitPct || pnl <= -stopLossPct || current > upper[len(upper)-1] {
		exit := usecase.ExitSignal{
			Symbol:     openSignal.Symbol,
			StrategyID: p.strategy.ID,
			ExitPrice:  float32(current),
		}
		return p.usecase.GenerateSellSignal(exit)
	}
	return nil
}
