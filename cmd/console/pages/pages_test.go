package pages

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go-trade-bot/cmd/console/apiclient"
	"go-trade-bot/cmd/console/dependencies"
	"go-trade-bot/internal/configuration"

	ui "github.com/gizak/termui/v3"
	"github.com/gizak/termui/v3/widgets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestServerAndDeps(t *testing.T) (*httptest.Server, *dependencies.Dependencies) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/account":
			_ = json.NewEncoder(w).Encode(apiclient.AccountView{ID: 1, Amount: 12480.55, Currency: "USDT"})
		case r.URL.Path == "/strategy":
			_ = json.NewEncoder(w).Encode([]apiclient.StrategyView{
				{
					ID:               1,
					Name:             "grid_btc",
					StrategyName:     "grid",
					Status:           "productive",
					Mode:             "live",
					MonitoredSymbols: []string{"BTCUSDT"},
					StrategyConfiguration: apiclient.StrategyConfigurationView{
						Cycle:         5,
						Configuration: json.RawMessage(`{"grid_spacing_pct": 1.5}`),
					},
					UpdatedAt: time.Now(),
				},
			})
		case r.URL.Path == "/strategy/performance":
			_ = json.NewEncoder(w).Encode([]apiclient.StrategyPerformanceView{
				{Name: "grid_btc", Symbol: "BTCUSDT", Profit: 318.40, Trades: 47},
			})
		case r.URL.Path == "/broker/prices":
			_ = json.NewEncoder(w).Encode([]apiclient.TickerPriceView{{Symbol: "BTCUSDT", Price: 61204.10}})
		case r.URL.Path == "/broker/klines":
			_ = json.NewEncoder(w).Encode([]apiclient.CandleView{{Close: 61204.10}})
		case r.URL.Path == "/signal":
			_ = json.NewEncoder(w).Encode([]apiclient.SignalView{
				{
					ID:         10,
					Symbol:     "BTCUSDT",
					StrategyID: 1,
					Strategy:   apiclient.StrategyView{ID: 1, Name: "grid_btc"},
					Status:     "open",
					Orders: []apiclient.OrderView{
						{
							ID:             1,
							EntryPrice:     60120.00,
							Quantity:       0.05,
							InvestedAmount: 3006.00,
						},
					},
					CreatedAt: time.Now().Add(-2 * time.Hour),
				},
			})
		case r.URL.Path == "/backtest" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode([]apiclient.BacktestRunView{
				{
					ID:             1,
					StrategyID:     1,
					Symbol:         "BTCUSDT",
					Passed:         true,
					Sharpe:         1.24,
					HTMLReportPath: "/tmp/report.html",
				},
			})
		case r.URL.Path == "/backtest" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(apiclient.BacktestRunView{
				ID:             1,
				StrategyID:     1,
				Symbol:         "BTCUSDT",
				Passed:         true,
				Sharpe:         1.24,
				HTMLReportPath: "/tmp/report.html",
			})
		case r.URL.Path == "/signal/close/10":
			w.WriteHeader(http.StatusAccepted)
		case r.URL.Path == "/optimize" && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(apiclient.OptimizationRunView{
				ID: 1, StrategyID: 1, Status: "pending", TotalCombinations: 6,
			})
		case r.URL.Path == "/optimize/1":
			_ = json.NewEncoder(w).Encode(apiclient.OptimizationRunView{
				ID: 1, StrategyID: 1, Status: "completed", Progress: 6, TotalCombinations: 6,
				BestConfig:  map[string]float64{"rsi_period": 14, "stop_loss_pct": 2.0},
				BestMetrics: &apiclient.BacktestMetricsView{SharpeRatio: 1.24, MaxDrawdownPct: 12.1, WinRatePct: 59.4},
			})
		case r.URL.Path == "/optimize/1/results":
			_ = json.NewEncoder(w).Encode(apiclient.OptimizationResultsView{
				ID:         1,
				BestConfig: map[string]float64{"rsi_period": 14, "stop_loss_pct": 2.0},
				BestMetrics: apiclient.BacktestMetricsView{
					SharpeRatio: 1.24, MaxDrawdownPct: 12.1, WinRatePct: 59.4, ProfitFactor: 1.8, TotalTrades: 20,
				},
				Grid: []apiclient.OptimizationResultItemView{
					{Params: map[string]float64{"rsi_period": 10, "stop_loss_pct": 1.0}, Metrics: &apiclient.BacktestMetricsView{SharpeRatio: 0.61, MaxDrawdownPct: 20.0, WinRatePct: 40.0, ProfitFactor: 1.1}},
					{Params: map[string]float64{"rsi_period": 14, "stop_loss_pct": 2.0}, Metrics: &apiclient.BacktestMetricsView{SharpeRatio: 1.24, MaxDrawdownPct: 12.1, WinRatePct: 59.4, ProfitFactor: 1.8}},
				},
			})
		case strings.HasPrefix(r.URL.Path, "/strategy/") && strings.HasSuffix(r.URL.Path, "/performance/history"):
			_ = json.NewEncoder(w).Encode([]apiclient.PerformanceHistoryPointView{
				{PeriodStart: time.Now().AddDate(0, 0, -2), PeriodEnd: time.Now().AddDate(0, 0, -1), Profit: 12.5, Trades: 2},
				{PeriodStart: time.Now().AddDate(0, 0, -1), PeriodEnd: time.Now(), Profit: 42.10, Trades: 3},
			})
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))

	cfg := &configuration.Configuration{
		APIBaseURL: ts.URL,
	}
	deps := &dependencies.Dependencies{
		Cfg: cfg,
		API: apiclient.NewClient(ts.URL),
	}
	return ts, deps
}

