package modules_test

// fix-01 acceptance tests: strategy cycles driven through the worker's real
// fx wiring (SignalModule + AccountModule + EngineModule + CacheModule +
// IndicatorsModule) and handler.StrategyProcessor, against a SQLite DB and a
// fake "real" exchange client that fails the test if an order method is ever
// called on a dryrun cycle.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go-trade-bot/app/engine"
	"go-trade-bot/app/entities"
	handler "go-trade-bot/app/handler/tasks/strategy"
	handlermocks "go-trade-bot/app/handler/tasks/strategy/mocks"
	"go-trade-bot/app/strategies"
	signalusecase "go-trade-bot/app/usecase/signal"
	"go-trade-bot/cmd/worker/modules"
	"go-trade-bot/internal/configuration"
	"go-trade-bot/internal/exchange"
	"go-trade-bot/internal/metrics"
	"go-trade-bot/internal/notifier"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
	"gorm.io/datatypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const fix01StrategyName = "fix01-golong-test"

// fix01LongEnabled gates the test strategy's ShouldLong so later cycles only
// exercise the stop path.
var fix01LongEnabled atomic.Bool

type fix01Strategy struct{}

func (fix01Strategy) Name() string                 { return fix01StrategyName }
func (fix01Strategy) Before(strategies.Context)    {}
func (fix01Strategy) After(strategies.Context)     {}
func (fix01Strategy) Terminate(strategies.Context) {}
func (fix01Strategy) ShouldLong(ctx strategies.Context) bool {
	return ctx.Position == nil && fix01LongEnabled.Load()
}
func (fix01Strategy) GoLong(ctx strategies.Context) strategies.Signal {
	return strategies.Signal{
		Buy:      &strategies.Order{Price: ctx.Price},
		StopLoss: &strategies.Order{Price: ctx.Price * 0.98},
	}
}
func (fix01Strategy) ShouldShort(strategies.Context) bool                  { return false }
func (fix01Strategy) GoShort(strategies.Context) strategies.Signal         { return strategies.Signal{} }
func (fix01Strategy) UpdatePosition(strategies.Context) *strategies.Signal { return nil }

func init() {
	strategies.Register(fix01StrategyName, func(entities.Strategy) strategies.Strategy { return fix01Strategy{} })
}

// fakeRealExchange stands in for the worker's real client (production
// adapter, or testnet adapter when Testnet=true). With allowOrders=false any
// PlaceOrder/CancelOrder fails the test.
type fakeRealExchange struct {
	t           *testing.T
	allowOrders bool

	mu      sync.Mutex
	candles []exchange.Candle
	price   float64
	placed  []exchange.PlaceOrderRequest
	seq     int
}

func (f *fakeRealExchange) setMarket(candles []exchange.Candle, price float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.candles, f.price = candles, price
}

func (f *fakeRealExchange) PlaceOrder(_ context.Context, o exchange.PlaceOrderRequest) (exchange.OrderResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.allowOrders {
		f.t.Errorf("REAL exchange PlaceOrder called on a non-live cycle: %+v", o)
		return exchange.OrderResult{}, fmt.Errorf("real PlaceOrder forbidden")
	}
	f.placed = append(f.placed, o)
	f.seq++
	id := fmt.Sprintf("%d", 900000+f.seq)
	if o.Type == exchange.OrderTypeStopMarket {
		return exchange.OrderResult{BrokerOrderID: id, Status: exchange.OrderStatusNew}, nil
	}
	return exchange.OrderResult{BrokerOrderID: id, Status: exchange.OrderStatusFilled, ExecutedQty: o.Quantity, AvgFillPrice: f.price, FilledAt: time.Now()}, nil
}

func (f *fakeRealExchange) CancelOrder(_ context.Context, symbol, id string) error {
	if !f.allowOrders {
		f.t.Errorf("REAL exchange CancelOrder called on a non-live cycle: %s %s", symbol, id)
		return fmt.Errorf("real CancelOrder forbidden")
	}
	return nil
}

func (f *fakeRealExchange) GetOrder(context.Context, string, string) (exchange.OrderResult, error) {
	return exchange.OrderResult{}, exchange.ErrOrderNotFound
}

func (f *fakeRealExchange) ListKline(context.Context, string, string, int) ([]exchange.Candle, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]exchange.Candle(nil), f.candles...), nil
}

func (f *fakeRealExchange) ListTickerPrices(_ context.Context, symbol string) ([]exchange.TickerPrice, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return []exchange.TickerPrice{{Symbol: symbol, Price: f.price}}, nil
}

