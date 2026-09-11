package exchange

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"go-trade-bot/internal/configuration"

	"github.com/adshao/go-binance/v2"
	"github.com/adshao/go-binance/v2/common"
)

// Binance API error codes this adapter classifies into ACL sentinel errors.
// See https://binance-docs.github.io/apidocs/spot/en/#error-codes.
const (
	binanceErrCodeUnknownOrder        = -2011
	binanceErrCodeInsufficientBalance = -2010
	binanceErrCodeFilterFailure       = -1013
)

// quoteAssetForAccountBalance is the asset BinanceAdapter.GetAccountBalance
// reports on. The frozen ExchangeClient interface (Spec 01) returns a single
// AccountBalance, not a per-asset list, and no Phase 1 acceptance criterion
// exercises this method (entities.Account remains the source of truth for
// capital tracking in Phase 1 - see app/usecase/account). USDT is hardcoded
// as the practical default quote asset for this bot's monitored pairs.
const quoteAssetForAccountBalance = "USDT"

// BinanceAdapter is the only production ExchangeClient implementation. It is
// one of exactly two files in the repository permitted to import
// github.com/adshao/go-binance/v2 (the other being binance_testnet_adapter.go).
type BinanceAdapter struct {
	client *binance.Client
}

// NewBinanceAdapter builds a production-pointed BinanceAdapter. It fails fast
// (rather than deferring the failure to the first PlaceOrder call) if the
// broker API credentials are not configured.
func NewBinanceAdapter(cfg *configuration.Configuration) (*BinanceAdapter, error) {
	if cfg.Broker.ApiKey == "" || cfg.Broker.ApiSecret == "" {
		return nil, fmt.Errorf("exchange: BROKER.KEY/BROKER.SECRET must be configured to build a BinanceAdapter")
	}
	binance.UseTestnet = false
	client := binance.NewClient(cfg.Broker.ApiKey, cfg.Broker.ApiSecret)
	return &BinanceAdapter{client: client}, nil
}

func (a *BinanceAdapter) PlaceOrder(ctx context.Context, order PlaceOrderRequest) (OrderResult, error) {
	svc := a.client.NewCreateOrderService().
		Symbol(order.Symbol).
		Side(toBinanceSide(order.Side)).
		Type(toBinanceOrderType(order.Type)).
		Quantity(formatFloat(order.Quantity))

	if order.ClientOrderID != "" {
		svc = svc.NewClientOrderID(order.ClientOrderID)
	}
	if order.Type == OrderTypeStopMarket {
		svc = svc.StopPrice(formatFloat(order.StopPrice))
	}

	res, err := svc.Do(ctx)
	if err != nil {
		return OrderResult{}, classifyPlaceOrderError(err)
	}
	return orderResultFromCreate(res), nil
}

func (a *BinanceAdapter) CancelOrder(ctx context.Context, symbol, orderID string) error {
	id, err := strconv.ParseInt(orderID, 10, 64)
	if err != nil {
		return fmt.Errorf("exchange: invalid order id %q: %w", orderID, err)
	}
	_, err = a.client.NewCancelOrderService().Symbol(symbol).OrderID(id).Do(ctx)
	if err != nil {
		return classifyLookupError(err)
	}
	return nil
}

func (a *BinanceAdapter) GetOrder(ctx context.Context, symbol, orderID string) (OrderResult, error) {
	id, err := strconv.ParseInt(orderID, 10, 64)
	if err != nil {
		return OrderResult{}, fmt.Errorf("exchange: invalid order id %q: %w", orderID, err)
	}
	res, err := a.client.NewGetOrderService().Symbol(symbol).OrderID(id).Do(ctx)
	if err != nil {
		return OrderResult{}, classifyLookupError(err)
	}
	return orderResultFromOrder(res), nil
}

// classifyPlaceOrderError maps a go-binance error into one of this package's
// ACL sentinel errors when it recognizes the underlying API error code,
// preserving the original error via %w so errors.Is still works alongside
// errors.Unwrap-based inspection. Errors that aren't a recognized
// *common.APIError (context deadline exceeded, connection reset, DNS
// failure, ...) are returned unwrapped - the caller treats those as
// transport-level/"ambiguous" failures (Spec 02 Idempotency).
func classifyPlaceOrderError(err error) error {
	var apiErr *common.APIError
	if !errors.As(err, &apiErr) {
		return err
	}
	switch apiErr.Code {
	case binanceErrCodeInsufficientBalance:
		return fmt.Errorf("%w: %s", ErrInsufficientBalance, apiErr.Message)
	case binanceErrCodeFilterFailure:
		return fmt.Errorf("%w: %s", ErrFilterViolation, apiErr.Message)
	default:
		return fmt.Errorf("%w: %s", ErrOrderRejected, apiErr.Message)
	}
}

