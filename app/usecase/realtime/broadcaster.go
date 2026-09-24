package realtime

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"go-trade-bot/app/entities"
	"go-trade-bot/internal/exchange"
)

type EventType = string

const (
	EventPriceUpdate    EventType = "price_update"
	EventPositionUpdate EventType = "position_update"
	EventHeartbeat      EventType = "heartbeat"
)

type Event struct {
	Type EventType `json:"type"`
	Data any       `json:"data"`
}

func (e Event) JSON() string {
	b, err := json.Marshal(e.Data)
	if err != nil {
		return "{}"
	}
	return string(b)
}

type PriceUpdatePayload struct {
	Symbol    string    `json:"symbol"`
	Price     float64   `json:"price"`
	Timestamp time.Time `json:"timestamp"`
}

type PositionUpdatePayload struct {
	SignalID         uint      `json:"signal_id"`
	Symbol           string    `json:"symbol"`
	StrategyID       uint      `json:"strategy_id"`
	EntryPrice       float64   `json:"entry_price"`
	CurrentPrice     float64   `json:"current_price"`
	UnrealizedPnL    float64   `json:"unrealized_pnl"`
	UnrealizedPnLPct float64   `json:"unrealized_pnl_pct"`
	Quantity         float64   `json:"quantity"`
	OpenedAt         time.Time `json:"opened_at"`
}

type HeartbeatPayload struct {
	Timestamp time.Time `json:"timestamp"`
}

type SignalRepository interface {
	GetAllOpenSignals() ([]entities.Signal, error)
}

type StrategyRepository interface {
	GetAll(ctx context.Context) ([]entities.Strategy, error)
}

type DashboardBroadcaster struct {
	exchangeClient exchange.ExchangeClient
	signalRepo     SignalRepository
	strategyRepo   StrategyRepository

	subscribersMu sync.RWMutex
	subscribers   map[chan Event]struct{}
}

func NewDashboardBroadcaster(
	ex exchange.ExchangeClient,
	sigRepo SignalRepository,
	stratRepo StrategyRepository,
) *DashboardBroadcaster {
	return &DashboardBroadcaster{
		exchangeClient: ex,
		signalRepo:     sigRepo,
		strategyRepo:   stratRepo,
		subscribers:    make(map[chan Event]struct{}),
	}
}

func (b *DashboardBroadcaster) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 64)
	b.subscribersMu.Lock()
	b.subscribers[ch] = struct{}{}
	b.subscribersMu.Unlock()

	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			b.subscribersMu.Lock()
			delete(b.subscribers, ch)
			close(ch)
			b.subscribersMu.Unlock()
		})
	}

	return ch, unsubscribe
}

func (b *DashboardBroadcaster) Broadcast(event Event) {
	b.subscribersMu.RLock()
	defer b.subscribersMu.RUnlock()

	for ch := range b.subscribers {
		select {
		case ch <- event:
		default:
			// Client channel full, skip to prevent blocking
		}
	}
}

func (b *DashboardBroadcaster) SubscriberCount() int {
	b.subscribersMu.RLock()
	defer b.subscribersMu.RUnlock()
	return len(b.subscribers)
}

func (b *DashboardBroadcaster) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 5 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	heartbeatTicker := time.NewTicker(15 * time.Second)
	defer heartbeatTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-heartbeatTicker.C:
			b.Broadcast(Event{
				Type: EventHeartbeat,
				Data: HeartbeatPayload{Timestamp: time.Now().UTC()},
			})
		case <-ticker.C:
			b.pollOnce(ctx)
		}
	}
}

func (b *DashboardBroadcaster) PollOnce(ctx context.Context) {
	b.pollOnce(ctx)
}

func (b *DashboardBroadcaster) pollOnce(ctx context.Context) {
	if b.strategyRepo == nil || b.exchangeClient == nil {
		return
	}

	// 1. Gather all active monitored symbols
	strategies, err := b.strategyRepo.GetAll(ctx)
	if err != nil {
		return
	}

	symbolSet := make(map[string]struct{})
	for _, strat := range strategies {
		if strat.Status != entities.Disabled {
			for _, sym := range strat.MonitoredSymbols {
				if sym != "" {
					symbolSet[sym] = struct{}{}
				}
			}
		}
	}

	// 2. Fetch prices
	currentPrices := make(map[string]float64)
	for sym := range symbolSet {
		tickers, err := b.exchangeClient.ListTickerPrices(ctx, sym)
		if err == nil && len(tickers) > 0 {
			priceFloat := tickers[0].Price
			currentPrices[sym] = priceFloat
			b.Broadcast(Event{
				Type: EventPriceUpdate,
				Data: PriceUpdatePayload{
					Symbol:    sym,
					Price:     priceFloat,
					Timestamp: time.Now().UTC(),
				},
			})
		}
	}

	// 3. Fetch open signals
	if b.signalRepo == nil {
		return
	}

	openSignals, err := b.signalRepo.GetAllOpenSignals()
	if err != nil {
		return
	}

	for _, sig := range openSignals {
		curPrice, hasPrice := currentPrices[sig.Symbol]
		if !hasPrice {
			// Try fetching ticker directly if not in monitored set
			tickers, err := b.exchangeClient.ListTickerPrices(ctx, sig.Symbol)
			if err == nil && len(tickers) > 0 {
				curPrice = tickers[0].Price
				hasPrice = true
			}
		}

		var entryPrice, quantity float64
		if len(sig.Orders) > 0 {
			entryPrice = float64(sig.Orders[0].EntryPrice)
			quantity = float64(sig.Orders[0].Quantity)
		}

		if hasPrice && entryPrice > 0 {
			unrealized := (curPrice - entryPrice) * quantity
			unrealizedPct := ((curPrice - entryPrice) / entryPrice) * 100

			b.Broadcast(Event{
				Type: EventPositionUpdate,
				Data: PositionUpdatePayload{
					SignalID:         sig.ID,
					Symbol:           sig.Symbol,
					StrategyID:       sig.StrategyID,
					EntryPrice:       entryPrice,
					CurrentPrice:     curPrice,
					UnrealizedPnL:    unrealized,
					UnrealizedPnLPct: unrealizedPct,
					Quantity:         quantity,
					OpenedAt:         sig.CreatedAt,
				},
			})
		}
	}
}
