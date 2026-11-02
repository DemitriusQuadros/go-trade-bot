package usecase_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"go-trade-bot/app/entities"
	usecase "go-trade-bot/app/usecase/signal"
	"go-trade-bot/internal/exchange"
	"go-trade-bot/internal/futuresmath"
)

func TestSignalUseCase_GenerateShortSignal(t *testing.T) {
	t.Run("places sell order, protective buy stop-loss above entry, and take-profit below entry", func(t *testing.T) {
		signalUC, mockRepo, mockAccountUseCase, mockExchange, mockNotifier := newSignalUseCase()
		mockNotifier.On("Send", mock.Anything, mock.Anything).Return(nil).Maybe()

		tp := 48000.0
		entrySignal := usecase.EntrySignal{
			Symbol:          "BTCUSDT",
			StrategyID:      1,
			EntryPrice:      50000,
			MarginType:      entities.Isolated,
			Leverage:        10,
			MarketType:      "futures",
			StopLossPct:     2,
			TakeProfitPrice: &tp,
		}

		mockAccountUseCase.On("CanOpenOrder").Return(true, nil).Once()
		mockAccountUseCase.On("GetDisponibleAmout").Return(float32(1000), nil).Once()
		mockRepo.On("GetOpenSignals", entrySignal.Symbol, entrySignal.StrategyID).Return(entities.Signal{}, nil).Once()

		// Short entry order: SideSell, PositionSideShort
		mockExchange.On("PlaceOrder", mock.Anything, mock.MatchedBy(func(r exchange.PlaceOrderRequest) bool {
			return r.Side == exchange.SideSell && r.PositionSide == exchange.PositionSideShort && r.Type == exchange.OrderTypeMarket
		})).Return(exchange.OrderResult{
			BrokerOrderID: "short-1001",
			Status:        exchange.OrderStatusFilled,
			ExecutedQty:   0.2, // 1000 USDT margin * 10x leverage = 10,000 notional / 50,000 = 0.2 BTC
			AvgFillPrice:  50000,
			FilledAt:      time.Now(),
		}, nil).Once()

		// Protective stop-loss for Short: SideBuy, PositionSideShort, StopPrice above fill (50000 * 1.02 = 51000)
		mockExchange.On("PlaceOrder", mock.Anything, mock.MatchedBy(func(r exchange.PlaceOrderRequest) bool {
			return r.Side == exchange.SideBuy &&
				r.PositionSide == exchange.PositionSideShort &&
				r.Type == exchange.OrderTypeStopMarket &&
				r.ReduceOnly == true &&
				r.StopPrice == 51000
		})).Return(exchange.OrderResult{
			BrokerOrderID: "stop-1001",
			Status:        exchange.OrderStatusNew,
		}, nil).Once()

		var createdSignal entities.Signal
		mockRepo.On("Create", mock.MatchedBy(func(s entities.Signal) bool {
			createdSignal = s
			return s.PositionSide == entities.PositionSideShort && len(s.Orders) == 1
		})).Return(nil).Once()

		mockAccountUseCase.On("DeductOrder", float32(1000.0)).Return(nil).Once()

		err := signalUC.GenerateShortSignal(entrySignal)
		require.NoError(t, err)

		require.Equal(t, entities.PositionSideShort, createdSignal.PositionSide)
		require.Len(t, createdSignal.Orders, 1)
		order := createdSignal.Orders[0]
		assert.Equal(t, entities.PositionSideShort, order.PositionSide)
		assert.Equal(t, float32(10.0), order.Leverage)
		assert.Equal(t, float32(1000.0), order.InvestedAmount)
		assert.Equal(t, float32(1000.0), order.InitialMargin)
		assert.Equal(t, float32(48000.0), order.TakeProfitPrice)
		assert.Equal(t, float32(51000.0), order.StopLossPrice)
		assert.Greater(t, order.LiquidationPrice, float32(50000.0), "short liquidation price must be above entry price")

		mockRepo.AssertExpectations(t)
		mockExchange.AssertExpectations(t)
		mockAccountUseCase.AssertExpectations(t)
	})
}