func TestRegisterPages(t *testing.T) {
	ts, deps := setupTestServerAndDeps(t)
	defer ts.Close()

	header := widgets.NewParagraph()
	tabPane := widgets.NewTabPane("Dashboard", "Strategies", "Positions", "Backtest", "Results", "Log", "Optimize")

	pageList := RegisterPages(header, tabPane, deps)
	require.Len(t, pageList, 7)

	for _, p := range pageList {
		drawable := p.Render()
		assert.NotNil(t, drawable)
	}
}

func TestStrategiesPage_Events(t *testing.T) {
	ts, deps := setupTestServerAndDeps(t)
	defer ts.Close()

	header := widgets.NewParagraph()
	tabPane := widgets.NewTabPane("Dashboard", "Strategies", "Positions", "Backtest", "Results", "Log")

	sp := NewStrategiesPage()
	sp.Set(header, tabPane, deps)
	_ = sp.Render()

	// Up / Down navigation
	err := sp.HandleEvent(ui.Event{ID: "<Down>"})
	assert.NoError(t, err)
	err = sp.HandleEvent(ui.Event{ID: "<Up>"})
	assert.NoError(t, err)

	// Enter details
	err = sp.HandleEvent(ui.Event{ID: "<Enter>"})
	assert.NoError(t, err)
	assert.True(t, sp.detailVisible)

	// Escape closes details
	err = sp.HandleEvent(ui.Event{ID: "<Escape>"})
	assert.NoError(t, err)
	assert.False(t, sp.detailVisible)

	// Enable, Disable, Mode cycle, Backtest handoff
	backtestTriggered := false
	sp.OnBacktestRequested = func(strategyID uint) {
		backtestTriggered = true
	}
	err = sp.HandleEvent(ui.Event{ID: "e"})
	assert.NoError(t, err)
	err = sp.HandleEvent(ui.Event{ID: "d"})
	assert.NoError(t, err)
	err = sp.HandleEvent(ui.Event{ID: "r"})
	assert.NoError(t, err)
	err = sp.HandleEvent(ui.Event{ID: "b"})
	assert.NoError(t, err)
	assert.True(t, backtestTriggered)
}