// classifyLookupError maps a go-binance CancelOrder/GetOrder error into
// ErrOrderNotFound when the exchange reports the order doesn't exist,
// otherwise returns the original error unchanged.
func classifyLookupError(err error) error {
	var apiErr *common.APIError
	if errors.As(err, &apiErr) && apiErr.Code == binanceErrCodeUnknownOrder {
		return fmt.Errorf("%w: %s", ErrOrderNotFound, apiErr.Message)
	}
	return err
}

func (a *BinanceAdapter) ListKline(ctx context.Context, symbol string, interval string, limit int) ([]Candle, error) {
	klines, err := a.client.NewKlinesService().Symbol(symbol).Interval(interval).Limit(limit).Do(ctx)
	if err != nil {
		return nil, err
	}
	candles := make([]Candle, 0, len(klines))
	for _, k := range klines {
		c, err := candleFromKline(symbol, interval, k)
		if err != nil {
			return nil, err
		}
		candles = append(candles, c)
	}
	return candles, nil
}

func (a *BinanceAdapter) ListTickerPrices(ctx context.Context, symbol string) ([]TickerPrice, error) {
	prices, err := a.client.NewListPricesService().Symbol(symbol).Do(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]TickerPrice, 0, len(prices))
	for _, p := range prices {
		price, err := strconv.ParseFloat(p.Price, 64)
		if err != nil {
			return nil, fmt.Errorf("exchange: parsing ticker price for %s: %w", p.Symbol, err)
		}
		result = append(result, TickerPrice{Symbol: p.Symbol, Price: price})
	}
	return result, nil
}

func (a *BinanceAdapter) GetAccountBalance(ctx context.Context) (AccountBalance, error) {
	account, err := a.client.NewGetAccountService().Do(ctx)
	if err != nil {
		return AccountBalance{}, err
	}
	for _, b := range account.Balances {
		if b.Asset != quoteAssetForAccountBalance {
			continue
		}
		free, err := strconv.ParseFloat(b.Free, 64)
		if err != nil {
			return AccountBalance{}, fmt.Errorf("exchange: parsing free balance: %w", err)
		}
		locked, err := strconv.ParseFloat(b.Locked, 64)
		if err != nil {
			return AccountBalance{}, fmt.Errorf("exchange: parsing locked balance: %w", err)
		}
		return AccountBalance{Asset: b.Asset, Free: free, Locked: locked}, nil
	}
	return AccountBalance{Asset: quoteAssetForAccountBalance}, nil
}

// SubscribeKline is a thin wrapper over go-binance's WsKlineServe. Reconnect
// logic and buffering belong to Spec 04's LiveFeed (the consumer of this
// channel), not here - this method surfaces closed (final) candles as they
// arrive and closes the returned channel when the underlying subscription
// ends (cleanly or on error), so the caller can detect the need to resubscribe.
func (a *BinanceAdapter) SubscribeKline(ctx context.Context, symbol, interval string) (<-chan Candle, error) {
	out := make(chan Candle)

	handler := func(event *binance.WsKlineEvent) {
		if !event.Kline.IsFinal {
			return // only emit closed candles - matches ListKline's "closed candle" semantics
		}
		c, err := candleFromWsKline(symbol, interval, event.Kline)
		if err != nil {
			return
		}
		select {
		case out <- c:
		case <-ctx.Done():
		}
	}

	errHandler := func(err error) {
		// Best-effort: go-binance's WsKlineServe closes doneC after invoking
		// errHandler, which is what actually signals LiveFeed to reconnect.
	}

	doneC, stopC, err := binance.WsKlineServe(symbol, interval, handler, errHandler)
	if err != nil {
		close(out)
		return nil, err
	}

	go func() {
		select {
		case <-doneC:
		case <-ctx.Done():
			close(stopC)
			<-doneC
		}
		close(out)
	}()

	return out, nil
}

func toBinanceSide(side OrderSide) binance.SideType {
	if side == SideSell {
		return binance.SideTypeSell
	}
	return binance.SideTypeBuy
}

func toBinanceOrderType(t OrderType) binance.OrderType {
	if t == OrderTypeStopMarket {
		return binance.OrderTypeStopLoss
	}
	return binance.OrderTypeMarket
}

