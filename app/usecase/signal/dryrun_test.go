package usecase_test

import (
	"testing"
	"time"

	"go-trade-bot/app/entities"
	usecase "go-trade-bot/app/usecase/signal"
	"go-trade-bot/app/usecase/signal/mocks"
	"go-trade-bot/internal/exchange"
	"go-trade-bot/internal/notifier"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// fix-01: a simulated (SIM-) position must never be closed with a real order
// (e.g. POST /api/signal/close/{id} on cmd/api, whose client is real).
func TestGenerateSellSignal_SimulatedPositionOnRealClient_RefusedWithoutExchangeCalls(t *testing.T) {
	signalUC, mockRepo, _, mockExchange, mockNotifier := newSignalUseCase()
	mockRepo.On("GetOpenSignals", "BTCUSDT", uint(1)).Return(entities.Signal{ID: 9, Status: entities.Open, Orders: []entities.Order{
		{BrokerOrderID: "SIM-1-1", StopLossOrderID: "SIM-STOP-1-2", Quantity: 1, EntryPrice: 100},
	}}, nil).Once()
	mockNotifier.On("Send", mock.Anything, eventOfType(notifier.EventStrategyError)).Return(nil).Once()

	err := signalUC.GenerateSellSignal(usecase.ExitSignal{Symbol: "BTCUSDT", StrategyID: 1})

	assert.ErrorContains(t, err, "simulated")
	mockExchange.AssertNotCalled(t, "PlaceOrder", mock.Anything, mock.Anything)
	mockExchange.AssertNotCalled(t, "CancelOrder", mock.Anything, mock.Anything, mock.Anything)
	mockRepo.AssertNotCalled(t, "Update", mock.Anything)
}

// fix-01: the dryrun usecase must never "close" a real position (its coins
// and real stop would stay on the exchange).
func TestGenerateSellSignal_RealPositionOnDryRunClient_Refused(t *testing.T) {
	mockRepo := new(mocks.SignalRepository)
	inner := new(mocks.ExchangeClient)
	dry := exchange.NewDryRunExchange(inner, 0, nil)
	signalUC := usecase.NewSignalUseCase(mockRepo, usecase.NewDryRunAccount(new(mocks.AccountUseCase)), dry, nil, nil, nil)
	mockRepo.On("GetOpenSignals", "BTCUSDT", uint(1)).Return(entities.Signal{ID: 9, Status: entities.Open, Orders: []entities.Order{
		{BrokerOrderID: "1001", Quantity: 1, EntryPrice: 100},
	}}, nil).Once()

	err := signalUC.GenerateSellSignal(usecase.ExitSignal{Symbol: "BTCUSDT", StrategyID: 1})

	assert.ErrorContains(t, err, "real")
	inner.AssertNotCalled(t, "PlaceOrder", mock.Anything, mock.Anything)
	inner.AssertNotCalled(t, "ListTickerPrices", mock.Anything, mock.Anything)
	mockRepo.AssertNotCalled(t, "Update", mock.Anything)
}

// Live regression: a real (non-SIM) position on a real client closes exactly
// as before, and the buy path now records Signal.Mode.
func TestGenerateBuySignal_RecordsMode(t *testing.T) {
	signalUC, mockRepo, mockAccountUseCase, mockExchange, mockNotifier := newSignalUseCase()
	mockAccountUseCase.On("CanOpenOrder").Return(true, nil).Once()
	mockAccountUseCase.On("GetDisponibleAmout").Return(float32(1000), nil).Once()
	mockAccountUseCase.On("DeductOrder", mock.Anything).Return(nil).Once()
	mockRepo.On("GetOpenSignals", "BTCUSDT", uint(1)).Return(entities.Signal{}, nil).Once()
	mockExchange.On("PlaceOrder", mock.Anything, mock.Anything).Return(exchange.OrderResult{
		BrokerOrderID: "1001", Status: exchange.OrderStatusFilled, ExecutedQty: 0.02, AvgFillPrice: 50000,
	}, nil).Once()
	mockRepo.On("Create", mock.MatchedBy(func(s entities.Signal) bool { return s.Mode == "live" })).Return(nil).Once()
	mockNotifier.On("Send", mock.Anything, mock.Anything).Return(nil)

	err := signalUC.GenerateBuySignal(usecase.EntrySignal{Symbol: "BTCUSDT", StrategyID: 1, Mode: "live", EntryPrice: 50000})
	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
}

func TestReconcile_SimulatedStopReportsSimulatedExitReason(t *testing.T) {
	mockRepo := new(mocks.SignalRepository)
	inner := new(mocks.ExchangeClient)
	notify := new(mocks.NotificationSender)
	mockAccount := new(mocks.AccountUseCase)
	mockAccount.On("AddOrder", mock.Anything).Return(nil).Once()
	dry := exchange.NewDryRunExchange(inner, 0, nil)
	signalUC := usecase.NewSignalUseCase(mockRepo, usecase.NewDryRunAccount(mockAccount), dry, notify, nil, nil)

	dry.TriggerStop("SIM-STOP-1-2", 1, 95, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	mockRepo.On("GetOpenSignals", "BTCUSDT", uint(1)).Return(entities.Signal{ID: 9, Status: entities.Open, Orders: []entities.Order{
		{BrokerOrderID: "SIM-1-1", StopLossOrderID: "SIM-STOP-1-2", Quantity: 1, EntryPrice: 100},
	}}, nil).Once()
	mockRepo.On("Update", mock.MatchedBy(func(s entities.Signal) bool {
		return s.Status == entities.Closed && s.Orders[0].ExitPrice == 95
	})).Return(nil).Once()
	notify.On("Send", mock.Anything, mock.MatchedBy(func(e notifier.Event) bool {
		return e.Type == notifier.EventPositionClosed && e.Data["exit_reason"] == usecase.ExitReasonSimulatedStopLoss
	})).Return(nil).Once()

	err := signalUC.GenerateSellSignal(usecase.ExitSignal{Symbol: "BTCUSDT", StrategyID: 1, Mode: "dryrun", ExitReason: usecase.ExitReasonSimulatedStopLoss})
	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
	notify.AssertExpectations(t)
	inner.AssertNotCalled(t, "PlaceOrder", mock.Anything, mock.Anything)
	inner.AssertNotCalled(t, "CancelOrder", mock.Anything, mock.Anything, mock.Anything)
}

func TestDryRunAccount_DelegatesToVirtualAccount(t *testing.T) {
	inner := new(mocks.AccountUseCase)
	inner.On("CanOpenOrder").Return(true, nil).Once()
	inner.On("GetDisponibleAmout").Return(float32(250), nil).Once()
	inner.On("DeductOrder", float32(100)).Return(nil).Once()
	inner.On("AddOrder", float32(100)).Return(nil).Once()
	acc := usecase.NewDryRunAccount(inner)

	ok, err := acc.CanOpenOrder()
	assert.NoError(t, err)
	assert.True(t, ok)
	amt, err := acc.GetDisponibleAmout()
	assert.NoError(t, err)
	assert.Equal(t, float32(250), amt)
	assert.NoError(t, acc.DeductOrder(100))
	assert.NoError(t, acc.AddOrder(100))

	inner.AssertExpectations(t)
}
