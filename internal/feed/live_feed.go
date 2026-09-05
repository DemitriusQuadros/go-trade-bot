package feed

import (
	"context"
	"sync"
	"time"

	"go-trade-bot/internal/exchange"
	"go-trade-bot/internal/metrics"
)

const (
	metricWebsocketReconnects = "websocket_reconnects_total"
	metricWebsocketConnected  = "websocket_connected"
	metricFeedCandleDelay     = "feed_candle_delay_seconds"

	// bufferCapacity is generous relative to any realistic strategy cycle
	// length (1-60 minutes), small relative to the 4GB homelab RAM budget
	// (Spec 04).
	bufferCapacity = 500

	initialBackoff = 1 * time.Second
	maxBackoff     = 30 * time.Second
)

// LiveFeed is the WebSocket-backed Feed implementation (Spec 04): it
// subscribes via ExchangeClient.SubscribeKline and pushes candles to a
// bounded buffered channel that Next() reads from. A background goroutine
// owns the subscription and reconnects with exponential backoff on an
// unexpected disconnect.
type LiveFeed struct {
	client    exchange.ExchangeClient
	symbol    string
	interval  string
	collector *metrics.MetricsCollector

	buffer    chan exchange.Candle
	done      chan struct{}
	closeOnce sync.Once

	ctx    context.Context
	cancel context.CancelFunc

	mu           sync.Mutex
	lastOpenTime time.Time
}

// NewLiveFeed subscribes to symbol/interval and starts the background
// reader/reconnect goroutine. A hard subscription failure at construction
// time (e.g. an invalid symbol) is returned synchronously (Spec 04 AC#8) -
// it is not swallowed into the reconnect loop, since it's a different class
// of failure than a transient network drop.
func NewLiveFeed(client exchange.ExchangeClient, symbol, interval string, collector *metrics.MetricsCollector) (*LiveFeed, error) {
	ctx, cancel := context.WithCancel(context.Background())

	candles, err := client.SubscribeKline(ctx, symbol, interval)
	if err != nil {
		cancel()
		return nil, err
	}

	f := &LiveFeed{
		client:    client,
		symbol:    symbol,
		interval:  interval,
		collector: collector,
		buffer:    make(chan exchange.Candle, bufferCapacity),
		done:      make(chan struct{}),
		ctx:       ctx,
		cancel:    cancel,
	}

	go f.run(candles)

	return f, nil
}

// Next blocks until a candle is available or the feed is closed.
func (f *LiveFeed) Next() (exchange.Candle, bool) {
	select {
	case c, ok := <-f.buffer:
		if !ok {
			return exchange.Candle{}, false
		}
		if f.collector != nil && !c.OpenTime.IsZero() {
			delay := time.Since(c.OpenTime).Seconds()
			f.collector.SetGauge(metricFeedCandleDelay, map[string]string{"symbol": f.symbol, "feed_type": "live"}, delay)
		}
		return c, true
	case <-f.done:
		return exchange.Candle{}, false
	}
}

// Close stops the feed. Any goroutine blocked in Next() returns promptly
// with (zero-value, false).
func (f *LiveFeed) Close() error {
	f.closeOnce.Do(func() {
		close(f.done)
		f.cancel()
	})
	return nil
}

// run owns the subscription for the lifetime of the feed: it forwards
// candles into the bounded buffer (blocking, never dropping - Spec 04 AC#5),
// and on an unexpected channel close, reconnects with exponential backoff.
func (f *LiveFeed) run(candles <-chan exchange.Candle) {
	for {
		receivedAny := false
		for c := range candles {
			if !receivedAny {
				f.setConnected(true)
				receivedAny = true
			}
			if !f.shouldDeliver(c) {
				continue
			}
			select {
			case f.buffer <- c:
			case <-f.done:
				return
			}
		}

		select {
		case <-f.done:
			return
		default:
		}

		// Unexpected disconnect (the channel closed without an explicit
		// Close() call): mark disconnected and reconnect with backoff.
		f.setConnected(false)

		var err error
		candles, err = f.reconnect()
		if err != nil {
			return // f.done was closed while reconnecting
		}
	}
}

// reconnect retries SubscribeKline with exponential backoff (1s, 2s, 4s, ...
// capped at 30s), retried indefinitely - a live-trading feed must not give
// up permanently (Spec 04). Returns a non-nil error only when f.done closes
// during the wait, signaling run() to stop.
func (f *LiveFeed) reconnect() (<-chan exchange.Candle, error) {
	backoff := initialBackoff
	for {
		f.incrementReconnects()

		select {
		case <-f.done:
			return nil, context.Canceled
		case <-time.After(backoff):
		}

		candles, err := f.client.SubscribeKline(f.ctx, f.symbol, f.interval)
		if err == nil {
			return candles, nil
		}

		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// shouldDeliver dedups by OpenTime: a candle whose OpenTime is not strictly
// greater than the last delivered one for this symbol is discarded - this
// is what prevents a stale-candle replay/duplicate delivery around a
// reconnect (Spec 04 AC#7).
func (f *LiveFeed) shouldDeliver(c exchange.Candle) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !c.OpenTime.After(f.lastOpenTime) {
		return false
	}
	f.lastOpenTime = c.OpenTime
	return true
}

func (f *LiveFeed) setConnected(connected bool) {
	if f.collector == nil {
		return
	}
	value := 0.0
	if connected {
		value = 1.0
	}
	f.collector.SetGauge(metricWebsocketConnected, map[string]string{"symbol": f.symbol}, value)
}

func (f *LiveFeed) incrementReconnects() {
	if f.collector == nil {
		return
	}
	f.collector.IncrementCounter(metricWebsocketReconnects, map[string]string{"symbol": f.symbol})
}
