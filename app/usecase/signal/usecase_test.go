package usecase_test

import (
	"context"
	"errors"
	usecase "go-trade-bot/app/usecase/signal"
	"testing"
	"time"

	"go-trade-bot/app/entities"
	"go-trade-bot/app/usecase/signal/mocks"

	"github.com/adshao/go-binance/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestSignalUseCase_GenerateBuySignal(t *testing.T) {
	mockRepo := new(mocks.SignalRepository)
	mockAccountUseCase := new(mocks.AccountUseCase)
	mockBroker := new(mocks.Broker)
	signalUC := usecase.NewSignalUseCase(mockRepo, mockAccountUseCase, mockBroker)

	t.Run("should create a buy signal", func(t *testing.T) {
		entrySignal := usecase.EntrySignal{
			Symbol:     "BTCUSDT",
			StrategyID: 1,
			EntryPrice: 50000,
			MarginType: entities.Isolated,
		}
		mockAccountUseCase.On("CanOpenOrder").Return(true, nil).Once()
		mockAccountUseCase.On("GetDisponibleAmout").Return(float32(1000), nil).Once()
		mockAccountUseCase.On("DeductOrder", float32(1000)).Return(nil).Once()
		mockRepo.On("GetOpenSignals", entrySignal.Symbol, entrySignal.StrategyID).Return(entities.Signal{}, nil).Once()
		mockRepo.On("Create", mock.Anything).Return(nil).Once()

		err := signalUC.GenerateBuySignal(entrySignal)
		assert.NoError(t, err)

		mockRepo.AssertExpectations(t)
	})

	t.Run("should not create a buy signal if one already exists", func(t *testing.T) {
		entrySignal := usecase.EntrySignal{
			Symbol:     "BTCUSDT",
			StrategyID: 1,
			EntryPrice: 50000,
			MarginType: entities.Isolated,
		}

		existingSignal := entities.Signal{
			ID:         1,
			Symbol:     entrySignal.Symbol,
			Status:     entities.Open,
			StrategyID: entrySignal.StrategyID,
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		}
		mockAccountUseCase.On("CanOpenOrder").Return(true, nil).Once()
		mockRepo.On("GetOpenSignals", entrySignal.Symbol, entrySignal.StrategyID).Return(existingSignal, nil).Once()

		err := signalUC.GenerateBuySignal(entrySignal)
		assert.NoError(t, err)

		mockRepo.AssertExpectations(t)
	})

	t.Run("should return error if GetOpenSignals fails", func(t *testing.T) {
		entrySignal := usecase.EntrySignal{
			Symbol:     "BTCUSDT",
			StrategyID: 1,
			EntryPrice: 50000,
			MarginType: entities.Isolated,
		}
		mockAccountUseCase.On("CanOpenOrder").Return(true, nil).Once()
		mockRepo.On("GetOpenSignals", entrySignal.Symbol, entrySignal.StrategyID).Return(entities.Signal{}, errors.New("database error")).Once()
		err := signalUC.GenerateBuySignal(entrySignal)
		assert.Error(t, err)
		assert.Equal(t, "database error", err.Error())
		mockRepo.AssertExpectations(t)
	})
	t.Run("should return error if Create fails", func(t *testing.T) {
		entrySignal := usecase.EntrySignal{
			Symbol:     "BTCUSDT",
			StrategyID: 1,
			EntryPrice: 50000,
			MarginType: entities.Isolated,
		}
		mockAccountUseCase.On("CanOpenOrder").Return(true, nil).Once()
		mockAccountUseCase.On("GetDisponibleAmout").Return(float32(1000), nil).Once()
		mockRepo.On("GetOpenSignals", entrySignal.Symbol, entrySignal.StrategyID).Return(entities.Signal{}, nil).Once()
		mockRepo.On("Create", mock.Anything).Return(errors.New("database error")).Once()

		err := signalUC.GenerateBuySignal(entrySignal)
		assert.Error(t, err)
		assert.Equal(t, "database error", err.Error())

		mockRepo.AssertExpectations(t)
	})
	t.Run("should create a buy signal with default leverage", func(t *testing.T) {
		entrySignal := usecase.EntrySignal{
			Symbol:     "BTCUSDT",
			StrategyID: 1,
			EntryPrice: 50000,
			MarginType: entities.Isolated,
		}
		mockAccountUseCase.On("CanOpenOrder").Return(true, nil).Once()
		mockAccountUseCase.On("GetDisponibleAmout").Return(float32(1000), nil).Once()
		mockAccountUseCase.On("DeductOrder", float32(1000)).Return(nil).Once()
		mockRepo.On("GetOpenSignals", entrySignal.Symbol, entrySignal.StrategyID).Return(entities.Signal{}, nil).Once()
		mockRepo.On("Create", mock.Anything).Return(nil).Once()

		err := signalUC.GenerateBuySignal(entrySignal)
		assert.NoError(t, err)

		mockRepo.AssertExpectations(t)
	})
	t.Run("should create a buy signal with default leverage and margin type", func(t *testing.T) {
		entrySignal := usecase.EntrySignal{
			Symbol:     "BTCUSDT",
			StrategyID: 1,
			EntryPrice: 50000,
		}
		mockAccountUseCase.On("CanOpenOrder").Return(true, nil).Once()
		mockAccountUseCase.On("GetDisponibleAmout").Return(float32(1000), nil).Once()
		mockAccountUseCase.On("DeductOrder", float32(1000)).Return(nil).Once()
		mockRepo.On("GetOpenSignals", entrySignal.Symbol, entrySignal.StrategyID).Return(entities.Signal{}, nil).Once()
		mockRepo.On("Create", mock.Anything).Return(nil).Once()

		err := signalUC.GenerateBuySignal(entrySignal)
		assert.NoError(t, err)

		mockRepo.AssertExpectations(t)
	})
}

