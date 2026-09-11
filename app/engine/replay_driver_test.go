package engine_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"go-trade-bot/app/engine"
	"go-trade-bot/app/engine/mocks"
	"go-trade-bot/app/entities"
	"go-trade-bot/app/strategies"
	"go-trade-bot/internal/exchange"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockWarmupSource struct {
	calledWithSymbol    string
	calledWithTimeframe string
	calledWithWindow    int
	returnCandles       []exchange.Candle
	returnErr           error
}

func (m *mockWarmupSource) Warmup(ctx context.Context, symbol, timeframe string, window int) ([]exchange.Candle, error) {
	m.calledWithSymbol = symbol
	m.calledWithTimeframe = timeframe
	m.calledWithWindow = window
	return m.returnCandles, m.returnErr
}

type emptyFeed struct{}

func (f *emptyFeed) Next() (exchange.Candle, bool) {
	return exchange.Candle{}, false
}

func (f *emptyFeed) Close() error {
	return nil
}

func TestReplayDriver_Warmup(t *testing.T) {
	ctx := context.Background()
	feed := &emptyFeed{}
	strat := new(mocks.Strategy)
	dbStrat := entities.Strategy{
		ID:   1,
		Name: "mock_strat",
		StrategyConfiguration: entities.StrategyConfiguration{
			Cycle: entities.FifteenMinutes,
		},
	}

	warmup := &mockWarmupSource{
		returnCandles: []exchange.Candle{
			{Open: 100, Close: 105, OpenTime: time.Now().Add(-15 * time.Minute)},
		},
	}

	driver := engine.NewReplayDriver(feed, nil, nil, strat, dbStrat, "BTCUSDT", strategies.ModeDryRun, nil)
	driver.WarmupSource = warmup

	trades, err := driver.Run(ctx)
	require.NoError(t, err)
	assert.Empty(t, trades)

	assert.Equal(t, "BTCUSDT", warmup.calledWithSymbol)
	assert.Equal(t, "15m", warmup.calledWithTimeframe)
	assert.Equal(t, 100, warmup.calledWithWindow)
}

func TestReplayDriver_Warmup_FailureDegradesGracefully(t *testing.T) {
	ctx := context.Background()
	feed := &emptyFeed{}
	strat := new(mocks.Strategy)
	dbStrat := entities.Strategy{
		ID:   1,
		Name: "mock_strat",
		StrategyConfiguration: entities.StrategyConfiguration{
			Cycle: entities.OneMinute,
		},
	}

	warmup := &mockWarmupSource{
		returnErr: errors.New("network timeout"),
	}

	driver := engine.NewReplayDriver(feed, nil, nil, strat, dbStrat, "ETHUSDT", strategies.ModeDryRun, nil)
	driver.WarmupSource = warmup

	// Should not return error even if warmup fails
	trades, err := driver.Run(ctx)
	require.NoError(t, err)
	assert.Empty(t, trades)
	assert.Equal(t, "ETHUSDT", warmup.calledWithSymbol)
}
