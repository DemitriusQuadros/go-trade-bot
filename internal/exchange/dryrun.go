package exchange

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go-trade-bot/internal/metrics"
)

// SimulatedOrderIDPrefix marks every order ID minted by DryRunExchange
// (fix-01). A Signal/Order row whose BrokerOrderID/StopLossOrderID carries it
// is simulated - no real exchange order backs it.
const SimulatedOrderIDPrefix = "SIM-"

// simulatedStopIDPrefix marks resting simulated STOP_MARKET orders, so
// GetOrder can answer for them on any replica without in-memory state.
const simulatedStopIDPrefix = SimulatedOrderIDPrefix + "STOP-"

// MetricDryRunSimulatedOrders counts every order DryRunExchange simulates,
// labelled {side, type}. Real order metrics (order_execution_*) never see
// simulated orders: the worker's dryrun SignalUseCase has no metrics
// collector.
const MetricDryRunSimulatedOrders = "dryrun_simulated_orders_total"

// OrderTypeStopMarketTriggered is the `type` label value
// dryrun_simulated_orders_total uses for a simulated stop that fired.
const OrderTypeStopMarketTriggered = "STOP_MARKET_TRIGGERED"

// recentFillTTL bounds how long DryRunExchange remembers a simulated market
// fill for GetOrder, so the map can't grow without bound.
const recentFillTTL = 24 * time.Hour

// IsSimulatedOrderID reports whether id was minted by DryRunExchange.
func IsSimulatedOrderID(id string) bool {
	return strings.HasPrefix(id, SimulatedOrderIDPrefix)
}

// SimulatedClient is implemented by ExchangeClients that never place real
// orders and mint SimulatedOrderIDPrefix IDs (DryRunExchange). SignalUseCase
// uses it to refuse closing a simulated position on a real exchange and
// vice versa.
type SimulatedClient interface {
	SimulatesOrders() bool
}

// IsSimulatedClient reports whether c declares itself a simulated client.
func IsSimulatedClient(c ExchangeClient) bool {
	s, ok := c.(SimulatedClient)
	return ok && s.SimulatesOrders()
}

// DryRunExchange is the worker's order-execution client for effective mode
// dryrun (fix-01). Market data is real; orders are simulated.
//
// Structural safety: its only path to a real exchange is a *ReadOnlyClient
// it builds itself, so even a bug that reaches a real order method gets
// ErrReadOnlyClient, never an exchange call.
//
//   - Reads (ListKline, ListTickerPrices, GetAccountBalance, SubscribeKline,
//     GetOrder for non-SIM IDs) delegate to the read-only client.
//   - PlaceOrder MARKET fills immediately at the latest ticker price, moved
//     against the taker by SlippagePct. Fees are applied by SignalUseCase from
//     its FeePct (the worker sets it to DryRun.FeePct), exactly as the backtest
//     simulator's fees are.
//   - PlaceOrder STOP_MARKET returns a resting SIM-STOP- order. The stop itself
//     is persisted on the Order row (StopLossOrderID/StopLossPrice) by
//     SignalUseCase; app/engine.SimulatedStopEvaluator evaluates it each cycle
//     and fires it through TriggerStop.
//   - CancelOrder/GetOrder answer for SIM- IDs.
//
// FillDelay (DryRun.FillDelay) is not applied: a worker cycle has no later
// candle to fill against, so market orders fill immediately.
type DryRunExchange struct {
	readOnly    *ReadOnlyClient
	slippagePct float64
	metrics     *metrics.MetricsCollector

	// Now is the simulated fill clock (time.Now by default; tests override it).
	Now func() time.Time

	seq       atomic.Int64
	mu        sync.Mutex
	triggered map[string]OrderResult // stops fired by TriggerStop, not yet cleared
	fills     map[string]OrderResult // recent market fills, for GetOrder
}

var (
	_ ExchangeClient  = (*DryRunExchange)(nil)
	_ SimulatedClient = (*DryRunExchange)(nil)
)

// NewDryRunExchange wraps real in a ReadOnlyClient and simulates every order
// on top of it. collector may be nil.
func NewDryRunExchange(real ExchangeClient, slippagePct float64, collector *metrics.MetricsCollector) *DryRunExchange {
	return &DryRunExchange{
		readOnly: NewReadOnlyClient(real, func(op string) {
			log.Printf("SAFETY: dryrun exchange blocked a real %s call - simulated orders must never reach the exchange", op)
		}),
		slippagePct: slippagePct,
		metrics:     collector,
		Now:         time.Now,
		triggered:   make(map[string]OrderResult),
		fills:       make(map[string]OrderResult),
	}
}

// SimulatesOrders marks DryRunExchange as a SimulatedClient.
func (d *DryRunExchange) SimulatesOrders() bool { return true }

func (d *DryRunExchange) nextID(prefix string) string {
	return fmt.Sprintf("%s%d-%d", prefix, d.Now().UnixNano(), d.seq.Add(1))
}

func (d *DryRunExchange) countOrder(side, typ string) {
	if d.metrics == nil {
		return
	}
	d.metrics.IncrementCounter(MetricDryRunSimulatedOrders, map[string]string{"side": side, "type": typ})
}

