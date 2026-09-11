package engine

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go-trade-bot/app/repository/candle"
	"go-trade-bot/internal/exchange"
)

type MarketDataSource interface {
	ListKlineBefore(ctx context.Context, symbol, interval string, asOf time.Time, limit int) ([]exchange.Candle, error)
}

type CandleRepoMarketDataSource struct {
	repo candle.Repository
}

func NewCandleRepoMarketDataSource(repo candle.Repository) *CandleRepoMarketDataSource {
	return &CandleRepoMarketDataSource{repo: repo}
}

func (s *CandleRepoMarketDataSource) ListKlineBefore(ctx context.Context, symbol, interval string, asOf time.Time, limit int) ([]exchange.Candle, error) {
	return s.repo.RangeBefore(ctx, symbol, interval, asOf, limit)
}

type RealExchangeMarketDataSource struct {
	client exchange.ExchangeClient
}

func NewRealExchangeMarketDataSource(client exchange.ExchangeClient) *RealExchangeMarketDataSource {
	return &RealExchangeMarketDataSource{client: client}
}

func (s *RealExchangeMarketDataSource) ListKlineBefore(ctx context.Context, symbol, interval string, asOf time.Time, limit int) ([]exchange.Candle, error) {
	return s.client.ListKline(ctx, symbol, interval, limit)
}

type FillPolicy struct {
	SlippagePct float64
	FillDelay   time.Duration
}

type restingOrder struct {
	OrderID       string
	ClientOrderID string
	Symbol        string
	Side          exchange.OrderSide
	Type          exchange.OrderType
	Quantity      float64
	StopPrice     float64
	CreatedAt     time.Time
}

type pendingDelayedOrder struct {
	Request   exchange.PlaceOrderRequest
	OrderTime time.Time
	OrderID   string
}

type SimulatedFillExchange struct {
	source         MarketDataSource
	policy         FillPolicy
	currentSimTime time.Time
	currentCandle  exchange.Candle
	restingOrders  map[string]*restingOrder
	executedOrders map[string]exchange.OrderResult
	pendingOrders  []*pendingDelayedOrder
	orderSeq       int64
	mu             sync.Mutex
}

func NewSimulatedFillExchange(source MarketDataSource, policy FillPolicy) *SimulatedFillExchange {
	return &SimulatedFillExchange{
		source:         source,
		policy:         policy,
		restingOrders:  make(map[string]*restingOrder),
		executedOrders: make(map[string]exchange.OrderResult),
		pendingOrders:  make([]*pendingDelayedOrder, 0),
	}
}

// SetSimulatedTime advances "now" for ListKline anti-lookahead anchoring and checks resting/delayed orders.
func (e *SimulatedFillExchange) SetSimulatedTime(t time.Time, currentCandle exchange.Candle) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.currentSimTime = t
	e.currentCandle = currentCandle

	// 1. Process pending delayed market orders whose delay elapsed
	remainingPending := make([]*pendingDelayedOrder, 0, len(e.pendingOrders))
	for _, p := range e.pendingOrders {
		if !currentCandle.OpenTime.Before(p.OrderTime.Add(e.policy.FillDelay)) {
			// Fill order at current candle close with slippage
			fillPrice := currentCandle.Close
			if e.policy.SlippagePct > 0 {
				if p.Request.Side == exchange.SideBuy {
					fillPrice = fillPrice * (1 + e.policy.SlippagePct/100.0)
				} else if p.Request.Side == exchange.SideSell {
					fillPrice = fillPrice * (1 - e.policy.SlippagePct/100.0)
				}
			}

			res := exchange.OrderResult{
				BrokerOrderID: p.OrderID,
				ClientOrderID: p.Request.ClientOrderID,
				Status:        exchange.OrderStatusFilled,
				ExecutedQty:   p.Request.Quantity,
				AvgFillPrice:  fillPrice,
			}
			e.executedOrders[p.OrderID] = res
			if p.Request.ClientOrderID != "" {
				e.executedOrders[p.Request.ClientOrderID] = res
			}
		} else {
			remainingPending = append(remainingPending, p)
		}
	}
	e.pendingOrders = remainingPending

	// 2. Check resting STOP_MARKET orders
	for id, order := range e.restingOrders {
		if order.Type == exchange.OrderTypeStopMarket {
			if order.Side == exchange.SideSell && currentCandle.Low <= order.StopPrice {
				// Stop loss triggered
				fillPrice := order.StopPrice
				res := exchange.OrderResult{
					BrokerOrderID: order.OrderID,
					ClientOrderID: order.ClientOrderID,
					Status:        exchange.OrderStatusFilled,
					ExecutedQty:   order.Quantity,
					AvgFillPrice:  fillPrice,
				}
				e.executedOrders[order.OrderID] = res
				if order.ClientOrderID != "" {
					e.executedOrders[order.ClientOrderID] = res
				}
				delete(e.restingOrders, id)
			}
		}
	}
}

