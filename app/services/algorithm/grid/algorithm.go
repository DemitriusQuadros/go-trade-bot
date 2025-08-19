package grid

import (
	"context"
	"encoding/json"
	"go-trade-bot/app/entities"
	usecase "go-trade-bot/app/usecase/signal"
	"go-trade-bot/internal/broker"
	"log"
	"strconv"

	"github.com/markcheno/go-talib"
)

var key = "grid-"

type GridProcessor struct {
	strategy entities.Strategy
	broker   broker.Broker
	usecase  SignalUseCase
	cache    Cache
}

type GridOrder struct {
	Type  string
	Price float64
}

type Cache interface {
	Get(key string) (any, bool)
	Set(key string, value any)
	Delete(key string)
}

type SignalUseCase interface {
	GenerateBuySignal(e usecase.EntrySignal) error
	GenerateSellSignal(e usecase.ExitSignal) error
	GetOpenSignal(symbol string, strategyId uint) (entities.Signal, error)
}

func NewGridProcessor(s entities.Strategy, b broker.Broker, ss SignalUseCase, c Cache) GridProcessor {
	return GridProcessor{
		strategy: s,
		broker:   b,
		usecase:  ss,
		cache:    c,
	}
}

func (p GridProcessor) Execute() error {
	for _, symbol := range p.strategy.MonitoredSymbols {
		if err := p.RunGridAlgorithm(context.Background(), symbol); err != nil {
			log.Printf("Error executing %s for symbol %s: %v", p.strategy.Name, symbol, err)
			continue
		}
	}
	return nil
}

func (p GridProcessor) RunGridAlgorithm(ctx context.Context, symbol string) error {
	v, _ := p.cache.Get(key + symbol)

	existGrid, ok := v.([]GridOrder)

	if !ok {
		existGrid = []GridOrder{}
	}

	var config map[string]interface{}
	err := json.Unmarshal(p.strategy.StrategyConfiguration.Configuration, &config)
	if err != nil {
		return err
	}

	if len(existGrid) > 0 {
		p.monitore(ctx, symbol, existGrid, config)
	} else {
		p.buildGridForSymbol(ctx, symbol, config)
	}

	return nil
}

func (p GridProcessor) monitore(ctx context.Context, symbol string, grid []GridOrder, config map[string]interface{}) error {
	stopLossPct, _ := config["stop_loss_pct"].(float64)

	ticker, err := p.broker.ListTickerPrices(ctx, symbol)
	if err != nil {
		return err
	}
	current, err := strconv.ParseFloat(ticker[0].Price, 64)
	if err != nil {
		log.Printf("Erro parsing value for grid symbol %s err %s", symbol, err.Error())
	}

	openSignal, err := p.usecase.GetOpenSignal(symbol, p.strategy.ID)
	if err != nil {
		return err
	}

	if openSignal.ID != 0 {
		entryPrice := openSignal.Orders[0].EntryPrice
		pnl := (current - float64(entryPrice)) / float64(entryPrice) * 100

		if pnl <= -stopLossPct {
			return p.usecase.GenerateSellSignal(usecase.ExitSignal{
				Symbol:     openSignal.Symbol,
				StrategyID: p.strategy.ID,
				ExitPrice:  float32(current),
			})
		}
	}

	for _, order := range grid {
		if order.Type == "buy" {
			if current <= order.Price {
				log.Printf("[GRID] %s triggered %s at %.2f", symbol, order.Type, current)
				return p.usecase.GenerateBuySignal(usecase.EntrySignal{
					Symbol:     symbol,
					StrategyID: p.strategy.ID,
					EntryPrice: float32(current),
					MarginType: entities.MarginType(entities.Isolated),
				})
			}
		}

		if order.Type == "sell" {
			if current >= order.Price {
				if openSignal.ID != 0 {
					log.Printf("[GRID] %s triggered %s at %.2f", symbol, order.Type, current)
					return p.usecase.GenerateSellSignal(usecase.ExitSignal{
						Symbol:     openSignal.Symbol,
						StrategyID: p.strategy.ID,
						ExitPrice:  float32(current),
					})
				}

			}
		}
	}
	return nil
}

func (p GridProcessor) buildGridForSymbol(ctx context.Context, symbol string, config map[string]interface{}) error {
	klines, err := p.broker.ListKline(ctx, symbol, p.strategy.GetBrokerInterval(), 100)
	if err != nil {
		return err
	}
	if len(klines) == 0 {
		return nil
	}
	gridLevelsFloat, _ := config["grid_levels"].(float64)
	gridLevels := int(gridLevelsFloat)
	gridSpacingPct, _ := config["grid_spacing_pct"].(float64)
	volumeFilter, _ := config["volume_filter"].(float64)
	takeProfitPct, _ := config["take_profit_pct"].(float64)
	rsiPeriodFloat, _ := config["rsi_period"].(float64)
	rsiBuyThreshold, _ := config["rsi_buy_threshold"].(float64)
	rsiSellThreshold, _ := config["rsi_sell_threshold"].(float64)

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

	gridData := []GridOrder{}
	hasBuySignal := false
	for _, price := range gridPrices {
		if price < latestClose {
			if currentRSI < rsiBuyThreshold {
				hasBuySignal = true
				gridData = append(gridData, GridOrder{
					Type:  "buy",
					Price: price,
				})
			}
		} else {
			profit := ((price - latestClose) / latestClose) * 100
			if profit >= takeProfitPct && hasBuySignal {
				gridData = append(gridData, GridOrder{
					Type:  "sell",
					Price: price,
				})
			}
		}
	}

	if len(gridData) > 0 {
		p.cache.Set(key+symbol, gridData)
	}

	return nil
}