// PlaceOrder simulates a MARKET fill or records a resting STOP_MARKET. It
// never calls the real exchange's order methods.
func (d *DryRunExchange) PlaceOrder(ctx context.Context, order PlaceOrderRequest) (OrderResult, error) {
	if order.Quantity <= 0 {
		return OrderResult{}, fmt.Errorf("%w: simulated order quantity must be > 0", ErrOrderRejected)
	}

	switch order.Type {
	case OrderTypeMarket:
		prices, err := d.readOnly.ListTickerPrices(ctx, order.Symbol)
		if err != nil {
			return OrderResult{}, fmt.Errorf("dryrun: ticker price for %s: %w", order.Symbol, err)
		}
		if len(prices) == 0 || prices[0].Price <= 0 {
			return OrderResult{}, fmt.Errorf("dryrun: no ticker price for %s", order.Symbol)
		}
		fill := prices[0].Price
		switch order.Side {
		case SideBuy:
			fill *= 1 + d.slippagePct/100
		case SideSell:
			fill *= 1 - d.slippagePct/100
		}
		res := OrderResult{
			BrokerOrderID: d.nextID(SimulatedOrderIDPrefix),
			ClientOrderID: order.ClientOrderID,
			Status:        OrderStatusFilled,
			ExecutedQty:   order.Quantity,
			AvgFillPrice:  fill,
			FilledAt:      d.Now(),
		}
		d.mu.Lock()
		d.pruneFillsLocked(res.FilledAt)
		d.fills[res.BrokerOrderID] = res
		d.mu.Unlock()
		d.countOrder(string(order.Side), string(order.Type))
		return res, nil

	case OrderTypeStopMarket:
		if order.StopPrice <= 0 {
			return OrderResult{}, fmt.Errorf("%w: simulated stop requires StopPrice > 0", ErrOrderRejected)
		}
		d.countOrder(string(order.Side), string(order.Type))
		return OrderResult{
			BrokerOrderID: d.nextID(simulatedStopIDPrefix),
			ClientOrderID: order.ClientOrderID,
			Status:        OrderStatusNew,
			FilledAt:      d.Now(),
		}, nil

	default:
		return OrderResult{}, fmt.Errorf("%w: dryrun does not simulate order type %q", ErrOrderRejected, order.Type)
	}
}

func (d *DryRunExchange) pruneFillsLocked(now time.Time) {
	for id, f := range d.fills {
		if now.Sub(f.FilledAt) > recentFillTTL {
			delete(d.fills, id)
		}
	}
}

// TriggerStop marks the resting simulated stop orderID as filled at
// stopPrice moved against the seller by SlippagePct. Until ClearTriggeredStop,
// CancelOrder(orderID) reports ErrOrderNotFound and GetOrder(orderID) returns
// the fill - the same "stop already triggered" shape a real exchange gives,
// so SignalUseCase.GenerateSellSignal closes the position through its normal
// stop reconciliation path.
func (d *DryRunExchange) TriggerStop(orderID string, quantity, stopPrice float64, at time.Time) OrderResult {
	res := OrderResult{
		BrokerOrderID: orderID,
		Status:        OrderStatusFilled,
		ExecutedQty:   quantity,
		AvgFillPrice:  stopPrice * (1 - d.slippagePct/100),
		FilledAt:      at,
	}
	d.mu.Lock()
	d.triggered[orderID] = res
	d.mu.Unlock()
	d.countOrder(string(SideSell), OrderTypeStopMarketTriggered)
	return res
}

// ClearTriggeredStop forgets a TriggerStop record once the position is
// reconciled (or the attempt failed; the evaluator re-triggers next cycle).
func (d *DryRunExchange) ClearTriggeredStop(orderID string) {
	d.mu.Lock()
	delete(d.triggered, orderID)
	d.mu.Unlock()
}

// CancelOrder cancels a resting simulated order. Non-SIM IDs go to the
// read-only client, which always refuses.
func (d *DryRunExchange) CancelOrder(ctx context.Context, symbol, orderID string) error {
	if !IsSimulatedOrderID(orderID) {
		return d.readOnly.CancelOrder(ctx, symbol, orderID)
	}
	d.mu.Lock()
	_, fired := d.triggered[orderID]
	d.mu.Unlock()
	if fired {
		return ErrOrderNotFound
	}
	// A resting simulated stop lives only on its Order row; cancelling it is
	// a no-op here - SignalUseCase closes the row.
	return nil
}

// GetOrder answers for SIM- IDs from simulation state; other IDs are a real
// (read-only) lookup.
func (d *DryRunExchange) GetOrder(ctx context.Context, symbol, orderID string) (OrderResult, error) {
	if !IsSimulatedOrderID(orderID) {
		return d.readOnly.GetOrder(ctx, symbol, orderID)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if res, ok := d.triggered[orderID]; ok {
		return res, nil
	}
	if res, ok := d.fills[orderID]; ok {
		return res, nil
	}
	if strings.HasPrefix(orderID, simulatedStopIDPrefix) {
		return OrderResult{BrokerOrderID: orderID, Status: OrderStatusNew}, nil
	}
	return OrderResult{}, ErrOrderNotFound
}

func (d *DryRunExchange) ListKline(ctx context.Context, symbol, interval string, limit int) ([]Candle, error) {
	return d.readOnly.ListKline(ctx, symbol, interval, limit)
}

func (d *DryRunExchange) ListTickerPrices(ctx context.Context, symbol string) ([]TickerPrice, error) {
	return d.readOnly.ListTickerPrices(ctx, symbol)
}

// GetAccountBalance returns the REAL (read-only) balance. A virtual dryrun
// balance is a follow-up (fix-01 out of scope).
func (d *DryRunExchange) GetAccountBalance(ctx context.Context) (AccountBalance, error) {
	return d.readOnly.GetAccountBalance(ctx)
}

func (d *DryRunExchange) SubscribeKline(ctx context.Context, symbol, interval string) (<-chan Candle, error) {
	return d.readOnly.SubscribeKline(ctx, symbol, interval)
}
