package exchange

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"go-trade-bot/internal/configuration"

	"github.com/adshao/go-binance/v2/common"
	"github.com/adshao/go-binance/v2/futures"
)

// Binance Futures API error codes classified into ACL sentinel errors.
// See https://binance-docs.github.io/apidocs/futures/en/#error-codes.
const (
	futuresErrCodeUnknownOrder              = -2011 // ErrOrderNotFound
	futuresErrCodeInsufficientBalance       = -2010 // ErrInsufficientBalance
	futuresErrCodeInsufficientMargin        = -2019 // ErrInsufficientBalance
	futuresErrCodeReduceOnlyRejected        = -2022 // ErrReduceOnlyRejected
	futuresErrCodeWouldImmediatelyTrigger   = -2021 // ErrFilterViolation
	futuresErrCodeMinNotional               = -4164 // ErrFilterViolation
	futuresErrCodePrecisionOverMax          = -1111 // ErrFilterViolation
	futuresErrCodeFilterFailure             = -1013 // ErrFilterViolation
	futuresErrCodeInvalidQuantity           = -4003 // ErrFilterViolation
	futuresErrCodeMarginTypeNoChange        = -4046 // Idempotent success -> nil
	futuresErrCodePositionSideNoChange      = -4059 // Idempotent success -> nil
)

type symbolFilters struct {
	stepSize float64
	tickSize float64
}

// BinanceFuturesAdapter is the production USD-M Futures exchange client adapter.
type BinanceFuturesAdapter struct {
	client           *futures.Client
	mu               sync.RWMutex
	positionMode     PositionMode
	leverageCache    map[string]int
	marginCache      map[string]MarginMode
	filtersCache     map[string]symbolFilters
	filtersFetchedAt time.Time
}

var (
	_ ExchangeClient         = (*BinanceFuturesAdapter)(nil)
	_ HistoricalKlineFetcher = (*BinanceFuturesAdapter)(nil)
	_ FuturesClient          = (*BinanceFuturesAdapter)(nil)
)

// NewBinanceFuturesAdapter builds a production-pointed BinanceFuturesAdapter.
// Fails fast if broker API credentials are missing.
func NewBinanceFuturesAdapter(cfg *configuration.Configuration) (*BinanceFuturesAdapter, error) {
	if cfg.Broker.ApiKey == "" || cfg.Broker.ApiSecret == "" {
		return nil, fmt.Errorf("exchange: BROKER.KEY/BROKER.SECRET must be configured to build a BinanceFuturesAdapter")
	}
	futures.UseTestnet = false
	client := futures.NewClient(cfg.Broker.ApiKey, cfg.Broker.ApiSecret)
	posMode := PositionMode(cfg.PositionMode)
	if posMode == "" {
		posMode = PositionMode(cfg.Broker.PositionMode)
	}
	if posMode != PositionModeHedge {
		posMode = PositionModeOneWay
	}
	return &BinanceFuturesAdapter{
		client:        client,
		positionMode:  posMode,
		leverageCache: make(map[string]int),
		marginCache:   make(map[string]MarginMode),
		filtersCache:  make(map[string]symbolFilters),
	}, nil
}

func (a *BinanceFuturesAdapter) getPositionMode() PositionMode {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.positionMode == "" {
		return PositionModeOneWay
	}
	return a.positionMode
}

func stepPrecision(step float64) int {
	if step <= 0 {
		return 8
	}
	s := strconv.FormatFloat(step, 'f', -1, 64)
	if idx := strings.IndexByte(s, '.'); idx != -1 {
		return len(s) - idx - 1
	}
	return 0
}

// roundStepSize floors val to the nearest multiple of step.
// If step <= 0, val is returned unchanged.
func roundStepSize(val, step float64) float64 {
	if step <= 0 {
		return val
	}
	floored := math.Floor(val/step+1e-9) * step
	prec := stepPrecision(step)
	s := strconv.FormatFloat(floored, 'f', prec, 64)
	parsed, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return floored
	}
	return parsed
}

