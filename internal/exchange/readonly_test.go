package exchange

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// mockInner is a testify mock of ExchangeClient.
type mockInner struct{ mock.Mock }

func (m *mockInner) PlaceOrder(ctx context.Context, o PlaceOrderRequest) (OrderResult, error) {
	args := m.Called(ctx, o)
	return args.Get(0).(OrderResult), args.Error(1)
}
func (m *mockInner) CancelOrder(ctx context.Context, symbol, id string) error {
	return m.Called(ctx, symbol, id).Error(0)
}
func (m *mockInner) GetOrder(ctx context.Context, symbol, id string) (OrderResult, error) {
	args := m.Called(ctx, symbol, id)
	return args.Get(0).(OrderResult), args.Error(1)
}
func (m *mockInner) ListKline(ctx context.Context, symbol, interval string, limit int) ([]Candle, error) {
	args := m.Called(ctx, symbol, interval, limit)
	return args.Get(0).([]Candle), args.Error(1)
}
func (m *mockInner) ListTickerPrices(ctx context.Context, symbol string) ([]TickerPrice, error) {
	args := m.Called(ctx, symbol)
	return args.Get(0).([]TickerPrice), args.Error(1)
}
func (m *mockInner) GetAccountBalance(ctx context.Context) (AccountBalance, error) {
	args := m.Called(ctx)
	return args.Get(0).(AccountBalance), args.Error(1)
}
func (m *mockInner) SubscribeKline(ctx context.Context, symbol, interval string) (<-chan Candle, error) {
	args := m.Called(ctx, symbol, interval)
	return args.Get(0).(<-chan Candle), args.Error(1)
}

// A-02 AC#2: PlaceOrder/CancelOrder return ErrReadOnlyClient and never
// reach the inner client; the blocked-attempt hook fires each time.
func TestReadOnlyClient_BlocksOrderMutations(t *testing.T) {
	inner := &mockInner{}
	var blocked []string
	c := NewReadOnlyClient(inner, func(op string) { blocked = append(blocked, op) })

	_, err := c.PlaceOrder(context.Background(), PlaceOrderRequest{})
	assert.ErrorIs(t, err, ErrReadOnlyClient)
	err = c.CancelOrder(context.Background(), "BTCUSDT", "1")
	assert.ErrorIs(t, err, ErrReadOnlyClient)

	inner.AssertNotCalled(t, "PlaceOrder", mock.Anything, mock.Anything)
	inner.AssertNotCalled(t, "CancelOrder", mock.Anything, mock.Anything, mock.Anything)
	assert.Equal(t, []string{"place_order", "cancel_order"}, blocked)

	// nil hook is fine
	_, err = NewReadOnlyClient(inner, nil).PlaceOrder(context.Background(), PlaceOrderRequest{})
	assert.ErrorIs(t, err, ErrReadOnlyClient)
}

func TestReadOnlyClient_DelegatesReads(t *testing.T) {
	inner := &mockInner{}
	ch := make(<-chan Candle)
	inner.On("GetOrder", mock.Anything, "BTCUSDT", "7").Return(OrderResult{}, nil).Once()
	inner.On("ListKline", mock.Anything, "BTCUSDT", "1m", 10).Return([]Candle{{}}, nil).Once()
	inner.On("ListTickerPrices", mock.Anything, "BTCUSDT").Return([]TickerPrice{{}}, nil).Once()
	inner.On("GetAccountBalance", mock.Anything).Return(AccountBalance{}, nil).Once()
	inner.On("SubscribeKline", mock.Anything, "BTCUSDT", "1m").Return(ch, nil).Once()

	c := NewReadOnlyClient(inner, nil)
	ctx := context.Background()
	_, err := c.GetOrder(ctx, "BTCUSDT", "7")
	require.NoError(t, err)
	k, err := c.ListKline(ctx, "BTCUSDT", "1m", 10)
	require.NoError(t, err)
	assert.Len(t, k, 1)
	_, err = c.ListTickerPrices(ctx, "BTCUSDT")
	require.NoError(t, err)
	_, err = c.GetAccountBalance(ctx)
	require.NoError(t, err)
	_, err = c.SubscribeKline(ctx, "BTCUSDT", "1m")
	require.NoError(t, err)
	inner.AssertExpectations(t)
}
