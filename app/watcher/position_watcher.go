package watcher

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"go-trade-bot/app/entities"
	usecase "go-trade-bot/app/usecase/signal"
	"go-trade-bot/internal/exchange"
	"go-trade-bot/internal/metrics"
)

const (
	ExitReasonTakeProfit        = "take_profit"
	ExitReasonSimulatedStopLoss = "simulated_stop_loss"

	metricWatcherTicksTotal = "position_watcher_ticks_total"
	metricWatcherExitsTotal = "position_watcher_exits_total"

	streamInitialBackoff = 1 * time.Second
	streamMaxBackoff     = 30 * time.Second
)

// WatchedPosition holds the runtime targets for an open position.
type WatchedPosition struct {
	SignalID        uint
	StrategyID      uint
	StrategyName    string
	Symbol          string
	Mode            string
	TakeProfitPrice float64
	StopLossPrice   float64
	Quantity        float64
	EntryPrice      float64
}

// SignalSeller is what PositionWatcher needs to execute an exit.
type SignalSeller interface {
	GenerateSellSignal(e usecase.ExitSignal) error
}

// SignalRepo is what PositionWatcher needs to load initial open signals.
type SignalRepo interface {
	GetAllOpenSignals() ([]entities.Signal, error)
}

type symbolStream struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// PositionWatcher continuously monitors open positions against real-time
// book ticker feeds, executing Take-Profit and simulated Stop-Loss exits
// immediately on incoming price ticks.
type PositionWatcher struct {
	exchangeClient exchange.ExchangeClient
	liveSeller     SignalSeller
	dryRunSeller   SignalSeller
	signalRepo     SignalRepo
	metrics        *metrics.MetricsCollector

	mu        sync.RWMutex
	positions map[uint]*WatchedPosition
	bySymbol  map[string]map[uint]*WatchedPosition
	streams   map[string]*symbolStream
	closing   sync.Map // signalID -> struct{}{}

	ctx    context.Context
	cancel context.CancelFunc
}

// NewPositionWatcher creates a new PositionWatcher instance.
func NewPositionWatcher(
	ex exchange.ExchangeClient,
	liveSeller SignalSeller,
	dryRunSeller SignalSeller,
	repo SignalRepo,
	collector *metrics.MetricsCollector,
) *PositionWatcher {
	return &PositionWatcher{
		exchangeClient: ex,
		liveSeller:     liveSeller,
		dryRunSeller:   dryRunSeller,
		signalRepo:     repo,
		metrics:        collector,
		positions:      make(map[uint]*WatchedPosition),
		bySymbol:       make(map[string]map[uint]*WatchedPosition),
		streams:        make(map[string]*symbolStream),
	}
}

// Start begins tracking open positions from the database, launches
// the periodic sync reconciler, and starts WebSocket streams for monitored symbols.
func (w *PositionWatcher) Start(parentCtx context.Context) error {
	w.mu.Lock()
	w.ctx, w.cancel = context.WithCancel(parentCtx)
	w.mu.Unlock()

	w.syncOpenPositions()
	go w.syncLoop(w.ctx)

	return nil
}

func (w *PositionWatcher) syncLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.syncOpenPositions()
		}
	}
}

func (w *PositionWatcher) syncOpenPositions() {
	if w.signalRepo == nil {
		return
	}
	signals, err := w.signalRepo.GetAllOpenSignals()
	if err != nil {
		log.Printf("[watcher] error loading open signals: %v", err)
		return
	}
	for _, s := range signals {
		w.TrackSignal(s)
	}
}

// Stop gracefully stops all symbol streams.
func (w *PositionWatcher) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.cancel != nil {
		w.cancel()
	}

	for sym, st := range w.streams {
		st.cancel()
		<-st.done
		delete(w.streams, sym)
	}
}

