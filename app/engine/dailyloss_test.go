package engine_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"go-trade-bot/app/engine/mocks"
	"go-trade-bot/app/entities"
	"go-trade-bot/app/strategies"
	"go-trade-bot/internal/notifier"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func dailyLossStrategy(limit string) entities.Strategy {
	s := dbStrategyFixture()
	s.StrategyConfiguration.Configuration = []byte(fmt.Sprintf(`{"stop_loss_pct": 2.0%s}`, limit))
	return s
}

// R-01: once realized PnL for the UTC day reaches -limit, the flat strategy
// gets no entry hooks and no order, and the operator is told once per day.
func TestEngine_Run_DailyLossLimit_BlocksEntries(t *testing.T) {
	e, signalUC, accountReader, exchangeClient, notifySender := newTestEngine()
	stubCandleFetches(exchangeClient, "BTCUSDT")
	accountReader.On("GetAccount").Return(entities.Account{Amount: 1000}, nil)
	signalUC.On("GetOpenSignal", "BTCUSDT", uint(1)).Return(entities.Signal{}, nil)
	signalUC.On("RealizedPnL", uint(1), mock.Anything, mock.Anything).Return(-60.0, nil)
	notifySender.On("Send", mock.Anything, mock.MatchedBy(func(ev notifier.Event) bool {
		return ev.Type == notifier.EventStrategyError
	})).Return(nil).Once()

	strategy := new(mocks.Strategy)
	strategy.On("Before", mock.Anything).Return()
	strategy.On("After", mock.Anything).Return()

	for i := 0; i < 3; i++ { // three cycles, one notification (Once() fails a second Send)
		require.NoError(t, e.Run(context.Background(), strategy, dailyLossStrategy(`, "max_daily_loss_usd": 50`), "BTCUSDT", strategies.ModeDryRun))
	}

	strategy.AssertNotCalled(t, "ShouldLong", mock.Anything)
	strategy.AssertNotCalled(t, "GoLong", mock.Anything)
	signalUC.AssertNotCalled(t, "GenerateBuySignal", mock.Anything)
	notifySender.AssertExpectations(t)
}

func TestEngine_Run_DailyLossLimit_BelowLimitStillTrades(t *testing.T) {
	e, signalUC, accountReader, exchangeClient, _ := newTestEngine()
	stubCandleFetches(exchangeClient, "BTCUSDT")
	accountReader.On("GetAccount").Return(entities.Account{Amount: 1000}, nil)
	signalUC.On("GetOpenSignal", "BTCUSDT", uint(1)).Return(entities.Signal{}, nil)
	signalUC.On("RealizedPnL", uint(1), mock.Anything, mock.Anything).Return(-49.99, nil)
	signalUC.On("GenerateBuySignal", mock.Anything).Return(nil).Once()

	strategy := new(mocks.Strategy)
	strategy.On("Before", mock.Anything).Return()
	strategy.On("ShouldLong", mock.Anything).Return(true)
	strategy.On("GoLong", mock.Anything).Return(strategies.Signal{Buy: &strategies.Order{Qty: 1, Price: 100}})
	strategy.On("After", mock.Anything).Return()

	require.NoError(t, e.Run(context.Background(), strategy, dailyLossStrategy(`, "max_daily_loss_usd": 50`), "BTCUSDT", strategies.ModeDryRun))
	signalUC.AssertExpectations(t)
}

// No limit configured: the ledger is never queried (mock would panic on an
// unexpected RealizedPnL call).
func TestEngine_Run_NoDailyLossLimit_DoesNotQuery(t *testing.T) {
	e, signalUC, accountReader, exchangeClient, _ := newTestEngine()
	stubCandleFetches(exchangeClient, "BTCUSDT")
	accountReader.On("GetAccount").Return(entities.Account{Amount: 1000}, nil)
	signalUC.On("GetOpenSignal", "BTCUSDT", uint(1)).Return(entities.Signal{}, nil)

	strategy := new(mocks.Strategy)
	strategy.On("Before", mock.Anything).Return()
	strategy.On("ShouldLong", mock.Anything).Return(false)
	strategy.On("ShouldShort", mock.Anything).Return(false)
	strategy.On("After", mock.Anything).Return()

	require.NoError(t, e.Run(context.Background(), strategy, dailyLossStrategy(``), "BTCUSDT", strategies.ModeDryRun))
	signalUC.AssertNotCalled(t, "RealizedPnL", mock.Anything, mock.Anything, mock.Anything)
}

// A failed PnL read must fail closed: no entry, and the error surfaces.
func TestEngine_Run_DailyLossLimit_ReadErrorFailsClosed(t *testing.T) {
	e, signalUC, accountReader, exchangeClient, _ := newTestEngine()
	stubCandleFetches(exchangeClient, "BTCUSDT")
	accountReader.On("GetAccount").Return(entities.Account{Amount: 1000}, nil)
	signalUC.On("GetOpenSignal", "BTCUSDT", uint(1)).Return(entities.Signal{}, nil)
	signalUC.On("RealizedPnL", uint(1), mock.Anything, mock.Anything).Return(0.0, errors.New("db down"))

	strategy := new(mocks.Strategy)
	strategy.On("Before", mock.Anything).Return()
	strategy.On("After", mock.Anything).Return()

	err := e.Run(context.Background(), strategy, dailyLossStrategy(`, "max_daily_loss_usd": 50`), "BTCUSDT", strategies.ModeDryRun)
	assert.Error(t, err)
	strategy.AssertNotCalled(t, "ShouldLong", mock.Anything)
	signalUC.AssertNotCalled(t, "GenerateBuySignal", mock.Anything)
}

// An open position keeps being managed while halted: exits are never blocked.
func TestEngine_Run_DailyLossLimit_DoesNotBlockExits(t *testing.T) {
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

	require.NoError(t, e.Run(context.Background(), strategy, dailyLossStrategy(`, "max_daily_loss_usd": 50`), "BTCUSDT", strategies.ModeDryRun))
	strategy.AssertCalled(t, "UpdatePosition", mock.Anything)
	signalUC.AssertNotCalled(t, "RealizedPnL", mock.Anything, mock.Anything, mock.Anything)
}

// End to end through the backtest driver: a script that enters and exits on
// every candle bleeds fees, so a small limit halts it for the rest of the UTC
// day and it resumes at the next reset - on simulated time, not wall-clock.
func TestDailyLossLimit_Backtest_HaltsAndResumesNextDay(t *testing.T) {
	const limit = 3.0
	unlimited := dailyLossRun(t, "dl_unlimited", 0)
	limited := dailyLossRun(t, "dl_limited", limit)

	require.NotEmpty(t, unlimited.entries)
	assert.Less(t, len(limited.entries), len(unlimited.entries), "limit must block entries")

	day1 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	day2 := day1.Add(24 * time.Hour)
	var lossDay1 float64
	var day1Entries, day2Entries int
	for _, e := range limited.entries {
		switch {
		case e.at.Before(day2):
			day1Entries++
			lossDay1 += e.profit
		default:
			day2Entries++
		}
	}
	assert.LessOrEqual(t, lossDay1, -limit, "day 1 halted only after the limit was reached")
	assert.Greater(t, day2Entries, 0, "trading resumes after the UTC reset")
	t.Logf("unlimited=%d limited=%d (day1=%d lossDay1=%.2f, day2=%d)", len(unlimited.entries), len(limited.entries), day1Entries, lossDay1, day2Entries)
	assert.Contains(t, limited.labels, "daily_loss_halt")
	assert.NotContains(t, unlimited.labels, "daily_loss_halt")
}
