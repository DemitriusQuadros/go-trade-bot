package mlgrpc_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"go-trade-bot/app/engine"
	enginemocks "go-trade-bot/app/engine/mocks"
	"go-trade-bot/app/entities"
	"go-trade-bot/app/strategies"
	"go-trade-bot/app/strategies/mlgrpc"
	"go-trade-bot/app/strategies/mlgrpc/mocks"
	signalmocks "go-trade-bot/app/usecase/signal/mocks"
	"go-trade-bot/internal/exchange"
	"go-trade-bot/internal/grpc/strategypb"
	"go-trade-bot/internal/indicators"
	"go-trade-bot/internal/memcache"
	"go-trade-bot/internal/notifier"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func testContext() strategies.Context {
	return strategies.Context{
		Candles: []exchange.Candle{
			{Symbol: "BTCUSDT", Timeframe: "1m", Close: 100},
		},
		Account:   strategies.Account{Available: 1000},
		Config:    map[string]interface{}{"rsi_period": 14.0, "nested": map[string]interface{}{"a": 1.0}},
		Price:     100,
		Timeframe: "1m",
		Symbol:    "BTCUSDT",
		Mode:      strategies.ModeDryRun,
	}
}

// ---- AC#1: happy path, every RPC round-trips successfully ----

func TestMLGrpcStrategy_HappyPath_AllHooks(t *testing.T) {
	client := mocks.NewMLStrategyClient(t)
	client.On("Name", mock.Anything, mock.Anything).Return(&strategypb.NameResponse{Name: "mlgrpc_dummy"}, nil)
	client.On("Before", mock.Anything, mock.Anything).Return(&strategypb.Empty{}, nil)
	client.On("ShouldLong", mock.Anything, mock.Anything).Return(&strategypb.BoolResponse{Value: true}, nil)
	client.On("GoLong", mock.Anything, mock.Anything).Return(&strategypb.SignalResponse{
		Signal: &strategypb.Signal{Buy: &strategypb.Order{Qty: 1, Price: 100}},
	}, nil)
	client.On("ShouldShort", mock.Anything, mock.Anything).Return(&strategypb.BoolResponse{Value: false}, nil)
	client.On("GoShort", mock.Anything, mock.Anything).Return(&strategypb.SignalResponse{Signal: &strategypb.Signal{}}, nil)
	client.On("UpdatePosition", mock.Anything, mock.Anything).Return(&strategypb.NullableSignalResponse{Signal: nil}, nil)
	client.On("After", mock.Anything, mock.Anything).Return(&strategypb.Empty{}, nil)
	client.On("Terminate", mock.Anything, mock.Anything).Return(&strategypb.Empty{}, nil)

	s := mlgrpc.NewMLGrpcStrategy(client, time.Second)
	ctx := testContext()

	assert.Equal(t, "mlgrpc_dummy", s.Name())
	assert.NotPanics(t, func() { s.Before(ctx) })
	assert.True(t, s.ShouldLong(ctx))
	sig := s.GoLong(ctx)
	require.NotNil(t, sig.Buy)
	assert.Equal(t, 1.0, sig.Buy.Qty)
	assert.False(t, s.ShouldShort(ctx))
	assert.NotPanics(t, func() { s.GoShort(ctx) })
	assert.Nil(t, s.UpdatePosition(ctx))
	assert.NotPanics(t, func() { s.After(ctx) })
	assert.NotPanics(t, func() { s.Terminate(ctx) })
}

// ---- AC#2: RPC failure panics ----

func TestMLGrpcStrategy_RPCError_Panics(t *testing.T) {
	client := mocks.NewMLStrategyClient(t)
	client.On("ShouldLong", mock.Anything, mock.Anything).Return((*strategypb.BoolResponse)(nil), errors.New("connection refused"))

	s := mlgrpc.NewMLGrpcStrategy(client, time.Second)

	assert.Panics(t, func() { s.ShouldLong(testContext()) })
}

// ---- AC#3: timeout treated identically to unreachable ----