func (f *fakeRealExchange) GetAccountBalance(context.Context) (exchange.AccountBalance, error) {
	return exchange.AccountBalance{Asset: "USDT", Free: 1000}, nil
}

func (f *fakeRealExchange) SubscribeKline(context.Context, string, string) (<-chan exchange.Candle, error) {
	return make(chan exchange.Candle), nil
}

type captureNotifier struct {
	mu     sync.Mutex
	events []notifier.Event
}

func (c *captureNotifier) Send(_ context.Context, e notifier.Event) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, e)
	return nil
}

func (c *captureNotifier) ofType(t notifier.EventType) []notifier.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []notifier.Event
	for _, e := range c.events {
		if e.Type == t {
			out = append(out, e)
		}
	}
	return out
}

type workerStack struct {
	db       *gorm.DB
	real     *fakeRealExchange
	notes    *captureNotifier
	realEng  *engine.Engine
	dryRun   modules.DryRunEngine
	strategy entities.Strategy
	proc     *handler.StrategyProcessor
}

func newWorkerStack(t *testing.T, mode string, ceiling strategies.ExecutionMode, testnet, allowRealOrders bool) *workerStack {
	t.Helper()
	return newWorkerStackWithSender(t, mode, ceiling, testnet, allowRealOrders, nil)
}

// newWorkerStackWithSender is newWorkerStack with the NotificationSender
// the trading path receives built by sender (nil = the capture notifier
// itself). Used by the agents-platform C-01 bridge fan-out test.
func newWorkerStackWithSender(t *testing.T, mode string, ceiling strategies.ExecutionMode, testnet, allowRealOrders bool, sender func(*captureNotifier) notifier.NotificationSender) *workerStack {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&entities.Signal{}, &entities.Order{}, &entities.Account{}))
	require.NoError(t, db.Create(&entities.Account{ID: 1, Amount: 1000, AvailableOrders: 2, Currency: "USDT"}).Error)

	s := &workerStack{
		db:    db,
		real:  &fakeRealExchange{t: t, allowOrders: allowRealOrders},
		notes: &captureNotifier{},
	}
	cfg := &configuration.Configuration{DryRun: configuration.DryRunConfig{SlippagePct: 0.1, FeePct: 0.1}}

	app := fx.New(
		fx.NopLogger,
		fx.Provide(
			func() *configuration.Configuration { return cfg },
			func() *gorm.DB { return db },
			func() exchange.ExchangeClient { return s.real },
			func() notifier.NotificationSender {
				if sender != nil {
					return sender(s.notes)
				}
				return s.notes
			},
			func() *metrics.MetricsCollector { return nil },
		),
		modules.CacheModule,
		modules.IndicatorsModule,
		modules.SignalModule,
		modules.AccountModule,
		modules.EngineModule,
		fx.Populate(&s.realEng, &s.dryRun),
	)
	require.NoError(t, app.Err())

	s.strategy = entities.Strategy{
		ID: 42, Name: "fix01", StrategyName: fix01StrategyName, Status: entities.Productive, Mode: mode,
		MonitoredSymbols: datatypes.JSONSlice[string]{"BTCUSDT"},
		StrategyConfiguration: entities.StrategyConfiguration{
			Cycle:         entities.OneMinute,
			Configuration: datatypes.JSON(`{"stop_loss_pct": 2}`),
		},
	}

	repo := new(handlermocks.StrategyRepository)
	repo.On("GetByID", mock.Anything, s.strategy.ID).Return(s.strategy, nil)
	repo.On("SaveExecution", mock.Anything, mock.Anything).Return(nil)
	w := new(handlermocks.StrategyWorker)
	w.On("EnqueueStrategyTask", mock.Anything).Return(nil)

	var procNotes notifier.NotificationSender = s.notes
	if sender != nil {
		procNotes = sender(s.notes)
	}
	s.proc = handler.NewStrategyProcessor(nil, w, repo, s.realEng, s.dryRun.Engine, procNotes, ceiling, testnet)
	return s
}

func (s *workerStack) setClock(now time.Time) {
	s.dryRun.Exchange.Now = func() time.Time { return now }
	s.dryRun.Stops.Now = func() time.Time { return now }
}

func (s *workerStack) cycle(t *testing.T) {
	t.Helper()
	payload, err := json.Marshal(entities.Strategy{ID: s.strategy.ID, Name: s.strategy.Name})
	require.NoError(t, err)
	require.NoError(t, s.proc.HandleStrategyTask(context.Background(), asynq.NewTask(handler.StrategyTask+s.strategy.Name, payload)))
}

