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

	"github.com/adshao/go-binance/v2"
	"github.com/markcheno/go-talib"
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
			log.Printf("Error executing %s for symbol %s: %v", p.strategy.Name, symbol, err)
			continue
		}
	}
	return nil
}

func (p ScalpingProcessor) RunScalpingAlgorithm(ctx context.Context, symbol string) error {
	klines, err := p.broker.ListKline(ctx, symbol, p.strategy.GetBrokerInterval(), 60)
	if err != nil {
		return err
	}
	if len(klines) < 10 {
		return fmt.Errorf("not enough candles to analyze")
	}

	var config map[string]interface{}
	err = json.Unmarshal(p.strategy.StrategyConfiguration.Configuration, &config)
	if err != nil {
		return err
	}

	takeProfitPct, _ := config["take_profit_pct"].(float64)
	stopLossPct, _ := config["stop_loss_pct"].(float64)

	openSignal, err := p.usecase.GetOpenSignal(symbol, p.strategy.ID)
	if err != nil {
		return err
	}
	// has open signal need to close first
	if openSignal.ID != 0 {
		return p.generateSell(ctx, symbol, openSignal, takeProfitPct, stopLossPct)
	}

	// Generate a buy signal validating volume and RSI
	if validateVolume(klines) && validateRSI(klines) && p.isUptrend(ctx, symbol) {
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
				MarginType: entities.MarginType(entities.Isolated),
			}

			p.usecase.GenerateBuySignal(entry)
		}
	}
	return nil
}

func validateVolume(klines []*binance.Kline) bool {
	avgVolume := 0.0
	for _, k := range klines {
		vol, _ := strconv.ParseFloat(k.Volume, 64)
		avgVolume += vol
	}
	avgVolume /= float64(len(klines))

	latestVolume, _ := strconv.ParseFloat(klines[len(klines)-1].Volume, 64)
	if latestVolume < avgVolume {
		return false
	}
	return true
}

func validateRSI(klines []*binance.Kline) bool {
	closes := make([]float64, len(klines))
	for i, k := range klines {
		closes[i], _ = strconv.ParseFloat(k.Close, 64)
	}

	rsi := talib.Rsi(closes, 14)
	latestRSI := rsi[len(rsi)-1]
	if latestRSI > 70 {
		return false
	}

	return true
}

func (p ScalpingProcessor) generateSell(ctx context.Context, symbol string, openSignal entities.Signal, takeProfitPct float64, stopLossPct float64) error {
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

func (p ScalpingProcessor) isUptrend(ctx context.Context, symbol string) bool {
	longTermKlines, err := p.broker.ListKline(ctx, symbol, "15m", 50)
	if err != nil || len(longTermKlines) < 20 {
		log.Printf("Failed to fetch long-term klines for trend analysis: %v", err)
		return false
	}

	longCloses := make([]float64, len(longTermKlines))
	for i, k := range longTermKlines {
		longCloses[i], _ = strconv.ParseFloat(k.Close, 64)
	}

	ema := talib.Ema(longCloses, 20)
	if len(ema) == 0 {
		log.Println("EMA calculation failed for trend analysis")
		return false
	}

	currentPrice := longCloses[len(longCloses)-1]
	currentEMA := ema[len(ema)-1]

	return currentPrice > currentEMA
}