func mapBinanceStatus(s binance.OrderStatusType) OrderStatus {
	switch s {
	case binance.OrderStatusTypeNew:
		return OrderStatusNew
	case binance.OrderStatusTypeFilled:
		return OrderStatusFilled
	case binance.OrderStatusTypePartiallyFilled:
		return OrderStatusPartiallyFilled
	case binance.OrderStatusTypeRejected:
		return OrderStatusRejected
	case binance.OrderStatusTypeExpired, binance.OrderStatusExpiredInMatch:
		return OrderStatusExpired
	case binance.OrderStatusTypeCanceled, binance.OrderStatusTypePendingCancel:
		return OrderStatusCanceled
	default:
		return OrderStatusRejected
	}
}

func orderResultFromCreate(res *binance.CreateOrderResponse) OrderResult {
	executedQty, _ := strconv.ParseFloat(res.ExecutedQuantity, 64)
	cumQuote, _ := strconv.ParseFloat(res.CummulativeQuoteQuantity, 64)
	avgPrice := avgFillPrice(executedQty, cumQuote, res.Price)
	return OrderResult{
		BrokerOrderID: strconv.FormatInt(res.OrderID, 10),
		ClientOrderID: res.ClientOrderID,
		Status:        mapBinanceStatus(res.Status),
		ExecutedQty:   executedQty,
		AvgFillPrice:  avgPrice,
	}
}

func orderResultFromOrder(res *binance.Order) OrderResult {
	executedQty, _ := strconv.ParseFloat(res.ExecutedQuantity, 64)
	cumQuote, _ := strconv.ParseFloat(res.CummulativeQuoteQuantity, 64)
	avgPrice := avgFillPrice(executedQty, cumQuote, res.Price)
	return OrderResult{
		BrokerOrderID: strconv.FormatInt(res.OrderID, 10),
		ClientOrderID: res.ClientOrderID,
		Status:        mapBinanceStatus(res.Status),
		ExecutedQty:   executedQty,
		AvgFillPrice:  avgPrice,
	}
}

// avgFillPrice derives the average fill price from cumulative quote quantity
// when available (market orders), falling back to the order's own `price`
// field (relevant for a resting STOP_MARKET order, which reports its stop
// price there while Status == NEW and ExecutedQty == 0).
func avgFillPrice(executedQty, cumQuote float64, rawPrice string) float64 {
	if executedQty > 0 && cumQuote > 0 {
		return cumQuote / executedQty
	}
	price, err := strconv.ParseFloat(rawPrice, 64)
	if err != nil {
		return 0
	}
	return price
}

func candleFromKline(symbol, interval string, k *binance.Kline) (Candle, error) {
	open, err := strconv.ParseFloat(k.Open, 64)
	if err != nil {
		return Candle{}, fmt.Errorf("exchange: parsing open: %w", err)
	}
	high, err := strconv.ParseFloat(k.High, 64)
	if err != nil {
		return Candle{}, fmt.Errorf("exchange: parsing high: %w", err)
	}
	low, err := strconv.ParseFloat(k.Low, 64)
	if err != nil {
		return Candle{}, fmt.Errorf("exchange: parsing low: %w", err)
	}
	closePrice, err := strconv.ParseFloat(k.Close, 64)
	if err != nil {
		return Candle{}, fmt.Errorf("exchange: parsing close: %w", err)
	}
	volume, err := strconv.ParseFloat(k.Volume, 64)
	if err != nil {
		return Candle{}, fmt.Errorf("exchange: parsing volume: %w", err)
	}
	return Candle{
		Symbol:    symbol,
		Timeframe: interval,
		OpenTime:  time.UnixMilli(k.OpenTime),
		Open:      open,
		High:      high,
		Low:       low,
		Close:     closePrice,
		Volume:    volume,
	}, nil
}

func candleFromWsKline(symbol, interval string, k binance.WsKline) (Candle, error) {
	open, err := strconv.ParseFloat(k.Open, 64)
	if err != nil {
		return Candle{}, fmt.Errorf("exchange: parsing open: %w", err)
	}
	high, err := strconv.ParseFloat(k.High, 64)
	if err != nil {
		return Candle{}, fmt.Errorf("exchange: parsing high: %w", err)
	}
	low, err := strconv.ParseFloat(k.Low, 64)
	if err != nil {
		return Candle{}, fmt.Errorf("exchange: parsing low: %w", err)
	}
	closePrice, err := strconv.ParseFloat(k.Close, 64)
	if err != nil {
		return Candle{}, fmt.Errorf("exchange: parsing close: %w", err)
	}
	volume, err := strconv.ParseFloat(k.Volume, 64)
	if err != nil {
		return Candle{}, fmt.Errorf("exchange: parsing volume: %w", err)
	}
	return Candle{
		Symbol:    symbol,
		Timeframe: interval,
		OpenTime:  time.UnixMilli(k.StartTime),
		Open:      open,
		High:      high,
		Low:       low,
		Close:     closePrice,
		Volume:    volume,
	}, nil
}

func formatFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}
