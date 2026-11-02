package exchange

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"go-trade-bot/internal/configuration"
	"go-trade-bot/internal/futuresmath"

	"github.com/adshao/go-binance/v2"
	"github.com/adshao/go-binance/v2/common"
	"github.com/adshao/go-binance/v2/futures"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrecisionHelpers(t *testing.T) {
	// Step size rounding
	assert.Equal(t, 0.123, roundStepSize(0.12345, 0.001))
	assert.Equal(t, 0.12, roundStepSize(0.12345, 0.01))
	assert.Equal(t, 1.0, roundStepSize(1.5, 1.0))
	assert.Equal(t, 5.5, roundStepSize(5.5, 0))
	assert.Equal(t, 5.5, roundStepSize(5.5, -1))

	// Tick size rounding
	assert.Equal(t, 100.12, roundTickSize(100.12345, 0.01))
	assert.Equal(t, 100.1, roundTickSize(100.12345, 0.1))
	assert.Equal(t, 50.5, roundTickSize(50.5, 0))
	assert.Equal(t, 50.5, roundTickSize(50.5, -0.5))

	// Step precision helper
	assert.Equal(t, 3, stepPrecision(0.001))
	assert.Equal(t, 2, stepPrecision(0.01))
	assert.Equal(t, 0, stepPrecision(1.0))
	assert.Equal(t, 8, stepPrecision(0))
}

