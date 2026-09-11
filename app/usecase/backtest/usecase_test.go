package backtest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go-trade-bot/app/entities"
	backtesthandler "go-trade-bot/app/handler/web/backtest"
	"go-trade-bot/app/strategies"
	"go-trade-bot/app/strategies/script"
	"go-trade-bot/app/usecase/backtest"
	"go-trade-bot/internal/exchange"
	"go-trade-bot/internal/metrics_provider"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
)

// backtestNopStore is a no-op ScriptStateStore for the script strategy used in
// these threshold tests (the metrics provider is mocked, so trades from the
// script are irrelevant here).
type backtestNopStore struct{}

func (backtestNopStore) Load(context.Context, uint, string) (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}
func (backtestNopStore) Save(context.Context, uint, string, map[string]interface{}) error { return nil }

func init() {
	runner := script.NewRunner(script.DefaultHookTimeout, nil)
	strategies.Register("script", func(db entities.Strategy) strategies.Strategy {
		return script.NewScriptStrategy(db, backtestNopStore{}, runner)
	})
}

// noTradeScript is a minimal valid script that never enters a position.
const noTradeScript = `function should_long(ctx) return false end`

type mockCandleRepo struct {
	candles []entities.Candle
}

func (m *mockCandleRepo) Upsert(ctx context.Context, candles []entities.Candle) error {
	m.candles = append(m.candles, candles...)
	return nil
}

