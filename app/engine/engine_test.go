package engine_test

import (
	"context"
	"errors"
	"testing"

	"go-trade-bot/app/engine"
	"go-trade-bot/app/engine/mocks"
	"go-trade-bot/app/entities"
	"go-trade-bot/app/strategies"
	usecase "go-trade-bot/app/usecase/signal"
	signalmocks "go-trade-bot/app/usecase/signal/mocks"
	"go-trade-bot/internal/exchange"
	"go-trade-bot/internal/indicators"
	"go-trade-bot/internal/memcache"
	"go-trade-bot/internal/notifier"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func newTestEngine() (*engine.Engine, *mocks.SignalUseCase, *mocks.AccountReader, *signalmocks.ExchangeClient, *signalmocks.NotificationSender) {
	signalUC := new(mocks.SignalUseCase)
	accountReader := new(mocks.AccountReader)
	exchangeClient := new(signalmocks.ExchangeClient)
	notifySender := new(signalmocks.NotificationSender)

	e := engine.NewEngine(exchangeClient, indicators.NewTalibAdapter(), signalUC, accountReader, notifySender, memcache.NewInMemoryCache())
	return e, signalUC, accountReader, exchangeClient, notifySender
}

func stubCandleFetches(exchangeClient *signalmocks.ExchangeClient, symbol string) {
	cycleCandles := make([]exchange.Candle, 30)
	for i := range cycleCandles {
		cycleCandles[i] = exchange.Candle{Symbol: symbol, Close: 100 + float64(i)}
	}
	exchangeClient.On("ListKline", mock.Anything, symbol, mock.Anything, mock.Anything).Return(cycleCandles, nil).Maybe()
}

func dbStrategyFixture() entities.Strategy {
	return entities.Strategy{
		ID:   1,
		Name: "test-strategy",
		StrategyConfiguration: entities.StrategyConfiguration{
			Cycle:         entities.OneMinute,
			Configuration: []byte(`{"stop_loss_pct": 2.0}`),
		},
	}
}

// AC#1: no open position => Before -> ShouldLong -> (false) -> ShouldShort ->
// After, never UpdatePosition.
func TestEngine_Run_NoPosition_HookSequence(t *testing.T) {
	e, signalUC, accountReader, exchangeClient, _ := newTestEngine()
	stubCandleFetches(exchangeClient, "BTCUSDT")
	accountReader.On("GetAccount").Return(entities.Account{Amount: 1000}, nil)
	signalUC.On("GetOpenSignal", "BTCUSDT", uint(1)).Return(entities.Signal{}, nil)

	strategy := new(mocks.Strategy)
	strategy.On("Before", mock.Anything).Return()
	strategy.On("ShouldLong", mock.Anything).Return(false)
	strategy.On("ShouldShort", mock.Anything).Return(false)
	strategy.On("After", mock.Anything).Return()

	err := e.Run(context.Background(), strategy, dbStrategyFixture(), "BTCUSDT", strategies.ModeDryRun)
	assert.NoError(t, err)

	strategy.AssertNotCalled(t, "GoLong", mock.Anything)
	strategy.AssertNotCalled(t, "GoShort", mock.Anything)
	strategy.AssertNotCalled(t, "UpdatePosition", mock.Anything)
	strategy.AssertExpectations(t)
}

// AC#2: an open position => Before -> UpdatePosition -> After, never
// ShouldLong/GoLong/ShouldShort/GoShort.
func TestEngine_Run_WithPosition_HookSequence(t *testing.T) {
	e, signalUC, accountReader, exchangeClient, _ := newTestEngine()
	stubCandleFetches(exchangeClient, "BTCUSDT")
	accountReader.On("GetAccount").Return(entities.Account{Amount: 1000}, nil)
	signalUC.On("GetOpenSignal", "BTCUSDT", uint(1)).Return(entities.Signal{
		ID: 5, Symbol: "BTCUSDT", Status: entities.Open,
		Orders: []entities.Order{{EntryPrice: 100, Quantity: 1}},
	}, nil)

	strategy := new(mocks.Strategy)
	strategy.On("Before", mock.Anything).Return()
	strategy.On("UpdatePosition", mock.MatchedBy(func(ctx strategies.Context) bool {
		return ctx.Position != nil && ctx.Position.EntryPrice == 100
	})).Return((*strategies.Signal)(nil))
	strategy.On("After", mock.Anything).Return()

	err := e.Run(context.Background(), strategy, dbStrategyFixture(), "BTCUSDT", strategies.ModeDryRun)
	assert.NoError(t, err)

	strategy.AssertNotCalled(t, "ShouldLong", mock.Anything)
	strategy.AssertNotCalled(t, "GoLong", mock.Anything)
	strategy.AssertNotCalled(t, "ShouldShort", mock.Anything)
	strategy.AssertNotCalled(t, "GoShort", mock.Anything)
}

// AC#3: ShouldLong=true, GoLong returns Signal{Buy} with no explicit
// StopLoss => GenerateBuySignal called using the strategy's configured
// stop_loss_pct, not a strategy-supplied stop price.
func TestEngine_Run_GoLong_UsesConfiguredStopLossPct(t *testing.T) {
	e, signalUC, accountReader, exchangeClient, _ := newTestEngine()
	stubCandleFetches(exchangeClient, "BTCUSDT")
	accountReader.On("GetAccount").Return(entities.Account{Amount: 1000}, nil)
	signalUC.On("GetOpenSignal", "BTCUSDT", uint(1)).Return(entities.Signal{}, nil)
	signalUC.On("GenerateBuySignal", mock.MatchedBy(func(e usecase.EntrySignal) bool {
		return e.Symbol == "BTCUSDT" && e.StrategyID == 1 && e.StopLossPct == 2.0 && e.StopLossPrice == nil && e.EntryPrice == 50000
	})).Return(nil)

	strategy := new(mocks.Strategy)
	strategy.On("Before", mock.Anything).Return()
	strategy.On("ShouldLong", mock.Anything).Return(true)
	strategy.On("GoLong", mock.Anything).Return(strategies.Signal{Buy: &strategies.Order{Qty: 0.02, Price: 50000}})
	strategy.On("After", mock.Anything).Return()

	err := e.Run(context.Background(), strategy, dbStrategyFixture(), "BTCUSDT", strategies.ModeDryRun)
	assert.NoError(t, err)
	signalUC.AssertExpectations(t)
}

// AC#4: GoLong returns Signal{} (no Buy) despite ShouldLong=true => no order
// placed, a strategy.error webhook fires, and Run returns an error (which
// the caller records as StrategyExecution{Status: Error}).
func TestEngine_Run_GoLong_NoBuyOrder_IsAnError(t *testing.T) {
	e, signalUC, accountReader, exchangeClient, notifySender := newTestEngine()
	stubCandleFetches(exchangeClient, "BTCUSDT")
	accountReader.On("GetAccount").Return(entities.Account{Amount: 1000}, nil)
	signalUC.On("GetOpenSignal", "BTCUSDT", uint(1)).Return(entities.Signal{}, nil)
	notifySender.On("Send", mock.Anything, mock.MatchedBy(func(e notifier.Event) bool {
		return e.Type == notifier.EventStrategyError
	})).Return(nil)

	strategy := new(mocks.Strategy)
	strategy.On("Before", mock.Anything).Return()
	strategy.On("ShouldLong", mock.Anything).Return(true)
	strategy.On("GoLong", mock.Anything).Return(strategies.Signal{})
	strategy.On("After", mock.Anything).Return()

	err := e.Run(context.Background(), strategy, dbStrategyFixture(), "BTCUSDT", strategies.ModeDryRun)
	assert.Error(t, err)
	signalUC.AssertNotCalled(t, "GenerateBuySignal", mock.Anything)
	notifySender.AssertExpectations(t)
}

// AC#5: UpdatePosition returns nil => no GenerateSellSignal call, position
// unchanged.
func TestEngine_Run_UpdatePosition_Nil_NoSell(t *testing.T) {
	e, signalUC, accountReader, exchangeClient, _ := newTestEngine()
	stubCandleFetches(exchangeClient, "BTCUSDT")
	accountReader.On("GetAccount").Return(entities.Account{Amount: 1000}, nil)
	signalUC.On("GetOpenSignal", "BTCUSDT", uint(1)).Return(entities.Signal{
		ID: 5, Symbol: "BTCUSDT", Status: entities.Open,
		Orders: []entities.Order{{EntryPrice: 100, Quantity: 1}},
	}, nil)

	strategy := new(mocks.Strategy)
	strategy.On("Before", mock.Anything).Return()
	strategy.On("UpdatePosition", mock.Anything).Return((*strategies.Signal)(nil))
	strategy.On("After", mock.Anything).Return()

	err := e.Run(context.Background(), strategy, dbStrategyFixture(), "BTCUSDT", strategies.ModeDryRun)
	assert.NoError(t, err)
	signalUC.AssertNotCalled(t, "GenerateSellSignal", mock.Anything)
}

// AC#6: UpdatePosition returns &Signal{Sell} => GenerateSellSignal is called.
func TestEngine_Run_UpdatePosition_Sell_CallsGenerateSellSignal(t *testing.T) {
	e, signalUC, accountReader, exchangeClient, _ := newTestEngine()
	stubCandleFetches(exchangeClient, "BTCUSDT")
	accountReader.On("GetAccount").Return(entities.Account{Amount: 1000}, nil)
	signalUC.On("GetOpenSignal", "BTCUSDT", uint(1)).Return(entities.Signal{
		ID: 5, Symbol: "BTCUSDT", Status: entities.Open,
		Orders: []entities.Order{{EntryPrice: 100, Quantity: 1}},
	}, nil)
	signalUC.On("GenerateSellSignal", mock.MatchedBy(func(e usecase.ExitSignal) bool {
		return e.Symbol == "BTCUSDT" && e.ExitPrice == 110
	})).Return(nil)

	strategy := new(mocks.Strategy)
	strategy.On("Before", mock.Anything).Return()
	strategy.On("UpdatePosition", mock.Anything).Return(&strategies.Signal{Sell: &strategies.Order{Qty: 1, Price: 110}})
	strategy.On("After", mock.Anything).Return()

	err := e.Run(context.Background(), strategy, dbStrategyFixture(), "BTCUSDT", strategies.ModeDryRun)
	assert.NoError(t, err)
	signalUC.AssertExpectations(t)
}

// AC#7: ShouldShort=true => no exchange call, strategy.error
// logged/webhooked, cycle completes without a panic (and without being
// recorded as an error - Run returns nil).
func TestEngine_Run_ShouldShort_NotSupported(t *testing.T) {
	e, signalUC, accountReader, exchangeClient, notifySender := newTestEngine()
	stubCandleFetches(exchangeClient, "BTCUSDT")
	accountReader.On("GetAccount").Return(entities.Account{Amount: 1000}, nil)
	signalUC.On("GetOpenSignal", "BTCUSDT", uint(1)).Return(entities.Signal{}, nil)
	notifySender.On("Send", mock.Anything, mock.MatchedBy(func(e notifier.Event) bool {
		return e.Type == notifier.EventStrategyError
	})).Return(nil)

	strategy := new(mocks.Strategy)
	strategy.On("Before", mock.Anything).Return()
	strategy.On("ShouldLong", mock.Anything).Return(false)
	strategy.On("ShouldShort", mock.Anything).Return(true)
	strategy.On("GoShort", mock.Anything).Return(strategies.Signal{})
	strategy.On("After", mock.Anything).Return()

	err := e.Run(context.Background(), strategy, dbStrategyFixture(), "BTCUSDT", strategies.ModeDryRun)
	assert.NoError(t, err)
	signalUC.AssertNotCalled(t, "GenerateBuySignal", mock.Anything)
	signalUC.AssertNotCalled(t, "GenerateSellSignal", mock.Anything)
}

// AC#9: a panic from any hook is recovered at the cycle boundary (not
// propagated to crash the worker), and Terminate is NOT called for this
// occurrence.
func TestEngine_Run_RecoversFromPanic(t *testing.T) {
	e, signalUC, accountReader, exchangeClient, notifySender := newTestEngine()
	stubCandleFetches(exchangeClient, "BTCUSDT")
	accountReader.On("GetAccount").Return(entities.Account{Amount: 1000}, nil)
	signalUC.On("GetOpenSignal", "BTCUSDT", uint(1)).Return(entities.Signal{}, nil)
	notifySender.On("Send", mock.Anything, mock.Anything).Return(nil)

	strategy := new(mocks.Strategy)
	strategy.On("Before", mock.Anything).Run(func(args mock.Arguments) {
		panic("division by zero")
	})

	assert.NotPanics(t, func() {
		err := e.Run(context.Background(), strategy, dbStrategyFixture(), "BTCUSDT", strategies.ModeDryRun)
		assert.Error(t, err)
	})
	strategy.AssertNotCalled(t, "Terminate", mock.Anything)
}

// Edge case: the exchange fails to return candles at all - the cycle fails
// before any hook is invoked.
func TestEngine_Run_CandleFetchFailure(t *testing.T) {
	e, _, _, exchangeClient, _ := newTestEngine()
	exchangeClient.On("ListKline", mock.Anything, "BTCUSDT", mock.Anything, mock.Anything).
		Return(nil, errors.New("network error"))

	strategy := new(mocks.Strategy)

	err := e.Run(context.Background(), strategy, dbStrategyFixture(), "BTCUSDT", strategies.ModeDryRun)
	assert.Error(t, err)
	strategy.AssertNotCalled(t, "Before", mock.Anything)
}