func TestStrategiesPage_HistorySparkline(t *testing.T) {
	ts, deps := setupTestServerAndDeps(t)
	defer ts.Close()

	header := widgets.NewParagraph()
	tabPane := widgets.NewTabPane("Dashboard", "Strategies", "Positions", "Backtest", "Results", "Log", "Optimize")

	sp := NewStrategiesPage()
	sp.Set(header, tabPane, deps)
	_ = sp.Render()

	// AC#1: opening the overlay initializes bucket=daily and
	// symbol=MonitoredSymbols[0], and kicks off the history fetch.
	err := sp.HandleEvent(ui.Event{ID: "<Enter>"})
	require.NoError(t, err)
	sp.mu.RLock()
	assert.True(t, sp.detailVisible)
	assert.Equal(t, "daily", sp.historyBucket)
	assert.Equal(t, "BTCUSDT", sp.historySymbol)
	sp.mu.RUnlock()

	require.Eventually(t, func() bool {
		sp.mu.RLock()
		defer sp.mu.RUnlock()
		return !sp.historyLoading
	}, 2*time.Second, 20*time.Millisecond, "expected history fetch to resolve")

	sp.mu.RLock()
	assert.Len(t, sp.historyData, 2)
	assert.NoError(t, sp.historyErr)
	sp.mu.RUnlock()

	drawable, _, footer := sp.buildDetailOverlay(sp.strategies[0], 160, 50)
	assert.NotNil(t, drawable)
	require.NotNil(t, footer)
	assert.Contains(t, footer.Text, "[g] toggle bucket")
	assert.NotContains(t, footer.Text, "[y] toggle symbol", "single-symbol strategy must not show the [y] hint")

	// AC#3: [g] cycles the bucket and re-fetches.
	err = sp.HandleEvent(ui.Event{ID: "g"})
	require.NoError(t, err)
	sp.mu.RLock()
	assert.Equal(t, "weekly", sp.historyBucket)
	sp.mu.RUnlock()

	require.Eventually(t, func() bool {
		sp.mu.RLock()
		defer sp.mu.RUnlock()
		return !sp.historyLoading
	}, 2*time.Second, 20*time.Millisecond)

	// AC#3a: [y] is a no-op for a single-symbol strategy.
	err = sp.HandleEvent(ui.Event{ID: "y"})
	require.NoError(t, err)
	sp.mu.RLock()
	assert.Equal(t, "BTCUSDT", sp.historySymbol)
	sp.mu.RUnlock()

	// AC#7: closing and reopening resets bucket/symbol to defaults.
	err = sp.HandleEvent(ui.Event{ID: "<Escape>"})
	require.NoError(t, err)
	err = sp.HandleEvent(ui.Event{ID: "<Enter>"})
	require.NoError(t, err)
	sp.mu.RLock()
	assert.Equal(t, "daily", sp.historyBucket)
	sp.mu.RUnlock()
}

func TestStrategiesPage_HistorySparkline_EmptyAndError(t *testing.T) {
	sp := NewStrategiesPage()
	sp.historyBucket = "daily"
	sp.historySymbol = "BTCUSDT"
	strat := apiclient.StrategyView{MonitoredSymbols: []string{"BTCUSDT"}}

	// AC#4: an empty (but successful) fetch shows "No history yet", not an
	// empty sparkline.
	sp.historyData = nil
	sp.historyErr = nil
	sp.historyLoading = false
	section := sp.buildHistorySection()
	para, ok := section.(*widgets.Paragraph)
	require.True(t, ok)
	assert.Equal(t, "No history yet", para.Text)

	// AC#5: a failed fetch shows components.Error scoped to just this section.
	sp.historyErr = apiclient.ErrAPI{StatusCode: 500, Body: "boom"}
	section = sp.buildHistorySection()
	errPara, ok := section.(*widgets.Paragraph)
	require.True(t, ok)
	assert.Contains(t, errPara.Text, "Failed to load")

	// Multi-symbol strategy: [y] hint appears.
	strat.MonitoredSymbols = []string{"BTCUSDT", "ETHUSDT"}
	sp.historyErr = nil
	footer := sp.buildHistoryFooter(strat)
	assert.Contains(t, footer.Text, "[y] toggle symbol")
}