func TestSignalUseCase_ShortExit_PnL(t *testing.T) {
	t.Run("calculates correct short profit (Entry - Exit) * Qty - Fees - Funding", func(t *testing.T) {
		signalUC, mockRepo, mockAccountUseCase, mockExchange, mockNotifier := newSignalUseCase()
		mockNotifier.On("Send", mock.Anything, mock.Anything).Return(nil).Maybe()
		signalUC.FeePct = 0.05 // 5 bps futures fee

		openSignal := entities.Signal{
			ID:           5,
			Symbol:       "BTCUSDT",
			Status:       entities.Open,
			StrategyID:   1,
			PositionSide: entities.PositionSideShort,
			Orders: []entities.Order{
				{
					ID:                 10,
					BrokerOrderID:      "short-1001",
					StopLossOrderID:    "stop-1001",
					PositionSide:       entities.PositionSideShort,
					EntryPrice:         50000,
					Quantity:           0.2,
					InvestedAmount:     1000,
					InitialMargin:      1000,
					Leverage:           10,
					EntryFee:           5.0, // 0.2 * 50000 * 0.05% = 5 USDT
					AccumulatedFunding: 2.0, // 2 USDT funding paid
					MarginType:         entities.Isolated,
				},
			},
		}

		mockRepo.On("GetOpenSignals", "BTCUSDT", uint(1)).Return(openSignal, nil).Once()
		mockExchange.On("CancelOrder", mock.Anything, "BTCUSDT", "stop-1001").Return(nil).Once()

		// Closing short: market BUY with ReduceOnly
		mockExchange.On("PlaceOrder", mock.Anything, mock.MatchedBy(func(r exchange.PlaceOrderRequest) bool {
			return r.Side == exchange.SideBuy &&
				r.PositionSide == exchange.PositionSideShort &&
				r.ReduceOnly == true &&
				r.Type == exchange.OrderTypeMarket
		})).Return(exchange.OrderResult{
			BrokerOrderID: "cover-1001",
			Status:        exchange.OrderStatusFilled,
			ExecutedQty:   0.2,
			AvgFillPrice:  45000, // profit on short!
			FilledAt:      time.Now(),
		}, nil).Once()

		// gross profit = (50000 - 45000) * 0.2 = 1000 USDT
		// exit fee = 0.2 * 45000 * 0.05% = 4.5 USDT
		// net profit = 1000 - (4.5 + 5.0 + 2.0) = 988.5 USDT
		var updatedSignal entities.Signal
		mockRepo.On("Update", mock.MatchedBy(func(s entities.Signal) bool {
			updatedSignal = s
			return s.Status == entities.Closed
		})).Return(nil).Once()

		expectedCredit := float32(1000.0 + 988.5)
		mockAccountUseCase.On("AddOrder", mock.MatchedBy(func(credit float32) bool {
			return assert.InDelta(t, expectedCredit, credit, 0.01)
		})).Return(nil).Once()

		err := signalUC.GenerateSellSignal(usecase.ExitSignal{
			Symbol:     "BTCUSDT",
			StrategyID: 1,
			ExitPrice:  45000,
			ExitReason: "take_profit",
		})
		require.NoError(t, err)

		assert.InDelta(t, 988.5, float64(updatedSignal.Orders[0].Profit), 0.01)
		assert.Equal(t, float32(4.5), updatedSignal.Orders[0].ExitFee)

		mockRepo.AssertExpectations(t)
		mockExchange.AssertExpectations(t)
		mockAccountUseCase.AssertExpectations(t)
	})

	t.Run("isolated liquidation caps loss at -InvestedAmount", func(t *testing.T) {
		signalUC, mockRepo, mockAccountUseCase, mockExchange, mockNotifier := newSignalUseCase()
		mockNotifier.On("Send", mock.Anything, mock.Anything).Return(nil).Maybe()

		openSignal := entities.Signal{
			ID:           6,
			Symbol:       "BTCUSDT",
			Status:       entities.Open,
			StrategyID:   1,
			PositionSide: entities.PositionSideLong,
			Orders: []entities.Order{
				{
					ID:             11,
					BrokerOrderID:  "long-1001",
					PositionSide:   entities.PositionSideLong,
					EntryPrice:     50000,
					Quantity:       1.0,
					InvestedAmount: 5000, // 10x on 50,000 notional
					InitialMargin:  5000,
					Leverage:       10,
					EntryFee:       25.0,
					MarginType:     entities.Isolated,
				},
			},
		}

		mockRepo.On("GetOpenSignals", "BTCUSDT", uint(1)).Return(openSignal, nil).Once()

		// Extreme gap down to 40000: gross loss = -10,000, worse than initial margin 5,000
		mockExchange.On("PlaceOrder", mock.Anything, mock.Anything).Return(exchange.OrderResult{
			BrokerOrderID: "liq-close",
			Status:        exchange.OrderStatusFilled,
			ExecutedQty:   1.0,
			AvgFillPrice:  40000,
			FilledAt:      time.Now(),
		}, nil).Once()

		var updatedSignal entities.Signal
		mockRepo.On("Update", mock.MatchedBy(func(s entities.Signal) bool {
			updatedSignal = s
			return s.Status == entities.Closed
		})).Return(nil).Once()

		// Credit = InvestedAmount + Profit = 5000 + (-5000) = 0
		mockAccountUseCase.On("AddOrder", float32(0.0)).Return(nil).Once()

		err := signalUC.GenerateSellSignal(usecase.ExitSignal{
			Symbol:     "BTCUSDT",
			StrategyID: 1,
			ExitPrice:  40000,
			ExitReason: entities.ExitReasonLiquidation,
		})
		require.NoError(t, err)

		assert.Equal(t, float32(-5000.0), updatedSignal.Orders[0].Profit)

		mockRepo.AssertExpectations(t)
		mockAccountUseCase.AssertExpectations(t)
	})
}

