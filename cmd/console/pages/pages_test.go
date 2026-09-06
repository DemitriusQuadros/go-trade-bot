package pages

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	tabPane := widgets.NewTabPane("Dashboard", "Strategies", "Positions", "Backtest", "Results", "Log")

	pageList := RegisterPages(header, tabPane, deps)
	require.Len(t, pageList, 6)

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

func TestPositionsPage_Events(t *testing.T) {
	ts, deps := setupTestServerAndDeps(t)
	defer ts.Close()

	header := widgets.NewParagraph()
	tabPane := widgets.NewTabPane("Dashboard", "Strategies", "Positions", "Backtest", "Results", "Log")

	pp := NewPositionsPage()
	pp.Set(header, tabPane, deps)
	pp.fetchSignals()
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
