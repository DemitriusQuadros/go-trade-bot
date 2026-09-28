package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"go-trade-bot/app/entities"
	"go-trade-bot/app/strategies"
	usecase "go-trade-bot/app/usecase/signal"
	"go-trade-bot/internal/exchange"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeStopSignals struct {
	open    entities.Signal
	sells   []usecase.ExitSignal
	sellErr error
}

func (f *fakeStopSignals) GetOpenSignal(string, uint) (entities.Signal, error) { return f.open, nil }
func (f *fakeStopSignals) GenerateSellSignal(e usecase.ExitSignal) error {
	f.sells = append(f.sells, e)
	return f.sellErr
}

type triggerCall struct {
	id         string
	qty, price float64
	at         time.Time
}

type fakeStopExchange struct {
	candles  []exchange.Candle
	triggers []triggerCall
	cleared  []string
}

func (f *fakeStopExchange) ListKline(context.Context, string, string, int) ([]exchange.Candle, error) {
	return f.candles, nil
}
func (f *fakeStopExchange) TriggerStop(id string, qty, price float64, at time.Time) exchange.OrderResult {
	f.triggers = append(f.triggers, triggerCall{id, qty, price, at})
	return exchange.OrderResult{BrokerOrderID: id, Status: exchange.OrderStatusFilled, ExecutedQty: qty, AvgFillPrice: price * 0.999, FilledAt: at}
}
func (f *fakeStopExchange) ClearTriggeredStop(id string) { f.cleared = append(f.cleared, id) }

type fakeWatermarks struct {
	marks map[uint]time.Time
}

func (f *fakeWatermarks) UpdateSimStopEvaluatedAt(id uint, at time.Time) error {
	if f.marks == nil {
		f.marks = map[uint]time.Time{}
	}
	f.marks[id] = at
	return nil
}

var stopT0 = time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

func stopTestStrategy() entities.Strategy {
	return entities.Strategy{ID: 7, Name: "S7", StrategyConfiguration: entities.StrategyConfiguration{Cycle: entities.OneMinute}}
}

func openSimSignal(stopID string, stop float32, watermark *time.Time) entities.Signal {
	return entities.Signal{ID: 3, Status: entities.Open, Orders: []entities.Order{{
		ID: 11, BrokerOrderID: "SIM-1-1", StopLossOrderID: stopID, StopLossPrice: stop,
		Quantity: 2, EntryPrice: 100, CreatedAt: stopT0, SimStopEvaluatedAt: watermark,
	}}}
}

func candleAt(minute int, low float64) exchange.Candle {
	return exchange.Candle{OpenTime: stopT0.Add(time.Duration(minute) * time.Minute), Low: low, Close: 100, High: 101, Open: 100}
}

func newEvaluator(sig *fakeStopSignals, ex *fakeStopExchange, wm *fakeWatermarks, now time.Time) *SimulatedStopEvaluator {
	ev := NewSimulatedStopEvaluator(sig, ex, wm)
	ev.Now = func() time.Time { return now }
	return ev
}

func TestSimulatedStop_FiresOnFirstClosedPostEntryCandleBelowStop(t *testing.T) {
	sig := &fakeStopSignals{open: openSimSignal("SIM-STOP-1-2", 98, nil)}
	ex := &fakeStopExchange{candles: []exchange.Candle{
		candleAt(-1, 90), // opened before the entry fill: ignored
		candleAt(0, 99),
		candleAt(1, 97.5), // closed, below the stop: fires
		candleAt(2, 80),   // still forming at now: ignored
	}}
	wm := &fakeWatermarks{}
	ev := newEvaluator(sig, ex, wm, stopT0.Add(2*time.Minute+30*time.Second))

	require.NoError(t, ev.BeforeCycle(context.Background(), stopTestStrategy(), "BTCUSDT", strategies.ModeDryRun))

	require.Len(t, ex.triggers, 1)
	assert.Equal(t, triggerCall{"SIM-STOP-1-2", 2, 98, stopT0.Add(2 * time.Minute)}, ex.triggers[0])
	assert.Equal(t, []string{"SIM-STOP-1-2"}, ex.cleared)
	require.Len(t, sig.sells, 1)
	assert.Equal(t, usecase.ExitReasonSimulatedStopLoss, sig.sells[0].ExitReason)
	assert.Equal(t, "dryrun", sig.sells[0].Mode)
	assert.InDelta(t, 98*0.999, float64(sig.sells[0].ExitPrice), 1e-4)
	assert.Empty(t, wm.marks, "watermark is not advanced when the stop fired")
}

