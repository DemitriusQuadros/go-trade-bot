package usecase

import (
	"fmt"
	"go-trade-bot/app/entities"
	"time"
)

type EntrySignal struct {
	Symbol     string
	StrategyID uint
	EntryPrice float32
	MarginType entities.MarginType
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
type AccountUseCase interface {
	DeductOrder(entryPrice float32) error
	AddOrder(exitPrice float32) error
	GetDisponibleAmout() (float32, error)
	CanOpenOrder() (bool, error)
}

type SignalUseCase struct {
	Repository     SignalRepository
	AccountUseCase AccountUseCase
}

func NewSignalUseCase(repository SignalRepository, ac AccountUseCase) SignalUseCase {
	return SignalUseCase{
		Repository:     repository,
		AccountUseCase: ac,
	}
}

func (s SignalUseCase) GenerateBuySignal(e EntrySignal) error {
	canOpen, err := s.AccountUseCase.CanOpenOrder()
	if err != nil {
		return fmt.Errorf("failed to check if order can be opened: %w", err)
	}

	if !canOpen {
		return nil
	}

	openSignal, err := s.Repository.GetOpenSignals(e.Symbol, e.StrategyID)
	if err != nil {
		return err
	}

	if openSignal.ID != 0 {
		return nil
	}

	investedAmount, _ := s.AccountUseCase.GetDisponibleAmout()

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
				Quantity:       investedAmount / e.EntryPrice,
				InvestedAmount: investedAmount,
				MarginType:     e.MarginType,
				EntryFee:       calculateEntryFee(investedAmount),
				ExitFee:        0,
				Leverage:       0,
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

	s.AccountUseCase.DeductOrder(investedAmount)

	return nil
}

func (s SignalUseCase) GenerateSellSignal(e ExitSignal) error {
	openSignal, err := s.Repository.GetOpenSignals(e.Symbol, e.StrategyID)
	if err != nil {
		return err
	}

	if openSignal.ID != 0 {
		openSignal.Status = entities.SignalStatus(entities.Closed)
		openSignal.Orders[0].ExitPrice = e.ExitPrice
		openSignal.Orders[0].ExitFee = calculateExitFee(openSignal.Orders[0], e.ExitPrice)
		openSignal.Orders[0].UpdatedAt = time.Now()
		openSignal.Orders[0].IsClosing = true
		profit := (e.ExitPrice - openSignal.Orders[0].EntryPrice) * float32(openSignal.Orders[0].Quantity)
		profit = profit - (openSignal.Orders[0].ExitFee + openSignal.Orders[0].EntryFee)
		openSignal.Orders[0].Profit = profit

		err = s.Repository.Update(openSignal)
		if err != nil {
			return err
		}
		s.AccountUseCase.AddOrder(openSignal.Orders[0].InvestedAmount + profit)
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
