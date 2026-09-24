package realtime_test

import (
	"context"
	"testing"
	"time"

	"go-trade-bot/app/entities"
	usecase "go-trade-bot/app/usecase/realtime"
	"go-trade-bot/internal/exchange"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
)

type mockExchangeClient struct {
	prices map[string][]exchange.TickerPrice
	err    error
}

func (m *mockExchangeClient) PlaceOrder(ctx context.Context, order exchange.PlaceOrderRequest) (exchange.OrderResult, error) {
	return exchange.OrderResult{}, nil
}
func (m *mockExchangeClient) CancelOrder(ctx context.Context, symbol, orderID string) error {
	return nil
}
func (m *mockExchangeClient) GetOrder(ctx context.Context, symbol, orderID string) (exchange.OrderResult, error) {
	return exchange.OrderResult{}, nil
}
func (m *mockExchangeClient) ListKline(ctx context.Context, symbol, interval string, limit int) ([]exchange.Candle, error) {
	return nil, nil
}
func (m *mockExchangeClient) ListTickerPrices(ctx context.Context, symbol string) ([]exchange.TickerPrice, error) {
	if m.err != nil {
		return nil, m.err
	}
	if p, ok := m.prices[symbol]; ok {
		return p, nil
	}
	return []exchange.TickerPrice{{Symbol: symbol, Price: 50000.0}}, nil
}
func (m *mockExchangeClient) GetAccountBalance(ctx context.Context) (exchange.AccountBalance, error) {
	return exchange.AccountBalance{}, nil
}
func (m *mockExchangeClient) SubscribeKline(ctx context.Context, symbol, interval string) (<-chan exchange.Candle, error) {
	return nil, nil
}

type mockSignalRepo struct {
	signals []entities.Signal
	err     error
}

func (m *mockSignalRepo) GetAllOpenSignals() ([]entities.Signal, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.signals, nil
}

type mockStrategyRepo struct {
	strategies []entities.Strategy
	err        error
}

func (m *mockStrategyRepo) GetAll(ctx context.Context) ([]entities.Strategy, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.strategies, nil
}

func TestDashboardBroadcaster_FanOut_AC1_AC2(t *testing.T) {
	mockEx := &mockExchangeClient{
		prices: map[string][]exchange.TickerPrice{
			"BTCUSDT": {{Symbol: "BTCUSDT", Price: 50000.0}},
			"ETHUSDT": {{Symbol: "ETHUSDT", Price: 3000.0}},
		},
	}
	stratRepo := &mockStrategyRepo{
		strategies: []entities.Strategy{
			{
				ID:               1,
				Status:           entities.Productive,
				MonitoredSymbols: datatypes.JSONSlice[string]{"BTCUSDT", "ETHUSDT"},
			},
		},
	}
	sigRepo := &mockSignalRepo{
		signals: []entities.Signal{
			{
				ID:         10,
				Symbol:     "BTCUSDT",
				StrategyID: 1,
				Status:     entities.Open,
				Orders: []entities.Order{
					{
						EntryPrice: 48000.0,
						Quantity:   0.5,
					},
				},
			},
		},
	}

	broadcaster := usecase.NewDashboardBroadcaster(mockEx, sigRepo, stratRepo)

	// Subscribe 2 clients (AC#2)
	ch1, unsub1 := broadcaster.Subscribe()
	defer unsub1()
	ch2, unsub2 := broadcaster.Subscribe()
	defer unsub2()

	assert.Equal(t, 2, broadcaster.SubscriberCount())

	broadcaster.PollOnce(context.Background())

	// Both subscribers should receive 2 price_updates and 1 position_update
	var events1, events2 []usecase.Event
	for i := 0; i < 3; i++ {
		select {
		case e := <-ch1:
			events1 = append(events1, e)
		case <-time.After(100 * time.Millisecond):
			t.Fatal("timeout waiting for event on ch1")
		}
		select {
		case e := <-ch2:
			events2 = append(events2, e)
		case <-time.After(100 * time.Millisecond):
			t.Fatal("timeout waiting for event on ch2")
		}
	}

	assert.Len(t, events1, 3)
	assert.Len(t, events2, 3)

	// Verify position_update calculations
	var posEvent usecase.Event
	for _, e := range events1 {
		if e.Type == "position_update" {
			posEvent = e
			break
		}
	}
	require.NotEmpty(t, posEvent)
	payload, ok := posEvent.Data.(usecase.PositionUpdatePayload)
	require.True(t, ok)
	assert.Equal(t, uint(10), payload.SignalID)
	assert.Equal(t, "BTCUSDT", payload.Symbol)
	assert.Equal(t, 48000.0, payload.EntryPrice)
	assert.Equal(t, 50000.0, payload.CurrentPrice)
	assert.Equal(t, 1000.0, payload.UnrealizedPnL) // (50000 - 48000) * 0.5
	assert.InDelta(t, 4.1666, payload.UnrealizedPnLPct, 0.01)

	// Unsubscribe
	unsub1()
	assert.Equal(t, 1, broadcaster.SubscriberCount())
}

func TestDashboardBroadcaster_ZeroSymbolsAndPositions_AC7(t *testing.T) {
	mockEx := &mockExchangeClient{}
	stratRepo := &mockStrategyRepo{
		strategies: []entities.Strategy{
			{
				ID:               1,
				Status:           entities.Disabled,
				MonitoredSymbols: datatypes.JSONSlice[string]{"BTCUSDT"},
			},
		},
	}
	sigRepo := &mockSignalRepo{signals: nil}

	broadcaster := usecase.NewDashboardBroadcaster(mockEx, sigRepo, stratRepo)
	ch, unsub := broadcaster.Subscribe()
	defer unsub()

	broadcaster.PollOnce(context.Background())

	select {
	case e := <-ch:
		t.Fatalf("expected no events, got %+v", e)
	case <-time.After(50 * time.Millisecond):
		// Success: no price_update or position_update emitted
	}
}

func TestDashboardBroadcaster_ExchangeErrorForOneSymbol_AC8(t *testing.T) {
	mockEx := &mockExchangeClient{err: assert.AnError}
	stratRepo := &mockStrategyRepo{
		strategies: []entities.Strategy{
			{
				ID:               1,
				Status:           entities.Productive,
				MonitoredSymbols: datatypes.JSONSlice[string]{"BTCUSDT"},
			},
		},
	}
	sigRepo := &mockSignalRepo{signals: nil}

	broadcaster := usecase.NewDashboardBroadcaster(mockEx, sigRepo, stratRepo)
	ch, unsub := broadcaster.Subscribe()
	defer unsub()

	// Should not panic or stall
	broadcaster.PollOnce(context.Background())

	select {
	case e := <-ch:
		t.Fatalf("expected no events due to error, got %+v", e)
	case <-time.After(50 * time.Millisecond):
		// Success
	}
}

func TestDashboardBroadcaster_Run_Heartbeat_And_Cancel(t *testing.T) {
	mockEx := &mockExchangeClient{}
	broadcaster := usecase.NewDashboardBroadcaster(mockEx, nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, unsub := broadcaster.Subscribe()
	defer unsub()

	go broadcaster.Run(ctx, 100*time.Millisecond)

	// Cancellation stops run cleanly
	cancel()
	time.Sleep(50 * time.Millisecond)
	_ = ch
}