func TestMLGrpcStrategy_DeadlineExceeded_PanicsIdenticallyToRPCError(t *testing.T) {
	client := mocks.NewMLStrategyClient(t)
	client.On("GoLong", mock.Anything, mock.Anything).Return((*strategypb.SignalResponse)(nil), context.DeadlineExceeded)

	s := mlgrpc.NewMLGrpcStrategy(client, time.Millisecond)

	assert.Panics(t, func() { s.GoLong(testContext()) })
}

// ---- AC#4: Config JSON round-trip fidelity ----

func TestMLGrpcStrategy_ConfigJSON_RoundTripsNestedStructures(t *testing.T) {
	client := mocks.NewMLStrategyClient(t)

	var capturedConfigJSON string
	client.On("Before", mock.Anything, mock.MatchedBy(func(sc *strategypb.StrategyContext) bool {
		capturedConfigJSON = sc.GetConfigJson()
		return true
	})).Return(&strategypb.Empty{}, nil)

	s := mlgrpc.NewMLGrpcStrategy(client, time.Second)
	ctx := testContext()
	s.Before(ctx)

	var decoded map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(capturedConfigJSON), &decoded))
	assert.Equal(t, 14.0, decoded["rsi_period"])
	nested, ok := decoded["nested"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, 1.0, nested["a"])
}

// ---- AC#5: nil vs empty signal ----

func TestMLGrpcStrategy_UpdatePosition_NilSignal_ReturnsNil(t *testing.T) {
	client := mocks.NewMLStrategyClient(t)
	client.On("UpdatePosition", mock.Anything, mock.Anything).Return(&strategypb.NullableSignalResponse{Signal: nil}, nil)

	s := mlgrpc.NewMLGrpcStrategy(client, time.Second)
	assert.Nil(t, s.UpdatePosition(testContext()))
}

func TestMLGrpcStrategy_UpdatePosition_EmptySignal_ReturnsNonNilEmptySignal(t *testing.T) {
	client := mocks.NewMLStrategyClient(t)
	client.On("UpdatePosition", mock.Anything, mock.Anything).Return(&strategypb.NullableSignalResponse{Signal: &strategypb.Signal{}}, nil)

	s := mlgrpc.NewMLGrpcStrategy(client, time.Second)
	sig := s.UpdatePosition(testContext())
	require.NotNil(t, sig, "an explicit empty Signal must NOT be conflated with the nil/hold case")
	assert.Nil(t, sig.Buy)
	assert.Nil(t, sig.Sell)
}

// ---- AC#6: nil Position -> unset proto field, not zero-valued message ----

func TestMLGrpcStrategy_NilPosition_LeavesProtoPositionUnset(t *testing.T) {
	client := mocks.NewMLStrategyClient(t)

	var captured *strategypb.StrategyContext
	client.On("Before", mock.Anything, mock.MatchedBy(func(sc *strategypb.StrategyContext) bool {
		captured = sc
		return true
	})).Return(&strategypb.Empty{}, nil)

	s := mlgrpc.NewMLGrpcStrategy(client, time.Second)
	ctx := testContext()
	ctx.Position = nil
	s.Before(ctx)

	require.NotNil(t, captured)
	assert.Nil(t, captured.Position, "no open position must serialize as an unset proto field, not a zero-valued Position message")
}

func TestMLGrpcStrategy_WithPosition_SerializesFields(t *testing.T) {
	client := mocks.NewMLStrategyClient(t)

	slPrice := 95.0
	var captured *strategypb.StrategyContext
	client.On("Before", mock.Anything, mock.MatchedBy(func(sc *strategypb.StrategyContext) bool {
		captured = sc
		return true
	})).Return(&strategypb.Empty{}, nil)

	s := mlgrpc.NewMLGrpcStrategy(client, time.Second)
	ctx := testContext()
	ctx.Position = &strategies.Position{
		Symbol:          "BTCUSDT",
		EntryPrice:      100,
		Quantity:        2,
		StopLossPrice:   &slPrice,
		StopLossOrderID: "order-123",
	}
	s.Before(ctx)

	require.NotNil(t, captured.Position)
	assert.Equal(t, "BTCUSDT", captured.Position.Symbol)
	assert.Equal(t, 100.0, captured.Position.EntryPrice)
	assert.Equal(t, 2.0, captured.Position.Quantity)
	require.NotNil(t, captured.Position.StopLossPrice)
	assert.Equal(t, 95.0, *captured.Position.StopLossPrice)
	assert.Equal(t, "order-123", captured.Position.StopLossOrderId)
}

