package engine_test

import (
	"context"
	"math"
	"testing"
	"time"

	"go-trade-bot/app/engine"
	"go-trade-bot/app/entities"
	"go-trade-bot/app/repository/candle"
	"go-trade-bot/app/strategies"
	"go-trade-bot/app/strategies/script"
	usecase "go-trade-bot/app/usecase/signal"
	"go-trade-bot/internal/exchange"
	"go-trade-bot/internal/feed"
	"go-trade-bot/internal/indicators"
	"go-trade-bot/internal/memcache"
	"go-trade-bot/internal/metrics_provider"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type memorySignalRepo struct {
	signals []entities.Signal
}

func (m *memorySignalRepo) Create(signal entities.Signal) error {
	signal.ID = uint(len(m.signals) + 1)
	m.signals = append(m.signals, signal)
	return nil
}

func (m *memorySignalRepo) GetOpenSignals(symbol string, strategyId uint) (entities.Signal, error) {
	for _, s := range m.signals {
		if s.Symbol == symbol && s.StrategyID == strategyId && s.Status == entities.Open {
			return s, nil
		}
	}
	return entities.Signal{}, nil
}

func (m *memorySignalRepo) Update(signal entities.Signal) error {
	for i, s := range m.signals {
		if s.ID == signal.ID {
			m.signals[i] = signal
			return nil
		}
	}
	return nil
}

func (m *memorySignalRepo) GetByID(id uint) (entities.Signal, error) {
	for _, s := range m.signals {
		if s.ID == id {
			return s, nil
		}
	}
	return entities.Signal{}, nil
}

func (m *memorySignalRepo) GetAll() ([]entities.Signal, error) {
	return m.signals, nil
}

func (m *memorySignalRepo) GetAllOpenSignals() ([]entities.Signal, error) {
	var res []entities.Signal
	for _, s := range m.signals {
		if s.Status == entities.Open {
			res = append(res, s)
		}
	}
	return res, nil
}

func (m *memorySignalRepo) GetAllClosedSignals() ([]entities.Signal, error) {
	var res []entities.Signal
	for _, s := range m.signals {
		if s.Status == entities.Closed {
			res = append(res, s)
		}
	}
	return res, nil
}

type memoryAccountUseCase struct {
	amount float32
}

func (a *memoryAccountUseCase) DeductOrder(entryPrice float32) error {
	a.amount -= entryPrice
	return nil
}

func (a *memoryAccountUseCase) AddOrder(exitPrice float32) error {
	a.amount += exitPrice
	return nil
}

func (a *memoryAccountUseCase) GetDisponibleAmout() (float32, error) {
	return 100.0, nil
}

func (a *memoryAccountUseCase) CanOpenOrder() (bool, error) {
	return a.amount > 0, nil
}

func (a *memoryAccountUseCase) GetAccount() (entities.Account, error) {
	return entities.Account{ID: 1, Amount: a.amount, AvailableOrders: 5, Currency: "USDT"}, nil
}

type fixedSequenceExchange struct {
	candles []exchange.Candle
	client  *engine.SimulatedFillExchange
}

func (f *fixedSequenceExchange) PlaceOrder(ctx context.Context, order exchange.PlaceOrderRequest) (exchange.OrderResult, error) {
	return f.client.PlaceOrder(ctx, order)
}

func (f *fixedSequenceExchange) CancelOrder(ctx context.Context, symbol, orderID string) error {
	return f.client.CancelOrder(ctx, symbol, orderID)
}

func (f *fixedSequenceExchange) GetOrder(ctx context.Context, symbol, orderID string) (exchange.OrderResult, error) {
	return f.client.GetOrder(ctx, symbol, orderID)
}

func (f *fixedSequenceExchange) ListKline(ctx context.Context, symbol, interval string, limit int) ([]exchange.Candle, error) {
	return f.client.ListKline(ctx, symbol, interval, limit)
}

func (f *fixedSequenceExchange) ListTickerPrices(ctx context.Context, symbol string) ([]exchange.TickerPrice, error) {
	return f.client.ListTickerPrices(ctx, symbol)
}

func (f *fixedSequenceExchange) GetAccountBalance(ctx context.Context) (exchange.AccountBalance, error) {
	return f.client.GetAccountBalance(ctx)
}

func (f *fixedSequenceExchange) SubscribeKline(ctx context.Context, symbol, interval string) (<-chan exchange.Candle, error) {
	ch := make(chan exchange.Candle, len(f.candles)+1)
	for _, c := range f.candles {
		ch <- c
	}
	return ch, nil
}

type countedFeed struct {
	feed.Feed
	count int
	max   int
}

func (c *countedFeed) Next() (exchange.Candle, bool) {
	if c.count >= c.max {
		return exchange.Candle{}, false
	}
	candle, ok := c.Feed.Next()
	if !ok {
		return candle, false
	}
	c.count++
	return candle, true
}

func generateSyntheticCandles(count int) []entities.Candle {
	baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	candles := make([]entities.Candle, count)

	price := 100.0
	for i := 0; i < count; i++ {
		// Generate oscillating wave with trend
		wave := math.Sin(float64(i)*0.05) * 5.0
		noise := math.Cos(float64(i)*0.2) * 1.5
		price = 100.0 + wave + noise + float64(i)*0.01

		candles[i] = entities.Candle{
			Symbol:    "BTCUSDT",
			Timeframe: "1m",
			OpenTime:  baseTime.Add(time.Duration(i) * time.Minute),
			Open:      price - 0.5,
			High:      price + 1.0,
			Low:       price - 1.0,
			Close:     price + 0.2,
			Volume:    100.0 + math.Abs(wave)*10.0,
		}
	}
	return candles
}

// parityNopStore is a no-op ScriptStateStore for the parity test's script
// strategy (which does not use persistent state).
type parityNopStore struct{}

func (parityNopStore) Load(context.Context, uint, string) (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}
func (parityNopStore) Save(context.Context, uint, string, map[string]interface{}) error { return nil }

func init() {
	runner := script.NewRunner(script.DefaultHookTimeout, nil)
	strategies.Register("script", func(db entities.Strategy) strategies.Strategy {
		return script.NewScriptStrategy(db, parityNopStore{}, runner)
	})
}

// parityScriptSource ports the retired template's RSI<45 entry / 1%
// take-profit exit to an equivalent Lua script, so the feed/driver parity
// test keeps its original trading behavior without the deleted template pkg.
const parityScriptSource = `
function should_long(ctx)
  local rsi = ind.rsi(14)
  return rsi < 45
end
function go_long(ctx)
  return {buy = {price = ctx.price}}
end
function update_position(ctx)
  local entry = ctx.position.entry_price
  local pnl = (ctx.price - entry) / entry * 100
  if pnl >= 1.0 then
    return {sell = {price = ctx.price}}
  end
  return nil
end
`

func TestSimulatorParity_Template(t *testing.T) {
	candles := generateSyntheticCandles(300)
	symbol := "BTCUSDT"
	timeframe := "1m"

	stratConfigJSON := datatypes.JSON(`{
		"stop_loss_pct": 1.0,
		"position_sizing": {
			"type": "pct_capital",
			"value": 10
		}
	}`)
	dbStrat := entities.Strategy{
		ID:               1,
		Name:             "script_rsi",
		StrategyName:     "script",
		ScriptSource:     parityScriptSource,
		MonitoredSymbols: datatypes.JSONSlice[string]{symbol},
		StrategyConfiguration: entities.StrategyConfiguration{
			Cycle:         entities.OneMinute,
			Configuration: stratConfigJSON,
		},
	}

	// -------------------------------------------------------------
	// Run A: ReplayFeed + ReplayDriver
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
	stratA, _ := strategies.Get("script", dbStrat)
	driverA := engine.NewReplayDriver(replayFeedA, simExchangeA, engineA, stratA, dbStrat, symbol, strategies.ModeBacktest, signalRepoA)

	tradesA, err := driverA.Run(context.Background())
	require.NoError(t, err)

	// -------------------------------------------------------------
	// Run B: LiveFeed (wrapping fixed sequence) + ReplayDriver
	// -------------------------------------------------------------
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
	signalUCA_B := usecase.NewSignalUseCase(signalRepoB, accountUCB, simExchangeB, nil, nil, nil)
	indicatorProviderB := indicators.NewTalibAdapter()
	cacheB := memcache.NewInMemoryCache()

	engineB := engine.NewEngine(simExchangeB, indicatorProviderB, signalUCA_B, accountUCB, nil, cacheB, nil)
	stratB, _ := strategies.Get("script", dbStratB(dbStrat))
	driverB := engine.NewReplayDriver(countedLiveFeedB, simExchangeB, engineB, stratB, dbStratB(dbStrat), symbol, strategies.ModeBacktest, signalRepoB)

	tradesB, err := driverB.Run(context.Background())
	require.NoError(t, err)

	// -------------------------------------------------------------
	// Parity Assertions
	// -------------------------------------------------------------
	require.Equal(t, len(tradesA), len(tradesB), "trade count mismatch between ReplayFeed and LiveFeed")

	for i := range tradesA {
		assert.Equal(t, tradesA[i].Symbol, tradesB[i].Symbol)
		assert.False(t, tradesA[i].EntryTime.IsZero())
		assert.False(t, tradesB[i].EntryTime.IsZero())
		assert.InDelta(t, tradesA[i].EntryPrice, tradesB[i].EntryPrice, 0.001)
		assert.False(t, tradesA[i].ExitTime.IsZero())
		assert.False(t, tradesB[i].ExitTime.IsZero())
		assert.InDelta(t, tradesA[i].ExitPrice, tradesB[i].ExitPrice, 0.001)
		assert.InDelta(t, tradesA[i].Profit, tradesB[i].Profit, 0.001)
		assert.Equal(t, tradesA[i].ExitReason, tradesB[i].ExitReason)
	}

	metricsProvider := metrics_provider.NewCinarMetricsAdapter()
	metricsA := metricsProvider.Compute(tradesA, 1000, 525600)
	metricsB := metricsProvider.Compute(tradesB, 1000, 525600)

	assert.InDelta(t, metricsA.TotalReturnPct, metricsB.TotalReturnPct, 0.001)
	assert.InDelta(t, metricsA.SharpeRatio, metricsB.SharpeRatio, 0.001)
	assert.InDelta(t, metricsA.MaxDrawdownPct, metricsB.MaxDrawdownPct, 0.001)
}

func dbStratB(s entities.Strategy) entities.Strategy {
	s.ID = 2
	return s
}