// TrackSignal registers or updates an open signal for real-time monitoring.
func (w *PositionWatcher) TrackSignal(s entities.Signal) {
	if len(s.Orders) == 0 {
		return
	}

	order := s.Orders[0]
	wp := &WatchedPosition{
		SignalID:        s.ID,
		StrategyID:      s.StrategyID,
		StrategyName:    s.Strategy.Name,
		Symbol:          s.Symbol,
		Mode:            s.Mode,
		TakeProfitPrice: float64(order.TakeProfitPrice),
		StopLossPrice:   float64(order.StopLossPrice),
		Quantity:        float64(order.Quantity),
		EntryPrice:      float64(order.EntryPrice),
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	w.positions[s.ID] = wp
	if w.bySymbol[s.Symbol] == nil {
		w.bySymbol[s.Symbol] = make(map[uint]*WatchedPosition)
	}
	w.bySymbol[s.Symbol][s.ID] = wp

	// Ensure a WebSocket stream is running for this symbol
	w.ensureStreamLocked(s.Symbol)
}

// UntrackSignal removes a closed signal from monitoring and shuts down the
// symbol stream if no other open positions remain for that symbol.
func (w *PositionWatcher) UntrackSignal(signalID uint) {
	w.mu.Lock()
	defer w.mu.Unlock()

	wp, ok := w.positions[signalID]
	if !ok {
		return
	}

	delete(w.positions, signalID)
	if symbolMap, exists := w.bySymbol[wp.Symbol]; exists {
		delete(symbolMap, signalID)
		if len(symbolMap) == 0 {
			delete(w.bySymbol, wp.Symbol)
			w.stopStreamLocked(wp.Symbol)
		}
	}
}

// IsClosing reports whether signalID is currently executing an exit.
func (w *PositionWatcher) IsClosing(signalID uint) bool {
	_, closing := w.closing.Load(signalID)
	return closing
}

// OnTick is invoked when a new BookTicker arrives for a symbol.
// Exported for unit testing and direct invocation.
func (w *PositionWatcher) OnTick(ticker exchange.BookTicker) {
	if w.metrics != nil {
		w.metrics.IncrementCounter(metricWatcherTicksTotal, map[string]string{"symbol": ticker.Symbol})
	}

	w.mu.RLock()
	positions := make([]*WatchedPosition, 0, len(w.bySymbol[ticker.Symbol]))
	for _, p := range w.bySymbol[ticker.Symbol] {
		positions = append(positions, p)
	}
	w.mu.RUnlock()

	for _, wp := range positions {
		w.evaluatePosition(wp, ticker)
	}
}

func (w *PositionWatcher) evaluatePosition(wp *WatchedPosition, ticker exchange.BookTicker) {
	if _, closing := w.closing.Load(wp.SignalID); closing {
		return
	}

	// For selling out of a long position, BestBidPrice is the available market fill price.
	bidPrice := ticker.BestBidPrice
	if bidPrice <= 0 {
		return
	}

	// 1. Check Take-Profit
	if wp.TakeProfitPrice > 0 && bidPrice >= wp.TakeProfitPrice {
		w.triggerExit(wp, bidPrice, ExitReasonTakeProfit)
		return
	}

	// 2. Check Dry-Run Simulated Stop-Loss
	// Live mode stop-losses are exchange-native STOP_MARKET orders on Binance.
	if wp.Mode == "dryrun" && wp.StopLossPrice > 0 && bidPrice <= wp.StopLossPrice {
		w.triggerExit(wp, bidPrice, ExitReasonSimulatedStopLoss)
		return
	}
}

func (w *PositionWatcher) triggerExit(wp *WatchedPosition, triggerPrice float64, reason string) {
	// Atomic lock to guarantee single exit execution
	if _, loaded := w.closing.LoadOrStore(wp.SignalID, struct{}{}); loaded {
		return
	}

	go func() {
		defer w.closing.Delete(wp.SignalID)

		log.Printf("[watcher] %s/%s signal %d hit %s at %.4f (target/stop was TP:%.4f SL:%.4f)",
			wp.StrategyName, wp.Symbol, wp.SignalID, reason, triggerPrice, wp.TakeProfitPrice, wp.StopLossPrice)

		exitSignal := usecase.ExitSignal{
			Symbol:       wp.Symbol,
			StrategyID:   wp.StrategyID,
			StrategyName: wp.StrategyName,
			Mode:         wp.Mode,
			ExitPrice:    float32(triggerPrice),
			ExitReason:   reason,
		}

		var err error
		if wp.Mode == "dryrun" && w.dryRunSeller != nil {
			err = w.dryRunSeller.GenerateSellSignal(exitSignal)
		} else if w.liveSeller != nil {
			err = w.liveSeller.GenerateSellSignal(exitSignal)
		} else {
			err = fmt.Errorf("no signal seller configured for mode %q", wp.Mode)
		}

		if err != nil {
			log.Printf("[watcher] failed to execute %s for %s signal %d: %v", reason, wp.Symbol, wp.SignalID, err)
			return
		}

		if w.metrics != nil {
			w.metrics.IncrementCounter(metricWatcherExitsTotal, map[string]string{
				"symbol": wp.Symbol,
				"reason": reason,
				"mode":   wp.Mode,
			})
		}

		w.UntrackSignal(wp.SignalID)
	}()
}

func (w *PositionWatcher) ensureStreamLocked(symbol string) {
	if _, exists := w.streams[symbol]; exists {
		return
	}

	ctx, cancel := context.WithCancel(w.ctx)
	done := make(chan struct{})
	w.streams[symbol] = &symbolStream{
		cancel: cancel,
		done:   done,
	}

	go w.runSymbolStream(ctx, symbol, done)
}

func (w *PositionWatcher) stopStreamLocked(symbol string) {
	st, exists := w.streams[symbol]
	if !exists {
		return
	}
	delete(w.streams, symbol)
	st.cancel()
}

func (w *PositionWatcher) runSymbolStream(ctx context.Context, symbol string, done chan struct{}) {
	defer close(done)

	backoff := streamInitialBackoff
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		ticks, err := w.exchangeClient.SubscribeBookTicker(ctx, symbol)
		if err != nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
				backoff *= 2
				if backoff > streamMaxBackoff {
					backoff = streamMaxBackoff
				}
				continue
			}
		}

		backoff = streamInitialBackoff
		for {
			select {
			case <-ctx.Done():
				return
			case tick, ok := <-ticks:
				if !ok {
					// Stream ended unexpectedly; reconnect with backoff
					goto reconnect
				}
				w.OnTick(tick)
			}
		}

	reconnect:
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
			backoff *= 2
			if backoff > streamMaxBackoff {
				backoff = streamMaxBackoff
			}
		}
	}
}
