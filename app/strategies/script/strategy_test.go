package script_test

import (
	"context"
	"errors"
	"testing"

	"time"

	"go-trade-bot/app/engine"
	"go-trade-bot/app/entities"
	scriptstate_repo "go-trade-bot/app/repository/scriptstate"
	"go-trade-bot/app/strategies"
	"go-trade-bot/app/strategies/script"
	signalusecase "go-trade-bot/app/usecase/signal"
	"go-trade-bot/internal/exchange"
	"go-trade-bot/internal/indicators"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// --- fakes -----------------------------------------------------------------

type fakeStore struct {
	state    map[string]interface{}
	saveErr  error
	loadErr  error
	saveSeen map[string]interface{}
}

func (f *fakeStore) Load(_ context.Context, _ uint, _ string) (map[string]interface{}, error) {
	if f.loadErr != nil {
		return nil, f.loadErr
	}
	if f.state == nil {
		return map[string]interface{}{}, nil
	}
	return f.state, nil
}

func (f *fakeStore) Save(_ context.Context, _ uint, _ string, state map[string]interface{}) error {
	f.saveSeen = state
	if f.saveErr != nil {
		return f.saveErr
	}
	f.state = state
	return nil
}

func newRunner() *script.Runner { return script.NewRunner(script.DefaultHookTimeout, nil) }

func ctxFor(symbol string) strategies.Context {
	return strategies.Context{Symbol: symbol, Mode: strategies.ModeBacktest}
}

// --- AC#4: should_long error -> false, no panic ----------------------------

func TestScriptStrategy_ShouldLongErrorFailsClosed(t *testing.T) {
	strat := script.NewScriptStrategy(
		entities.Strategy{ID: 1, Name: "s", ScriptSource: `function should_long(ctx) error("boom") end`},
		&fakeStore{}, newRunner(),
	)
	require.NotPanics(t, func() {
		require.False(t, strat.ShouldLong(ctxFor("BTCUSDT")))
	})
}

// --- AC#5: go_long error -> empty Signal -----------------------------------

func TestScriptStrategy_GoLongErrorReturnsEmptySignal(t *testing.T) {
	strat := script.NewScriptStrategy(
		entities.Strategy{ID: 1, Name: "s", ScriptSource: `function go_long(ctx) error("boom") end`},
		&fakeStore{}, newRunner(),
	)
	sig := strat.GoLong(ctxFor("BTCUSDT"))
	require.Nil(t, sig.Buy)
	require.Nil(t, sig.Sell)
}

// --- AC#10: state-save failure must not discard the computed Signal --------

func TestScriptStrategy_SaveFailureDoesNotLoseSignal(t *testing.T) {
	store := &fakeStore{saveErr: errors.New("db down")}
	strat := script.NewScriptStrategy(
		entities.Strategy{ID: 1, Name: "s", ScriptSource: `function go_long(ctx) return {buy={qty=1, price=100}} end`},
		store, newRunner(),
	)
	sig := strat.GoLong(ctxFor("BTCUSDT"))
	require.NotNil(t, sig.Buy)
	require.Equal(t, 100.0, sig.Buy.Price)
	require.Equal(t, 1.0, sig.Buy.Qty)
}

// --- AC#8: two script rows resolve independently, no shared-instance bleed --

func TestScriptStrategy_TwoRowsIndependent(t *testing.T) {
	runner := newRunner()
	store := &fakeStore{}
	strategies.Register("script", func(db entities.Strategy) strategies.Strategy {
		return script.NewScriptStrategy(db, store, runner)
	})
	t.Cleanup(func() { /* registry has no unregister; test-scoped name reused per run */ })

	a, ok := strategies.Get("script", entities.Strategy{ID: 1, Name: "alpha", ScriptSource: `function should_long(ctx) return true end`})
	require.True(t, ok)
	b, ok := strategies.Get("script", entities.Strategy{ID: 2, Name: "beta", ScriptSource: `function should_long(ctx) return false end`})
	require.True(t, ok)

	require.NotSame(t, a, b)
	require.Equal(t, "alpha", a.Name())
	require.Equal(t, "beta", b.Name())
	require.True(t, a.ShouldLong(ctxFor("BTCUSDT")))
	require.False(t, b.ShouldLong(ctxFor("BTCUSDT")))
}

// --- fake exchange/signal for the AC#3 engine integration test -------------

func mustTime() time.Time { return time.Unix(1_700_000_000, 0).UTC() }

// stubExchange returns a single fixed candle for any ListKline call and
// zero-values everything else - enough for the engine's buildContext.
type stubExchange struct{ candle exchange.Candle }

func (s *stubExchange) PlaceOrder(context.Context, exchange.PlaceOrderRequest) (exchange.OrderResult, error) {
	return exchange.OrderResult{}, nil
}
func (s *stubExchange) CancelOrder(context.Context, string, string) error { return nil }
func (s *stubExchange) GetOrder(context.Context, string, string) (exchange.OrderResult, error) {
	return exchange.OrderResult{}, nil
}
func (s *stubExchange) ListKline(_ context.Context, _, _ string, _ int) ([]exchange.Candle, error) {
	return []exchange.Candle{s.candle}, nil
}
func (s *stubExchange) ListTickerPrices(context.Context, string) ([]exchange.TickerPrice, error) {
	return nil, nil
}
func (s *stubExchange) GetAccountBalance(context.Context) (exchange.AccountBalance, error) {
	return exchange.AccountBalance{}, nil
}
func (s *stubExchange) SubscribeKline(context.Context, string, string) (<-chan exchange.Candle, error) {
	return nil, nil
}

// stubSignalUC reports "no open position" so the engine runs the entry path.
type stubSignalUC struct{}

func (s *stubSignalUC) GenerateBuySignal(signalusecase.EntrySignal) error  { return nil }
func (s *stubSignalUC) GenerateSellSignal(signalusecase.ExitSignal) error  { return nil }
func (s *stubSignalUC) GetOpenSignal(string, uint) (entities.Signal, error) {
	return entities.Signal{}, nil
}

func TestScriptStrategy_StatePersistsAcrossEngineRuns(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:script_ac3?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&entities.ScriptState{}))
	store := script.NewScriptStateStore(scriptstate_repo.NewRepository(db))
	runner := newRunner()

	source := `
function before(ctx)
  state.cycles = (state.cycles or 0) + 1
end
function should_long(ctx)
  return false
end
`
	dbStrat := entities.Strategy{ID: 7, Name: "counter", StrategyName: "script", ScriptSource: source}
	strat := script.NewScriptStrategy(dbStrat, store, runner)

	eng := engine.NewEngine(
		&stubExchange{candle: exchange.Candle{Symbol: "BTCUSDT", Close: 100, OpenTime: mustTime()}},
		indicators.NewTalibAdapter(),
		&stubSignalUC{},
		nil, nil, nil, nil,
	)

	for i := 0; i < 3; i++ {
		require.NoError(t, eng.Run(context.Background(), strat, dbStrat, "BTCUSDT", strategies.ModeBacktest))
	}

	final, err := store.Load(context.Background(), 7, "BTCUSDT")
	require.NoError(t, err)
	require.Equal(t, 3.0, final["cycles"])
}
