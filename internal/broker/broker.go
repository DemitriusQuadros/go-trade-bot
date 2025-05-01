package broker

import (
	"context"
	"fmt"
	"go-trade-bot/internal/configuration"

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