func (e *SimulatedFillExchange) PlaceOrder(ctx context.Context, order exchange.PlaceOrderRequest) (exchange.OrderResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.orderSeq++
	orderID := fmt.Sprintf("sim_ord_%d", e.orderSeq)
	clientOrderID := order.ClientOrderID
	if clientOrderID == "" {
		clientOrderID = orderID
	}

	if order.Type == exchange.OrderTypeStopMarket {
		r := &restingOrder{
			OrderID:       orderID,
			ClientOrderID: clientOrderID,
			Symbol:        order.Symbol,
			Side:          order.Side,
			Type:          order.Type,
			Quantity:      order.Quantity,
			StopPrice:     order.StopPrice,
			CreatedAt:     e.currentSimTime,
		}
		e.restingOrders[orderID] = r
		e.restingOrders[clientOrderID] = r
		return exchange.OrderResult{
			BrokerOrderID: orderID,
			ClientOrderID: clientOrderID,
			Status:        exchange.OrderStatusNew,
			ExecutedQty:   0,
		}, nil
	}

	// Market order with FillDelay
	if e.policy.FillDelay > 0 {
		e.pendingOrders = append(e.pendingOrders, &pendingDelayedOrder{
			Request:   order,
			OrderTime: e.currentSimTime,
			OrderID:   orderID,
		})
		return exchange.OrderResult{
			BrokerOrderID: orderID,
			ClientOrderID: clientOrderID,
			Status:        exchange.OrderStatusNew,
			ExecutedQty:   0,
		}, nil
	}

	// Instant Market fill
	fillPrice := e.currentCandle.Close
	if e.policy.SlippagePct > 0 {
		if order.Side == exchange.SideBuy {
			fillPrice = fillPrice * (1 + e.policy.SlippagePct/100.0)
		} else if order.Side == exchange.SideSell {
			fillPrice = fillPrice * (1 - e.policy.SlippagePct/100.0)
		}
	}

	res := exchange.OrderResult{
		BrokerOrderID: orderID,
		ClientOrderID: clientOrderID,
		Status:        exchange.OrderStatusFilled,
		ExecutedQty:   order.Quantity,
		AvgFillPrice:  fillPrice,
	}
	e.executedOrders[orderID] = res
	e.executedOrders[clientOrderID] = res

	return res, nil
}

func (e *SimulatedFillExchange) CancelOrder(ctx context.Context, symbol, orderID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if _, ok := e.restingOrders[orderID]; ok {
		delete(e.restingOrders, orderID)
		return nil
	}

	// If not found in resting orders, check if it already triggered and filled
	return exchange.ErrOrderNotFound
}

func (e *SimulatedFillExchange) GetOrder(ctx context.Context, symbol, orderID string) (exchange.OrderResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if res, ok := e.executedOrders[orderID]; ok {
		return res, nil
	}

	if r, ok := e.restingOrders[orderID]; ok {
		return exchange.OrderResult{
			BrokerOrderID: r.OrderID,
			ClientOrderID: r.ClientOrderID,
			Status:        exchange.OrderStatusNew,
			ExecutedQty:   0,
			AvgFillPrice:  0,
		}, nil
	}

	return exchange.OrderResult{}, exchange.ErrOrderNotFound
}

func (e *SimulatedFillExchange) ListKline(ctx context.Context, symbol, interval string, limit int) ([]exchange.Candle, error) {
	e.mu.Lock()
	asOf := e.currentSimTime
	e.mu.Unlock()

	return e.source.ListKlineBefore(ctx, symbol, interval, asOf, limit)
}

func (e *SimulatedFillExchange) ListTickerPrices(ctx context.Context, symbol string) ([]exchange.TickerPrice, error) {
	e.mu.Lock()
	price := e.currentCandle.Close
	e.mu.Unlock()

	return []exchange.TickerPrice{
		{
			Symbol: symbol,
			Price:  price,
		},
	}, nil
}

func (e *SimulatedFillExchange) GetAccountBalance(ctx context.Context) (exchange.AccountBalance, error) {
	return exchange.AccountBalance{
		Asset:  "USDT",
		Free:   1000000.0,
		Locked: 0,
	}, nil
}

func (e *SimulatedFillExchange) SubscribeKline(ctx context.Context, symbol, interval string) (<-chan exchange.Candle, error) {
	ch := make(chan exchange.Candle)
	return ch, nil
}
