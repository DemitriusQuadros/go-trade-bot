package engine_test

import (
	"context"
	"testing"
	"time"

	"go-trade-bot/app/engine"
	"go-trade-bot/app/entities"
	"go-trade-bot/app/repository/candle"
	signalrepo "go-trade-bot/app/repository/signal"
	"go-trade-bot/app/strategies"
	usecase "go-trade-bot/app/usecase/signal"
	"go-trade-bot/internal/exchange"
	"go-trade-bot/internal/feed"
	"go-trade-bot/internal/futuresmath"
	"go-trade-bot/internal/indicators"
	"go-trade-bot/internal/memcache"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// futuresParityScriptSource implements a leveraged futures strategy that executes
// both long (RSI < 42) and short (RSI > 58) entries, closing profitable trades via
// update_position (PnL >= 1%) while relying on stop_loss_pct (2%) for adverse moves.
const futuresParityScriptSource = `
function should_long(ctx)
  if #ctx.candles < 20 then return false end
  local rsi = ind.rsi(14)
  return rsi < 42
end

function go_long(ctx)
  return {buy = {price = ctx.price}}
end

function should_short(ctx)
  if #ctx.candles < 20 then return false end
  local rsi = ind.rsi(14)
  return rsi > 58
end

function go_short(ctx)
  return {sell = {price = ctx.price}}
end

function update_position(ctx)
  local entry = ctx.position.entry_price
  local side = ctx.position.side
  local pnl = 0
  if side == "short" then
    pnl = (entry - ctx.price) / entry * 100
  else
    pnl = (ctx.price - entry) / entry * 100
  end
  if pnl >= 1.0 then
    if side == "short" then
      return {buy = {price = ctx.price}}
    else
      return {sell = {price = ctx.price}}
    end
  end
  return nil
end
`

// TestSimulatorFuturesParity_LongAndShortCycles verifies deterministic parity between
// ReplayFeed (Run A) and LiveFeed (Run B) with BacktestStopEvaluator wired as PreCycle hook.
// Tests leveraged futures execution with 10x leverage, isolated margin, 2% stop-loss,
// and 10% capital position sizing across both LONG and SHORT cycles.
func TestSimulatorFuturesParity_LongAndShortCycles(t *testing.T) {
	candles := generateSyntheticCandles(300)
	symbol := "BTCUSDT"
	timeframe := "1m"

	stratConfigJSON := datatypes.JSON(`{
		"market_type": "futures",
		"leverage": 10,
		"margin_type": "isolated",
		"stop_loss_pct": 2.0,
		"position_sizing": {
			"type": "pct_capital",
			"value": 10
		}
	}`)

	dbStratA := entities.Strategy{
		ID:               101,
		Name:             "script_futures_long_short_A",
		StrategyName:     "script",
		ScriptSource:     futuresParityScriptSource,
		MonitoredSymbols: datatypes.JSONSlice[string]{symbol},
		StrategyConfiguration: entities.StrategyConfiguration{
			Cycle:         entities.OneMinute,
			Configuration: stratConfigJSON,
		},
	}

	// -------------------------------------------------------------
	// Run A: ReplayFeed + SimulatedFillExchange + BacktestStopEvaluator
	// -------------------------------------------------------------
	dbA, err := gorm.Open(sqlite.Open("file:"+t.Name()+"_A?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, dbA.AutoMigrate(&entities.Candle{}))
	repoA := candle.NewCandleRepository(dbA)
	require.NoError(t, repoA.Upsert(context.Background(), candles))

	replayFeedA, err := feed.NewReplayFeed(context.Background(), repoA, symbol, timeframe, candles[0].OpenTime, candles[len(candles)-1].OpenTime.Add(time.Minute))
	require.NoError(t, err)

	dataSourceA := engine.NewCandleRepoMarketDataSource(repoA)
	simExchangeA := engine.NewSimulatedFillExchange(dataSourceA, engine.FillPolicy{})
	signalRepoA := &memorySignalRepo{}
	accountUCA := &memoryAccountUseCase{amount: 1000.0}
	signalUCA := usecase.NewSignalUseCase(signalRepoA, accountUCA, simExchangeA, nil, nil, nil)
	indicatorProviderA := indicators.NewTalibAdapter()
	cacheA := memcache.NewInMemoryCache()

	engineA := engine.NewEngine(simExchangeA, indicatorProviderA, signalUCA, accountUCA, nil, cacheA, nil)
	engineA.PreCycle = engine.NewBacktestStopEvaluator(signalUCA, simExchangeA)

	stratA, ok := strategies.Get("script", dbStratA)
	require.True(t, ok)
	driverA := engine.NewReplayDriver(replayFeedA, simExchangeA, engineA, stratA, dbStratA, symbol, strategies.ModeBacktest, signalRepoA)

	tradesA, err := driverA.Run(context.Background())
	require.NoError(t, err)

	// -------------------------------------------------------------
	// Run B: LiveFeed (wrapping fixed sequence) + SimulatedFillExchange + BacktestStopEvaluator
	// -------------------------------------------------------------
	dbStratB := entities.Strategy{
		ID:               102,
		Name:             "script_futures_long_short_B",
		StrategyName:     "script",
		ScriptSource:     futuresParityScriptSource,
		MonitoredSymbols: datatypes.JSONSlice[string]{symbol},
		StrategyConfiguration: entities.StrategyConfiguration{
			Cycle:         entities.OneMinute,
			Configuration: stratConfigJSON,
		},
	}

	dbB, err := gorm.Open(sqlite.Open("file:"+t.Name()+"_B?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, dbB.AutoMigrate(&entities.Candle{}))
	repoB := candle.NewCandleRepository(dbB)
	require.NoError(t, repoB.Upsert(context.Background(), candles))

	var exchangeCandles []exchange.Candle
	for _, c := range candles {
		exchangeCandles = append(exchangeCandles, exchange.Candle{
			Symbol:    c.Symbol,
			Timeframe: c.Timeframe,
			OpenTime:  c.OpenTime,
			Open:      c.Open,
			High:      c.High,
			Low:       c.Low,
			Close:     c.Close,
			Volume:    c.Volume,
		})
	}

	dataSourceB := engine.NewCandleRepoMarketDataSource(repoB)
	simExchangeB := engine.NewSimulatedFillExchange(dataSourceB, engine.FillPolicy{})
	fixedEx := &fixedSequenceExchange{
		candles: exchangeCandles,
		client:  simExchangeB,
	}

	liveFeedB, err := feed.NewLiveFeed(fixedEx, symbol, timeframe, nil)
	require.NoError(t, err)
	defer liveFeedB.Close()

	countedLiveFeedB := &countedFeed{Feed: liveFeedB, max: len(exchangeCandles)}

	signalRepoB := &memorySignalRepo{}
	accountUCB := &memoryAccountUseCase{amount: 1000.0}
	signalUCB := usecase.NewSignalUseCase(signalRepoB, accountUCB, simExchangeB, nil, nil, nil)
	indicatorProviderB := indicators.NewTalibAdapter()
	cacheB := memcache.NewInMemoryCache()

	engineB := engine.NewEngine(simExchangeB, indicatorProviderB, signalUCB, accountUCB, nil, cacheB, nil)
	engineB.PreCycle = engine.NewBacktestStopEvaluator(signalUCB, simExchangeB)

	stratB, ok := strategies.Get("script", dbStratB)
	require.True(t, ok)
	driverB := engine.NewReplayDriver(countedLiveFeedB, simExchangeB, engineB, stratB, dbStratB, symbol, strategies.ModeBacktest, signalRepoB)

	tradesB, err := driverB.Run(context.Background())
	require.NoError(t, err)

	// -------------------------------------------------------------
	// Parity Assertions
	// -------------------------------------------------------------
	require.NotEmpty(t, tradesA, "must generate at least one trade in Run A")
	require.Equal(t, len(tradesA), len(tradesB), "trade count mismatch between ReplayFeed and LiveFeed")

	var hasLong, hasShort bool
	for i := range tradesA {
		if tradesA[i].Side == "LONG" {
			hasLong = true
		}
		if tradesA[i].Side == "SHORT" {
			hasShort = true
		}

		assert.Equal(t, tradesA[i].Symbol, tradesB[i].Symbol)
		assert.Equal(t, tradesA[i].Side, tradesB[i].Side, "trade %d position side mismatch", i)
		assert.Equal(t, float64(10.0), tradesA[i].Leverage, "trade %d must have 10x leverage", i)
		assert.Equal(t, tradesA[i].Leverage, tradesB[i].Leverage, "trade %d leverage mismatch", i)
		assert.InDelta(t, tradesA[i].LiquidationPrice, tradesB[i].LiquidationPrice, 0.001, "trade %d liquidation price mismatch", i)
		assert.InDelta(t, tradesA[i].EntryPrice, tradesB[i].EntryPrice, 0.001, "trade %d entry price mismatch", i)
		assert.InDelta(t, tradesA[i].ExitPrice, tradesB[i].ExitPrice, 0.001, "trade %d exit price mismatch", i)
		assert.InDelta(t, tradesA[i].Profit, tradesB[i].Profit, 0.001, "trade %d profit mismatch", i)
		assert.Equal(t, tradesA[i].ExitReason, tradesB[i].ExitReason, "trade %d exit reason mismatch", i)
	}

	assert.True(t, hasLong, "strategy must have executed at least one LONG cycle")
	assert.True(t, hasShort, "strategy must have executed at least one SHORT cycle")

	// Verify Signal and Order records parity in repositories
	sigsA, err := signalRepoA.GetAll()
	require.NoError(t, err)
	sigsB, err := signalRepoB.GetAll()
	require.NoError(t, err)
	require.Equal(t, len(sigsA), len(sigsB), "persisted signals count mismatch")

	for i := range sigsA {
		assert.Equal(t, sigsA[i].PositionSide, sigsB[i].PositionSide)
		require.Len(t, sigsA[i].Orders, 1)
		require.Len(t, sigsB[i].Orders, 1)
		oA := sigsA[i].Orders[0]
		oB := sigsB[i].Orders[0]

		assert.Equal(t, oA.PositionSide, oB.PositionSide)
		assert.Equal(t, float32(10.0), oA.Leverage)
		assert.Equal(t, oA.Leverage, oB.Leverage)
		assert.InDelta(t, oA.InitialMargin, oB.InitialMargin, 0.001)
		assert.Greater(t, oA.InitialMargin, float32(0))
		assert.InDelta(t, oA.LiquidationPrice, oB.LiquidationPrice, 0.001)
		assert.Greater(t, oA.LiquidationPrice, float32(0))
		assert.InDelta(t, oA.EntryPrice, oB.EntryPrice, 0.001)
		assert.InDelta(t, oA.ExitPrice, oB.ExitPrice, 0.001)
		assert.InDelta(t, oA.Profit, oB.Profit, 0.001)
		assert.Equal(t, oA.ExitReason, oB.ExitReason)
	}
}

// TestSimulatorFuturesParity_FundingAndLiquidation tests a sequence of 1h candles spanning
// an 8h UTC boundary (08:00:00 UTC) where a 20x leveraged position accrues funding at 08:00:00 UTC
// and then hits its isolated liquidation price on a subsequent candle wick.
// Verifies that both BacktestStopEvaluator and SimulatedStopEvaluator accrue identical
// AccumulatedFunding and trigger entities.ExitReasonLiquidation at the exact LiquidationPrice.
func TestSimulatorFuturesParity_FundingAndLiquidation(t *testing.T) {
	runTest := func(t *testing.T, side entities.PositionSideType) {
		t.Helper()
		symbol := "BTCUSDT"
		strategyID := uint(701)
		strat := entities.Strategy{
			ID:   strategyID,
			Name: "funding_liq_test",
			StrategyConfiguration: entities.StrategyConfiguration{
				Cycle: entities.OneHour,
			},
		}

		entryT := time.Date(2025, 1, 1, 7, 0, 0, 0, time.UTC)
		boundaryT := time.Date(2025, 1, 1, 8, 0, 0, 0, time.UTC)
		wickT := time.Date(2025, 1, 1, 9, 0, 0, 0, time.UTC)

		entryPrice := 100.0
		qty := 10.0
		leverage := 20.0
		initialMargin := float32((qty * entryPrice) / leverage) // 50.0 USDT

		fmSide := futuresmath.SideLong
		if side == entities.PositionSideShort {
			fmSide = futuresmath.SideShort
		}
		initLiq := float32(futuresmath.IsolatedLiquidationPrice(symbol, fmSide, entryPrice, qty, leverage, 0, 0))

		// -------------------------------------------------------------
		// BacktestStopEvaluator Setup (Replay path)
		// -------------------------------------------------------------
		dbBacktest, err := gorm.Open(sqlite.Open("file:"+t.Name()+"_bt?mode=memory&cache=shared"), &gorm.Config{})
		require.NoError(t, err)
		require.NoError(t, dbBacktest.AutoMigrate(&entities.Signal{}, &entities.Order{}, &entities.Account{}))
		repoBacktest := signalrepo.NewSignalRepository(dbBacktest)
		accountBacktest := &memoryAccountUseCase{amount: 1000.0}
		exBacktest := engine.NewSimulatedFillExchange(nil, engine.FillPolicy{})
		signalUCBacktest := usecase.NewSignalUseCase(repoBacktest, accountBacktest, exBacktest, nil, nil, nil)

		initOrderBacktest := entities.Order{
			BrokerOrderID:    "sim_ord_bt_1",
			StopLossOrderID:  "", // No stop-loss: liquidation only
			PositionSide:     side,
			EntryPrice:       float32(entryPrice),
			Quantity:         float32(qty),
			InvestedAmount:   initialMargin,
			InitialMargin:    initialMargin,
			Leverage:         float32(leverage),
			MarginType:       entities.Isolated,
			LiquidationPrice: initLiq,
			CreatedAt:        entryT,
			UpdatedAt:        entryT,
		}
		initSignalBacktest := entities.Signal{
			ID:           1,
			Symbol:       symbol,
			StrategyID:   strategyID,
			Status:       entities.Open,
			PositionSide: side,
			CreatedAt:    entryT,
			UpdatedAt:    entryT,
			Orders:       []entities.Order{initOrderBacktest},
		}
		require.NoError(t, repoBacktest.Create(initSignalBacktest))

		backtestEvaluator := engine.NewBacktestStopEvaluator(signalUCBacktest, exBacktest)

		// -------------------------------------------------------------
		// SimulatedStopEvaluator Setup (DryRun path)
		// -------------------------------------------------------------
		dbSim, err := gorm.Open(sqlite.Open("file:"+t.Name()+"_sim?mode=memory&cache=shared"), &gorm.Config{})
		require.NoError(t, err)
		require.NoError(t, dbSim.AutoMigrate(&entities.Signal{}, &entities.Order{}, &entities.Account{}))
		repoSim := signalrepo.NewSignalRepository(dbSim)
		accountSim := &memoryAccountUseCase{amount: 1000.0}

		initOrderSim := entities.Order{
			BrokerOrderID:    exchange.SimulatedOrderIDPrefix + "1",
			StopLossOrderID:  "",
			PositionSide:     side,
			EntryPrice:       float32(entryPrice),
			Quantity:         float32(qty),
			InvestedAmount:   initialMargin,
			InitialMargin:    initialMargin,
			Leverage:         float32(leverage),
			MarginType:       entities.Isolated,
			LiquidationPrice: initLiq,
			CreatedAt:        entryT,
			UpdatedAt:        entryT,
		}
		initSignalSim := entities.Signal{
			ID:           1,
			Symbol:       symbol,
			StrategyID:   strategyID,
			Status:       entities.Open,
			PositionSide: side,
			CreatedAt:    entryT,
			UpdatedAt:    entryT,
			Orders:       []entities.Order{initOrderSim},
		}
		require.NoError(t, repoSim.Create(initSignalSim))

		fakeExSim := &dryRunParityExchange{}
		signalUCSim := usecase.NewSignalUseCase(repoSim, accountSim, fakeExSim, nil, nil, nil)
		simEvaluator := engine.NewSimulatedStopEvaluator(signalUCSim, fakeExSim, repoSim)

		// -------------------------------------------------------------
		// Step 1: 08:00:00 UTC Funding Boundary Crossing
		// -------------------------------------------------------------
		candle07 := exchange.Candle{
			Symbol:    symbol,
			Timeframe: "1h",
			OpenTime:  entryT,
			Open:      100, High: 101, Low: 99, Close: 100,
		}
		candle08 := exchange.Candle{
			Symbol:    symbol,
			Timeframe: "1h",
			OpenTime:  boundaryT,
			Open:      100, High: 101, Low: 99, Close: 100,
		}

		// Replay/Backtest path at 08:00:00 UTC
		exBacktest.SetSimulatedTime(boundaryT, candle08)
		require.NoError(t, backtestEvaluator.BeforeCycle(context.Background(), strat, symbol, strategies.ModeBacktest))

		// DryRun path evaluating candle07 which closed at 08:00:00 UTC
		fakeExSim.candles = []exchange.Candle{candle07}
		simEvaluator.Now = func() time.Time { return boundaryT.Add(time.Minute) }
		require.NoError(t, simEvaluator.BeforeCycle(context.Background(), strat, symbol, strategies.ModeDryRun))

		// Verify funding settlement parity
		sigAfterFundingBT, err := repoBacktest.GetOpenSignals(symbol, strategyID)
		require.NoError(t, err)
		sigAfterFundingSim, err := repoSim.GetOpenSignals(symbol, strategyID)
		require.NoError(t, err)

		orderBT := sigAfterFundingBT.Orders[0]
		orderSim := sigAfterFundingSim.Orders[0]

		require.NotZero(t, orderBT.AccumulatedFunding, "Backtest must have accrued funding at 08:00 UTC")
		require.NotZero(t, orderSim.AccumulatedFunding, "Simulated must have accrued funding at 08:00 UTC")

		assert.Equal(t, orderBT.AccumulatedFunding, orderSim.AccumulatedFunding, "AccumulatedFunding mismatch between Backtest and Simulated")
		assert.Equal(t, orderBT.LiquidationPrice, orderSim.LiquidationPrice, "LiquidationPrice mismatch after funding settlement")
		require.NotNil(t, orderBT.LastFundingTime)
		require.NotNil(t, orderSim.LastFundingTime)
		assert.Equal(t, boundaryT, *orderBT.LastFundingTime)
		assert.Equal(t, boundaryT, *orderSim.LastFundingTime)

		postFundingLiq := float64(orderBT.LiquidationPrice)

		// -------------------------------------------------------------
		// Step 2: 09:00:00 UTC Liquidation Wick
		// -------------------------------------------------------------
		var candleWick exchange.Candle
		if side == entities.PositionSideLong {
			// Long liquidated when Low <= LiquidationPrice
			candleWick = exchange.Candle{
				Symbol:    symbol,
				Timeframe: "1h",
				OpenTime:  wickT,
				Open:      100,
				High:      101,
				Low:       postFundingLiq - 1.0, // Pierces long liquidation price
				Close:     postFundingLiq,
			}
		} else {
			// Short liquidated when High >= LiquidationPrice
			candleWick = exchange.Candle{
				Symbol:    symbol,
				Timeframe: "1h",
				OpenTime:  wickT,
				Open:      100,
				High:      postFundingLiq + 1.0, // Pierces short liquidation price
				Low:       99,
				Close:     postFundingLiq,
			}
		}

		// Replay/Backtest path at 09:00:00 UTC
		exBacktest.SetSimulatedTime(wickT, candleWick)
		require.NoError(t, backtestEvaluator.BeforeCycle(context.Background(), strat, symbol, strategies.ModeBacktest))

		// DryRun path evaluating candleWick which closed at 10:00:00 UTC
		fakeExSim.candles = []exchange.Candle{candle07, candle08, candleWick}
		simEvaluator.Now = func() time.Time { return wickT.Add(time.Hour).Add(time.Minute) }
		require.NoError(t, simEvaluator.BeforeCycle(context.Background(), strat, symbol, strategies.ModeDryRun))

		// Verify liquidation parity
		closedSignalBT, err := repoBacktest.GetByID(sigAfterFundingBT.ID)
		require.NoError(t, err)
		closedSignalSim, err := repoSim.GetByID(sigAfterFundingSim.ID)
		require.NoError(t, err)

		assert.Equal(t, entities.Closed, closedSignalBT.Status, "Backtest signal must be closed by liquidation")
		assert.Equal(t, entities.Closed, closedSignalSim.Status, "Simulated signal must be closed by liquidation")

		closedOrderBT := closedSignalBT.Orders[0]
		closedOrderSim := closedSignalSim.Orders[0]

		// 1. Both accrue identical AccumulatedFunding
		assert.Equal(t, closedOrderBT.AccumulatedFunding, closedOrderSim.AccumulatedFunding, "AccumulatedFunding must remain identical at exit")

		// 2. Both trigger entities.ExitReasonLiquidation
		assert.Equal(t, entities.ExitReasonLiquidation, closedOrderBT.ExitReason, "Backtest exit reason must be liquidation")
		assert.Equal(t, entities.ExitReasonLiquidation, closedOrderSim.ExitReason, "Simulated exit reason must be liquidation")

		// 3. Both exit at the exact LiquidationPrice
		assert.InDelta(t, postFundingLiq, float64(closedOrderBT.ExitPrice), 0.001, "Backtest must exit at exact liquidation price")
		assert.InDelta(t, postFundingLiq, float64(closedOrderSim.ExitPrice), 0.001, "Simulated must exit at exact liquidation price")
		assert.Equal(t, closedOrderBT.ExitPrice, closedOrderSim.ExitPrice, "ExitPrice mismatch between Backtest and Simulated")
	}

	t.Run("Long 20x Position: Funding at 08:00 UTC and Liquidation on Wick", func(t *testing.T) {
		runTest(t, entities.PositionSideLong)
	})

	t.Run("Short 20x Position: Funding at 08:00 UTC and Liquidation on Wick", func(t *testing.T) {
		runTest(t, entities.PositionSideShort)
	})
}

// dryRunParityExchange satisfies engine.SimulatedStopExchange and exchange.ExchangeClient for Test 2.
type dryRunParityExchange struct {
	candles []exchange.Candle
}

func (f *dryRunParityExchange) ListKline(ctx context.Context, symbol, interval string, limit int) ([]exchange.Candle, error) {
	return f.candles, nil
}
func (f *dryRunParityExchange) TriggerStop(orderID string, quantity, stopPrice float64, at time.Time) exchange.OrderResult {
	return exchange.OrderResult{AvgFillPrice: stopPrice, Status: exchange.OrderStatusFilled}
}
func (f *dryRunParityExchange) ClearTriggeredStop(orderID string) {}
func (f *dryRunParityExchange) SimulatesOrders() bool              { return true }

func (f *dryRunParityExchange) PlaceOrder(ctx context.Context, order exchange.PlaceOrderRequest) (exchange.OrderResult, error) {
	return exchange.OrderResult{BrokerOrderID: "sim-place-1", Status: exchange.OrderStatusFilled, AvgFillPrice: 100.0, ExecutedQty: order.Quantity}, nil
}
func (f *dryRunParityExchange) CancelOrder(ctx context.Context, symbol, orderID string) error {
	return nil
}
func (f *dryRunParityExchange) GetOrder(ctx context.Context, symbol, orderID string) (exchange.OrderResult, error) {
	return exchange.OrderResult{}, nil
}
func (f *dryRunParityExchange) ListTickerPrices(ctx context.Context, symbol string) ([]exchange.TickerPrice, error) {
	return nil, nil
}
func (f *dryRunParityExchange) GetAccountBalance(ctx context.Context) (exchange.AccountBalance, error) {
	return exchange.AccountBalance{Asset: "USDT", Free: 1000}, nil
}
func (f *dryRunParityExchange) SubscribeKline(ctx context.Context, symbol, interval string) (<-chan exchange.Candle, error) {
	return nil, nil
}
func (f *dryRunParityExchange) SubscribeBookTicker(ctx context.Context, symbol string) (<-chan exchange.BookTicker, error) {
	return nil, nil
}
