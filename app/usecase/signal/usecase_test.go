package usecase_test

import (
	"context"
	"errors"
	"fmt"
	usecase "go-trade-bot/app/usecase/signal"
	"testing"

	"go-trade-bot/app/entities"
	"go-trade-bot/app/usecase/signal/mocks"
	"go-trade-bot/internal/exchange"
	"go-trade-bot/internal/notifier"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func newSignalUseCase() (usecase.SignalUseCase, *mocks.SignalRepository, *mocks.AccountUseCase, *mocks.ExchangeClient, *mocks.NotificationSender) {
	mockRepo := new(mocks.SignalRepository)
	mockAccountUseCase := new(mocks.AccountUseCase)
	mockExchange := new(mocks.ExchangeClient)
	mockNotifier := new(mocks.NotificationSender)
	signalUC := usecase.NewSignalUseCase(mockRepo, mockAccountUseCase, mockExchange, mockNotifier, nil)
	return signalUC, mockRepo, mockAccountUseCase, mockExchange, mockNotifier
}

func eventOfType(t notifier.EventType) interface{} {
	return mock.MatchedBy(func(e notifier.Event) bool {
		return e.Type == t
	})
}

func TestSignalUseCase_GenerateBuySignal(t *testing.T) {
	t.Run("should place a buy order, submit a stop-loss and create the signal on fill", func(t *testing.T) {
		signalUC, mockRepo, mockAccountUseCase, mockExchange, mockNotifier := newSignalUseCase()

		entrySignal := usecase.EntrySignal{
			Symbol:      "BTCUSDT",
			StrategyID:  1,
			EntryPrice:  50000,
			MarginType:  entities.Isolated,
			StopLossPct: 2,
		}

		mockAccountUseCase.On("CanOpenOrder").Return(true, nil).Once()
		mockAccountUseCase.On("GetDisponibleAmout").Return(float32(1000), nil).Once()
		mockRepo.On("GetOpenSignals", entrySignal.Symbol, entrySignal.StrategyID).Return(entities.Signal{}, nil).Once()

		mockExchange.On("PlaceOrder", mock.Anything, mock.MatchedBy(func(r exchange.PlaceOrderRequest) bool {
			return r.Side == exchange.SideBuy && r.Type == exchange.OrderTypeMarket
		})).Return(exchange.OrderResult{
			BrokerOrderID: "1001",
			Status:        exchange.OrderStatusFilled,
			ExecutedQty:   0.02,
			AvgFillPrice:  50000,
		}, nil).Once()

		mockExchange.On("PlaceOrder", mock.Anything, mock.MatchedBy(func(r exchange.PlaceOrderRequest) bool {
			return r.Side == exchange.SideSell && r.Type == exchange.OrderTypeStopMarket
		})).Return(exchange.OrderResult{
			BrokerOrderID: "1002",
			Status:        exchange.OrderStatusNew,
		}, nil).Once()

		mockRepo.On("Create", mock.MatchedBy(func(s entities.Signal) bool {
			order := s.Orders[0]
			return order.BrokerOrderID == "1001" &&
				order.StopLossOrderID == "1002" &&
				order.EntryPrice == 50000 &&
				order.Quantity == 0.02 &&
				order.ExecutedQty == 0.02
		})).Return(nil).Once()

		mockAccountUseCase.On("DeductOrder", float32(0.02*50000)).Return(nil).Once()
		mockNotifier.On("Send", mock.Anything, eventOfType(notifier.EventPositionOpened)).Return(nil).Once()

		err := signalUC.GenerateBuySignal(entrySignal)
		assert.NoError(t, err)

		mockRepo.AssertExpectations(t)
		mockExchange.AssertExpectations(t)
	})

	t.Run("should not place an order if account cannot open one", func(t *testing.T) {
		signalUC, _, mockAccountUseCase, mockExchange, _ := newSignalUseCase()
		mockAccountUseCase.On("CanOpenOrder").Return(false, nil).Once()

		err := signalUC.GenerateBuySignal(usecase.EntrySignal{Symbol: "BTCUSDT", StrategyID: 1})
		assert.NoError(t, err)
		mockExchange.AssertNotCalled(t, "PlaceOrder", mock.Anything, mock.Anything)
	})

	t.Run("should not place an order if a signal already exists", func(t *testing.T) {
		signalUC, mockRepo, mockAccountUseCase, mockExchange, _ := newSignalUseCase()
		entrySignal := usecase.EntrySignal{Symbol: "BTCUSDT", StrategyID: 1, EntryPrice: 50000}
		existingSignal := entities.Signal{ID: 1, Symbol: entrySignal.Symbol, Status: entities.Open, StrategyID: entrySignal.StrategyID}

		mockAccountUseCase.On("CanOpenOrder").Return(true, nil).Once()
		mockRepo.On("GetOpenSignals", entrySignal.Symbol, entrySignal.StrategyID).Return(existingSignal, nil).Once()

		err := signalUC.GenerateBuySignal(entrySignal)
		assert.NoError(t, err)
		mockExchange.AssertNotCalled(t, "PlaceOrder", mock.Anything, mock.Anything)
	})

	t.Run("should return error if GetOpenSignals fails", func(t *testing.T) {
		signalUC, mockRepo, mockAccountUseCase, _, _ := newSignalUseCase()
		entrySignal := usecase.EntrySignal{Symbol: "BTCUSDT", StrategyID: 1, EntryPrice: 50000}

		mockAccountUseCase.On("CanOpenOrder").Return(true, nil).Once()
		mockRepo.On("GetOpenSignals", entrySignal.Symbol, entrySignal.StrategyID).Return(entities.Signal{}, errors.New("database error")).Once()

		err := signalUC.GenerateBuySignal(entrySignal)
		assert.Error(t, err)
		assert.Equal(t, "database error", err.Error())
	})

	t.Run("should not create a signal when the exchange rejects the order", func(t *testing.T) {
		signalUC, mockRepo, mockAccountUseCase, mockExchange, mockNotifier := newSignalUseCase()
		entrySignal := usecase.EntrySignal{Symbol: "BTCUSDT", StrategyID: 1, EntryPrice: 50000}

		mockAccountUseCase.On("CanOpenOrder").Return(true, nil).Once()
		mockAccountUseCase.On("GetDisponibleAmout").Return(float32(1000), nil).Once()
		mockRepo.On("GetOpenSignals", entrySignal.Symbol, entrySignal.StrategyID).Return(entities.Signal{}, nil).Once()
		mockExchange.On("PlaceOrder", mock.Anything, mock.Anything).Return(exchange.OrderResult{}, exchange.ErrInsufficientBalance).Once()
		mockNotifier.On("Send", mock.Anything, eventOfType(notifier.EventStrategyError)).Return(nil).Once()

		err := signalUC.GenerateBuySignal(entrySignal)
		assert.Error(t, err)
		mockRepo.AssertNotCalled(t, "Create", mock.Anything)
		mockAccountUseCase.AssertNotCalled(t, "DeductOrder", mock.Anything)
	})

	t.Run("should not create a signal when executed quantity is zero (zero-balance race)", func(t *testing.T) {
		signalUC, mockRepo, mockAccountUseCase, mockExchange, mockNotifier := newSignalUseCase()
		entrySignal := usecase.EntrySignal{Symbol: "BTCUSDT", StrategyID: 1, EntryPrice: 50000}

		mockAccountUseCase.On("CanOpenOrder").Return(true, nil).Once()
		mockAccountUseCase.On("GetDisponibleAmout").Return(float32(1000), nil).Once()
		mockRepo.On("GetOpenSignals", entrySignal.Symbol, entrySignal.StrategyID).Return(entities.Signal{}, nil).Once()
		mockExchange.On("PlaceOrder", mock.Anything, mock.Anything).Return(exchange.OrderResult{
			Status:      exchange.OrderStatusRejected,
			ExecutedQty: 0,
		}, nil).Once()
		mockNotifier.On("Send", mock.Anything, eventOfType(notifier.EventStrategyError)).Return(nil).Once()

		err := signalUC.GenerateBuySignal(entrySignal)
		assert.Error(t, err)
		mockRepo.AssertNotCalled(t, "Create", mock.Anything)
	})

	t.Run("should size the created order to the actual filled quantity on a partial fill", func(t *testing.T) {
		signalUC, mockRepo, mockAccountUseCase, mockExchange, mockNotifier := newSignalUseCase()
		entrySignal := usecase.EntrySignal{Symbol: "BTCUSDT", StrategyID: 1, EntryPrice: 50000}

		mockAccountUseCase.On("CanOpenOrder").Return(true, nil).Once()
		mockAccountUseCase.On("GetDisponibleAmout").Return(float32(1000), nil).Once()
		mockRepo.On("GetOpenSignals", entrySignal.Symbol, entrySignal.StrategyID).Return(entities.Signal{}, nil).Once()

		mockExchange.On("PlaceOrder", mock.Anything, mock.MatchedBy(func(r exchange.PlaceOrderRequest) bool {
			return r.Side == exchange.SideBuy
		})).Return(exchange.OrderResult{
			BrokerOrderID: "2001",
			Status:        exchange.OrderStatusPartiallyFilled,
			ExecutedQty:   0.012, // 60% of the requested 0.02
			AvgFillPrice:  50000,
		}, nil).Once()

		mockRepo.On("Create", mock.MatchedBy(func(s entities.Signal) bool {
			return s.Orders[0].Quantity == float32(0.012)
		})).Return(nil).Once()
		mockAccountUseCase.On("DeductOrder", float32(0.012*50000)).Return(nil).Once()
		mockNotifier.On("Send", mock.Anything, mock.Anything).Return(nil)

		err := signalUC.GenerateBuySignal(entrySignal)
		assert.NoError(t, err)
		mockRepo.AssertExpectations(t)
	})

	t.Run("should not submit a stop-loss when StopLossPct is not configured", func(t *testing.T) {
		signalUC, mockRepo, mockAccountUseCase, mockExchange, mockNotifier := newSignalUseCase()
		entrySignal := usecase.EntrySignal{Symbol: "BTCUSDT", StrategyID: 1, EntryPrice: 50000, StopLossPct: 0}

		mockAccountUseCase.On("CanOpenOrder").Return(true, nil).Once()
		mockAccountUseCase.On("GetDisponibleAmout").Return(float32(1000), nil).Once()
		mockRepo.On("GetOpenSignals", entrySignal.Symbol, entrySignal.StrategyID).Return(entities.Signal{}, nil).Once()
		mockExchange.On("PlaceOrder", mock.Anything, mock.Anything).Return(exchange.OrderResult{
			BrokerOrderID: "3001", Status: exchange.OrderStatusFilled, ExecutedQty: 0.02, AvgFillPrice: 50000,
		}, nil).Once() // exactly one call - no stop-loss attempt
		mockRepo.On("Create", mock.MatchedBy(func(s entities.Signal) bool {
			return s.Orders[0].StopLossOrderID == ""
		})).Return(nil).Once()
		mockAccountUseCase.On("DeductOrder", mock.Anything).Return(nil).Once()
		mockNotifier.On("Send", mock.Anything, mock.Anything).Return(nil)

		err := signalUC.GenerateBuySignal(entrySignal)
		assert.NoError(t, err)
		mockExchange.AssertNumberOfCalls(t, "PlaceOrder", 1)
	})

	t.Run("should leave the position open and unprotected after the stop-loss retry also fails", func(t *testing.T) {
		signalUC, mockRepo, mockAccountUseCase, mockExchange, mockNotifier := newSignalUseCase()
		entrySignal := usecase.EntrySignal{Symbol: "BTCUSDT", StrategyID: 1, EntryPrice: 50000, StopLossPct: 2}

		mockAccountUseCase.On("CanOpenOrder").Return(true, nil).Once()
		mockAccountUseCase.On("GetDisponibleAmout").Return(float32(1000), nil).Once()
		mockRepo.On("GetOpenSignals", entrySignal.Symbol, entrySignal.StrategyID).Return(entities.Signal{}, nil).Once()

		mockExchange.On("PlaceOrder", mock.Anything, mock.MatchedBy(func(r exchange.PlaceOrderRequest) bool {
			return r.Side == exchange.SideBuy
		})).Return(exchange.OrderResult{
			BrokerOrderID: "4001", Status: exchange.OrderStatusFilled, ExecutedQty: 0.02, AvgFillPrice: 50000,
		}, nil).Once()

		mockExchange.On("PlaceOrder", mock.Anything, mock.MatchedBy(func(r exchange.PlaceOrderRequest) bool {
			return r.Type == exchange.OrderTypeStopMarket
		})).Return(exchange.OrderResult{}, exchange.ErrOrderRejected).Twice() // first attempt + one retry

		mockRepo.On("Create", mock.MatchedBy(func(s entities.Signal) bool {
			return s.Orders[0].StopLossOrderID == "" // unprotected, but still created
		})).Return(nil).Once()
		mockAccountUseCase.On("DeductOrder", mock.Anything).Return(nil).Once()
		mockNotifier.On("Send", mock.Anything, eventOfType(notifier.EventStrategyError)).Return(nil).Once()
		mockNotifier.On("Send", mock.Anything, eventOfType(notifier.EventPositionOpened)).Return(nil).Once()

		err := signalUC.GenerateBuySignal(entrySignal)
		assert.NoError(t, err) // buy itself succeeded - not rolled back
		mockExchange.AssertNumberOfCalls(t, "PlaceOrder", 3)
		mockRepo.AssertExpectations(t)
	})
}

func TestSignalUseCase_GenerateSellSignal(t *testing.T) {
	t.Run("should cancel the resting stop and place a market sell, closing the signal", func(t *testing.T) {
		signalUC, mockRepo, mockAccountUseCase, mockExchange, mockNotifier := newSignalUseCase()

		exitSignal := usecase.ExitSignal{Symbol: "BTCUSDT", StrategyID: 1, ExitPrice: 60000, ExitReason: "take_profit"}
		openSignal := entities.Signal{
			ID: 1, Symbol: exitSignal.Symbol, Status: entities.Open, StrategyID: exitSignal.StrategyID,
			Orders: []entities.Order{{
				EntryPrice: 50000, Quantity: 0.02, MarginType: entities.Isolated,
				EntryFee: 0.1, StopLossOrderID: "9001", InvestedAmount: 1000,
			}},
		}

		mockRepo.On("GetOpenSignals", exitSignal.Symbol, exitSignal.StrategyID).Return(openSignal, nil).Once()
		mockExchange.On("CancelOrder", mock.Anything, exitSignal.Symbol, "9001").Return(nil).Once()
		mockExchange.On("PlaceOrder", mock.Anything, mock.MatchedBy(func(r exchange.PlaceOrderRequest) bool {
			return r.Side == exchange.SideSell && r.Type == exchange.OrderTypeMarket
		})).Return(exchange.OrderResult{
			BrokerOrderID: "9002", Status: exchange.OrderStatusFilled, ExecutedQty: 0.02, AvgFillPrice: 60000,
		}, nil).Once()
		mockRepo.On("Update", mock.MatchedBy(func(s entities.Signal) bool {
			return s.Status == entities.Closed && s.Orders[0].ExitPrice == 60000
		})).Return(nil).Once()
		mockAccountUseCase.On("AddOrder", mock.Anything).Return(nil).Once()
		mockNotifier.On("Send", mock.Anything, eventOfType(notifier.EventPositionClosed)).Return(nil).Once()

		err := signalUC.GenerateSellSignal(exitSignal)
		assert.NoError(t, err)
		mockExchange.AssertExpectations(t)
		mockRepo.AssertExpectations(t)
	})

	t.Run("should reconcile as closed when the stop already triggered on the exchange", func(t *testing.T) {
		signalUC, mockRepo, mockAccountUseCase, mockExchange, mockNotifier := newSignalUseCase()

		exitSignal := usecase.ExitSignal{Symbol: "ETHUSDT", StrategyID: 2}
		openSignal := entities.Signal{
			ID: 5, Symbol: exitSignal.Symbol, Status: entities.Open, StrategyID: exitSignal.StrategyID,
			Orders: []entities.Order{{
				EntryPrice: 3000, Quantity: 1, StopLossOrderID: "7001", InvestedAmount: 3000,
			}},
		}

		mockRepo.On("GetOpenSignals", exitSignal.Symbol, exitSignal.StrategyID).Return(openSignal, nil).Once()
		mockExchange.On("CancelOrder", mock.Anything, exitSignal.Symbol, "7001").
			Return(wrappedOrderNotFoundErr()).Once()
		mockExchange.On("GetOrder", mock.Anything, exitSignal.Symbol, "7001").Return(exchange.OrderResult{
			Status: exchange.OrderStatusFilled, ExecutedQty: 1, AvgFillPrice: 2940,
		}, nil).Once()
		mockRepo.On("Update", mock.MatchedBy(func(s entities.Signal) bool {
			return s.Status == entities.Closed && s.Orders[0].ExitPrice == 2940
		})).Return(nil).Once()
		mockAccountUseCase.On("AddOrder", mock.Anything).Return(nil).Once()
		mockNotifier.On("Send", mock.Anything, eventOfType(notifier.EventPositionClosed)).Return(nil).Once()

		err := signalUC.GenerateSellSignal(exitSignal)
		assert.NoError(t, err)
		mockExchange.AssertNotCalled(t, "PlaceOrder", mock.Anything, mock.Anything)
	})

	t.Run("should not sell when cancel fails for a reason other than order-not-found", func(t *testing.T) {
		signalUC, mockRepo, _, mockExchange, mockNotifier := newSignalUseCase()

		exitSignal := usecase.ExitSignal{Symbol: "BTCUSDT", StrategyID: 1}
		openSignal := entities.Signal{
			ID: 1, Symbol: exitSignal.Symbol, Status: entities.Open, StrategyID: exitSignal.StrategyID,
			Orders: []entities.Order{{StopLossOrderID: "9001", Quantity: 0.02, EntryPrice: 50000}},
		}

		mockRepo.On("GetOpenSignals", exitSignal.Symbol, exitSignal.StrategyID).Return(openSignal, nil).Once()
		mockExchange.On("CancelOrder", mock.Anything, exitSignal.Symbol, "9001").Return(errors.New("connection reset")).Once()
		mockNotifier.On("Send", mock.Anything, eventOfType(notifier.EventStrategyError)).Return(nil).Once()

		err := signalUC.GenerateSellSignal(exitSignal)
		assert.Error(t, err)
		mockExchange.AssertNotCalled(t, "PlaceOrder", mock.Anything, mock.Anything)
		mockRepo.AssertNotCalled(t, "Update", mock.Anything)
	})

	t.Run("should keep the signal open when the sell order fails", func(t *testing.T) {
		signalUC, mockRepo, _, mockExchange, mockNotifier := newSignalUseCase()

		exitSignal := usecase.ExitSignal{Symbol: "BTCUSDT", StrategyID: 1}
		openSignal := entities.Signal{
			ID: 1, Symbol: exitSignal.Symbol, Status: entities.Open, StrategyID: exitSignal.StrategyID,
			Orders: []entities.Order{{Quantity: 0.02, EntryPrice: 50000}},
		}

		mockRepo.On("GetOpenSignals", exitSignal.Symbol, exitSignal.StrategyID).Return(openSignal, nil).Once()
		mockExchange.On("PlaceOrder", mock.Anything, mock.Anything).Return(exchange.OrderResult{}, errors.New("timeout")).Once()
		mockNotifier.On("Send", mock.Anything, eventOfType(notifier.EventStrategyError)).Return(nil).Once()

		err := signalUC.GenerateSellSignal(exitSignal)
		assert.Error(t, err)
		mockRepo.AssertNotCalled(t, "Update", mock.Anything)
	})

	t.Run("should return error if GetOpenSignals fails", func(t *testing.T) {
		signalUC, mockRepo, _, _, _ := newSignalUseCase()
		exitSignal := usecase.ExitSignal{Symbol: "BTCUSDT", StrategyID: 1}

		mockRepo.On("GetOpenSignals", exitSignal.Symbol, exitSignal.StrategyID).Return(entities.Signal{}, errors.New("database error")).Once()

		err := signalUC.GenerateSellSignal(exitSignal)
		assert.Error(t, err)
		assert.Equal(t, "database error", err.Error())
	})

	t.Run("should return error if signal not found", func(t *testing.T) {
		signalUC, mockRepo, _, _, _ := newSignalUseCase()
		exitSignal := usecase.ExitSignal{Symbol: "BTCUSDT", StrategyID: 1}

		mockRepo.On("GetOpenSignals", exitSignal.Symbol, exitSignal.StrategyID).Return(entities.Signal{}, nil).Once()

		err := signalUC.GenerateSellSignal(exitSignal)
		assert.Error(t, err)
		assert.Equal(t, "signal not found for symbol BTCUSDT and strategy ID 1", err.Error())
	})

	t.Run("should return error if Update fails", func(t *testing.T) {
		signalUC, mockRepo, _, mockExchange, mockNotifier := newSignalUseCase()
		exitSignal := usecase.ExitSignal{Symbol: "BTCUSDT", StrategyID: 1}
		openSignal := entities.Signal{
			ID: 1, Symbol: exitSignal.Symbol, Status: entities.Open, StrategyID: exitSignal.StrategyID,
			Orders: []entities.Order{{Quantity: 0.02, EntryPrice: 50000, InvestedAmount: 1000}},
		}

		mockRepo.On("GetOpenSignals", exitSignal.Symbol, exitSignal.StrategyID).Return(openSignal, nil).Once()
		mockExchange.On("PlaceOrder", mock.Anything, mock.Anything).Return(exchange.OrderResult{
			Status: exchange.OrderStatusFilled, ExecutedQty: 0.02, AvgFillPrice: 60000,
		}, nil).Once()
		mockRepo.On("Update", mock.Anything).Return(errors.New("database error")).Once()
		mockNotifier.On("Send", mock.Anything, mock.Anything).Return(nil)

		err := signalUC.GenerateSellSignal(exitSignal)
		assert.Error(t, err)
		assert.Equal(t, "database error", err.Error())
	})
}

func TestSignalUseCase_Close(t *testing.T) {
	t.Run("should close a signal successfully", func(t *testing.T) {
		signalUC, mockRepo, mockAccountUseCase, mockExchange, mockNotifier := newSignalUseCase()

		signalID := uint(1)
		openSignal := entities.Signal{
			ID: signalID, Status: entities.Open, Symbol: "BTCUSDT",
			Orders: []entities.Order{{Quantity: 0.02, EntryPrice: 50000, InvestedAmount: 1000}},
		}
		mockRepo.On("GetByID", signalID).Return(openSignal, nil).Once()
		mockExchange.On("ListTickerPrices", mock.Anything, "BTCUSDT").Return([]exchange.TickerPrice{
			{Symbol: "BTCUSDT", Price: 60000},
		}, nil).Once()
		mockRepo.On("GetOpenSignals", openSignal.Symbol, openSignal.StrategyID).Return(openSignal, nil).Once()
		mockExchange.On("PlaceOrder", mock.Anything, mock.Anything).Return(exchange.OrderResult{
			Status: exchange.OrderStatusFilled, ExecutedQty: 0.02, AvgFillPrice: 60000,
		}, nil).Once()
		mockRepo.On("Update", mock.Anything).Return(nil).Once()
		mockAccountUseCase.On("AddOrder", mock.Anything).Return(nil).Once()
		mockNotifier.On("Send", mock.Anything, mock.Anything).Return(nil)

		err := signalUC.Close(context.TODO(), signalID)
		assert.NoError(t, err)
	})

	t.Run("should return error if GetByID fails", func(t *testing.T) {
		signalUC, mockRepo, _, _, _ := newSignalUseCase()
		signalID := uint(2)
		mockRepo.On("GetByID", signalID).Return(entities.Signal{}, errors.New("db error")).Once()

		err := signalUC.Close(context.TODO(), signalID)
		assert.Error(t, err)
		assert.Equal(t, "db error", err.Error())
	})

	t.Run("should return error if signal is not open", func(t *testing.T) {
		signalUC, mockRepo, _, _, _ := newSignalUseCase()
		signalID := uint(3)
		closedSignal := entities.Signal{ID: signalID, Status: entities.Closed, Symbol: "BTCUSDT"}
		mockRepo.On("GetByID", signalID).Return(closedSignal, nil).Once()

		err := signalUC.Close(context.TODO(), signalID)
		assert.Error(t, err)
		assert.Equal(t, "Signal is already closed", err.Error())
	})

	t.Run("should return error if ListTickerPrices fails", func(t *testing.T) {
		signalUC, mockRepo, _, mockExchange, _ := newSignalUseCase()
		signalID := uint(6)
		openSignal := entities.Signal{ID: signalID, Status: entities.Open, Symbol: "BTCUSDT"}
		mockRepo.On("GetByID", signalID).Return(openSignal, nil).Once()
		mockExchange.On("ListTickerPrices", mock.Anything, mock.Anything).Return(nil, errors.New("broker error")).Once()

		err := signalUC.Close(context.TODO(), signalID)
		assert.Error(t, err)
		assert.Equal(t, "broker error", err.Error())
	})
}

func wrappedOrderNotFoundErr() error {
	return fmt.Errorf("%w: Unknown order sent.", exchange.ErrOrderNotFound)
}