func (s *workerStack) signals(t *testing.T) []entities.Signal {
	t.Helper()
	var out []entities.Signal
	require.NoError(t, s.db.Preload("Orders").Order("id").Find(&out).Error)
	return out
}

func (s *workerStack) account(t *testing.T) entities.Account {
	t.Helper()
	var a entities.Account
	require.NoError(t, s.db.First(&a, 1).Error)
	return a
}

var fix01T0 = time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

func minuteCandle(minute int, low float64) exchange.Candle {
	return exchange.Candle{Symbol: "BTCUSDT", Timeframe: "1m", OpenTime: fix01T0.Add(time.Duration(minute) * time.Minute), Open: 100, High: 101, Low: low, Close: 100, Volume: 10}
}

// AC1 + AC2: a dryrun GoLong-with-stop cycle through the real worker wiring
// never touches the real exchange's order methods and persists a simulated
// position; a later candle under the stop closes it via the simulated stop,
// and a repeat cycle over the same candle does nothing.
func TestWorkerDryRunCycle_SimulatedEntryAndPersistedStop(t *testing.T) {
	s := newWorkerStack(t, "dryrun", strategies.ModeLive, false, false)

	// Cycle 1: entry.
	fix01LongEnabled.Store(true)
	s.setClock(fix01T0)
	s.real.setMarket([]exchange.Candle{minuteCandle(-3, 99.5), minuteCandle(-2, 99.5), minuteCandle(-1, 99.5)}, 100)
	s.cycle(t)

	sigs := s.signals(t)
	require.Len(t, sigs, 1)
	sig := sigs[0]
	assert.Equal(t, entities.Open, sig.Status)
	assert.Equal(t, "dryrun", sig.Mode)
	require.Len(t, sig.Orders, 1)
	order := sig.Orders[0]
	assert.True(t, exchange.IsSimulatedOrderID(order.BrokerOrderID), order.BrokerOrderID)
	assert.True(t, exchange.IsSimulatedOrderID(order.StopLossOrderID), order.StopLossOrderID)
	assert.InDelta(t, 100.1, order.EntryPrice, 1e-3)         // ticker 100 + 0.1% slippage
	assert.InDelta(t, 98.0, order.StopLossPrice, 1e-3) // the strategy's own stop price (100*0.98) wins over stop_loss_pct
	assert.InDelta(t, 5.0, order.Quantity, 1e-3)             // 1000/2 orders / 100
	assert.InDelta(t, float64(order.InvestedAmount)*0.001, order.EntryFee, 1e-3)
	assert.Empty(t, s.real.placed)
	acc := s.account(t)
	assert.Equal(t, float32(499.5), acc.Amount, "simulated fills deduct from virtual account")
	assert.Equal(t, int64(1), acc.AvailableOrders)
	require.Len(t, s.notes.ofType(notifier.EventPositionOpened), 1)

	// Cycle 2: a closed candle above the stop - no trigger, watermark persisted.
	fix01LongEnabled.Store(false)
	s.setClock(fix01T0.Add(90 * time.Second))
	s.real.setMarket([]exchange.Candle{minuteCandle(-1, 99.5), minuteCandle(0, 99), minuteCandle(1, 50)}, 99)
	s.cycle(t)
	sig = s.signals(t)[0]
	assert.Equal(t, entities.Open, sig.Status)
	require.NotNil(t, sig.Orders[0].SimStopEvaluatedAt)
	assert.True(t, sig.Orders[0].SimStopEvaluatedAt.Equal(fix01T0), "watermark = last closed candle evaluated (10:00); the forming 10:01 candle is not evaluated")

	// Cycle 3: the 10:01 candle has closed with Low under the stop -> the
	// simulated stop fires and closes the position through GenerateSellSignal.
	s.setClock(fix01T0.Add(150 * time.Second))
	s.real.setMarket([]exchange.Candle{minuteCandle(0, 99), minuteCandle(1, 97), minuteCandle(2, 40)}, 97)
	s.cycle(t)

	sigs = s.signals(t)
	require.Len(t, sigs, 1)
	closed := sigs[0]
	assert.Equal(t, entities.Closed, closed.Status)
	stop := float64(order.StopLossPrice)
	assert.InDelta(t, stop*(1-0.001), closed.Orders[0].ExitPrice, 1e-3) // stop minus slippage
	assert.True(t, closed.Orders[0].IsClosing)
	assert.NotZero(t, closed.Orders[0].ExitFee)
	closedEvents := s.notes.ofType(notifier.EventPositionClosed)
	require.Len(t, closedEvents, 1)
	assert.Equal(t, signalusecase.ExitReasonSimulatedStopLoss, closedEvents[0].Data["exit_reason"])
	assert.Equal(t, "dryrun", closedEvents[0].Mode)

	// Cycle 4: same candles again - nothing happens.
	s.cycle(t)
	again := s.signals(t)
	require.Len(t, again, 1)
	assert.Equal(t, closed.Orders[0].ExitPrice, again[0].Orders[0].ExitPrice)
	assert.Equal(t, entities.Closed, again[0].Status)
	assert.Len(t, s.notes.ofType(notifier.EventPositionClosed), 1)
	assert.Len(t, s.notes.ofType(notifier.EventPositionOpened), 1)

	assert.Empty(t, s.real.placed, "no real order was ever placed")
	assert.InDelta(t, 988.02, float64(s.account(t).Amount), 1e-2, "simulated stop loss credits remaining capital minus loss and broker fees")
}