func (m *mockCandleRepo) Range(ctx context.Context, symbol, timeframe string, from, to time.Time) ([]exchange.Candle, error) {
	var res []exchange.Candle
	for _, c := range m.candles {
		if c.Symbol == symbol && c.Timeframe == timeframe && !c.OpenTime.Before(from) && c.OpenTime.Before(to) {
			res = append(res, exchange.Candle{
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
	}
	return res, nil
}

func (m *mockCandleRepo) RangeBefore(ctx context.Context, symbol, timeframe string, before time.Time, limit int) ([]exchange.Candle, error) {
	var res []exchange.Candle
	for i := len(m.candles) - 1; i >= 0; i-- {
		c := m.candles[i]
		if c.Symbol == symbol && c.Timeframe == timeframe && (c.OpenTime.Before(before) || c.OpenTime.Equal(before)) {
			res = append([]exchange.Candle{{
				Symbol:    c.Symbol,
				Timeframe: c.Timeframe,
				OpenTime:  c.OpenTime,
				Open:      c.Open,
				High:      c.High,
				Low:       c.Low,
				Close:     c.Close,
				Volume:    c.Volume,
			}}, res...)
			if len(res) == limit {
				break
			}
		}
	}
	return res, nil
}

func (m *mockCandleRepo) LatestOpenTime(ctx context.Context, symbol, timeframe string) (time.Time, error) {
	if len(m.candles) == 0 {
		return time.Time{}, nil
	}
	return m.candles[len(m.candles)-1].OpenTime, nil
}

func (m *mockCandleRepo) Count(ctx context.Context, symbol, timeframe string) (int64, error) {
	return int64(len(m.candles)), nil
}

func (m *mockCandleRepo) CountInRange(ctx context.Context, symbol, timeframe string, from, to time.Time) (int64, error) {
	count := int64(0)
	for _, c := range m.candles {
		if !c.OpenTime.Before(from) && c.OpenTime.Before(to) {
			count++
		}
	}
	return count, nil
}

type mockStrategyRepo struct {
	strategy entities.Strategy
}

func (m *mockStrategyRepo) GetByID(ctx context.Context, id uint) (entities.Strategy, error) {
	return m.strategy, nil
}

type mockBacktestRepo struct {
	runs []entities.BacktestRun
}

func (m *mockBacktestRepo) Create(ctx context.Context, run *entities.BacktestRun) error {
	run.ID = uint(len(m.runs) + 1)
	m.runs = append(m.runs, *run)
	return nil
}

func (m *mockBacktestRepo) GetByID(ctx context.Context, id uint) (entities.BacktestRun, error) {
	for _, r := range m.runs {
		if r.ID == id {
			return r, nil
		}
	}
	return entities.BacktestRun{}, fmt.Errorf("backtest run %d not found", id)
}

func (m *mockBacktestRepo) ListByStrategy(ctx context.Context, strategyID uint) ([]entities.BacktestRun, error) {
	var res []entities.BacktestRun
	for _, r := range m.runs {
		if r.StrategyID == strategyID {
			res = append(res, r)
		}
	}
	return res, nil
}

func (m *mockBacktestRepo) ListRunsWithReport(ctx context.Context, strategyID uint) ([]entities.BacktestRun, error) {
	var res []entities.BacktestRun
	for _, r := range m.runs {
		if r.StrategyID == strategyID && r.HTMLReportPath != "" {
			res = append(res, r)
		}
	}
	return res, nil
}

func (m *mockBacktestRepo) Update(ctx context.Context, run entities.BacktestRun) error {
	for i, r := range m.runs {
		if r.ID == run.ID {
			m.runs[i] = run
			return nil
		}
	}
	return nil
}

type mockMetricsProvider struct {
	metrics metrics_provider.BacktestMetrics
}

func (m *mockMetricsProvider) Compute(trades []metrics_provider.TradeLogEntry, startingBalance, periodsPerYear float64) metrics_provider.BacktestMetrics {
	return m.metrics
}

func generateCandles(count int) []entities.Candle {
	baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	candles := make([]entities.Candle, count)
	for i := 0; i < count; i++ {
		candles[i] = entities.Candle{
			Symbol:    "BTCUSDT",
			Timeframe: "1m",
			OpenTime:  baseTime.Add(time.Duration(i) * time.Minute),
			Open:      100.0,
			High:      105.0,
			Low:       95.0,
			Close:     102.0,
			Volume:    10.0,
		}
	}
	return candles
}

func TestBacktestUseCase_ThresholdPolicy_PassAndFail(t *testing.T) {
	candles := generateCandles(100)
	candleRepo := &mockCandleRepo{candles: candles}
	stratRepo := &mockStrategyRepo{
		strategy: entities.Strategy{
			ID:           1,
			Name:         "Script",
			StrategyName: "script",
			ScriptSource: noTradeScript,
			StrategyConfiguration: entities.StrategyConfiguration{
				Cycle: entities.OneMinute,
				Configuration: datatypes.JSON(`{
					"template": {
						"entry": {"conditions": [{"type": "indicator_threshold", "indicator": "rsi", "params": {"period": 14}, "comparator": "<", "value": 30.0}], "combinator": "AND"},
						"exit": {"conditions": [], "combinator": "OR", "take_profit_pct": 1.0}
					},
					"stop_loss_pct": 1.0,
					"position_sizing": {"type": "pct_capital", "value": 10}
				}`),
			},
		},
	}
	backtestRepo := &mockBacktestRepo{}

	// Case 1: Sharpe=1.2, DD=15.0 -> Passed=true (AC#2)
	metricsProvider1 := &mockMetricsProvider{
		metrics: metrics_provider.BacktestMetrics{
			SharpeRatio:    1.2,
			MaxDrawdownPct: 15.0,
			TotalTrades:    5,
			ProfitFactor:   2.0,
		},
	}
	uc1 := backtest.NewBacktestUseCase(
		candleRepo, backtestRepo, stratRepo, metricsProvider1,
		backtest.ThresholdPolicy{MinSharpe: 0.8, MaxDrawdownPct: 20.0},
		t.TempDir(), 20,
	)

	run1, err := uc1.Run(context.Background(), backtest.RunRequest{
		StrategyID: 1,
		Symbol:     "BTCUSDT",
		Timeframe:  "1m",
		StartDate:  candles[0].OpenTime,
		EndDate:    candles[len(candles)-1].OpenTime.Add(time.Minute),
	})
	require.NoError(t, err)
	assert.True(t, run1.Passed, "run should pass with Sharpe 1.2 and DD 15%")
	assert.NotEmpty(t, run1.HTMLReportPath)

	// Case 2: Sharpe=1.2, DD=25.0 -> Passed=false (AC#3 - drawdown violation)
	metricsProvider2 := &mockMetricsProvider{
		metrics: metrics_provider.BacktestMetrics{
			SharpeRatio:    1.2,
			MaxDrawdownPct: 25.0,
			TotalTrades:    5,
		},
	}
	uc2 := backtest.NewBacktestUseCase(
		candleRepo, backtestRepo, stratRepo, metricsProvider2,
		backtest.ThresholdPolicy{MinSharpe: 0.8, MaxDrawdownPct: 20.0},
		t.TempDir(), 20,
	)

	run2, err := uc2.Run(context.Background(), backtest.RunRequest{
		StrategyID: 1,
		Symbol:     "BTCUSDT",
		Timeframe:  "1m",
		StartDate:  candles[0].OpenTime,
		EndDate:    candles[len(candles)-1].OpenTime.Add(time.Minute),
	})
	require.NoError(t, err)
	assert.False(t, run2.Passed, "run should fail when drawdown violates threshold")
}

func TestBacktestUseCase_InfProfitFactor_And_ReportFailureResilience(t *testing.T) {
	candles := generateCandles(100)
	candleRepo := &mockCandleRepo{candles: candles}
	stratRepo := &mockStrategyRepo{
		strategy: entities.Strategy{
			ID:           1,
			Name:         "Script",
			StrategyName: "script",
			ScriptSource: noTradeScript,
			StrategyConfiguration: entities.StrategyConfiguration{
				Cycle: entities.OneMinute,
				Configuration: datatypes.JSON(`{
					"template": {
						"entry": {"conditions": [{"type": "indicator_threshold", "indicator": "rsi", "params": {"period": 14}, "comparator": "<", "value": 30.0}], "combinator": "AND"},
						"exit": {"conditions": [], "combinator": "OR", "take_profit_pct": 1.0}
					},
					"stop_loss_pct": 1.0,
					"position_sizing": {"type": "pct_capital", "value": 10}
				}`),
			},
		},
	}
	backtestRepo := &mockBacktestRepo{}

	// +Inf Profit Factor & invalid directory to trigger report failure (AC#4 & AC#5)
	metricsProvider := &mockMetricsProvider{
		metrics: metrics_provider.BacktestMetrics{
			SharpeRatio:    1.5,
			MaxDrawdownPct: 5.0,
			ProfitFactor:   math.Inf(1),
			TotalTrades:    3,
		},
	}
	uc := backtest.NewBacktestUseCase(
		candleRepo, backtestRepo, stratRepo, metricsProvider,
		backtest.DefaultThresholdPolicy(),
		"/invalid/directory/that/cannot/be/created/\x00", 20,
	)

	run, err := uc.Run(context.Background(), backtest.RunRequest{
		StrategyID: 1,
		Symbol:     "BTCUSDT",
		Timeframe:  "1m",
		StartDate:  candles[0].OpenTime,
		EndDate:    candles[len(candles)-1].OpenTime.Add(time.Minute),
	})
	require.NoError(t, err)
	assert.Empty(t, run.HTMLReportPath, "HTMLReportPath should be empty if report generation failed")
	assert.True(t, run.Passed)
	assert.False(t, math.IsInf(run.ProfitFactor, 1), "ProfitFactor stored in DB must be finite sentinel")
	assert.Equal(t, math.MaxFloat64, run.ProfitFactor)
}

func TestBacktestUseCase_RetentionPruning(t *testing.T) {
	candles := generateCandles(100)
	candleRepo := &mockCandleRepo{candles: candles}
	stratRepo := &mockStrategyRepo{
		strategy: entities.Strategy{
			ID:           1,
			Name:         "Script",
			StrategyName: "script",
			ScriptSource: noTradeScript,
			StrategyConfiguration: entities.StrategyConfiguration{
				Cycle: entities.OneMinute,
				Configuration: datatypes.JSON(`{
					"template": {
						"entry": {"conditions": [{"type": "indicator_threshold", "indicator": "rsi", "params": {"period": 14}, "comparator": "<", "value": 30.0}], "combinator": "AND"},
						"exit": {"conditions": [], "combinator": "OR", "take_profit_pct": 1.0}
					},
					"stop_loss_pct": 1.0,
					"position_sizing": {"type": "pct_capital", "value": 10}
				}`),
			},
		},
	}
	backtestRepo := &mockBacktestRepo{}
	tempDir := t.TempDir()

	metricsProvider := &mockMetricsProvider{
		metrics: metrics_provider.BacktestMetrics{
			SharpeRatio:    1.0,
			MaxDrawdownPct: 10.0,
			TotalTrades:    1,
		},
	}

	// Retention limit of 2 reports
	uc := backtest.NewBacktestUseCase(
		candleRepo, backtestRepo, stratRepo, metricsProvider,
		backtest.DefaultThresholdPolicy(),
		tempDir, 2,
	)

	// Run 1
	run1, err := uc.Run(context.Background(), backtest.RunRequest{
		StrategyID: 1, Symbol: "BTCUSDT", Timeframe: "1m",
		StartDate: candles[0].OpenTime, EndDate: candles[len(candles)-1].OpenTime.Add(time.Minute),
	})
	require.NoError(t, err)
	require.FileExists(t, run1.HTMLReportPath)
	firstReportFile := run1.HTMLReportPath

	// Run 2
	run2, err := uc.Run(context.Background(), backtest.RunRequest{
		StrategyID: 1, Symbol: "BTCUSDT", Timeframe: "1m",
		StartDate: candles[0].OpenTime, EndDate: candles[len(candles)-1].OpenTime.Add(time.Minute),
	})
	require.NoError(t, err)
	require.FileExists(t, run2.HTMLReportPath)

	// Run 3 (triggers retention pruning of run1's report file)
	run3, err := uc.Run(context.Background(), backtest.RunRequest{
		StrategyID: 1, Symbol: "BTCUSDT", Timeframe: "1m",
		StartDate: candles[0].OpenTime, EndDate: candles[len(candles)-1].OpenTime.Add(time.Minute),
	})
	require.NoError(t, err)
	require.FileExists(t, run3.HTMLReportPath)

	// Verify run1 report file was deleted
	assert.NoFileExists(t, firstReportFile, "oldest report file should be deleted")
	// Verify run1 DB row still exists but has empty HTMLReportPath
	fetched1, err := backtestRepo.GetByID(context.Background(), run1.ID)
	require.NoError(t, err)
	assert.Equal(t, run1.ID, fetched1.ID)
	assert.Empty(t, fetched1.HTMLReportPath, "HTMLReportPath should be cleared on pruned DB row")
}

// --- backend-05 Step 6: script strategy cutover verification ----------------

// tradeScript enters long whenever price dips below 100 and exits at a 0.5%
// take-profit - guaranteed to fire repeatedly against oscillatingCandles.
const tradeScript = `
function should_long(ctx)
  return ctx.price < 100
end
function go_long(ctx)
  return {buy = {price = ctx.price}}
end
function update_position(ctx)
  local entry = ctx.position.entry_price
  if ctx.price >= entry * 1.005 then
    return {sell = {price = ctx.price}}
  end
  return nil
end
`

func oscillatingCandles(count int) []entities.Candle {
	baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	candles := make([]entities.Candle, count)
	for i := 0; i < count; i++ {
		price := 100.0 + math.Sin(float64(i)*0.35)*8.0
		candles[i] = entities.Candle{
			Symbol:    "BTCUSDT",
			Timeframe: "1m",
			OpenTime:  baseTime.Add(time.Duration(i) * time.Minute),
			Open:      price - 0.2,
			High:      price + 1.0,
			Low:       price - 1.0,
			Close:     price,
			Volume:    10.0,
		}
	}
	return candles
}

func newScriptBacktestUseCase(t *testing.T) (*backtest.BacktestUseCase, []entities.Candle) {
	t.Helper()
	candles := oscillatingCandles(200)
	candleRepo := &mockCandleRepo{candles: candles}
	stratRepo := &mockStrategyRepo{strategy: entities.Strategy{
		ID:           1,
		Name:         "Trader",
		StrategyName: "script",
		ScriptSource: tradeScript,
		StrategyConfiguration: entities.StrategyConfiguration{
			Cycle: entities.OneMinute,
			Configuration: datatypes.JSON(`{
				"stop_loss_pct": 2.0,
				"position_sizing": {"type": "pct_capital", "value": 10}
			}`),
		},
	}}
	uc := backtest.NewBacktestUseCase(
		candleRepo, &mockBacktestRepo{}, stratRepo,
		metrics_provider.NewCinarMetricsAdapter(),
		backtest.DefaultThresholdPolicy(), t.TempDir(), 20,
	)
	return uc, candles
}

// Step 6, usecase path: a real backtest of a script strategy produces trades.
func TestBacktestUseCase_ScriptStrategy_ProducesTrades(t *testing.T) {
	uc, candles := newScriptBacktestUseCase(t)
	run, err := uc.Run(context.Background(), backtest.RunRequest{
		StrategyID: 1,
		Symbol:     "BTCUSDT",
		Timeframe:  "1m",
		StartDate:  candles[0].OpenTime,
		EndDate:    candles[len(candles)-1].OpenTime.Add(time.Minute),
	})
	require.NoError(t, err)
	assert.Greater(t, run.TotalTrades, 0, "script strategy backtest should produce at least one trade")
}

// Step 6, POST /backtest handler path: the same script strategy driven through
// the HTTP handler produces total_trades > 0.
func TestBacktestHandler_ScriptStrategy_ProducesTrades(t *testing.T) {
	uc, candles := newScriptBacktestUseCase(t)
	h := backtesthandler.NewBacktestHandler(uc)

	body, _ := json.Marshal(map[string]any{
		"strategy_id": 1,
		"symbol":      "BTCUSDT",
		"timeframe":   "1m",
		"start_date":  candles[0].OpenTime,
		"end_date":    candles[len(candles)-1].OpenTime.Add(time.Minute),
	})
	req := httptest.NewRequest(http.MethodPost, "/backtest", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.RunBacktest(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var resp map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	tt, _ := resp["total_trades"].(float64)
	assert.Greater(t, tt, 0.0, "POST /backtest of a script strategy should report total_trades > 0")
}

// --- backend-07: execution trace persistence --------------------------------

func newScriptBacktestUseCaseWithSource(t *testing.T, strategyName, source string) (*backtest.BacktestUseCase, []entities.Candle) {
	t.Helper()
	candles := oscillatingCandles(200)
	candleRepo := &mockCandleRepo{candles: candles}
	stratRepo := &mockStrategyRepo{strategy: entities.Strategy{
		ID:           1,
		Name:         "Trader",
		StrategyName: strategyName,
		ScriptSource: source,
		StrategyConfiguration: entities.StrategyConfiguration{
			Cycle: entities.OneMinute,
			Configuration: datatypes.JSON(`{
				"stop_loss_pct": 2.0,
				"position_sizing": {"type": "pct_capital", "value": 10}
			}`),
		},
	}}
	uc := backtest.NewBacktestUseCase(
		candleRepo, &mockBacktestRepo{}, stratRepo,
		metrics_provider.NewCinarMetricsAdapter(),
		backtest.DefaultThresholdPolicy(), t.TempDir(), 20,
	)
	return uc, candles
}

func runScriptBacktest(t *testing.T, uc *backtest.BacktestUseCase, candles []entities.Candle) entities.BacktestRun {
	t.Helper()
	run, err := uc.Run(context.Background(), backtest.RunRequest{
		StrategyID: 1,
		Symbol:     "BTCUSDT",
		Timeframe:  "1m",
		StartDate:  candles[0].OpenTime,
		EndDate:    candles[len(candles)-1].OpenTime.Add(time.Minute),
	})
	require.NoError(t, err)
	return run
}

// AC#1: the trace has one record per cycle, not one per trade.
func TestBacktest_ExecutionTrace_OneRecordPerCycle(t *testing.T) {
	uc, candles := newScriptBacktestUseCaseWithSource(t, "script", tradeScript)
	run := runScriptBacktest(t, uc, candles)

	require.NotEmpty(t, run.ExecutionTraceJSON)
	var trace []script.TraceRecord
	require.NoError(t, json.Unmarshal(run.ExecutionTraceJSON, &trace))

	assert.Greater(t, len(trace), 0)
	assert.Greater(t, run.TotalTrades, 0)
	assert.Greater(t, len(trace), run.TotalTrades, "trace records should be per-cycle, far more than trades")
}

// AC#2: only signal-producing cycles carry a non-nil Signal.
func TestBacktest_ExecutionTrace_SignalOnlyOnSignalCycles(t *testing.T) {
	uc, candles := newScriptBacktestUseCaseWithSource(t, "script", tradeScript)
	run := runScriptBacktest(t, uc, candles)

	var trace []script.TraceRecord
	require.NoError(t, json.Unmarshal(run.ExecutionTraceJSON, &trace))

	withSignal, withoutSignal := 0, 0
	for _, r := range trace {
		if r.Signal != nil {
			withSignal++
		} else {
			withoutSignal++
		}
	}
	assert.Greater(t, withSignal, 0, "at least some cycles should produce a signal")
	assert.Greater(t, withoutSignal, 0, "most cycles should produce no signal")
}

// AC#3: RSI indicator calls and debug.log entries appear in the trace.
func TestBacktest_ExecutionTrace_CapturesIndicatorAndLog(t *testing.T) {
	source := `
function before(ctx)
  local r = ind.rsi(14)
  debug.log("rsi_val", r)
end
function should_long(ctx)
  return false
end
`
	uc, candles := newScriptBacktestUseCaseWithSource(t, "script", source)
	run := runScriptBacktest(t, uc, candles)

	var trace []script.TraceRecord
	require.NoError(t, json.Unmarshal(run.ExecutionTraceJSON, &trace))

	sawRSI, sawLog := false, false
	for _, r := range trace {
		for _, ind := range r.Indicators {
			if ind.Name == "rsi" {
				sawRSI = true
			}
		}
		for _, l := range r.Log {
			if l.Label == "rsi_val" {
				sawLog = true
			}
		}
	}
	assert.True(t, sawRSI, "expected at least one rsi indicator call in the trace")
	assert.True(t, sawLog, "expected the debug.log rsi_val entry in the trace")
}

// noTraceStrategy is a native Go strategy that does NOT implement
// script.TraceableStrategy (AC#4).
type noTraceStrategy struct{}

func (noTraceStrategy) Name() string                                         { return "notrace" }
func (noTraceStrategy) Before(strategies.Context)                            {}
func (noTraceStrategy) ShouldLong(strategies.Context) bool                   { return false }
func (noTraceStrategy) GoLong(strategies.Context) strategies.Signal          { return strategies.Signal{} }
func (noTraceStrategy) ShouldShort(strategies.Context) bool                  { return false }
func (noTraceStrategy) GoShort(strategies.Context) strategies.Signal         { return strategies.Signal{} }
func (noTraceStrategy) UpdatePosition(strategies.Context) *strategies.Signal { return nil }
func (noTraceStrategy) After(strategies.Context)                             {}
func (noTraceStrategy) Terminate(strategies.Context)                         {}

func init() {
	strategies.Register("notrace", func(_ entities.Strategy) strategies.Strategy { return noTraceStrategy{} })
}

// AC#4: a non-traceable strategy leaves ExecutionTraceJSON empty.
func TestBacktest_ExecutionTrace_EmptyForNonScriptStrategy(t *testing.T) {
	uc, candles := newScriptBacktestUseCaseWithSource(t, "notrace", "")
	run := runScriptBacktest(t, uc, candles)
	assert.Empty(t, run.ExecutionTraceJSON, "non-traceable strategy must not persist an execution trace")
}
