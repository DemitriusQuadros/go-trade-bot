package agentplatform_test

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"go-trade-bot/app/entities"
	"go-trade-bot/app/repository/agentplatform"
	"go-trade-bot/internal/metrics_provider"
	"go-trade-bot/internal/report/agentreport"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A-01 AC#6 end-to-end: a kpi_grid referencing backtest run 7 renders run
// 7's DB values through the real DataSource.
func TestReportDataSource_BacktestRunFeedsRenderer(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	trades, _ := json.Marshal([]metrics_provider.TradeLogEntry{
		{Symbol: "BTCUSDT", EntryTime: base, ExitTime: base.Add(time.Hour), EntryPrice: 100, ExitPrice: 110, Quantity: 1, Profit: 10, ExitReason: "take_profit"},
		{Symbol: "BTCUSDT", EntryTime: base.Add(2 * time.Hour), ExitTime: base.Add(3 * time.Hour), EntryPrice: 110, ExitPrice: 107, Quantity: 1, Profit: -3, ExitReason: "stop_loss"},
	})
	metricsJSON, _ := json.Marshal(metrics_provider.BacktestMetrics{EquityCurve: []metrics_provider.EquityPoint{{Time: base, Value: 1000}, {Time: base.Add(time.Hour), Value: 1010}, {Time: base.Add(3 * time.Hour), Value: 1007}}})
	run := entities.BacktestRun{ID: 7, StrategyID: 1, Symbol: "BTCUSDT", StartDate: base, EndDate: base.AddDate(0, 1, 0),
		Sharpe: 1.4321, MaxDrawdownPct: 6.25, WinRatePct: 50, ProfitFactor: 3.3333, TotalTrades: 2, TradeLogJSON: trades, MetricsJSON: metricsJSON}
	require.NoError(t, db.Create(&run).Error)

	ds := agentplatform.NewReportDataSource(db)
	s, err := ds.BacktestRun(ctx, 7)
	require.NoError(t, err)
	assert.Equal(t, 1.4321, s.Metrics.Sharpe)
	assert.Equal(t, 7.0, s.Metrics.NetPnL)
	assert.True(t, s.Metrics.MaxDrawdownIsPct)
	assert.Len(t, s.Trades, 2)
	assert.Len(t, s.Equity, 3)
	_, err = ds.BacktestRun(ctx, 8)
	assert.ErrorContains(t, err, "not found")

	html, err := agentreport.NewHTMLRenderer(ds).Render(ctx, agentreport.ReportMeta{Title: "t", Severity: "info", AgentName: "a"}, []agentreport.Block{
		{Type: "kpi_grid", Data: json.RawMessage(`{"source":{"kind":"backtest_run","id":7},"metrics":["sharpe","max_drawdown","profit_factor","net_pnl"],"sharpe":9.99}`)},
		{Type: "equity_chart", Data: json.RawMessage(`{"source":{"kind":"backtest_run","id":7}}`)},
		{Type: "trade_table", Data: json.RawMessage(`{"source":{"kind":"backtest_run","id":7},"limit":10}`)},
	})
	require.NoError(t, err)
	for _, want := range []string{">1.43<", ">6.25%<", ">3.33<", ">7.00<", "take_profit", "<path class=\"line"} {
		assert.Contains(t, html, want)
	}
	assert.NotContains(t, html, "9.99")
}

func TestReportDataSource_StrategyLive(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()
	strat := entities.Strategy{Name: "Live One", StrategyName: "script"}
	require.NoError(t, db.Create(&strat).Error)
	now := time.Now()
	mk := func(profit float32, closedAgo time.Duration, status entities.SignalStatus) {
		sig := entities.Signal{StrategyID: strat.ID, Symbol: "ETHUSDT", Status: status}
		require.NoError(t, db.Create(&sig).Error)
		require.NoError(t, db.Model(&sig).UpdateColumn("updated_at", now.Add(-closedAgo)).Error)
		require.NoError(t, db.Create(&entities.Order{SignalID: sig.ID, EntryPrice: 100, ExitPrice: 101, Quantity: 2, Profit: profit}).Error)
	}
	mk(10, 3*24*time.Hour, entities.Closed)
	mk(-4, 2*24*time.Hour, entities.Closed)
	mk(6, 24*time.Hour, entities.Closed)
	mk(100, 40*24*time.Hour, entities.Closed) // outside the window
	mk(50, time.Hour, entities.Open)          // still open

	s, err := agentplatform.NewReportDataSource(db).StrategyLive(ctx, strat.ID, 30)
	require.NoError(t, err)
	assert.Equal(t, 3, s.Metrics.TotalTrades)
	assert.InDelta(t, 12.0, s.Metrics.NetPnL, 1e-9)
	assert.InDelta(t, 200.0/3, s.Metrics.WinRatePct, 1e-9)
	assert.InDelta(t, 4.0, s.Metrics.ProfitFactor, 1e-9)
	assert.InDelta(t, 4.0, s.Metrics.MaxDrawdown, 1e-9)
	assert.False(t, s.Metrics.MaxDrawdownIsPct)
	assert.False(t, math.IsNaN(s.Metrics.Sharpe))
	assert.True(t, strings.Contains(s.Label, "Live One"))
	require.Len(t, s.Equity, 3)
	assert.InDelta(t, 12.0, s.Equity[2].Value, 1e-9)

	_, err = agentplatform.NewReportDataSource(db).StrategyLive(ctx, 999, 30)
	assert.Error(t, err)
}

func TestReportDataSource_ScriptVersions(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()
	strat := entities.Strategy{Name: "S", StrategyName: "script", ScriptSource: "v3"}
	require.NoError(t, db.Create(&strat).Error)
	ds := agentplatform.NewReportDataSource(db)

	prev, err := ds.PreviousScript(ctx, strat.ID)
	require.NoError(t, err)
	assert.Equal(t, "", prev, "no history yet")

	for _, src := range []string{"v1", "v2", "v3"} {
		require.NoError(t, db.Create(&entities.ScriptVersion{StrategyID: strat.ID, Source: src}).Error)
	}
	other := entities.ScriptVersion{StrategyID: strat.ID + 1, Source: "foreign"}
	require.NoError(t, db.Create(&other).Error)

	prev, err = ds.PreviousScript(ctx, strat.ID)
	require.NoError(t, err)
	assert.Equal(t, "v2", prev)
	cur, err := ds.CurrentScript(ctx, strat.ID)
	require.NoError(t, err)
	assert.Equal(t, "v3", cur)
	v1, err := ds.ScriptVersion(ctx, strat.ID, 1)
	require.NoError(t, err)
	assert.Equal(t, "v1", v1)
	_, err = ds.ScriptVersion(ctx, strat.ID, other.ID)
	assert.Error(t, err, "a version of another strategy is not resolvable")
}
