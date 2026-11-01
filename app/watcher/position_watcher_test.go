package watcher

import (
	"context"
	"sync"
	"testing"
	"time"

	"go-trade-bot/app/entities"
	usecase "go-trade-bot/app/usecase/signal"
	"go-trade-bot/internal/exchange"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type mockSeller struct {
	mock.Mock
}

func (m *mockSeller) GenerateSellSignal(e usecase.ExitSignal) error {
	args := m.Called(e)
	return args.Error(0)
}

type mockExchange struct {
	mock.Mock
}

func (m *mockExchange) PlaceOrder(ctx context.Context, order exchange.PlaceOrderRequest) (exchange.OrderResult, error) {
	return exchange.OrderResult{}, nil
}
func (m *mockExchange) CancelOrder(ctx context.Context, symbol, orderID string) error {
	return nil
}
func (m *mockExchange) GetOrder(ctx context.Context, symbol, orderID string) (exchange.OrderResult, error) {
	return exchange.OrderResult{}, nil
}
func (m *mockExchange) ListKline(ctx context.Context, symbol, interval string, limit int) ([]exchange.Candle, error) {
	return nil, nil
}
func (m *mockExchange) ListTickerPrices(ctx context.Context, symbol string) ([]exchange.TickerPrice, error) {
	return nil, nil
}
func (m *mockExchange) GetAccountBalance(ctx context.Context) (exchange.AccountBalance, error) {
	return exchange.AccountBalance{}, nil
}
func (m *mockExchange) SubscribeKline(ctx context.Context, symbol, interval string) (<-chan exchange.Candle, error) {
	return nil, nil
}
func (m *mockExchange) SubscribeBookTicker(ctx context.Context, symbol string) (<-chan exchange.BookTicker, error) {
	args := m.Called(ctx, symbol)
	return args.Get(0).(<-chan exchange.BookTicker), args.Error(1)
}

func TestPositionWatcher_TakeProfitTrigger(t *testing.T) {
	liveSeller := &mockSeller{}
	drySeller := &mockSeller{}
	ex := &mockExchange{}
	ex.On("SubscribeBookTicker", mock.Anything, "BTCUSDT").Return(make(<-chan exchange.BookTicker), nil)

	w := NewPositionWatcher(ex, liveSeller, drySeller, nil, nil)
	w.ctx, w.cancel = context.WithCancel(context.Background())
	defer w.Stop()

	sig := entities.Signal{
		ID:         101,
		Symbol:     "BTCUSDT",
		StrategyID: 1,
		Strategy:   entities.Strategy{ID: 1, Name: "TestStrat"},
		Mode:       "live",
		Orders: []entities.Order{
			{
				ID:              1,
				SignalID:        101,
				EntryPrice:      100.0,
				TakeProfitPrice: 105.0,
				StopLossPrice:   95.0,
				Quantity:        1.0,
			},
		},
	}

	w.TrackSignal(sig)

	var wg sync.WaitGroup
	wg.Add(1)
	liveSeller.On("GenerateSellSignal", mock.MatchedBy(func(e usecase.ExitSignal) bool {
		return e.Symbol == "BTCUSDT" && e.ExitReason == ExitReasonTakeProfit && e.ExitPrice >= 105.0
	})).Run(func(args mock.Arguments) {
		wg.Done()
	}).Return(nil).Once()

	// 1. Tick below TP: should NOT trigger
	w.OnTick(exchange.BookTicker{
		Symbol:       "BTCUSDT",
		BestBidPrice: 104.5,
	})
	assert.False(t, w.IsClosing(101))

	// 2. Tick at or above TP: triggers exit
	w.OnTick(exchange.BookTicker{
		Symbol:       "BTCUSDT",
		BestBidPrice: 105.2,
	})

	wg.Wait()
	liveSeller.AssertExpectations(t)
}

func TestPositionWatcher_DryRunSimulatedStopTrigger(t *testing.T) {
	liveSeller := &mockSeller{}
	drySeller := &mockSeller{}
	ex := &mockExchange{}
	ex.On("SubscribeBookTicker", mock.Anything, "ETHUSDT").Return(make(<-chan exchange.BookTicker), nil)

	w := NewPositionWatcher(ex, liveSeller, drySeller, nil, nil)
	w.ctx, w.cancel = context.WithCancel(context.Background())
	defer w.Stop()

	sig := entities.Signal{
		ID:         202,
		Symbol:     "ETHUSDT",
		StrategyID: 2,
		Strategy:   entities.Strategy{ID: 2, Name: "DryStrat"},
		Mode:       "dryrun",
		Orders: []entities.Order{
			{
				ID:              2,
				SignalID:        202,
				EntryPrice:      2000.0,
				TakeProfitPrice: 2100.0,
				StopLossPrice:   1900.0,
				Quantity:        2.0,
			},
		},
	}

	w.TrackSignal(sig)

	var wg sync.WaitGroup
	wg.Add(1)
	drySeller.On("GenerateSellSignal", mock.MatchedBy(func(e usecase.ExitSignal) bool {
		return e.Symbol == "ETHUSDT" && e.ExitReason == ExitReasonSimulatedStopLoss && e.ExitPrice <= 1900.0
	})).Run(func(args mock.Arguments) {
		wg.Done()
	}).Return(nil).Once()

	// Tick drops to or below SL: triggers exit
	w.OnTick(exchange.BookTicker{
		Symbol:       "ETHUSDT",
		BestBidPrice: 1895.0,
	})

	wg.Wait()
	drySeller.AssertExpectations(t)
}

func TestPositionWatcher_LiveModeDoesNotTriggerSoftwareStop(t *testing.T) {
	liveSeller := &mockSeller{}
	drySeller := &mockSeller{}
	ex := &mockExchange{}
	ex.On("SubscribeBookTicker", mock.Anything, "SOLUSDT").Return(make(<-chan exchange.BookTicker), nil)

	w := NewPositionWatcher(ex, liveSeller, drySeller, nil, nil)
	w.ctx, w.cancel = context.WithCancel(context.Background())
	defer w.Stop()

	// Live mode has exchange-native STOP_MARKET orders, so watcher ignores StopLossPrice for live
	sig := entities.Signal{
		ID:         303,
		Symbol:     "SOLUSDT",
		StrategyID: 3,
		Strategy:   entities.Strategy{ID: 3, Name: "LiveStrat"},
		Mode:       "live",
		Orders: []entities.Order{
			{
				ID:              3,
				SignalID:        303,
				EntryPrice:      150.0,
				TakeProfitPrice: 160.0,
				StopLossPrice:   140.0,
				Quantity:        5.0,
			},
		},
	}

	w.TrackSignal(sig)

	// Price drops below SL: liveSeller should NOT be called
	w.OnTick(exchange.BookTicker{
		Symbol:       "SOLUSDT",
		BestBidPrice: 139.0,
	})

	time.Sleep(50 * time.Millisecond)
	liveSeller.AssertNotCalled(t, "GenerateSellSignal", mock.Anything)
	assert.False(t, w.IsClosing(303))
}

func TestPositionWatcher_UntrackStopsStreamWhenEmpty(t *testing.T) {
	ex := &mockExchange{}
	ex.On("SubscribeBookTicker", mock.Anything, "DOGEUSDT").Return(make(<-chan exchange.BookTicker), nil)
	w := NewPositionWatcher(ex, nil, nil, nil, nil)
	w.ctx, w.cancel = context.WithCancel(context.Background())
	defer w.Stop()

	sig := entities.Signal{
		ID:         404,
		Symbol:     "DOGEUSDT",
		StrategyID: 4,
		Strategy:   entities.Strategy{ID: 4, Name: "DogeStrat"},
		Mode:       "dryrun",
		Orders: []entities.Order{
			{ID: 4, SignalID: 404, EntryPrice: 0.10, Quantity: 100},
		},
	}

	w.TrackSignal(sig)
	assert.Contains(t, w.bySymbol, "DOGEUSDT")

	w.UntrackSignal(404)
	assert.NotContains(t, w.bySymbol, "DOGEUSDT")
	assert.NotContains(t, w.positions, uint(404))
}
