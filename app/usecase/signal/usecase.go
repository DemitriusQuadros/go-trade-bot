package usecase

import (
	"fmt"
	"go-trade-bot/app/entities"
	"time"
)

type EntrySignal struct {
	Symbol         string
	StrategyID     uint
	EntryPrice     float32
	Leverage       float32
	InvestedAmount float32
	MarginType     entities.MarginType
}

type ExitSignal struct {
	Symbol     string
	StrategyID uint
	ExitPrice  float32
}

type SignalRepository interface {
	Create(signal entities.Signal) error
	GetOpenSignals(symbol string, strategyId uint) (entities.Signal, error)
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

func (s SignalUseCase) GenerateBuySignal(e EntrySignal) error {
	openSignal, err := s.Repository.GetOpenSignals(e.Symbol, e.StrategyID)
	if err != nil {
		return err
	}

	if openSignal.ID != 0 {
		return nil
	}

	if e.Leverage <= 0 {
		e.Leverage = 1
	}

	investedWithLeverage := e.InvestedAmount * e.Leverage

	signal := entities.Signal{
		Symbol:     e.Symbol,
		Status:     entities.Open,
		StrategyID: e.StrategyID,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
		Orders: []entities.Order{
			{
				EntryPrice:     e.EntryPrice,
				ExitPrice:      0,
				Quantity:       investedWithLeverage / e.EntryPrice,
				InvestedAmount: e.InvestedAmount,
				MarginType:     e.MarginType,
				EntryFee:       calculateEntryFee(investedWithLeverage),
				ExitFee:        0,
				Leverage:       e.Leverage,
				ExecutedQty:    0,
				IsClosing:      false,
				CreatedAt:      time.Now(),
				UpdatedAt:      time.Now(),
			},
		},
	}

	err = s.Repository.Create(signal)
	if err != nil {
		return err
	}
	return nil
}

func (s SignalUseCase) GenerateSellSignal(e ExitSignal) error {
	openSignal, err := s.Repository.GetOpenSignals(e.Symbol, e.StrategyID)
	if err != nil {
		return err
	}

	if openSignal.ID != 0 {
		openSignal.Status = entities.StrategyStatus(entities.Closed)
		openSignal.Orders[0].ExitPrice = e.ExitPrice
		openSignal.Orders[0].ExitFee = calculateExitFee(openSignal.Orders[0], e.ExitPrice)
		openSignal.Orders[0].UpdatedAt = time.Now()
		err = s.Repository.Update(openSignal)
		if err != nil {
			return err
		}
	} else {
		return fmt.Errorf("signal not found for symbol %s and strategy ID %d", e.Symbol, e.StrategyID)
	}

	return nil
}

func calculateEntryFee(InvestedAmount float32) float32 {
	feePct := 0.1
	fee := float32(float64(InvestedAmount) * feePct / 100)
	return fee
}

func calculateExitFee(order entities.Order, sellPrice float32) float32 {
	feePct := 0.1
	total := float64(order.Quantity) * float64(sellPrice)
	fee := float32(total * feePct / 100)
	return fee
}

func (s SignalUseCase) GetOpenSignal(symbol string, strategyId uint) (entities.Signal, error) {
	openSignal, err := s.Repository.GetOpenSignals(symbol, strategyId)
	if err != nil {
		return entities.Signal{}, err
	}
	if openSignal.ID == 0 {
		return entities.Signal{}, nil
	}
	return openSignal, nil
}