func TestPositionsPage_Events(t *testing.T) {
	ts, deps := setupTestServerAndDeps(t)
	defer ts.Close()

	header := widgets.NewParagraph()
	tabPane := widgets.NewTabPane("Dashboard", "Strategies", "Positions", "Backtest", "Results", "Log")

	pp := NewPositionsPage()
	pp.Set(header, tabPane, deps)
	pp.fetchSignals(context.Background())
	_ = pp.Render()

	// Select card
	err := pp.HandleEvent(ui.Event{ID: "<Right>"})
	assert.NoError(t, err)

	// Close confirmation
	err = pp.HandleEvent(ui.Event{ID: "c"})
	assert.NoError(t, err)
	assert.True(t, pp.confirming)

	// Cancel confirmation
	err = pp.HandleEvent(ui.Event{ID: "n"})
	assert.NoError(t, err)
	assert.False(t, pp.confirming)

	// Confirm close
	err = pp.HandleEvent(ui.Event{ID: "c"})
	assert.NoError(t, err)
	err = pp.HandleEvent(ui.Event{ID: "y"})
	assert.NoError(t, err)
}

func TestBacktestLauncherPage_Events(t *testing.T) {
	ts, deps := setupTestServerAndDeps(t)
	defer ts.Close()

	header := widgets.NewParagraph()
	tabPane := widgets.NewTabPane("Dashboard", "Strategies", "Positions", "Backtest", "Results", "Log")

	bl := NewBacktestLauncherPage()
	bl.Set(header, tabPane, deps)
	_ = bl.Render()

	// Tab next field
	err := bl.HandleEvent(ui.Event{ID: "<Tab>"})
	assert.NoError(t, err)

	// Enter run
	completed := false
	bl.OnBacktestCompleted = func(result any) {
		completed = true
	}
	err = bl.HandleEvent(ui.Event{ID: "<Enter>"})
	assert.NoError(t, err)

	time.Sleep(100 * time.Millisecond)
	assert.True(t, completed)
}

func TestBacktestResultsPage_Events(t *testing.T) {
	ts, deps := setupTestServerAndDeps(t)
	defer ts.Close()

	header := widgets.NewParagraph()
	tabPane := widgets.NewTabPane("Dashboard", "Strategies", "Positions", "Backtest", "Results", "Log")

	rp := NewBacktestResultsPage()
	rp.Set(header, tabPane, deps)
	rp.SetResult(apiclient.BacktestRunView{
		ID:             1,
		StrategyID:     1,
		Symbol:         "BTCUSDT",
		Passed:         true,
		HTMLReportPath: "/tmp/report.html",
	})
	_ = rp.Render()

	err := rp.HandleEvent(ui.Event{ID: "s"})
	assert.NoError(t, err)
	assert.Contains(t, rp.bannerMsg, "Already saved as run #1")
}

func TestExecutionLogPage_Events(t *testing.T) {
	ts, deps := setupTestServerAndDeps(t)
	defer ts.Close()

	header := widgets.NewParagraph()
	tabPane := widgets.NewTabPane("Dashboard", "Strategies", "Positions", "Backtest", "Results", "Log")

	el := NewExecutionLogPage()
	el.Set(header, tabPane, deps)
	el.pollAndDiff()
	_ = el.Render()

	// Filter
	err := el.HandleEvent(ui.Event{ID: "f"})
	assert.NoError(t, err)

	// Search
	err = el.HandleEvent(ui.Event{ID: "/"})
	assert.NoError(t, err)

	// Clear
	err = el.HandleEvent(ui.Event{ID: "<Escape>"})
	assert.NoError(t, err)
	assert.Equal(t, "", el.filterType)
	assert.Equal(t, "", el.filterQuery)
}