// 1. Verify PlaceOrder MARKET order sends newOrderRespType=RESULT, positionSide=BOTH in One-Way mode, and parses avgPrice and executedQty.
func TestPlaceOrder_Market_OneWay(t *testing.T) {
	var capturedRespType, capturedPosSide, capturedType, capturedSide, capturedSymbol, capturedQty string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		if r.URL.Path == "/fapi/v1/order" && r.Method == http.MethodPost {
			capturedRespType = r.Form.Get("newOrderRespType")
			capturedPosSide = r.Form.Get("positionSide")
			capturedType = r.Form.Get("type")
			capturedSide = r.Form.Get("side")
			capturedSymbol = r.Form.Get("symbol")
			capturedQty = r.Form.Get("quantity")

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(futures.CreateOrderResponse{
				Symbol:           "BTCUSDT",
				OrderID:          1001,
				ClientOrderID:    "client-1",
				Status:           futures.OrderStatusTypeFilled,
				ExecutedQuantity: "1.5",
				CumQuote:         "75000",
				AvgPrice:         "50000.0",
				PositionSide:     futures.PositionSideTypeBoth,
				UpdateTime:       1700000000000,
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	adapter, err := NewBinanceFuturesAdapter(&configuration.Configuration{
		Broker:       configuration.Broker{ApiKey: "apiKey", ApiSecret: "apiSecret"},
		PositionMode: "one_way",
	})
	require.NoError(t, err)
	adapter.client.BaseURL = srv.URL

	res, err := adapter.PlaceOrder(context.Background(), PlaceOrderRequest{
		Symbol:        "BTCUSDT",
		Side:          SideBuy,
		Type:          OrderTypeMarket,
		Quantity:      1.5,
		ClientOrderID: "client-1",
	})
	require.NoError(t, err)

	assert.Equal(t, "RESULT", capturedRespType)
	assert.Equal(t, "BOTH", capturedPosSide)
	assert.Equal(t, "MARKET", capturedType)
	assert.Equal(t, "BUY", capturedSide)
	assert.Equal(t, "BTCUSDT", capturedSymbol)
	assert.Equal(t, "1.5", capturedQty)

	assert.Equal(t, "1001", res.BrokerOrderID)
	assert.Equal(t, "client-1", res.ClientOrderID)
	assert.Equal(t, OrderStatusFilled, res.Status)
	assert.Equal(t, 1.5, res.ExecutedQty)
	assert.Equal(t, 50000.0, res.AvgFillPrice)
	assert.Equal(t, time.UnixMilli(1700000000000).UTC(), res.FilledAt)
}

// 2. Verify PlaceOrder in Hedge Mode (PositionModeHedge) sends positionSide=LONG or positionSide=SHORT and omits reduceOnly.
func TestPlaceOrder_HedgeMode_OmitsReduceOnly(t *testing.T) {
	var capturedPosSide string
	var hasReduceOnly bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		if r.URL.Path == "/fapi/v1/order" && r.Method == http.MethodPost {
			capturedPosSide = r.Form.Get("positionSide")
			hasReduceOnly = r.Form.Has("reduceOnly") && r.Form.Get("reduceOnly") != ""

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(futures.CreateOrderResponse{
				Symbol:           "BTCUSDT",
				OrderID:          2001,
				Status:           futures.OrderStatusTypeFilled,
				ExecutedQuantity: "1.0",
				AvgPrice:         "50000.0",
				UpdateTime:       1700000000000,
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	adapter, err := NewBinanceFuturesAdapter(&configuration.Configuration{
		Broker:       configuration.Broker{ApiKey: "apiKey", ApiSecret: "apiSecret"},
		PositionMode: "hedge",
	})
	require.NoError(t, err)
	adapter.client.BaseURL = srv.URL

	// Explicit PositionSideShort with ReduceOnly requested -> reduceOnly MUST be omitted in Hedge Mode
	_, err = adapter.PlaceOrder(context.Background(), PlaceOrderRequest{
		Symbol:       "BTCUSDT",
		Side:         SideBuy,
		Type:         OrderTypeMarket,
		Quantity:     1.0,
		PositionSide: PositionSideShort,
		ReduceOnly:   true,
	})
	require.NoError(t, err)
	assert.Equal(t, "SHORT", capturedPosSide)
	assert.False(t, hasReduceOnly, "reduceOnly must be omitted in Hedge mode")

	// Explicit PositionSideLong
	_, err = adapter.PlaceOrder(context.Background(), PlaceOrderRequest{
		Symbol:       "BTCUSDT",
		Side:         SideSell,
		Type:         OrderTypeMarket,
		Quantity:     1.0,
		PositionSide: PositionSideLong,
		ReduceOnly:   true,
	})
	require.NoError(t, err)
	assert.Equal(t, "LONG", capturedPosSide)
	assert.False(t, hasReduceOnly, "reduceOnly must be omitted in Hedge mode")

	// Inferred leg when PositionSide is empty: Buy entry -> LONG
	_, err = adapter.PlaceOrder(context.Background(), PlaceOrderRequest{
		Symbol:     "BTCUSDT",
		Side:       SideBuy,
		Type:       OrderTypeMarket,
		Quantity:   1.0,
		ReduceOnly: false,
	})
	require.NoError(t, err)
	assert.Equal(t, "LONG", capturedPosSide)
	assert.False(t, hasReduceOnly)

	// Inferred leg when PositionSide is empty: Sell entry -> SHORT
	_, err = adapter.PlaceOrder(context.Background(), PlaceOrderRequest{
		Symbol:     "BTCUSDT",
		Side:       SideSell,
		Type:       OrderTypeMarket,
		Quantity:   1.0,
		ReduceOnly: false,
	})
	require.NoError(t, err)
	assert.Equal(t, "SHORT", capturedPosSide)
	assert.False(t, hasReduceOnly)
}

// 3. Verify PlaceOrder STOP_MARKET sends type=STOP_MARKET, stopPrice, workingType=MARK_PRICE, priceProtect=true, and reduceOnly=true (in One-Way mode).
func TestPlaceOrder_StopMarket_OneWay(t *testing.T) {
	var capturedType, capturedStopPrice, capturedWorkingType, capturedPriceProtect, capturedReduceOnly string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		if r.URL.Path == "/fapi/v1/order" && r.Method == http.MethodPost {
			capturedType = r.Form.Get("type")
			capturedStopPrice = r.Form.Get("stopPrice")
			capturedWorkingType = r.Form.Get("workingType")
			capturedPriceProtect = r.Form.Get("priceProtect")
			capturedReduceOnly = r.Form.Get("reduceOnly")

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(futures.CreateOrderResponse{
				Symbol:        "BTCUSDT",
				OrderID:       3001,
				Status:        futures.OrderStatusTypeNew,
				Price:         "0",
				StopPrice:     "48000.0",
				OrigQuantity:  "0.5",
				Type:          futures.OrderTypeStopMarket,
				Side:          futures.SideTypeSell,
				WorkingType:   futures.WorkingTypeMarkPrice,
				PriceProtect:  true,
				ReduceOnly:    true,
				UpdateTime:    1700000000000,
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	adapter, err := NewBinanceFuturesAdapter(&configuration.Configuration{
		Broker:       configuration.Broker{ApiKey: "apiKey", ApiSecret: "apiSecret"},
		PositionMode: "one_way",
	})
	require.NoError(t, err)
	adapter.client.BaseURL = srv.URL

	res, err := adapter.PlaceOrder(context.Background(), PlaceOrderRequest{
		Symbol:    "BTCUSDT",
		Side:      SideSell,
		Type:      OrderTypeStopMarket,
		Quantity:  0.5,
		StopPrice: 48000.0,
	})
	require.NoError(t, err)

	assert.Equal(t, "STOP_MARKET", capturedType)
	assert.Equal(t, "48000", capturedStopPrice)
	assert.Equal(t, "MARK_PRICE", capturedWorkingType)
	assert.Equal(t, "true", capturedPriceProtect)
	assert.Equal(t, "true", capturedReduceOnly)

	assert.Equal(t, "3001", res.BrokerOrderID)
	assert.Equal(t, OrderStatusNew, res.Status)
	assert.Equal(t, 48000.0, res.AvgFillPrice) // resting stop falls back to stopPrice
}

// 4. Verify SetMarginType treats -4046 as nil (idempotent success) and caches per symbol.
func TestSetMarginType_IdempotentAndCaching(t *testing.T) {
	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		if r.URL.Path == "/fapi/v1/marginType" && r.Method == http.MethodPost {
			callCount.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": futuresErrCodeMarginTypeNoChange, // -4046
				"msg":  "No need to change margin type.",
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	adapter, err := NewBinanceFuturesAdapter(&configuration.Configuration{
		Broker: configuration.Broker{ApiKey: "apiKey", ApiSecret: "apiSecret"},
	})
	require.NoError(t, err)
	adapter.client.BaseURL = srv.URL

	// First call receives -4046, which should be treated as nil (success) and cached
	err = adapter.SetMarginType(context.Background(), "BTCUSDT", MarginModeIsolated)
	assert.NoError(t, err)
	assert.Equal(t, int32(1), callCount.Load())

	// Second call with same symbol and MarginMode should hit cache and NOT invoke HTTP endpoint
	err = adapter.SetMarginType(context.Background(), "BTCUSDT", MarginModeIsolated)
	assert.NoError(t, err)
	assert.Equal(t, int32(1), callCount.Load(), "cached margin type should not make another HTTP request")
}

// 5. Verify SetPositionMode treats -4059 as nil (idempotent success).
func TestSetPositionMode_Idempotent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		if r.URL.Path == "/fapi/v1/positionSide/dual" && r.Method == http.MethodPost {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": futuresErrCodePositionSideNoChange, // -4059
				"msg":  "No need to change position side.",
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	adapter, err := NewBinanceFuturesAdapter(&configuration.Configuration{
		Broker: configuration.Broker{ApiKey: "apiKey", ApiSecret: "apiSecret"},
	})
	require.NoError(t, err)
	adapter.client.BaseURL = srv.URL

	err = adapter.SetPositionMode(context.Background(), PositionModeHedge)
	assert.NoError(t, err)
	assert.Equal(t, PositionModeHedge, adapter.getPositionMode())
}

// 6. Verify error classification for -2019, -2022, -2021, -4164, -1111, -2011, etc.
func TestClassifyFuturesErrors(t *testing.T) {
	// Insufficient balance / margin
	err := classifyFuturesPlaceOrderError(&common.APIError{Code: futuresErrCodeInsufficientMargin, Message: "insufficient margin"})
	assert.ErrorIs(t, err, ErrInsufficientBalance)
	err = classifyFuturesPlaceOrderError(&common.APIError{Code: futuresErrCodeInsufficientBalance, Message: "insufficient balance"})
	assert.ErrorIs(t, err, ErrInsufficientBalance)

	// Reduce only rejected
	err = classifyFuturesPlaceOrderError(&common.APIError{Code: futuresErrCodeReduceOnlyRejected, Message: "reduce only rejected"})
	assert.ErrorIs(t, err, ErrReduceOnlyRejected)

	// Filter violations
	err = classifyFuturesPlaceOrderError(&common.APIError{Code: futuresErrCodeWouldImmediatelyTrigger, Message: "immediately trigger"})
	assert.ErrorIs(t, err, ErrFilterViolation)
	err = classifyFuturesPlaceOrderError(&common.APIError{Code: futuresErrCodeMinNotional, Message: "min notional"})
	assert.ErrorIs(t, err, ErrFilterViolation)
	err = classifyFuturesPlaceOrderError(&common.APIError{Code: futuresErrCodePrecisionOverMax, Message: "precision over max"})
	assert.ErrorIs(t, err, ErrFilterViolation)
	err = classifyFuturesPlaceOrderError(&common.APIError{Code: futuresErrCodeFilterFailure, Message: "filter failure"})
	assert.ErrorIs(t, err, ErrFilterViolation)
	err = classifyFuturesPlaceOrderError(&common.APIError{Code: futuresErrCodeInvalidQuantity, Message: "invalid quantity"})
	assert.ErrorIs(t, err, ErrFilterViolation)

	// Generic rejection
	err = classifyFuturesPlaceOrderError(&common.APIError{Code: -9999, Message: "generic error"})
	assert.ErrorIs(t, err, ErrOrderRejected)

	// Non-API error passes through
	baseErr := errors.New("timeout")
	err = classifyFuturesPlaceOrderError(baseErr)
	assert.Equal(t, baseErr, err)

	// Lookup errors
	err = classifyFuturesLookupError(&common.APIError{Code: futuresErrCodeUnknownOrder, Message: "unknown order"})
	assert.ErrorIs(t, err, ErrOrderNotFound)

	otherErr := errors.New("conn reset")
	assert.Equal(t, otherErr, classifyFuturesLookupError(otherErr))
}

// 7. Verify NewExchangeClientFromConfig routes MarketType="futures" (Testnet=false and Testnet=true) and MarketType="spot" accurately.
func TestNewExchangeClientFromConfig_Routing(t *testing.T) {
	// Futures live
	cfgFuturesLive := &configuration.Configuration{
		MarketType: "futures",
		Testnet:    false,
		Broker:     configuration.Broker{ApiKey: "key", ApiSecret: "secret"},
	}
	client, err := NewExchangeClientFromConfig(cfgFuturesLive)
	require.NoError(t, err)
	_, ok := client.(*BinanceFuturesAdapter)
	assert.True(t, ok, "expected *BinanceFuturesAdapter, got %T", client)

	// Futures testnet
	cfgFuturesTestnet := &configuration.Configuration{
		MarketType: "futures",
		Testnet:    true,
		Broker:     configuration.Broker{TestnetApiKey: "tkey", TestnetApiSecret: "tsecret"},
	}
	client, err = NewExchangeClientFromConfig(cfgFuturesTestnet)
	require.NoError(t, err)
	_, ok = client.(*BinanceFuturesTestnetAdapter)
	assert.True(t, ok, "expected *BinanceFuturesTestnetAdapter, got %T", client)
	assert.True(t, futures.UseTestnet)

	// Futures via Broker.MarketType fallback
	cfgBrokerFutures := &configuration.Configuration{
		Broker: configuration.Broker{
			MarketType: "futures",
			ApiKey:     "key",
			ApiSecret:  "secret",
		},
	}
	client, err = NewExchangeClientFromConfig(cfgBrokerFutures)
	require.NoError(t, err)
	_, ok = client.(*BinanceFuturesAdapter)
	assert.True(t, ok, "expected *BinanceFuturesAdapter from Broker.MarketType")

	// Spot live
	cfgSpotLive := &configuration.Configuration{
		MarketType: "spot",
		Testnet:    false,
		Broker:     configuration.Broker{ApiKey: "key", ApiSecret: "secret"},
	}
	client, err = NewExchangeClientFromConfig(cfgSpotLive)
	require.NoError(t, err)
	_, ok = client.(*BinanceAdapter)
	assert.True(t, ok, "expected *BinanceAdapter, got %T", client)

	// Spot testnet
	cfgSpotTestnet := &configuration.Configuration{
		MarketType: "spot",
		Testnet:    true,
		Broker:     configuration.Broker{TestnetApiKey: "tkey", TestnetApiSecret: "tsecret"},
	}
	client, err = NewExchangeClientFromConfig(cfgSpotTestnet)
	require.NoError(t, err)
	_, ok = client.(*BinanceTestnetAdapter)
	assert.True(t, ok, "expected *BinanceTestnetAdapter, got %T", client)
	assert.True(t, binance.UseTestnet)
}

// 8. Verify DryRunExchange TriggerStopWithSide (both SideSell and SideBuy) and FuturesClient in-memory methods never hit the network.
func TestDryRunExchange_FuturesAndTriggerStopWithSide(t *testing.T) {
	inner := &mockInner{}
	d := NewDryRunExchange(inner, 1.0, nil) // 1% slippage
	ctx := context.Background()

	// TriggerStopWithSide Sell (stopPrice 100 -> 99.0 fill)
	at := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	sellFill := d.TriggerStopWithSide("stop-sell", SideSell, 2.0, 100.0, at)
	assert.Equal(t, 99.0, sellFill.AvgFillPrice)
	assert.Equal(t, 2.0, sellFill.ExecutedQty)
	assert.Equal(t, OrderStatusFilled, sellFill.Status)

	// TriggerStopWithSide Buy (stopPrice 100 -> 101.0 fill)
	buyFill := d.TriggerStopWithSide("stop-buy", SideBuy, 3.0, 100.0, at)
	assert.Equal(t, 101.0, buyFill.AvgFillPrice)
	assert.Equal(t, 3.0, buyFill.ExecutedQty)

	// TriggerStop delegates to TriggerStopWithSide(..., SideSell, ...)
	legacyFill := d.TriggerStop("stop-legacy", 1.0, 200.0, at)
	assert.Equal(t, 198.0, legacyFill.AvgFillPrice)

	// Pure in-memory FuturesClient methods
	require.NoError(t, d.SetLeverage(ctx, "BTCUSDT", 25))
	require.NoError(t, d.SetMarginType(ctx, "BTCUSDT", MarginModeCross))
	require.NoError(t, d.SetPositionMode(ctx, PositionModeHedge))

	posMode, err := d.GetPositionMode(ctx)
	require.NoError(t, err)
	assert.Equal(t, PositionModeHedge, posMode)

	risk, err := d.GetPositionRisk(ctx, "BTCUSDT", PositionSideLong)
	require.NoError(t, err)
	assert.Equal(t, "BTCUSDT", risk.Symbol)
	assert.Equal(t, PositionSideLong, risk.PositionSide)

	// GetMarkPrice fallback to ListTickerPrices when inner is Spot (not FuturesClient)
	inner.On("ListTickerPrices", ctx, "BTCUSDT").Return([]TickerPrice{{Symbol: "BTCUSDT", Price: 50000.0}}, nil).Once()
	markPrice, fundingRate, _, err := d.GetMarkPrice(ctx, "BTCUSDT")
	require.NoError(t, err)
	assert.Equal(t, 50000.0, markPrice)
	assert.Equal(t, futuresmath.DefaultFundingRate8h, fundingRate)

	// Assert no order mutations ever reached the inner client
	assertNoRealOrders(t, inner)
}

func TestSwappableExchangeClient_FuturesClient(t *testing.T) {
	inner := &mockInner{}
	s := NewSwappableExchangeClient(inner)
	ctx := context.Background()

	// Spot inner does not implement FuturesClient -> ErrFuturesNotSupported
	err := s.SetLeverage(ctx, "BTCUSDT", 10)
	assert.ErrorIs(t, err, ErrFuturesNotSupported)

	err = s.SetMarginType(ctx, "BTCUSDT", MarginModeIsolated)
	assert.ErrorIs(t, err, ErrFuturesNotSupported)

	err = s.SetPositionMode(ctx, PositionModeHedge)
	assert.ErrorIs(t, err, ErrFuturesNotSupported)

	_, err = s.GetPositionMode(ctx)
	assert.ErrorIs(t, err, ErrFuturesNotSupported)

	_, err = s.GetPositionRisk(ctx, "BTCUSDT", PositionSideBoth)
	assert.ErrorIs(t, err, ErrFuturesNotSupported)

	_, _, _, err = s.GetMarkPrice(ctx, "BTCUSDT")
	assert.ErrorIs(t, err, ErrFuturesNotSupported)

	// Now swap to DryRunExchange which implements FuturesClient
	dry := NewDryRunExchange(inner, 0, nil)
	s.Swap(dry)

	require.NoError(t, s.SetLeverage(ctx, "BTCUSDT", 15))
	require.NoError(t, s.SetMarginType(ctx, "BTCUSDT", MarginModeIsolated))
	require.NoError(t, s.SetPositionMode(ctx, PositionModeOneWay))

	pm, err := s.GetPositionMode(ctx)
	require.NoError(t, err)
	assert.Equal(t, PositionModeOneWay, pm)
}

func TestReadOnlyClient_FuturesClient(t *testing.T) {
	inner := &mockInner{}
	var blockedOps []string
	ro := NewReadOnlyClient(inner, func(op string) {
		blockedOps = append(blockedOps, op)
	})
	ctx := context.Background()

	// Mutating operations fail with ErrReadOnlyClient and record blocked op
	assert.ErrorIs(t, ro.SetLeverage(ctx, "BTCUSDT", 10), ErrReadOnlyClient)
	assert.ErrorIs(t, ro.SetMarginType(ctx, "BTCUSDT", MarginModeIsolated), ErrReadOnlyClient)
	assert.ErrorIs(t, ro.SetPositionMode(ctx, PositionModeHedge), ErrReadOnlyClient)
	assert.Equal(t, []string{"set_leverage", "set_margin_type", "set_position_mode"}, blockedOps)

	// Read operations return ErrFuturesNotSupported when inner is spot-only
	_, err := ro.GetPositionMode(ctx)
	assert.ErrorIs(t, err, ErrFuturesNotSupported)
	_, err = ro.GetPositionRisk(ctx, "BTCUSDT", PositionSideBoth)
	assert.ErrorIs(t, err, ErrFuturesNotSupported)
	_, _, _, err = ro.GetMarkPrice(ctx, "BTCUSDT")
	assert.ErrorIs(t, err, ErrFuturesNotSupported)
}

func TestBinanceFuturesAdapter_ReadMethods(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/fapi/v1/leverage":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(futures.SymbolLeverage{
				Symbol:   "BTCUSDT",
				Leverage: 10,
			})
		case "/fapi/v2/positionRisk":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode([]*futures.PositionRisk{
				{
					Symbol:           "BTCUSDT",
					PositionSide:     "BOTH",
					PositionAmt:      "0.5",
					EntryPrice:       "50000.0",
					MarkPrice:        "51000.0",
					UnRealizedProfit: "500.0",
					LiquidationPrice: "40000.0",
					Leverage:         "10",
					MarginType:       "isolated",
					IsolatedMargin:   "2500.0",
				},
			})
		case "/fapi/v1/premiumIndex":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode([]*futures.PremiumIndex{
				{
					Symbol:          "BTCUSDT",
					MarkPrice:       "50500.0",
					LastFundingRate: "0.0001",
					NextFundingTime: 1700000000000,
				},
			})
		case "/fapi/v2/account":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(&futures.Account{
				Assets: []*futures.AccountAsset{
					{
						Asset:                  "USDT",
						AvailableBalance:       "5000.0",
						InitialMargin:          "500.0",
						OpenOrderInitialMargin: "100.0",
					},
				},
			})
		case "/fapi/v1/ticker/price", "/fapi/v2/ticker/price":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode([]*futures.SymbolPrice{
				{
					Symbol: "BTCUSDT",
					Price:  "50100.0",
				},
			})
		case "/fapi/v1/klines":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode([][]any{
				{
					1700000000000, "50000.0", "51000.0", "49000.0", "50500.0", "100.0",
					1700000060000, "5050000.0", 100, "50.0", "2500000.0", "0",
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	adapter, err := NewBinanceFuturesAdapter(&configuration.Configuration{
		Broker: configuration.Broker{ApiKey: "apiKey", ApiSecret: "apiSecret"},
	})
	require.NoError(t, err)
	adapter.client.BaseURL = srv.URL
	ctx := context.Background()

	// SetLeverage
	err = adapter.SetLeverage(ctx, "BTCUSDT", 10)
	require.NoError(t, err)

	// GetPositionRisk
	risk, err := adapter.GetPositionRisk(ctx, "BTCUSDT", PositionSideBoth)
	require.NoError(t, err)
	assert.Equal(t, "BTCUSDT", risk.Symbol)
	assert.Equal(t, 0.5, risk.PositionAmt)
	assert.Equal(t, 50000.0, risk.EntryPrice)
	assert.Equal(t, 51000.0, risk.MarkPrice)
	assert.Equal(t, 500.0, risk.UnRealizedProfit)
	assert.Equal(t, 40000.0, risk.LiquidationPrice)
	assert.Equal(t, 10, risk.Leverage)
	assert.Equal(t, "isolated", risk.MarginType)
	assert.Equal(t, 2500.0, risk.IsolatedMargin)

	// GetMarkPrice
	markPrice, fundingRate, nextFundingTime, err := adapter.GetMarkPrice(ctx, "BTCUSDT")
	require.NoError(t, err)
	assert.Equal(t, 50500.0, markPrice)
	assert.Equal(t, 0.0001, fundingRate)
	assert.Equal(t, time.UnixMilli(1700000000000).UTC(), nextFundingTime)

	// GetAccountBalance
	bal, err := adapter.GetAccountBalance(ctx)
	require.NoError(t, err)
	assert.Equal(t, "USDT", bal.Asset)
	assert.Equal(t, 5000.0, bal.Free)
	assert.Equal(t, 600.0, bal.Locked) // 500 + 100

	// ListTickerPrices
	prices, err := adapter.ListTickerPrices(ctx, "BTCUSDT")
	require.NoError(t, err)
	require.Len(t, prices, 1)
	assert.Equal(t, 50100.0, prices[0].Price)

	// ListKline
	candles, err := adapter.ListKline(ctx, "BTCUSDT", "1m", 1)
	require.NoError(t, err)
	require.Len(t, candles, 1)
	assert.Equal(t, 50000.0, candles[0].Open)
	assert.Equal(t, 50500.0, candles[0].Close)
}
