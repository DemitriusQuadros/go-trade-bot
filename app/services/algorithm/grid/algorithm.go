package grid

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

	gridLevelsFloat, ok := config["grid_levels"].(float64)
	if !ok {
		return fmt.Errorf("configuração inválida: grid_levels")
	}
	gridLevels := int(gridLevelsFloat)

	gridSpacingPct, ok := config["grid_spacing_pct"].(float64)
	if !ok {
		return fmt.Errorf("configuração inválida: grid_spacing_pct")
	}

	capitalPerOrder, _ := config["capital_per_order"].(float64)
	volumeFilter, _ := config["volume_filter"].(float64)

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

	gridPrices := make([]float64, gridLevels)
	for i := 0; i < gridLevels; i++ {
		gridPrices[i] = latestClose + (float64(i)-float64(gridLevels/2))*gridSpacing
	}

	for _, price := range gridPrices {
		if price < latestClose {
			entry := usecase.EntrySignal{
				Symbol:         symbol,
				StrategyID:     p.strategy.ID,
				EntryPrice:     float32(price),
				Leverage:       0,
				InvestedAmount: float32(capitalPerOrder),
				MarginType:     entities.MarginType(entities.Isolated),
			}

			p.usecase.GenerateBuySignal(entry)
		} else {
			exit := usecase.ExitSignal{
				Symbol:     symbol,
				StrategyID: p.strategy.ID,
				ExitPrice:  float32(price),
			}
			p.usecase.GenerateSellSignal(exit)
		}
	}

	return nil
}
