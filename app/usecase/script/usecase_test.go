package script_test

import (
	"context"
	"math"
	"testing"
	"time"

	"go-trade-bot/app/entities"
	strategyscript "go-trade-bot/app/strategies/script"
	scriptuc "go-trade-bot/app/usecase/script"
	"go-trade-bot/internal/exchange"
	"go-trade-bot/internal/indicators"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- fixtures & fakes -------------------------------------------------------

func fixtureCandles(symbol string, count int) []exchange.Candle {
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	out := make([]exchange.Candle, count)
	for i := 0; i < count; i++ {
		price := 100.0 + math.Sin(float64(i)*0.35)*8.0
		out[i] = exchange.Candle{
			Symbol:    symbol,
			Timeframe: "1m",
			OpenTime:  base.Add(time.Duration(i) * time.Minute),
			Open:      price - 0.2,
			High:      price + 1.0,
			Low:       price - 1.0,
			Close:     price,
			Volume:    10.0,
		}
	}
	return out
}

type spyLiveSource struct {
	candles   []exchange.Candle
	lastLimit int
	calls     int
}

func (s *spyLiveSource) ListKline(_ context.Context, _, _ string, limit int) ([]exchange.Candle, error) {
	s.lastLimit = limit
	s.calls++
	if limit < len(s.candles) {
		return s.candles[:limit], nil
	}
	return s.candles, nil
}

type spyHistoricalSource struct {
	candles    []exchange.Candle
	lastWindow int
	calls      int
}

func (s *spyHistoricalSource) RangeBefore(_ context.Context, _, _ string, _ time.Time, window int) ([]exchange.Candle, error) {
	s.lastWindow = window
	s.calls++
	if window < len(s.candles) {
		return s.candles[len(s.candles)-window:], nil
	}
	return s.candles, nil
}

type fakeStrategyRepo struct {
	strategy entities.Strategy
	err      error
}

func (f *fakeStrategyRepo) GetByID(_ context.Context, _ uint) (entities.Strategy, error) {
	return f.strategy, f.err
}

func newRunner() *strategyscript.Runner {
	return strategyscript.NewRunner(strategyscript.DefaultHookTimeout, nil)
}

// --- AC1: bare arithmetic ---------------------------------------------------

func TestEval_Arithmetic(t *testing.T) {
	live := &spyLiveSource{candles: fixtureCandles("BTCUSDT", 100)}
	uc := scriptuc.NewUseCase(live, nil, nil, newRunner())

	resp, err := uc.Eval(context.Background(), scriptuc.EvalRequest{Source: "return 2 + 2", Symbol: "BTCUSDT"})
	require.NoError(t, err)
	assert.Empty(t, resp.Error)
	assert.Equal(t, 4.0, resp.Result)
}

// --- AC2: ind.rsi(14) matches a direct IndicatorProvider.RSI over the same data

func TestEval_RSIMatchesDirectProvider(t *testing.T) {
	candles := fixtureCandles("BTCUSDT", 100)
	live := &spyLiveSource{candles: candles}
	uc := scriptuc.NewUseCase(live, nil, nil, newRunner())

	resp, err := uc.Eval(context.Background(), scriptuc.EvalRequest{Source: "return ind.rsi(14)", Symbol: "BTCUSDT"})
	require.NoError(t, err)
	require.Empty(t, resp.Error)

	rsiSeries := indicators.NewTalibAdapter().RSI(candles, 14)
	require.NotEmpty(t, rsiSeries)
	expected := rsiSeries[len(rsiSeries)-1]

	got, ok := resp.Result.(float64)
	require.True(t, ok, "result should be a number, got %T", resp.Result)
	assert.InDelta(t, expected, got, 1e-9)

	// The trace must contain the rsi indicator call.
	require.Len(t, resp.Trace, 1)
	sawRSI := false
	for _, ind := range resp.Trace[0].Indicators {
		if ind.Name == "rsi" {
			sawRSI = true
			assert.InDelta(t, expected, ind.Value, 1e-9)
		}
	}
	assert.True(t, sawRSI, "trace should contain the rsi call")
}

// --- AC3: syntactically invalid script --------------------------------------

func TestEval_InvalidScript(t *testing.T) {
	live := &spyLiveSource{candles: fixtureCandles("BTCUSDT", 100)}
	uc := scriptuc.NewUseCase(live, nil, nil, newRunner())

	resp, err := uc.Eval(context.Background(), scriptuc.EvalRequest{Source: "return (", Symbol: "BTCUSDT"})
	require.NoError(t, err)
	assert.NotEmpty(t, resp.Error)
	assert.Nil(t, resp.Result)
	assert.Empty(t, resp.Trace)
}

// --- AC8: default window == 100 candles requested ---------------------------

func TestEval_DefaultWindowIs100(t *testing.T) {
	live := &spyLiveSource{candles: fixtureCandles("BTCUSDT", 150)}
	uc := scriptuc.NewUseCase(live, nil, nil, newRunner())

	_, err := uc.Eval(context.Background(), scriptuc.EvalRequest{Source: "return 1", Symbol: "BTCUSDT"})
	require.NoError(t, err)
	assert.Equal(t, 100, live.lastLimit)
}

// --- fast-rerun -------------------------------------------------------------

const entryExitScript = `
function should_long(ctx)
  return ctx.price < 100
end
function go_long(ctx)
  return {buy = {price = ctx.price}}
end
function update_position(ctx)
  if ctx.price >= ctx.position.entry_price * 1.005 then
    return {sell = {price = ctx.price}}
  end
  return nil
end
`

// --- AC5: fast-rerun over a 100-candle window yields 100 records with
// signals on the entering/exiting cycles.
func TestFastRerun_HistoricalWindowProducesPerCandleTrace(t *testing.T) {
	candles := fixtureCandles("BTCUSDT", 100)
	hist := &spyHistoricalSource{candles: candles}
	repo := &fakeStrategyRepo{strategy: entities.Strategy{ID: 1, Name: "s", StrategyName: "script"}}
	uc := scriptuc.NewUseCase(nil, hist, repo, newRunner())

	end := candles[len(candles)-1].OpenTime.Add(time.Minute)
	resp, err := uc.FastRerun(context.Background(), scriptuc.FastRerunRequest{
		StrategyID: 1,
		Source:     entryExitScript,
		Symbol:     "BTCUSDT",
		EndTime:    &end,
	})
	require.NoError(t, err)
	assert.Empty(t, resp.Error)
	assert.Len(t, resp.Trace, 100, "one trace record per candle")

	signalCycles := 0
	for _, r := range resp.Trace {
		if r.Signal != nil {
			signalCycles++
		}
	}
	assert.Greater(t, signalCycles, 0, "the entry/exit script should produce signals within the window")
}

// --- AC9: end_time omitted -> live fetch, not historical --------------------

func TestFastRerun_NoEndTimeUsesLiveSource(t *testing.T) {
	candles := fixtureCandles("BTCUSDT", 100)
	live := &spyLiveSource{candles: candles}
	hist := &spyHistoricalSource{candles: candles}
	repo := &fakeStrategyRepo{strategy: entities.Strategy{ID: 1, StrategyName: "script"}}
	uc := scriptuc.NewUseCase(live, hist, repo, newRunner())

	_, err := uc.FastRerun(context.Background(), scriptuc.FastRerunRequest{
		StrategyID: 1,
		Source:     entryExitScript,
		Symbol:     "BTCUSDT",
		EndTime:    nil,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, live.calls, "live source should be used when end_time is omitted")
	assert.Equal(t, 0, hist.calls, "historical source must not be used when end_time is omitted")
	assert.Equal(t, 100, live.lastLimit)
}