// ---- AC#7: Name() RPC failure panics identically to any other hook ----

func TestMLGrpcStrategy_NameRPCFailure_PanicsIdenticallyToOtherHooks(t *testing.T) {
	client := mocks.NewMLStrategyClient(t)
	client.On("Name", mock.Anything, mock.Anything).Return((*strategypb.NameResponse)(nil), errors.New("unreachable"))

	s := mlgrpc.NewMLGrpcStrategy(client, time.Second)
	assert.Panics(t, func() { s.Name() })
}

// TestMLGrpcStrategy_EngineIntegration_UnreachablePanicsAreCaughtByExistingRecovery
// is this spec's primary acceptance target: an MLGrpcStrategy plugged into
// app/engine.Engine.Run and driven through the full hook sequence must be
// handled by the engine's EXISTING recover()+strategy_panics_total+webhook
// mechanism with zero new error-handling code in Engine - verifying that
// mechanism is implementation-agnostic (it doesn't know or care whether the
// panicking Strategy is native Go or gRPC-delegating).
func TestMLGrpcStrategy_EngineIntegration_UnreachablePanicsAreCaughtByExistingRecovery(t *testing.T) {
	client := mocks.NewMLStrategyClient(t)
	client.On("Before", mock.Anything, mock.Anything).Return((*strategypb.Empty)(nil), errors.New("connection refused: dummy server not running"))

	mlStrategy := mlgrpc.NewMLGrpcStrategy(client, time.Second)

	signalUC := new(enginemocks.SignalUseCase)
	accountReader := new(enginemocks.AccountReader)
	exchangeClient := new(signalmocks.ExchangeClient)
	notifySender := new(signalmocks.NotificationSender)

	cycleCandles := make([]exchange.Candle, 30)
	for i := range cycleCandles {
		cycleCandles[i] = exchange.Candle{Symbol: "BTCUSDT", Close: 100 + float64(i)}
	}
	exchangeClient.On("ListKline", mock.Anything, "BTCUSDT", mock.Anything, mock.Anything).Return(cycleCandles, nil).Maybe()
	accountReader.On("GetAccount").Return(entities.Account{Amount: 1000}, nil)
	signalUC.On("GetOpenSignal", "BTCUSDT", uint(1)).Return(entities.Signal{}, nil)

	var capturedEvent notifier.Event
	notifySender.On("Send", mock.Anything, mock.MatchedBy(func(ev notifier.Event) bool {
		capturedEvent = ev
		return ev.Type == notifier.EventStrategyError
	})).Return(nil)

	e := engine.NewEngine(exchangeClient, indicators.NewTalibAdapter(), signalUC, accountReader, notifySender, memcache.NewInMemoryCache(), nil)

	dbStrategy := entities.Strategy{
		ID:           1,
		Name:         "mlgrpc-test",
		StrategyName: "mlgrpc_dummy",
		StrategyConfiguration: entities.StrategyConfiguration{
			Cycle:         entities.OneMinute,
			Configuration: []byte(`{}`),
		},
	}

	var runErr error
	assert.NotPanics(t, func() {
		runErr = e.Run(context.Background(), mlStrategy, dbStrategy, "BTCUSDT", strategies.ModeDryRun)
	})
	assert.Error(t, runErr, "the engine must convert the panic into a returned error, not crash the worker")
	assert.Equal(t, true, capturedEvent.Data["panic"])
}