// roundTickSize rounds val to the nearest multiple of tick.
// If tick <= 0, val is returned unchanged.
func roundTickSize(val, tick float64) float64 {
	if tick <= 0 {
		return val
	}
	rounded := math.Round(val/tick) * tick
	prec := stepPrecision(tick)
	s := strconv.FormatFloat(rounded, 'f', prec, 64)
	parsed, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return rounded
	}
	return parsed
}

func (a *BinanceFuturesAdapter) ensureSymbolFilters(ctx context.Context, symbol string) (symbolFilters, error) {
	a.mu.RLock()
	if f, ok := a.filtersCache[symbol]; ok && time.Since(a.filtersFetchedAt) < time.Hour {
		a.mu.RUnlock()
		return f, nil
	}
	a.mu.RUnlock()

	info, err := a.client.NewExchangeInfoService().Do(ctx)
	if err != nil {
		// Graceful degradation when ExchangeInfo cannot be reached (e.g. mock test server)
		return symbolFilters{}, err
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	a.filtersFetchedAt = time.Now()
	for _, s := range info.Symbols {
		var f symbolFilters
		if lot := s.LotSizeFilter(); lot != nil {
			if step, err := strconv.ParseFloat(lot.StepSize, 64); err == nil {
				f.stepSize = step
			}
		}
		if price := s.PriceFilter(); price != nil {
			if tick, err := strconv.ParseFloat(price.TickSize, 64); err == nil {
				f.tickSize = tick
			}
		}
		a.filtersCache[s.Symbol] = f
	}
	return a.filtersCache[symbol], nil
}

func (a *BinanceFuturesAdapter) formatQuantity(ctx context.Context, symbol string, qty float64) string {
	filt, err := a.ensureSymbolFilters(ctx, symbol)
	if err != nil || filt.stepSize <= 0 {
		return formatFloat(qty)
	}
	rounded := roundStepSize(qty, filt.stepSize)
	prec := stepPrecision(filt.stepSize)
	return strconv.FormatFloat(rounded, 'f', prec, 64)
}

func (a *BinanceFuturesAdapter) formatPrice(ctx context.Context, symbol string, price float64) string {
	filt, err := a.ensureSymbolFilters(ctx, symbol)
	if err != nil || filt.tickSize <= 0 {
		return formatFloat(price)
	}
	rounded := roundTickSize(price, filt.tickSize)
	prec := stepPrecision(filt.tickSize)
	return strconv.FormatFloat(rounded, 'f', prec, 64)
}

func (a *BinanceFuturesAdapter) PlaceOrder(ctx context.Context, order PlaceOrderRequest) (OrderResult, error) {
	svc := a.client.NewCreateOrderService().
		NewOrderResponseType(futures.NewOrderRespTypeRESULT).
		Symbol(order.Symbol).
		Side(toFuturesSide(order.Side)).
		Quantity(a.formatQuantity(ctx, order.Symbol, order.Quantity))

	if order.ClientOrderID != "" {
		svc = svc.NewClientOrderID(order.ClientOrderID)
	}

	posMode := a.getPositionMode()
	if posMode == PositionModeHedge {
		var posSide futures.PositionSideType
		switch order.PositionSide {
		case PositionSideShort:
			posSide = futures.PositionSideTypeShort
		case PositionSideLong:
			posSide = futures.PositionSideTypeLong
		default: // empty or PositionSideBoth: infer leg
			if (order.Side == SideBuy && !order.ReduceOnly) || (order.Side == SideSell && order.ReduceOnly) {
				posSide = futures.PositionSideTypeLong
			} else {
				posSide = futures.PositionSideTypeShort
			}
		}
		svc = svc.PositionSide(posSide)
		// NEVER call .ReduceOnly(true) in Hedge Mode
	} else {
		svc = svc.PositionSide(futures.PositionSideTypeBoth)
		if order.ReduceOnly || order.Type == OrderTypeStopMarket {
			svc = svc.ReduceOnly(true)
		}
	}

	if order.Type == OrderTypeStopMarket {
		svc = svc.Type(futures.OrderTypeStopMarket).
			StopPrice(a.formatPrice(ctx, order.Symbol, order.StopPrice)).
			WorkingType(futures.WorkingTypeMarkPrice).
			PriceProtect(true)
	} else {
		svc = svc.Type(futures.OrderTypeMarket)
	}

	res, err := svc.Do(ctx)
	if err != nil {
		return OrderResult{}, classifyFuturesPlaceOrderError(err)
	}
	return futuresOrderResultFromCreate(res), nil
}

func (a *BinanceFuturesAdapter) CancelOrder(ctx context.Context, symbol, orderID string) error {
	id, err := strconv.ParseInt(orderID, 10, 64)
	if err != nil {
		return fmt.Errorf("exchange: invalid order id %q: %w", orderID, err)
	}
	_, err = a.client.NewCancelOrderService().Symbol(symbol).OrderID(id).Do(ctx)
	if err != nil {
		return classifyFuturesLookupError(err)
	}
	return nil
}

func (a *BinanceFuturesAdapter) GetOrder(ctx context.Context, symbol, orderID string) (OrderResult, error) {
	id, err := strconv.ParseInt(orderID, 10, 64)
	if err != nil {
		return OrderResult{}, fmt.Errorf("exchange: invalid order id %q: %w", orderID, err)
	}
	res, err := a.client.NewGetOrderService().Symbol(symbol).OrderID(id).Do(ctx)
	if err != nil {
		return OrderResult{}, classifyFuturesLookupError(err)
	}
	return futuresOrderResultFromOrder(res), nil
}

func (a *BinanceFuturesAdapter) ListKline(ctx context.Context, symbol string, interval string, limit int) ([]Candle, error) {
	klines, err := a.client.NewKlinesService().Symbol(symbol).Interval(interval).Limit(limit).Do(ctx)
	if err != nil {
		return nil, err
	}
	candles := make([]Candle, 0, len(klines))
	for _, k := range klines {
		c, err := futuresCandleFromKline(symbol, interval, k)
		if err != nil {
			return nil, err
		}
		candles = append(candles, c)
	}
	return candles, nil
}

func (a *BinanceFuturesAdapter) ListKlineRange(ctx context.Context, symbol, interval string, startTime, endTime time.Time, limit int) ([]Candle, error) {
	klines, err := a.client.NewKlinesService().
		Symbol(symbol).
		Interval(interval).
		StartTime(startTime.UnixMilli()).
		EndTime(endTime.UnixMilli()).
		Limit(limit).
		Do(ctx)
	if err != nil {
		return nil, err
	}
	candles := make([]Candle, 0, len(klines))
	for _, k := range klines {
		c, err := futuresCandleFromKline(symbol, interval, k)
		if err != nil {
			return nil, err
		}
		candles = append(candles, c)
	}
	return candles, nil
}

func (a *BinanceFuturesAdapter) ListTickerPrices(ctx context.Context, symbol string) ([]TickerPrice, error) {
	svc := a.client.NewListPricesService()
	if symbol != "" {
		svc = svc.Symbol(symbol)
	}
	prices, err := svc.Do(ctx)
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

func (a *BinanceFuturesAdapter) GetAccountBalance(ctx context.Context) (AccountBalance, error) {
	account, err := a.client.NewGetAccountService().Do(ctx)
	if err != nil {
		return AccountBalance{}, err
	}
	for _, asset := range account.Assets {
		if asset.Asset != quoteAssetForAccountBalance {
			continue
		}
		free, _ := strconv.ParseFloat(asset.AvailableBalance, 64)
		initMargin, _ := strconv.ParseFloat(asset.InitialMargin, 64)
		openOrderMargin, _ := strconv.ParseFloat(asset.OpenOrderInitialMargin, 64)
		locked := initMargin + openOrderMargin
		return AccountBalance{Asset: quoteAssetForAccountBalance, Free: free, Locked: locked}, nil
	}
	return AccountBalance{Asset: quoteAssetForAccountBalance}, nil
}

func (a *BinanceFuturesAdapter) SubscribeKline(ctx context.Context, symbol, interval string) (<-chan Candle, error) {
	out := make(chan Candle)

	handler := func(event *futures.WsKlineEvent) {
		if !event.Kline.IsFinal {
			return
		}
		c, err := futuresCandleFromWsKline(symbol, interval, event.Kline)
		if err != nil {
			return
		}
		select {
		case out <- c:
		case <-ctx.Done():
		}
	}

	errHandler := func(err error) {}

	doneC, stopC, err := futures.WsKlineServe(symbol, interval, handler, errHandler)
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

func (a *BinanceFuturesAdapter) SubscribeBookTicker(ctx context.Context, symbol string) (<-chan BookTicker, error) {
	out := make(chan BookTicker)

	handler := func(event *futures.WsBookTickerEvent) {
		bidPrice, err := strconv.ParseFloat(event.BestBidPrice, 64)
		if err != nil {
			return
		}
		askPrice, err := strconv.ParseFloat(event.BestAskPrice, 64)
		if err != nil {
			return
		}
		bidQty, _ := strconv.ParseFloat(event.BestBidQty, 64)
		askQty, _ := strconv.ParseFloat(event.BestAskQty, 64)

		bt := BookTicker{
			Symbol:       event.Symbol,
			BestBidPrice: bidPrice,
			BestBidQty:   bidQty,
			BestAskPrice: askPrice,
			BestAskQty:   askQty,
			Timestamp:    time.Now().UTC(),
		}
		select {
		case out <- bt:
		case <-ctx.Done():
		}
	}

	errHandler := func(err error) {}

	doneC, stopC, err := futures.WsBookTickerServe(symbol, handler, errHandler)
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

// SetLeverage changes the initial leverage for symbol. Caches applied leverage to prevent redundant API calls.
func (a *BinanceFuturesAdapter) SetLeverage(ctx context.Context, symbol string, leverage int) error {
	if leverage <= 0 {
		leverage = 1
	}
	a.mu.RLock()
	cached, ok := a.leverageCache[symbol]
	a.mu.RUnlock()
	if ok && cached == leverage {
		return nil
	}

	_, err := a.client.NewChangeLeverageService().Symbol(symbol).Leverage(leverage).Do(ctx)
	if err != nil {
		return err
	}

	a.mu.Lock()
	a.leverageCache[symbol] = leverage
	a.mu.Unlock()
	return nil
}

// SetMarginType changes the margin mode for symbol. Treats -4046 (no change needed) as success.
func (a *BinanceFuturesAdapter) SetMarginType(ctx context.Context, symbol string, marginMode MarginMode) error {
	a.mu.RLock()
	cached, ok := a.marginCache[symbol]
	a.mu.RUnlock()
	if ok && cached == marginMode {
		return nil
	}

	var mt futures.MarginType
	switch marginMode {
	case MarginModeCross:
		mt = futures.MarginTypeCrossed
	case MarginModeIsolated:
		mt = futures.MarginTypeIsolated
	default:
		mt = futures.MarginTypeIsolated
	}

	err := a.client.NewChangeMarginTypeService().Symbol(symbol).MarginType(mt).Do(ctx)
	if err != nil {
		var apiErr *common.APIError
		if errors.As(err, &apiErr) && apiErr.Code == futuresErrCodeMarginTypeNoChange {
			err = nil
		}
	}
	if err != nil {
		return err
	}

	a.mu.Lock()
	a.marginCache[symbol] = marginMode
	a.mu.Unlock()
	return nil
}

// SetPositionMode switches position mode (One-Way vs Hedge). Treats -4059 (no change needed) as success.
func (a *BinanceFuturesAdapter) SetPositionMode(ctx context.Context, mode PositionMode) error {
	dualSide := (mode == PositionModeHedge)
	err := a.client.NewChangePositionModeService().DualSide(dualSide).Do(ctx)
	if err != nil {
		var apiErr *common.APIError
		if errors.As(err, &apiErr) && apiErr.Code == futuresErrCodePositionSideNoChange {
			err = nil
		}
	}
	if err != nil {
		return err
	}

	a.mu.Lock()
	a.positionMode = mode
	a.mu.Unlock()
	return nil
}

// GetPositionMode queries the user's current Futures position mode.
func (a *BinanceFuturesAdapter) GetPositionMode(ctx context.Context) (PositionMode, error) {
	res, err := a.client.NewGetPositionModeService().Do(ctx)
	if err != nil {
		return "", err
	}
	mode := PositionModeOneWay
	if res.DualSidePosition {
		mode = PositionModeHedge
	}
	a.mu.Lock()
	a.positionMode = mode
	a.mu.Unlock()
	return mode, nil
}

// GetPositionRisk returns position risk for symbol and target side.
func (a *BinanceFuturesAdapter) GetPositionRisk(ctx context.Context, symbol string, side PositionSide) (PositionRisk, error) {
	risks, err := a.client.NewGetPositionRiskService().Symbol(symbol).Do(ctx)
	if err != nil {
		return PositionRisk{}, err
	}

	var matched *futures.PositionRisk
	isHedge := (side == PositionSideLong || side == PositionSideShort)
	for _, r := range risks {
		if !strings.EqualFold(r.Symbol, symbol) {
			continue
		}
		if isHedge {
			if strings.EqualFold(r.PositionSide, string(side)) {
				matched = r
				break
			}
		} else {
			if strings.EqualFold(r.PositionSide, "BOTH") {
				matched = r
				break
			}
			if matched == nil {
				matched = r
			}
		}
	}

	if matched == nil {
		return PositionRisk{
			Symbol:       symbol,
			PositionSide: side,
		}, nil
	}

	posAmt, _ := strconv.ParseFloat(matched.PositionAmt, 64)
	entryPrice, _ := strconv.ParseFloat(matched.EntryPrice, 64)
	markPrice, _ := strconv.ParseFloat(matched.MarkPrice, 64)
	unRealizedProfit, _ := strconv.ParseFloat(matched.UnRealizedProfit, 64)
	liquidationPrice, _ := strconv.ParseFloat(matched.LiquidationPrice, 64)
	leverage, _ := strconv.Atoi(matched.Leverage)
	isolatedMargin, _ := strconv.ParseFloat(matched.IsolatedMargin, 64)

	return PositionRisk{
		Symbol:           matched.Symbol,
		PositionSide:     PositionSide(strings.ToUpper(matched.PositionSide)),
		PositionAmt:      posAmt,
		EntryPrice:       entryPrice,
		MarkPrice:        markPrice,
		UnRealizedProfit: unRealizedProfit,
		LiquidationPrice: liquidationPrice,
		Leverage:         leverage,
		MarginType:       matched.MarginType,
		IsolatedMargin:   isolatedMargin,
	}, nil
}

// GetMarkPrice returns the current mark price, funding rate, and next funding time for symbol.
func (a *BinanceFuturesAdapter) GetMarkPrice(ctx context.Context, symbol string) (float64, float64, time.Time, error) {
	res, err := a.client.NewPremiumIndexService().Symbol(symbol).Do(ctx)
	if err != nil {
		return 0, 0, time.Time{}, err
	}
	if len(res) == 0 {
		return 0, 0, time.Time{}, fmt.Errorf("exchange: no premium index data for %s", symbol)
	}
	p := res[0]
	markPrice, err := strconv.ParseFloat(p.MarkPrice, 64)
	if err != nil {
		return 0, 0, time.Time{}, fmt.Errorf("exchange: parsing mark price: %w", err)
	}
	fundingRate, err := strconv.ParseFloat(p.LastFundingRate, 64)
	if err != nil {
		return 0, 0, time.Time{}, fmt.Errorf("exchange: parsing last funding rate: %w", err)
	}
	nextFundingTime := time.UnixMilli(p.NextFundingTime).UTC()
	return markPrice, fundingRate, nextFundingTime, nil
}

func toFuturesSide(side OrderSide) futures.SideType {
	if side == SideSell {
		return futures.SideTypeSell
	}
	return futures.SideTypeBuy
}

func classifyFuturesPlaceOrderError(err error) error {
	var apiErr *common.APIError
	if !errors.As(err, &apiErr) {
		return err
	}
	switch apiErr.Code {
	case futuresErrCodeInsufficientMargin, futuresErrCodeInsufficientBalance:
		return fmt.Errorf("%w: %s", ErrInsufficientBalance, apiErr.Message)
	case futuresErrCodeReduceOnlyRejected:
		return fmt.Errorf("%w: %s", ErrReduceOnlyRejected, apiErr.Message)
	case futuresErrCodeWouldImmediatelyTrigger, futuresErrCodeMinNotional,
		futuresErrCodePrecisionOverMax, futuresErrCodeFilterFailure, futuresErrCodeInvalidQuantity:
		return fmt.Errorf("%w: %s", ErrFilterViolation, apiErr.Message)
	default:
		return fmt.Errorf("%w: %s", ErrOrderRejected, apiErr.Message)
	}
}

func classifyFuturesLookupError(err error) error {
	var apiErr *common.APIError
	if errors.As(err, &apiErr) && apiErr.Code == futuresErrCodeUnknownOrder {
		return fmt.Errorf("%w: %s", ErrOrderNotFound, apiErr.Message)
	}
	return err
}

func mapFuturesStatus(s futures.OrderStatusType) OrderStatus {
	switch s {
	case futures.OrderStatusTypeNew:
		return OrderStatusNew
	case futures.OrderStatusTypeFilled:
		return OrderStatusFilled
	case futures.OrderStatusTypePartiallyFilled:
		return OrderStatusPartiallyFilled
	case futures.OrderStatusTypeRejected:
		return OrderStatusRejected
	case futures.OrderStatusTypeExpired:
		return OrderStatusExpired
	case futures.OrderStatusTypeCanceled:
		return OrderStatusCanceled
	default:
		return OrderStatusRejected
	}
}

func futuresAvgFillPrice(executedQty, cumQuote float64, avgPriceStr, stopPriceStr, priceStr string) float64 {
	if p, err := strconv.ParseFloat(avgPriceStr, 64); err == nil && p > 0 {
		return p
	}
	if executedQty > 0 && cumQuote > 0 {
		return cumQuote / executedQty
	}
	if p, err := strconv.ParseFloat(stopPriceStr, 64); err == nil && p > 0 {
		return p
	}
	if p, err := strconv.ParseFloat(priceStr, 64); err == nil && p > 0 {
		return p
	}
	return 0
}

func futuresOrderResultFromCreate(res *futures.CreateOrderResponse) OrderResult {
	executedQty, _ := strconv.ParseFloat(res.ExecutedQuantity, 64)
	cumQuote, _ := strconv.ParseFloat(res.CumQuote, 64)
	avgPrice := futuresAvgFillPrice(executedQty, cumQuote, res.AvgPrice, res.StopPrice, res.Price)
	return OrderResult{
		BrokerOrderID: strconv.FormatInt(res.OrderID, 10),
		ClientOrderID: res.ClientOrderID,
		Status:        mapFuturesStatus(res.Status),
		ExecutedQty:   executedQty,
		AvgFillPrice:  avgPrice,
		FilledAt:      time.UnixMilli(res.UpdateTime).UTC(),
	}
}

func futuresOrderResultFromOrder(res *futures.Order) OrderResult {
	executedQty, _ := strconv.ParseFloat(res.ExecutedQuantity, 64)
	cumQuote, _ := strconv.ParseFloat(res.CumQuote, 64)
	avgPrice := futuresAvgFillPrice(executedQty, cumQuote, res.AvgPrice, res.StopPrice, res.Price)
	return OrderResult{
		BrokerOrderID: strconv.FormatInt(res.OrderID, 10),
		ClientOrderID: res.ClientOrderID,
		Status:        mapFuturesStatus(res.Status),
		ExecutedQty:   executedQty,
		AvgFillPrice:  avgPrice,
		FilledAt:      time.UnixMilli(res.UpdateTime).UTC(),
	}
}

func futuresCandleFromKline(symbol, interval string, k *futures.Kline) (Candle, error) {
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

func futuresCandleFromWsKline(symbol, interval string, k futures.WsKline) (Candle, error) {
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
