package usecase

import (
	"go-trade-bot/app/entities"
	"log"
	"time"
)

type SignalRepository interface {
	Create(signal entities.Signal) error
	GetOpenSignals(symbol string) (entities.Signal, error)
	Update(signal entities.Signal) error
}

type SignalUseCase struct {
	Repository SignalRepository
}

func NewSignalUseCase(repository SignalRepository) SignalUseCase {
	return SignalUseCase{
		Repository: repository,
	}
}

func (s SignalUseCase) GenerateBuySignal(symbol string, strategyExecutionID uint, price float32, quantity float32) error {
	openSignal, err := s.Repository.GetOpenSignals(symbol)
	if err != nil {
		return err
	}

	if openSignal.ID != 0 {
		return nil
	}

	signal := entities.Signal{
		Symbol:              symbol,
		StrategyExecutionID: strategyExecutionID,
		Status:              entities.Open,
		Orders: []entities.Order{
			{
				Price:          price,
				Quantity:       quantity,
				OrderOperation: entities.Buy,
				CreatedAt:      time.Now(),
			},
		},
	}

	err = s.Repository.Create(signal)
	if err != nil {
		return err
	}
	return nil
}

func (s SignalUseCase) GenerateSellSignal(symbol string, strategyExecutionID uint, price float32) error {
	openSignal, err := s.Repository.GetOpenSignals(symbol)
	if err != nil {
		return err
	}
	if openSignal.ID != 0 {
		openSignal.Status = entities.StrategyStatus(entities.Closed)
		openSignal.Orders = append(openSignal.Orders, entities.Order{
			OrderOperation: entities.Sell,
			Price:          price,
			Quantity:       openSignal.Orders[0].Quantity,
			CreatedAt:      time.Now(),
		})

		err = s.Repository.Update(openSignal)
		if err != nil {
			return err
		}
	} else {
		log.Printf("No open signal found for symbol %s", symbol)
		return nil

	}

	return nil
}
