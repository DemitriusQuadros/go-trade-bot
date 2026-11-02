package usecase

import (
	"context"
	"testing"
	"time"

	"go-trade-bot/app/entities"
	"go-trade-bot/app/usecase/signal/mocks"
	"go-trade-bot/internal/exchange"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func newFuturesTestSignalUseCase() (SignalUseCase, *mocks.SignalRepository, *mocks.AccountUseCase, *mocks.ExchangeClient, *mocks.NotificationSender) {
	mockRepo := new(mocks.SignalRepository)
	mockAccountUseCase := new(mocks.AccountUseCase)
	mockExchange := new(mocks.ExchangeClient)
	mockNotifier := new(mocks.NotificationSender)
	signalUC := NewSignalUseCase(mockRepo, mockAccountUseCase, mockExchange, mockNotifier, nil, nil)
	return signalUC, mockRepo, mockAccountUseCase, mockExchange, mockNotifier
}

func TestLeveragedPositionSizing_PctCapital_Long(t *testing.T) {
	signalUC, mockRepo, mockAccountUseCase, mockExchange, mockNotifier := newFuturesTestSignalUseCase()
	mockNotifier.On("Send", mock.Anything, mock.Anything).Return(nil).Maybe()

	entrySignal := EntrySignal{
		Symbol:       "BTCUSDT",
		StrategyID:   1,
		StrategyName: "test-strat",
		EntryPrice:   100.0,
		PositionSide: entities.PositionSideLong,
		Leverage:     10.0,
		MarketType:   "futures",
		MarginType:   entities.Isolated,
		PositionSizing: &PositionSizingConfig{
			Type:  SizingPctCapital,
			Value: 10.0, // 10% of $1,000 capital = $100 margin
		},
	}

	mockAccountUseCase.On("CanOpenOrder").Return(true, nil).Once()
	mockAccountUseCase.On("GetDisponibleAmout").Return(float32(1000.0), nil).Once()
	mockAccountUseCase.On("GetAccount").Return(entities.Account{Amount: 1000.0}, nil).Maybe()
	mockRepo.On("GetOpenSignals", entrySignal.Symbol, entrySignal.StrategyID).Return(entities.Signal{}, nil).Once()

	// 10% of $1,000 = $100 initial margin. Leveraged notional = $100 * 10 = $1,000.
	// At price $100, quantity = $1,000 / $100 = 10 units.
	mockExchange.On("PlaceOrder", mock.Anything, mock.MatchedBy(func(r exchange.PlaceOrderRequest) bool {
		return r.Side == exchange.SideBuy &&
			r.PositionSide == exchange.PositionSideLong &&
			r.Quantity == 10.0 &&
			r.Type == exchange.OrderTypeMarket
	})).Return(exchange.OrderResult{
		BrokerOrderID: "long-fut-1",
		Status:        exchange.OrderStatusFilled,
		ExecutedQty:   10.0,
		AvgFillPrice:  100.0,
		FilledAt:      time.Now(),
	}, nil).Once()

	var createdSignal entities.Signal
	mockRepo.On("Create", mock.MatchedBy(func(s entities.Signal) bool {
		createdSignal = s
		return s.PositionSide == entities.PositionSideLong && len(s.Orders) == 1
	})).Return(nil).Once()

	// DeductOrder must deduct InitialMargin ($100), NOT the full $1,000 notional.
	mockAccountUseCase.On("DeductOrder", float32(100.0)).Return(nil).Once()

	err := signalUC.GenerateBuySignal(entrySignal)
	require.NoError(t, err)

	require.Len(t, createdSignal.Orders, 1)
	order := createdSignal.Orders[0]

	// 1. InitialMargin committed is $100
	assert.Equal(t, float32(100.0), order.InitialMargin)
	assert.Equal(t, float32(100.0), order.InvestedAmount)

	// 2. Leveraged notional is $1,000 (quantity = 10 at price $100, leverage = 10)
	assert.Equal(t, float32(10.0), order.Quantity)
	assert.Equal(t, float32(100.0), order.EntryPrice)
	notional := float64(order.Quantity) * float64(order.EntryPrice)
	assert.InDelta(t, 1000.0, notional, 1e-3)
	assert.Equal(t, float32(10.0), order.Leverage)

	// 3. Entry fee computed on the full notional ($1,000 * 0.05% = $0.50)
	assert.Equal(t, float32(0.5), order.EntryFee)

	// 4. Verification that DeductOrder deducted InitialMargin, not full notional
	mockAccountUseCase.AssertCalled(t, "DeductOrder", float32(100.0))
	mockAccountUseCase.AssertNotCalled(t, "DeductOrder", float32(1000.0))

	mockRepo.AssertExpectations(t)
	mockExchange.AssertExpectations(t)
	mockAccountUseCase.AssertExpectations(t)
}

func TestLeveragedPositionSizing_PctCapital_Short(t *testing.T) {
	signalUC, mockRepo, mockAccountUseCase, mockExchange, mockNotifier := newFuturesTestSignalUseCase()
	mockNotifier.On("Send", mock.Anything, mock.Anything).Return(nil).Maybe()

	entrySignal := EntrySignal{
		Symbol:       "BTCUSDT",
		StrategyID:   2,
		StrategyName: "test-short-strat",
		EntryPrice:   100.0,
		PositionSide: entities.PositionSideShort,
		Leverage:     10.0,
		MarketType:   "futures",
		MarginType:   entities.Isolated,
		PositionSizing: &PositionSizingConfig{
			Type:  SizingPctCapital,
			Value: 10.0, // 10% of $1,000 capital = $100 margin
		},
	}

	mockAccountUseCase.On("CanOpenOrder").Return(true, nil).Once()
	mockAccountUseCase.On("GetDisponibleAmout").Return(float32(1000.0), nil).Once()
	mockAccountUseCase.On("GetAccount").Return(entities.Account{Amount: 1000.0}, nil).Maybe()
	mockRepo.On("GetOpenSignals", entrySignal.Symbol, entrySignal.StrategyID).Return(entities.Signal{}, nil).Once()

	mockExchange.On("PlaceOrder", mock.Anything, mock.MatchedBy(func(r exchange.PlaceOrderRequest) bool {
		return r.Side == exchange.SideSell &&
			r.PositionSide == exchange.PositionSideShort &&
			r.Quantity == 10.0 &&
			r.Type == exchange.OrderTypeMarket
	})).Return(exchange.OrderResult{
		BrokerOrderID: "short-fut-1",
		Status:        exchange.OrderStatusFilled,
		ExecutedQty:   10.0,
		AvgFillPrice:  100.0,
		FilledAt:      time.Now(),
	}, nil).Once()

	var createdSignal entities.Signal
	mockRepo.On("Create", mock.MatchedBy(func(s entities.Signal) bool {
		createdSignal = s
		return s.PositionSide == entities.PositionSideShort && len(s.Orders) == 1
	})).Return(nil).Once()

	mockAccountUseCase.On("DeductOrder", float32(100.0)).Return(nil).Once()

	err := signalUC.GenerateShortSignal(entrySignal)
	require.NoError(t, err)

	require.Len(t, createdSignal.Orders, 1)
	order := createdSignal.Orders[0]

	assert.Equal(t, entities.PositionSideShort, order.PositionSide)
	assert.Equal(t, float32(100.0), order.InitialMargin)
	assert.Equal(t, float32(100.0), order.InvestedAmount)
	assert.Equal(t, float32(10.0), order.Quantity)
	assert.Equal(t, float32(100.0), order.EntryPrice)
	assert.Equal(t, float32(10.0), order.Leverage)
	assert.Equal(t, float32(0.5), order.EntryFee)

	mockAccountUseCase.AssertCalled(t, "DeductOrder", float32(100.0))
	mockAccountUseCase.AssertNotCalled(t, "DeductOrder", float32(1000.0))

	mockRepo.AssertExpectations(t)
	mockExchange.AssertExpectations(t)
	mockAccountUseCase.AssertExpectations(t)
}

func TestLeveragedPositionSizing_ScriptQtyClamping(t *testing.T) {
	t.Run("resolveScriptQty scales ceiling by leverage and clamps excessive requested qty", func(t *testing.T) {
		signalUC, _, mockAccountUseCase, _, mockNotifier := newFuturesTestSignalUseCase()
		mockNotifier.On("Send", mock.Anything, mock.Anything).Return(nil).Maybe()

		// Account capital is $1,000
		mockAccountUseCase.On("GetAccount").Return(entities.Account{Amount: 1000.0}, nil)

		sizer := NewDefaultPositionSizer()

		// Scenario A: position_sizing = 10% capital, leverage = 10
		// Ceiling = 10% of 1,000 = 100. Leveraged ceiling = 100 * 10 = 1,000.
		// Price = 100. Leveraged max qty = 1,000 / 100 = 10.
		entrySignalA := EntrySignal{
			Symbol:       "BTCUSDT",
			StrategyID:   1,
			EntryPrice:   100.0,
			Leverage:     10.0,
			MarketType:   "futures",
			RequestedQty: 25.0, // Requests 25 units, exceeding max of 10
			PositionSizing: &PositionSizingConfig{
				Type:  SizingPctCapital,
				Value: 10.0,
			},
		}

		var clampCalled bool
		var clampedReq, clampedAllowed float64
		entrySignalA.OnClamp = func(requested, allowed float64) {
			clampCalled = true
			clampedReq = requested
			clampedAllowed = allowed
		}

		qtyA, err := signalUC.resolveScriptQty(context.Background(), entrySignalA, sizer, 1000.0)
		require.NoError(t, err)
		assert.Equal(t, 10.0, qtyA, "excessive script qty must be clamped to leveraged notional ceiling (10 units)")
		assert.True(t, clampCalled)
		assert.Equal(t, 25.0, clampedReq)
		assert.Equal(t, 10.0, clampedAllowed)

		// Scenario B: script requests within ceiling (e.g. 5 units)
		entrySignalB := EntrySignal{
			Symbol:       "BTCUSDT",
			StrategyID:   1,
			EntryPrice:   100.0,
			Leverage:     10.0,
			MarketType:   "futures",
			RequestedQty: 5.0, // Requests 5 units, within max of 10
			PositionSizing: &PositionSizingConfig{
				Type:  SizingPctCapital,
				Value: 10.0,
			},
		}
		var clampCalledB bool
		entrySignalB.OnClamp = func(requested, allowed float64) { clampCalledB = true }

		qtyB, err := signalUC.resolveScriptQty(context.Background(), entrySignalB, sizer, 1000.0)
		require.NoError(t, err)
		assert.Equal(t, 5.0, qtyB, "valid script qty within ceiling must be honoured")
		assert.False(t, clampCalledB)

		// Scenario C: nil position_sizing -> ceiling is full capital $1,000
		// Leveraged ceiling = 1,000 * 10 = 10,000. Leveraged max qty = 10,000 / 100 = 100.
		entrySignalC := EntrySignal{
			Symbol:       "BTCUSDT",
			StrategyID:   1,
			EntryPrice:   100.0,
			Leverage:     10.0,
			MarketType:   "futures",
			RequestedQty: 150.0, // Requests 150 units, exceeding max of 100
		}
		qtyC, err := signalUC.resolveScriptQty(context.Background(), entrySignalC, sizer, 10000.0)
		require.NoError(t, err)
		assert.Equal(t, 100.0, qtyC, "excessive script qty clamped to full capital leveraged ceiling (100 units)")
	})

	t.Run("GenerateBuySignal end-to-end with clamped script qty computes InitialMargin = Notional / Leverage", func(t *testing.T) {
		signalUC, mockRepo, mockAccountUseCase, mockExchange, mockNotifier := newFuturesTestSignalUseCase()
		mockNotifier.On("Send", mock.Anything, mock.Anything).Return(nil).Maybe()

		var clampTriggered bool
		entrySignal := EntrySignal{
			Symbol:       "BTCUSDT",
			StrategyID:   3,
			StrategyName: "script-strat",
			EntryPrice:   100.0,
			Leverage:     10.0,
			MarketType:   "futures",
			MarginType:   entities.Isolated,
			RequestedQty: 25.0, // Exceeds 10% capital ($100 margin -> $1,000 notional -> 10 units)
			PositionSizing: &PositionSizingConfig{
				Type:  SizingPctCapital,
				Value: 10.0,
			},
			OnClamp: func(requested, allowed float64) {
				clampTriggered = true
			},
		}

		mockAccountUseCase.On("CanOpenOrder").Return(true, nil).Once()
		mockAccountUseCase.On("GetDisponibleAmout").Return(float32(1000.0), nil).Once()
		mockAccountUseCase.On("GetAccount").Return(entities.Account{Amount: 1000.0}, nil).Once()
		mockRepo.On("GetOpenSignals", entrySignal.Symbol, entrySignal.StrategyID).Return(entities.Signal{}, nil).Once()

		// Exchange must be called with clamped quantity = 10.0 (not 25.0)
		mockExchange.On("PlaceOrder", mock.Anything, mock.MatchedBy(func(r exchange.PlaceOrderRequest) bool {
			return r.Quantity == 10.0 && r.Side == exchange.SideBuy
		})).Return(exchange.OrderResult{
			BrokerOrderID: "clamped-fut-1",
			Status:        exchange.OrderStatusFilled,
			ExecutedQty:   10.0,
			AvgFillPrice:  100.0,
			FilledAt:      time.Now(),
		}, nil).Once()

		var createdSignal entities.Signal
		mockRepo.On("Create", mock.MatchedBy(func(s entities.Signal) bool {
			createdSignal = s
			return len(s.Orders) == 1
		})).Return(nil).Once()

		// InitialMargin = (10.0 * 100.0) / 10 = 100.0
		mockAccountUseCase.On("DeductOrder", float32(100.0)).Return(nil).Once()

		err := signalUC.GenerateBuySignal(entrySignal)
		require.NoError(t, err)

		assert.True(t, clampTriggered, "OnClamp must be called on excessive script quantity")
		require.Len(t, createdSignal.Orders, 1)
		order := createdSignal.Orders[0]

		assert.Equal(t, float32(10.0), order.Quantity)
		assert.Equal(t, float32(100.0), order.InitialMargin)
		assert.Equal(t, float32(100.0), order.InvestedAmount)

		mockAccountUseCase.AssertCalled(t, "DeductOrder", float32(100.0))
		mockAccountUseCase.AssertNotCalled(t, "DeductOrder", float32(250.0)) // not 25 qty margin
		mockAccountUseCase.AssertNotCalled(t, "DeductOrder", float32(1000.0)) // not notional
	})
}
