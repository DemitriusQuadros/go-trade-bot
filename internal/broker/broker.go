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

func NewBroker(cfg *configuration.Configuration) *Broker {
	client := binance.NewClient(cfg.BinanceApiKey, cfg.BinanceAPISecret)
	return &Broker{
		client: client,
	}
}

func (b Broker) ListTickerPrices(ctx context.Context) ([]*binance.SymbolPrice, error) {
	prices, err := b.client.NewListPricesService().Do(ctx)
	if err != nil {
		fmt.Println(err)
		return nil, err
	}
	return prices, nil
}