func TestSignalUseCase_SettleFunding(t *testing.T) {
	signalUC, mockRepo, _, _, _ := newSignalUseCase()

	now := time.Now().UTC()
	openSignal := entities.Signal{
		ID:         7,
		Symbol:     "BTCUSDT",
		Status:     entities.Open,
		StrategyID: 1,
		Orders: []entities.Order{
			{
				ID:                 12,
				AccumulatedFunding: 5.0,
				LiquidationPrice:   45000,
			},
		},
	}

	mockRepo.On("GetOpenSignals", "BTCUSDT", uint(1)).Return(openSignal, nil).Once()
	mockRepo.On("Update", mock.MatchedBy(func(s entities.Signal) bool {
		order := s.Orders[0]
		return order.AccumulatedFunding == 8.5 &&
			order.LiquidationPrice == 45100 &&
			order.LastFundingTime != nil &&
			order.LastFundingTime.Equal(now)
	})).Return(nil).Once()

	err := signalUC.SettleFunding("BTCUSDT", 1, 3.5, 45100, now)
	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
}

func TestSignalUseCase_LeverageSizing(t *testing.T) {
	signalUC, mockRepo, mockAccountUseCase, mockExchange, mockNotifier := newSignalUseCase()
	mockNotifier.On("Send", mock.Anything, mock.Anything).Return(nil).Maybe()

	entrySignal := usecase.EntrySignal{
		Symbol:       "BTCUSDT",
		StrategyID:   1,
		EntryPrice:   50000,
		MarginType:   entities.Isolated,
		Leverage:     10,
		MarketType:   "futures",
		RequestedQty: 0.1, // 0.1 BTC = 5000 notional, 500 margin
	}

	mockAccountUseCase.On("CanOpenOrder").Return(true, nil).Once()
	mockAccountUseCase.On("GetDisponibleAmout").Return(float32(1000), nil).Once()
	mockAccountUseCase.On("GetAccount").Return(entities.Account{Amount: 1000}, nil).Once()
	mockRepo.On("GetOpenSignals", entrySignal.Symbol, entrySignal.StrategyID).Return(entities.Signal{}, nil).Once()

	mockExchange.On("PlaceOrder", mock.Anything, mock.MatchedBy(func(r exchange.PlaceOrderRequest) bool {
		return r.Quantity == 0.1 && r.Side == exchange.SideBuy
	})).Return(exchange.OrderResult{
		BrokerOrderID: "long-1002",
		Status:        exchange.OrderStatusFilled,
		ExecutedQty:   0.1,
		AvgFillPrice:  50000,
		FilledAt:      time.Now(),
	}, nil).Once()

	var createdSignal entities.Signal
	mockRepo.On("Create", mock.MatchedBy(func(s entities.Signal) bool {
		createdSignal = s
		return len(s.Orders) == 1
	})).Return(nil).Once()

	// Initial margin = (0.1 * 50000) / 10 = 500
	mockAccountUseCase.On("DeductOrder", float32(500.0)).Return(nil).Once()

	err := signalUC.GenerateBuySignal(entrySignal)
	require.NoError(t, err)

	order := createdSignal.Orders[0]
	assert.Equal(t, float32(500.0), order.InitialMargin)
	assert.Equal(t, float32(500.0), order.InvestedAmount)

	expectedLiq := futuresmath.IsolatedLiquidationPrice("BTCUSDT", futuresmath.SideLong, 50000, 0.1, 10, 0, 0)
	assert.InDelta(t, expectedLiq, float64(order.LiquidationPrice), 1.0)
}
