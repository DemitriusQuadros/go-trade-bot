package grid

import (
	"context"
	"encoding/json"
	"go-trade-bot/app/entities"
	usecase "go-trade-bot/app/usecase/signal"
	"go-trade-bot/internal/broker"
	"log"
	"math"
	"strconv"
	"time"

	"github.com/markcheno/go-talib"
)

type GridProcessor struct {
	strategy entities.Strategy
	broker   broker.Broker
	usecase  SignalUseCase
}

type SignalUseCase interface {
	GenerateBuySignal(e usecase.EntrySignal) error
	GenerateSellSignal(e usecase.ExitSignal) error
	GetOpenSignal(symbol string, strategyId uint) (entities.Signal, error)
}

func NewGridProcessor(s entities.Strategy, b broker.Broker, ss SignalUseCase) GridProcessor {
	return GridProcessor{
		strategy: s,
		broker:   b,
		usecase:  ss,
	}
}

func (p GridProcessor) Execute() error {
	for _, symbol := range p.strategy.MonitoredSymbols {
		if err := p.RunGridAlgorithm(context.Background(), symbol); err != nil {
			log.Printf("Error executing grid algorithm for symbol %s: %v", symbol, err)
			continue
		}
	}
	log.Printf("Executing Grid Strategy: %s", p.strategy.Name)
	return nil
}

func (p GridProcessor) RunGridAlgorithm(ctx context.Context, symbol string) error {
	klines, err := p.broker.ListKline(ctx, symbol, p.strategy.GetBrokerInterval(), 100)
	if err != nil {
		return err
	}
	if len(klines) == 0 {
		return nil
	}

	var config map[string]interface{}
	err = json.Unmarshal(p.strategy.StrategyConfiguration.Configuration, &config)
	if err != nil {
		return err
	}

	gridLevelsFloat, _ := config["grid_levels"].(float64)
	gridLevels := int(gridLevelsFloat)
	gridSpacingPct, _ := config["grid_spacing_pct"].(float64)
	volumeFilter, _ := config["volume_filter"].(float64)
	takeProfitPct, _ := config["take_profit_pct"].(float64)
	stopLossPct, _ := config["stop_loss_pct"].(float64)
	rsiPeriodFloat, _ := config["rsi_period"].(float64)
	rsiBuyThreshold, _ := config["rsi_buy_threshold"].(float64)
	rsiSellThreshold, _ := config["rsi_sell_threshold"].(float64)
	leverage, _ := config["leverage"].(float64)

	closes := make([]float64, len(klines))
	for i, k := range klines {
		close, err := strconv.ParseFloat(k.Close, 64)
		if err != nil {
			return err
		}
		closes[i] = close
	}

	rsi := talib.Rsi(closes, int(rsiPeriodFloat))
	currentRSI := rsi[len(rsi)-1]

	latestClose, err := strconv.ParseFloat(klines[len(klines)-1].Close, 64)
	if err != nil {
		return err
	}
	gridSpacing := latestClose * gridSpacingPct / 100

	if volumeFilter > 0 {
		vol, err := p.broker.Get24hVolume(ctx, symbol)
		if err == nil && vol < volumeFilter {
			log.Printf("Volume under minimun (%.2f < %.2f), ignoring symbol %s", vol, volumeFilter, symbol)
			return nil
		}
	}

	if currentRSI > rsiSellThreshold {
		log.Printf("RSI above sell threshold (%.2f > %.2f), skipping %s", currentRSI, rsiSellThreshold, symbol)
		return nil
	}

	gridPrices := make([]float64, gridLevels)
	for i := 0; i < gridLevels; i++ {
		gridPrices[i] = latestClose + (float64(i)-float64(gridLevels/2))*gridSpacing
	}

	for _, price := range gridPrices {
		if price < latestClose {
			if currentRSI < rsiBuyThreshold {
				entry := usecase.EntrySignal{
					Symbol:     symbol,
					StrategyID: p.strategy.ID,
					EntryPrice: float32(price),
					Leverage:   float32(leverage),
					MarginType: entities.MarginType(entities.Isolated),
				}
				p.usecase.GenerateBuySignal(entry)
			}
		} else {
			openSignal, err := p.usecase.GetOpenSignal(symbol, p.strategy.ID)

			if err != nil {
				return err
			}

			if openSignal.ID != 0 {
				// do not sell if is under one minute of diference
				if time.Since(openSignal.CreatedAt) < time.Minute {
					continue
				}
				pnl := (price - float64(openSignal.Orders[0].EntryPrice)) / float64(openSignal.Orders[0].EntryPrice) * 100

				// ignoring low movements
				priceDiffPct := math.Abs(price-float64(openSignal.Orders[0].EntryPrice)) / float64(openSignal.Orders[0].EntryPrice) * 100
				if priceDiffPct < 0.1 {
					continue
				}

				if pnl >= takeProfitPct || pnl <= -stopLossPct {
					exit := usecase.ExitSignal{
						Symbol:     symbol,
						StrategyID: p.strategy.ID,
						ExitPrice:  float32(price),
					}
					p.usecase.GenerateSellSignal(exit)
				}
			}
		}
	}

	return nil
}
