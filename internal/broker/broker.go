package broker

import (
	"context"
	"fmt"
	"go-trade-bot/internal/configuration"
	"strconv"

	"github.com/adshao/go-binance/v2"
)

type Broker struct {
	client *binance.Client
}

func NewBroker(cfg *configuration.Configuration) Broker {
	client := binance.NewClient(cfg.Broker.ApiKey, cfg.Broker.ApiSecret)
	return Broker{
		client: client,
	}
}

func (b Broker) ListTickerPrices(ctx context.Context, symbol string) ([]*binance.SymbolPrice, error) {
	prices, err := b.client.NewListPricesService().Symbol(symbol).Do(ctx)
	if err != nil {
		fmt.Println(err)
		return nil, err
	}
	return prices, nil
}

func (b Broker) ListKline(ctx context.Context, symbol string, interval string, limit int) ([]*binance.Kline, error) {
	klines, err := b.client.NewKlinesService().Symbol(symbol).Interval(interval).Limit(limit).Do(ctx)
	if err != nil {
		fmt.Println(err)
		return nil, err
	}
	return klines, nil
}

func (b Broker) Get24hVolume(ctx context.Context, symbol string) (float64, error) {
	klines, err := b.client.NewKlinesService().Symbol(symbol).Interval("1d").Limit(1).Do(ctx)
	if err != nil {
		fmt.Println(err)
		return 0, err
	}
	if len(klines) == 0 {
		return 0, fmt.Errorf("no klines found for symbol %s", symbol)
	}
	volume, err := strconv.ParseFloat(klines[0].Volume, 64)
	if err != nil {
		return 0, err
	}
	return volume, nil
}