func TestSignalUseCase_GenerateSellSignal(t *testing.T) {
	mockRepo := new(mocks.SignalRepository)
	mockAccountUseCase := new(mocks.AccountUseCase)
	mockBroker := new(mocks.Broker)
	signalUC := usecase.NewSignalUseCase(mockRepo, mockAccountUseCase, mockBroker)

	t.Run("should create a sell signal", func(t *testing.T) {
		exitSignal := usecase.ExitSignal{
			Symbol:     "BTCUSDT",
			StrategyID: 1,
			ExitPrice:  60000,
		}

		openSignal := entities.Signal{
			ID:         1,
			Symbol:     exitSignal.Symbol,
			Status:     entities.Open,
			StrategyID: exitSignal.StrategyID,
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
			Orders: []entities.Order{
				{
					EntryPrice:  50000,
					ExitPrice:   50001,
					Quantity:    0.02,
					MarginType:  entities.Isolated,
					EntryFee:    0.1,
					ExitFee:     0,
					Leverage:    1,
					ExecutedQty: 0,
					IsClosing:   false,
					CreatedAt:   time.Now(),
					UpdatedAt:   time.Now(),
				},
			},
		}
		mockAccountUseCase.On("AddOrder", float32(198.7)).Return(nil).Once()
		mockRepo.On("GetOpenSignals", exitSignal.Symbol, exitSignal.StrategyID).Return(openSignal, nil).Once()
		mockRepo.On("Update", mock.Anything).Return(nil).Once()

		err := signalUC.GenerateSellSignal(exitSignal)
		assert.NoError(t, err)

		mockRepo.AssertExpectations(t)
	})

	t.Run("should return error if GetOpenSignals fails", func(t *testing.T) {
		exitSignal := usecase.ExitSignal{
			Symbol:     "BTCUSDT",
			StrategyID: 1,
			ExitPrice:  60000,
		}

		mockRepo.On("GetOpenSignals", exitSignal.Symbol, exitSignal.StrategyID).Return(entities.Signal{}, errors.New("database error")).Once()

		err := signalUC.GenerateSellSignal(exitSignal)
		assert.Error(t, err)
		assert.Equal(t, "database error", err.Error())

		mockRepo.AssertExpectations(t)
	})

	t.Run("should return error if Update fails", func(t *testing.T) {
		exitSignal := usecase.ExitSignal{
			Symbol:     "BTCUSDT",
			StrategyID: 1,
			ExitPrice:  60000,
		}

		openSignal := entities.Signal{
			ID:         1,
			Symbol:     exitSignal.Symbol,
			Status:     entities.Open,
			StrategyID: exitSignal.StrategyID,
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
			Orders: []entities.Order{
				{
					EntryPrice:     50000,
					ExitPrice:      0,
					Quantity:       0.02,
					InvestedAmount: 1000,
					MarginType:     entities.Isolated,
					EntryFee:       0.1,
					ExitFee:        0,
					Leverage:       1,
					ExecutedQty:    0,
					IsClosing:      false,
					CreatedAt:      time.Now(),
					UpdatedAt:      time.Now(),
				},
			},
		}

		mockRepo.On("GetOpenSignals", exitSignal.Symbol, exitSignal.StrategyID).Return(openSignal, nil).Once()
		mockRepo.On("Update", mock.Anything).
			Return(errors.New("database error")).Once()
		err := signalUC.GenerateSellSignal(exitSignal)
		assert.Error(t, err)
		assert.Equal(t, "database error", err.Error())
		mockRepo.AssertExpectations(t)
	})
	t.Run("should return error if signal not found", func(t *testing.T) {
		exitSignal := usecase.ExitSignal{
			Symbol:     "BTCUSDT",
			StrategyID: 1,
			ExitPrice:  60000,
		}

		mockRepo.On("GetOpenSignals", exitSignal.Symbol, exitSignal.StrategyID).Return(entities.Signal{}, nil).Once()

		err := signalUC.GenerateSellSignal(exitSignal)
		assert.Error(t, err)
		assert.Equal(t, "signal not found for symbol BTCUSDT and strategy ID 1", err.Error())

		mockRepo.AssertExpectations(t)
	})
}
func TestSignalUseCase_Close(t *testing.T) {
	mockRepo := new(mocks.SignalRepository)
	mockAccountUseCase := new(mocks.AccountUseCase)
	mockBroker := new(mocks.Broker)
	signalUC := usecase.NewSignalUseCase(mockRepo, mockAccountUseCase, mockBroker)

	t.Run("should close a signal successfully", func(t *testing.T) {
		signalID := uint(1)
		openSignal := entities.Signal{
			ID:     signalID,
			Status: entities.Open,
			Symbol: "BTCUSDT",
			Orders: []entities.Order{
				{
					InvestedAmount: 1000,
				},
			},
		}
		mockRepo.On("GetByID", signalID).Return(openSignal, nil).Once()
		mockBroker.On("ListTickerPrices", mock.Anything, mock.Anything).Return([]*binance.SymbolPrice{
			{Symbol: "BTCUSDT", Price: "60000"},
		}, nil).Once()
		mockRepo.On("Update", mock.Anything).Return(nil).Once()
		mockRepo.On("GetOpenSignals", openSignal.Symbol, openSignal.StrategyID).Return(openSignal, nil).Once()
		mockAccountUseCase.On("AddOrder", float32(1000)).Return(nil).Once()

		err := signalUC.Close(context.TODO(), signalID)
		assert.NoError(t, err)
		mockRepo.AssertExpectations(t)
		mockAccountUseCase.AssertExpectations(t)
		mockBroker.AssertExpectations(t)
	})

	t.Run("should return error if GetByID fails", func(t *testing.T) {
		signalID := uint(2)
		mockRepo.On("GetByID", signalID).Return(entities.Signal{}, errors.New("db error")).Once()
		err := signalUC.Close(context.TODO(), signalID)
		assert.Error(t, err)
		assert.Equal(t, "db error", err.Error())
		mockRepo.AssertExpectations(t)
	})

	t.Run("should return error if signal is not open", func(t *testing.T) {
		signalID := uint(3)
		closedSignal := entities.Signal{
			ID:     signalID,
			Status: entities.Closed,
			Symbol: "BTCUSDT",
		}
		mockRepo.On("GetByID", signalID).Return(closedSignal, nil).Once()

		err := signalUC.Close(context.TODO(), signalID)
		assert.Error(t, err)
		assert.Equal(t, "Signal is already closed", err.Error())
		mockRepo.AssertExpectations(t)
	})

	t.Run("should return error if Update fails", func(t *testing.T) {
		signalID := uint(4)
		openSignal := entities.Signal{
			ID:     signalID,
			Status: entities.Open,
			Symbol: "BTCUSDT",
			Orders: []entities.Order{
				{
					InvestedAmount: 1000,
				},
			},
		}
		mockRepo.On("GetByID", signalID).Return(openSignal, nil).Once()
		mockBroker.On("ListTickerPrices", mock.Anything, mock.Anything).Return([]*binance.SymbolPrice{
			{Symbol: "BTCUSDT", Price: "60000"},
		}, nil).Once()
		mockRepo.On("Update", mock.Anything).Return(errors.New("update error")).Once()
		mockRepo.On("GetOpenSignals", openSignal.Symbol, openSignal.StrategyID).Return(openSignal, nil).Once()

		err := signalUC.Close(context.TODO(), signalID)
		assert.Error(t, err)
		assert.Equal(t, "update error", err.Error())
		mockRepo.AssertExpectations(t)
		mockAccountUseCase.AssertExpectations(t)
	})

	t.Run("should return error if AddOrder fails", func(t *testing.T) {
		signalID := uint(5)
		openSignal := entities.Signal{
			ID:     signalID,
			Status: entities.Open,
			Symbol: "BTCUSDT",
			Orders: []entities.Order{
				{
					InvestedAmount: 1000,
				},
			},
		}
		mockRepo.On("GetByID", signalID).Return(openSignal, nil).Once()
		mockBroker.On("ListTickerPrices", mock.Anything, mock.Anything).Return([]*binance.SymbolPrice{
			{Symbol: "BTCUSDT", Price: "60000"},
		}, nil).Once()
		mockAccountUseCase.On("AddOrder", float32(1000)).Return(errors.New("account error")).Once()
		mockRepo.On("GetOpenSignals", openSignal.Symbol, openSignal.StrategyID).Return(openSignal, nil).Once()
		mockRepo.On("Update", mock.Anything).Return(nil).Once()

		err := signalUC.Close(context.TODO(), signalID)
		assert.Error(t, err)
		assert.Equal(t, "account error", err.Error())
		mockRepo.AssertExpectations(t)
		mockAccountUseCase.AssertExpectations(t)
	})

	t.Run("should return error if ListTickerPrice fails", func(t *testing.T) {
		signalID := uint(6)
		openSignal := entities.Signal{
			ID:     signalID,
			Status: entities.Open,
			Symbol: "BTCUSDT",
			Orders: []entities.Order{
				{
					InvestedAmount: 1000,
				},
			},
		}
		mockRepo.On("GetByID", signalID).Return(openSignal, nil).Once()
		mockBroker.On("ListTickerPrices", mock.Anything, mock.Anything).Return(nil, errors.New("broker error")).Once()

		err := signalUC.Close(context.TODO(), signalID)
		assert.Error(t, err)
		assert.Equal(t, "broker error", err.Error())
		mockRepo.AssertExpectations(t)
		mockBroker.AssertExpectations(t)
	})
}
