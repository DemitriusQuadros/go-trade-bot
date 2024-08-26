package usecase

import (
	"context"
	"go-trade-bot/app/symbol/entities"
	"go-trade-bot/internal/broker"
	"log"
	"strconv"
	"time"
)

type SymbolRepository interface {
	Save(ctx context.Context, symbol entities.Symbol) error
}
type SymbolUseCase struct {
	Broker     *broker.Broker
	Repository SymbolRepository
}

func NewSymbolUseCase(broker *broker.Broker, repository SymbolRepository) SymbolUseCase {
	return SymbolUseCase{
		Broker:     broker,
		Repository: repository,
	}
}

func (u SymbolUseCase) SaveSymbolPrice(ctx context.Context, symbol string) error {
	symbols, err := u.Broker.ListTickerPrices(ctx, symbol)

	if err != nil {
		return err
	}

	if len(symbols) == 0 {
		log.Printf("Symbol not found %s", symbol)
		return nil
	}

	s := symbols[0]
	price, err := strconv.ParseFloat(s.Price, 64)
	if err != nil {
		log.Fatal("fail to convert price to float64:", err)
		return nil
	}

	err = u.Repository.Save(ctx, entities.Symbol{
		Symbol:    s.Symbol,
		Price:     price,
		CreatedAt: time.Now(),
	})

	if err != nil {
		log.Fatal("fail to save symbol price:", err)
		return nil
	}

	return nil
}
