package feed

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"go-trade-bot/internal/exchange"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeExchangeClient is a hand-rolled exchange.ExchangeClient stand-in
// purpose-built for streaming/reconnect tests - the mockery-style
// argument-matching mocks used elsewhere in this repo are a poor fit for
// scripting a sequence of subscribe attempts returning different channels.
type fakeExchangeClient struct {
	mu        sync.Mutex
	responses []subscribeResponse
	calls     int
}

type subscribeResponse struct {
	ch  chan exchange.Candle
	err error
}

func (f *fakeExchangeClient) SubscribeKline(ctx context.Context, symbol, interval string) (<-chan exchange.Candle, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.calls >= len(f.responses) {
		// Keep returning the last scripted response indefinitely once the
		// script is exhausted, so a reconnect loop that outlives the script
		// doesn't panic on an out-of-range index.
		last := f.responses[len(f.responses)-1]
		f.calls++
		return last.ch, last.err
	}
	r := f.responses[f.calls]
	f.calls++
	return r.ch, r.err
}

func (f *fakeExchangeClient) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeExchangeClient) PlaceOrder(ctx context.Context, order exchange.PlaceOrderRequest) (exchange.OrderResult, error) {
	panic("not implemented for feed tests")
}
func (f *fakeExchangeClient) CancelOrder(ctx context.Context, symbol, orderID string) error {
	panic("not implemented for feed tests")
}
func (f *fakeExchangeClient) GetOrder(ctx context.Context, symbol, orderID string) (exchange.OrderResult, error) {
	panic("not implemented for feed tests")
}
func (f *fakeExchangeClient) ListKline(ctx context.Context, symbol, interval string, limit int) ([]exchange.Candle, error) {
	panic("not implemented for feed tests")
}
func (f *fakeExchangeClient) ListTickerPrices(ctx context.Context, symbol string) ([]exchange.TickerPrice, error) {
	panic("not implemented for feed tests")
}
func (f *fakeExchangeClient) GetAccountBalance(ctx context.Context) (exchange.AccountBalance, error) {
	panic("not implemented for feed tests")
}

func candleAt(seconds int64) exchange.Candle {
	return exchange.Candle{Symbol: "BTCUSDT", OpenTime: time.Unix(seconds, 0)}
}

func TestNewLiveFeed_SubscribeFailsSynchronously(t *testing.T) {
	client := &fakeExchangeClient{responses: []subscribeResponse{{err: errors.New("invalid symbol")}}}

	f, err := NewLiveFeed(client, "NOTREAL", "1m", nil)
	assert.Error(t, err)
	assert.Nil(t, f)
}

func TestLiveFeed_DeliversCandles(t *testing.T) {
	ch := make(chan exchange.Candle, 10)
	client := &fakeExchangeClient{responses: []subscribeResponse{{ch: ch}}}

	f, err := NewLiveFeed(client, "BTCUSDT", "1m", nil)
	require.NoError(t, err)
	defer f.Close()

	ch <- candleAt(1)
	ch <- candleAt(2)

	c1, ok := f.Next()
	require.True(t, ok)
	assert.Equal(t, time.Unix(1, 0), c1.OpenTime)

	c2, ok := f.Next()
	require.True(t, ok)
	assert.Equal(t, time.Unix(2, 0), c2.OpenTime)
}

func TestLiveFeed_Close_UnblocksNext(t *testing.T) {
	ch := make(chan exchange.Candle)
	client := &fakeExchangeClient{responses: []subscribeResponse{{ch: ch}}}

	f, err := NewLiveFeed(client, "BTCUSDT", "1m", nil)
	require.NoError(t, err)

	done := make(chan struct{})
	go func() {
		_, ok := f.Next()
		assert.False(t, ok)
		close(done)
	}()

	f.Close()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Next() did not unblock promptly after Close()")
	}
}

func TestLiveFeed_ReconnectsOnUnexpectedDisconnect(t *testing.T) {
	firstCh := make(chan exchange.Candle, 1)
	secondCh := make(chan exchange.Candle, 1)
	client := &fakeExchangeClient{responses: []subscribeResponse{
		{ch: firstCh},
		{ch: secondCh},
	}}

	f, err := NewLiveFeed(client, "BTCUSDT", "1m", nil)
	require.NoError(t, err)
	defer f.Close()

	firstCh <- candleAt(1)
	c1, ok := f.Next()
	require.True(t, ok)
	assert.Equal(t, time.Unix(1, 0), c1.OpenTime)

	// Simulate an unexpected disconnect: the underlying subscription channel
	// closes without an explicit Close() on the feed.
	close(firstCh)

	// The feed's reconnect loop should call SubscribeKline again (after the
	// initial 1s backoff) and start delivering from the second channel.
	secondCh <- candleAt(2)

	select {
	case c2, ok := <-f.buffer:
		require.True(t, ok)
		assert.Equal(t, time.Unix(2, 0), c2.OpenTime)
	case <-time.After(5 * time.Second):
		t.Fatal("feed did not reconnect and deliver from the second subscription")
	}

	assert.GreaterOrEqual(t, client.callCount(), 2)
}

func TestLiveFeed_ShouldDeliver_DedupsByOpenTime(t *testing.T) {
	f := &LiveFeed{}

	assert.True(t, f.shouldDeliver(candleAt(10)))
	// Same OpenTime again (e.g. redelivered around a reconnect) - discarded.
	assert.False(t, f.shouldDeliver(candleAt(10)))
	// An older OpenTime - discarded.
	assert.False(t, f.shouldDeliver(candleAt(5)))
	// Strictly newer - delivered.
	assert.True(t, f.shouldDeliver(candleAt(11)))
}

func TestLiveFeed_SetConnected_NilCollectorIsSafeNoOp(t *testing.T) {
	f := &LiveFeed{symbol: "BTCUSDT"}
	assert.NotPanics(t, func() {
		f.setConnected(true)
		f.setConnected(false)
		f.incrementReconnects()
	})
}