func TestOptimizeResultsPage_Events(t *testing.T) {
	ts, deps := setupTestServerAndDeps(t)
	defer ts.Close()

	header := widgets.NewParagraph()
	tabPane := widgets.NewTabPane("Dashboard", "Strategies", "Positions", "Backtest", "Results", "Log", "Optimize")

	op := NewOptimizeResultsPage()
	op.Set(header, tabPane, deps)
	_ = op.Render() // populates op.strategies via fetchStrategies

	// Tab cycles through form fields without error.
	err := op.HandleEvent(ui.Event{ID: "<Tab>"})
	assert.NoError(t, err)
	err = op.HandleEvent(ui.Event{ID: "<Right>"})
	assert.NoError(t, err)

	// [m] cycles the rank metric before any run has started.
	err = op.HandleEvent(ui.Event{ID: "m"})
	assert.NoError(t, err)
	assert.Equal(t, "max_drawdown_pct", op.displayMetric)
	assert.Equal(t, "max_drawdown_pct", op.form.RankMetric)

	// Submitting starts the run and, after the 2s poll picks up the
	// "completed" mock response, loads the full results grid.
	err = op.HandleEvent(ui.Event{ID: "<Enter>"})
	assert.NoError(t, err)

	op.mu.RLock()
	running := op.running
	op.mu.RUnlock()
	assert.True(t, running)

	require.Eventually(t, func() bool {
		op.mu.RLock()
		defer op.mu.RUnlock()
		return op.result != nil && !op.running
	}, 5*time.Second, 100*time.Millisecond, "expected optimization polling to complete")

	op.mu.RLock()
	defer op.mu.RUnlock()
	assert.Len(t, op.result.Grid, 2)
	bestY, bestX := op.bestCellFor(op.displayMetric)
	assert.GreaterOrEqual(t, bestY, 0)
	assert.GreaterOrEqual(t, bestX, 0)

	drawable := op.buildHeatmapArea(120)
	assert.NotNil(t, drawable)
}

func TestOptimizeResultsPage_HeatmapEdgeCases(t *testing.T) {
	op := NewOptimizeResultsPage()
	op.strategies = []apiclient.StrategyView{{ID: 1, Name: "grid_btc"}}
	op.axisXValues = []float64{10, 12}
	op.axisYValues = []float64{1.0}

	// Zero valid combinations must not render an empty heatmap.
	op.result = &apiclient.OptimizationResultsView{Grid: nil}
	drawable := op.buildHeatmapArea(120)
	para, ok := drawable.(*widgets.Paragraph)
	require.True(t, ok)
	assert.Contains(t, para.Text, "No valid results")

	// A single-row (1D sweep) grid is a legitimate degenerate case, not an error.
	op.result = &apiclient.OptimizationResultsView{
		BestConfig: map[string]float64{"rsi_period": 10, "stop_loss_pct": 1.0},
		Grid: []apiclient.OptimizationResultItemView{
			{Params: map[string]float64{"rsi_period": 10, "stop_loss_pct": 1.0}, Metrics: &apiclient.BacktestMetricsView{SharpeRatio: 0.5}},
			{Params: map[string]float64{"rsi_period": 12, "stop_loss_pct": 1.0}, Metrics: &apiclient.BacktestMetricsView{SharpeRatio: 0.9}},
		},
	}
	op.form.ParamX = "rsi_period"
	op.form.ParamY = "stop_loss_pct"
	op.form.RankMetric = "sharpe_ratio"
	op.displayMetric = "sharpe_ratio"
	drawable = op.buildHeatmapArea(120)
	_, isHeatmap := drawable.(*heatmapComposite)
	assert.True(t, isHeatmap)
}

func TestOptimizeResultsPage_MaxDrawdownInversion(t *testing.T) {
	// Given max_drawdown_pct is active, lower values must be treated as
	// "better" — the opposite direction from every other metric.
	metrics := apiclient.BacktestMetricsView{MaxDrawdownPct: 5.0}
	assert.Equal(t, 5.0, metricValue(metrics, "max_drawdown_pct"))

	op := NewOptimizeResultsPage()
	op.form.ParamX, op.form.ParamY = "rsi_period", "stop_loss_pct"
	op.form.RankMetric = "sharpe_ratio" // submitted metric differs from displayMetric below
	op.axisXValues = []float64{10, 12}
	op.axisYValues = []float64{1.0}
	op.result = &apiclient.OptimizationResultsView{
		Grid: []apiclient.OptimizationResultItemView{
			{Params: map[string]float64{"rsi_period": 10, "stop_loss_pct": 1.0}, Metrics: &apiclient.BacktestMetricsView{MaxDrawdownPct: 20.0}},
			{Params: map[string]float64{"rsi_period": 12, "stop_loss_pct": 1.0}, Metrics: &apiclient.BacktestMetricsView{MaxDrawdownPct: 5.0}},
		},
	}
	_, bestX := op.bestCellFor("max_drawdown_pct")
	assert.Equal(t, 1, bestX, "the LOWER drawdown (5.0, at index 1) must be selected as best")
}