func TestSimulatedStop_NoTriggerAdvancesWatermark_AndCandleIsNeverReEvaluated(t *testing.T) {
	sig := &fakeStopSignals{open: openSimSignal("SIM-STOP-1-2", 98, nil)}
	ex := &fakeStopExchange{candles: []exchange.Candle{candleAt(0, 99), candleAt(1, 98.5)}}
	wm := &fakeWatermarks{}
	now := stopT0.Add(3 * time.Minute)

	require.NoError(t, newEvaluator(sig, ex, wm, now).BeforeCycle(context.Background(), stopTestStrategy(), "BTCUSDT", strategies.ModeDryRun))
	assert.Empty(t, ex.triggers)
	assert.Equal(t, stopT0.Add(time.Minute), wm.marks[11])

	// A candle at or before the persisted watermark is never evaluated again,
	// even if (e.g. after a data correction) its Low is now under the stop.
	mark := wm.marks[11]
	sig.open = openSimSignal("SIM-STOP-1-2", 98, &mark)
	ex.candles = []exchange.Candle{candleAt(0, 50), candleAt(1, 50)}
	require.NoError(t, newEvaluator(sig, ex, wm, now).BeforeCycle(context.Background(), stopTestStrategy(), "BTCUSDT", strategies.ModeDryRun))
	assert.Empty(t, ex.triggers)
	assert.Empty(t, sig.sells)
}

func TestSimulatedStop_IgnoresRealStopsAndPositionsWithoutStop(t *testing.T) {
	for name, open := range map[string]entities.Signal{
		"no open signal":  {},
		"real stop id":    openSimSignal("123456", 98, nil),
		"no stop":         openSimSignal("", 0, nil),
		"zero stop price": openSimSignal("SIM-STOP-1-2", 0, nil),
	} {
		t.Run(name, func(t *testing.T) {
			sig := &fakeStopSignals{open: open}
			ex := &fakeStopExchange{candles: []exchange.Candle{candleAt(0, 1)}}
			wm := &fakeWatermarks{}
			require.NoError(t, newEvaluator(sig, ex, wm, stopT0.Add(time.Hour)).BeforeCycle(context.Background(), stopTestStrategy(), "BTCUSDT", strategies.ModeDryRun))
			assert.Empty(t, ex.triggers)
			assert.Empty(t, sig.sells)
			assert.Empty(t, wm.marks)
		})
	}
}

func TestSimulatedStop_FailedCloseIsReturnedAndRetriedNextCycle(t *testing.T) {
	sig := &fakeStopSignals{open: openSimSignal("SIM-STOP-1-2", 98, nil), sellErr: errors.New("db down")}
	ex := &fakeStopExchange{candles: []exchange.Candle{candleAt(0, 97)}}
	wm := &fakeWatermarks{}

	err := newEvaluator(sig, ex, wm, stopT0.Add(time.Hour)).BeforeCycle(context.Background(), stopTestStrategy(), "BTCUSDT", strategies.ModeDryRun)
	assert.ErrorContains(t, err, "db down")
	assert.Equal(t, []string{"SIM-STOP-1-2"}, ex.cleared)
	assert.Empty(t, wm.marks)
}

func TestSimulatedStop_RefusesNonDryRunModes(t *testing.T) {
	for _, mode := range []strategies.ExecutionMode{strategies.ModeLive, strategies.ModePaper, strategies.ModeBacktest} {
		sig := &fakeStopSignals{}
		err := newEvaluator(sig, &fakeStopExchange{}, &fakeWatermarks{}, stopT0).BeforeCycle(context.Background(), stopTestStrategy(), "BTCUSDT", mode)
		assert.Error(t, err, mode.String())
	}
}

type errHook struct{ calls int }

func (h *errHook) BeforeCycle(context.Context, entities.Strategy, string, strategies.ExecutionMode) error {
	h.calls++
	return errors.New("stop evaluation failed")
}

func TestEngineRun_PreCycleErrorAbortsBeforeContextAndHooks(t *testing.T) {
	hook := &errHook{}
	// Exchange/SignalUseCase are nil: reaching buildContext would panic
	// (recovered into a different error), so the exact error proves Run
	// stopped at the hook.
	e := &Engine{PreCycle: hook}
	err := e.Run(context.Background(), nil, stopTestStrategy(), "BTCUSDT", strategies.ModeDryRun)
	assert.EqualError(t, err, "stop evaluation failed")
	assert.Equal(t, 1, hook.calls)
}

func TestIntervalDuration(t *testing.T) {
	cases := map[string]time.Duration{"1m": time.Minute, "15m": 15 * time.Minute, "1h": time.Hour, "4h": 4 * time.Hour, "1d": 24 * time.Hour, "1w": 7 * 24 * time.Hour}
	for in, want := range cases {
		got, err := intervalDuration(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}
	for _, bad := range []string{"", "m", "0m", "5x", "abc"} {
		_, err := intervalDuration(bad)
		assert.Error(t, err, bad)
	}
}