// AC3: live cycles (ceiling live, testnet false) still place real orders on
// the process client, unchanged.
func TestWorkerLiveCycle_StillPlacesRealOrders(t *testing.T) {
	s := newWorkerStack(t, "live", strategies.ModeLive, false, true)
	fix01LongEnabled.Store(true)
	defer fix01LongEnabled.Store(false)
	s.real.setMarket([]exchange.Candle{minuteCandle(-2, 99.5), minuteCandle(-1, 99.5)}, 100)

	s.cycle(t)

	require.Len(t, s.real.placed, 2)
	assert.Equal(t, exchange.OrderTypeMarket, s.real.placed[0].Type)
	assert.Equal(t, exchange.SideBuy, s.real.placed[0].Side)
	assert.Equal(t, exchange.OrderTypeStopMarket, s.real.placed[1].Type)
	assert.InDelta(t, 98.0, s.real.placed[1].StopPrice, 1e-9)

	sigs := s.signals(t)
	require.Len(t, sigs, 1)
	assert.Equal(t, "live", sigs[0].Mode)
	assert.False(t, exchange.IsSimulatedOrderID(sigs[0].Orders[0].BrokerOrderID))
	assert.False(t, exchange.IsSimulatedOrderID(sigs[0].Orders[0].StopLossOrderID))
	assert.Less(t, s.account(t).Amount, float32(1000), "live fills still deduct from the account")
}

// AC4: paper cycles (ceiling paper, Testnet=true) still use the process
// client - which ExchangeModule builds as the testnet adapter when
// Testnet=true - not the simulator.
func TestWorkerPaperCycle_StillUsesProcessTestnetClient(t *testing.T) {
	s := newWorkerStack(t, "paper", strategies.ModePaper, true, true)
	fix01LongEnabled.Store(true)
	defer fix01LongEnabled.Store(false)
	s.real.setMarket([]exchange.Candle{minuteCandle(-2, 99.5), minuteCandle(-1, 99.5)}, 100)

	s.cycle(t)

	require.Len(t, s.real.placed, 2)
	sigs := s.signals(t)
	require.Len(t, sigs, 1)
	assert.Equal(t, "paper", sigs[0].Mode)
	assert.False(t, exchange.IsSimulatedOrderID(sigs[0].Orders[0].BrokerOrderID))

	// The real engine's order client is the process client itself, never
	// the simulator; the dryrun engine's is the simulator.
	realUC, ok := s.realEng.SignalUseCase.(signalusecase.SignalUseCase)
	require.True(t, ok)
	assert.Same(t, s.real, realUC.Exchange)
	assert.False(t, exchange.IsSimulatedClient(s.realEng.Exchange))
	assert.True(t, exchange.IsSimulatedClient(s.dryRun.Engine.Exchange))
}

// AC5 through the real wiring: a backtest-mode strategy runs no hook and
// places nothing.
func TestWorkerBacktestCycle_Refused(t *testing.T) {
	s := newWorkerStack(t, "backtest", strategies.ModeLive, false, false)
	fix01LongEnabled.Store(true)
	defer fix01LongEnabled.Store(false)
	s.real.setMarket([]exchange.Candle{minuteCandle(-1, 99.5)}, 100)

	s.cycle(t)

	assert.Empty(t, s.signals(t))
	assert.Empty(t, s.real.placed)
	require.Len(t, s.notes.ofType(notifier.EventStrategyError), 1)
}
