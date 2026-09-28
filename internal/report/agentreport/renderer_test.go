package agentreport

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var update = flag.Bool("update", false, "rewrite testdata/*.golden.html")

// fakeData is a deterministic DataSource. Backtest run 7 has distinctive
// metric values the kpi_grid test looks for.
type fakeData struct{}

var base = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

func (fakeData) BacktestRun(_ context.Context, id uint) (Series, error) {
	if id != 7 {
		return Series{}, fmt.Errorf("backtest run %d not found", id)
	}
	return Series{
		Label: "Backtest #7 · BTCUSDT",
		Metrics: Metrics{
			Sharpe: 1.2345, MaxDrawdown: 8.765, MaxDrawdownIsPct: true, WinRatePct: 61.5,
			ProfitFactor: 1.87, TotalTrades: 42, NetPnL: 123.45,
		},
		Equity: []EquityPoint{
			{Time: base, Value: 1000}, {Time: base.Add(24 * time.Hour), Value: 1040},
			{Time: base.Add(48 * time.Hour), Value: 1010}, {Time: base.Add(72 * time.Hour), Value: 1123.45},
		},
		Trades: []Trade{
			{Symbol: "BTCUSDT", EntryTime: base, ExitTime: base.Add(time.Hour), EntryPrice: 100, ExitPrice: 104, Quantity: 1, Profit: 4, ExitReason: "take_profit"},
			{Symbol: "BTCUSDT", EntryTime: base.Add(2 * time.Hour), ExitTime: base.Add(3 * time.Hour), EntryPrice: 104, ExitPrice: 101, Quantity: 1, Profit: -3, ExitReason: "stop_loss"},
		},
	}, nil
}

func (fakeData) StrategyLive(_ context.Context, strategyID uint, days int) (Series, error) {
	return Series{
		Label:   fmt.Sprintf("Strategy #%d live · last %d days", strategyID, days),
		Metrics: Metrics{Sharpe: 0.5, MaxDrawdown: 12.5, WinRatePct: 50, ProfitFactor: math.Inf(1), TotalTrades: 2, NetPnL: -1.5},
	}, nil
}

func (fakeData) ScriptVersion(_ context.Context, strategyID, versionID uint) (string, error) {
	if versionID == 1 {
		return "function OnCandle(ctx)\n  local rsi = ind.rsi(ctx.candles, 14)\n  if rsi < 30 then\n    return buy()\n  end\nend\n", nil
	}
	return "", fmt.Errorf("script version %d not found for strategy %d", versionID, strategyID)
}

func (fakeData) CurrentScript(_ context.Context, _ uint) (string, error) {
	return "function OnCandle(ctx)\n  local rsi = ind.rsi(ctx.candles, 21)\n  if rsi < 25 then\n    return buy()\n  end\nend\n", nil
}

func (fakeData) PreviousScript(ctx context.Context, strategyID uint) (string, error) {
	return fakeData{}.ScriptVersion(ctx, strategyID, 1)
}

func meta() ReportMeta {
	return ReportMeta{
		Title: "Weekly review", Severity: "warning", AgentName: "Risk Monitor", RunID: 99,
		CreatedAt: time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC), StrategyNames: []string{"RSI Momentum", "Breakout"},
	}
}

func block(t *testing.T, typ string, data any) Block {
	t.Helper()
	b, err := json.Marshal(data)
	require.NoError(t, err)
	return Block{Type: typ, Data: b}
}

func render(t *testing.T, blocks ...Block) string {
	t.Helper()
	html, err := NewHTMLRenderer(fakeData{}).Render(context.Background(), meta(), blocks)
	require.NoError(t, err)
	return html
}

func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden.html")
	if *update {
		require.NoError(t, os.MkdirAll("testdata", 0o755))
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err, "missing golden file - run `go test ./internal/report/agentreport -update`")
	assert.Equal(t, string(want), got)
}

func TestRender_GoldenPerBlockType(t *testing.T) {
	from, to := "a\nb\nc\n", "a\nB\nc\nd\n"
	cases := map[string]Block{
		"summary":        block(t, TypeSummary, map[string]any{"text": "First paragraph.\n\nSecond paragraph."}),
		"callout":        block(t, TypeCallout, map[string]any{"severity": "critical", "title": "Drawdown breach", "text": "Live drawdown exceeded the backtest's worst case."}),
		"kpi_grid":       block(t, TypeKPIGrid, map[string]any{"source": map[string]any{"kind": "backtest_run", "id": 7}, "metrics": AllowedMetrics}),
		"equity_chart":   block(t, TypeEquityChart, map[string]any{"source": map[string]any{"kind": "backtest_run", "id": 7}}),
		"trade_table":    block(t, TypeTradeTable, map[string]any{"source": map[string]any{"kind": "backtest_run", "id": 7}, "limit": 1}),
		"code_diff":      block(t, TypeCodeDiff, map[string]any{"strategy_id": 3, "from_version_id": 1}),
		"code_diff_text": block(t, TypeCodeDiff, map[string]any{"strategy_id": 3, "from_source": from, "to_source": to}),
		"recommendation": block(t, TypeRecommendation, map[string]any{"items": []map[string]any{{"title": "Widen the stop", "rationale": "Stops are hit by noise.", "action": "Run a backtest with stop 2%"}}}),
		"text_table":     block(t, TypeTextTable, map[string]any{"columns": []string{"Regime", "Observation"}, "rows": [][]string{{"Trending", "Entries late"}, {"Ranging", "Whipsaws"}}}),
		"live_kpis":      block(t, TypeKPIGrid, map[string]any{"source": map[string]any{"kind": "strategy_live", "strategy_id": 3, "days": 14}, "metrics": []string{"max_drawdown", "profit_factor", "net_pnl"}}),
	}
	for name, b := range cases {
		t.Run(name, func(t *testing.T) {
			assertGolden(t, name, render(t, b))
		})
	}
}

// AC#6: kpi_grid values come from the DB (DataSource), never from numbers
// the model put into the block data.
func TestRender_KPIGridUsesDBValuesAndIgnoresModelNumbers(t *testing.T) {
	b := Block{Type: TypeKPIGrid, Data: json.RawMessage(`{
		"source": {"kind": "backtest_run", "id": 7, "sharpe": 99.99},
		"metrics": ["sharpe", "total_trades", "net_pnl", "win_rate"],
		"values": {"sharpe": 77.77, "total_trades": 5555},
		"sharpe": 88.88
	}`)}
	html := render(t, b)
	assert.Contains(t, html, ">1.23<")   // sharpe from DB
	assert.Contains(t, html, ">42<")     // total trades from DB
	assert.Contains(t, html, ">123.45<") // net pnl from DB
	assert.Contains(t, html, ">61.5%<")
	for _, bogus := range []string{"99.99", "77.77", "5555", "88.88"} {
		assert.NotContains(t, html, bogus)
	}
}

// AC#7: model-supplied markup is escaped everywhere it can appear.
func TestRender_EscapesModelSuppliedHTML(t *testing.T) {
	evil := `<script>alert(1)</script><img src=x onerror=alert(2)>`
	blocks := []Block{
		block(t, TypeSummary, map[string]any{"text": evil}),
		block(t, TypeCallout, map[string]any{"severity": "info", "title": evil, "text": evil}),
		block(t, TypeRecommendation, map[string]any{"items": []map[string]any{{"title": evil, "rationale": evil, "action": evil}}}),
		block(t, TypeTextTable, map[string]any{"columns": []string{evil}, "rows": [][]string{{evil}}}),
		block(t, TypeCodeDiff, map[string]any{"strategy_id": 1, "from_source": "x", "to_source": evil}),
	}
	m := meta()
	m.Title = evil
	m.AgentName = evil
	m.StrategyNames = []string{evil}
	html, err := NewHTMLRenderer(fakeData{}).Render(context.Background(), m, blocks)
	require.NoError(t, err)

	assert.NotContains(t, html, "<script")
	assert.NotContains(t, html, "<img")
	assert.Contains(t, html, "&lt;script&gt;alert(1)&lt;/script&gt;")
}

// AC#8: unknown block types are rejected with the list of allowed types.
func TestValidate_UnknownBlockTypeListsAllowedTypes(t *testing.T) {
	err := Validate([]Block{{Type: "html", Data: json.RawMessage(`{"html":"<b>x</b>"}`)}})
	require.Error(t, err)
	for _, typ := range AllowedTypes {
		assert.Contains(t, err.Error(), typ)
	}
}

func TestValidate_Rules(t *testing.T) {
	tooMany := make([]Block, MaxBlocks+1)
	for i := range tooMany {
		tooMany[i] = Block{Type: TypeSummary, Data: json.RawMessage(`{"text":"x"}`)}
	}
	cases := map[string][]Block{
		"empty":         nil,
		"too many":      tooMany,
		"bad metric":    {{Type: TypeKPIGrid, Data: json.RawMessage(`{"source":{"kind":"backtest_run","id":1},"metrics":["alpha"]}`)}},
		"bad source":    {{Type: TypeEquityChart, Data: json.RawMessage(`{"source":{"kind":"csv","id":1}}`)}},
		"missing id":    {{Type: TypeEquityChart, Data: json.RawMessage(`{"source":{"kind":"backtest_run"}}`)}},
		"limit":         {{Type: TypeTradeTable, Data: json.RawMessage(`{"source":{"kind":"backtest_run","id":1},"limit":500}`)}},
		"ragged table":  {{Type: TypeTextTable, Data: json.RawMessage(`{"columns":["a","b"],"rows":[["x"]]}`)}},
		"callout sev":   {{Type: TypeCallout, Data: json.RawMessage(`{"severity":"emergency","text":"x"}`)}},
		"diff both":     {{Type: TypeCodeDiff, Data: json.RawMessage(`{"strategy_id":1,"from_version_id":1,"from_source":"a","to_source":"b"}`)}},
		"summary empty": {{Type: TypeSummary, Data: json.RawMessage(`{"text":"  "}`)}},
		"no data":       {{Type: TypeSummary}},
	}
	for name, blocks := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Error(t, Validate(blocks))
		})
	}
}

func TestRender_DataSourceErrorSurfaces(t *testing.T) {
	_, err := NewHTMLRenderer(fakeData{}).Render(context.Background(), meta(), []Block{
		{Type: TypeKPIGrid, Data: json.RawMessage(`{"source":{"kind":"backtest_run","id":8},"metrics":["sharpe"]}`)},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestRender_NoExternalResourcesOrScripts(t *testing.T) {
	html := render(t, block(t, TypeEquityChart, map[string]any{"source": map[string]any{"kind": "backtest_run", "id": 7}}))
	assert.NotContains(t, strings.ToLower(html), "<script")
	assert.NotContains(t, html, "http://")
	assert.NotContains(t, html, "https://")
	assert.Contains(t, html, "prefers-color-scheme: dark")
	assert.Contains(t, html, ".dark {")
	assert.True(t, strings.HasPrefix(html, "<!DOCTYPE html>\n<html lang=\"en\">"))
}

func TestFirstSummary(t *testing.T) {
	blocks := []Block{
		{Type: TypeCallout, Data: json.RawMessage(`{"severity":"info","text":"c"}`)},
		{Type: TypeSummary, Data: json.RawMessage(`{"text":"the summary"}`)},
	}
	assert.Equal(t, "the summary", FirstSummary(blocks))
	assert.Equal(t, "", FirstSummary(blocks[:1]))
}

func TestUnifiedDiff(t *testing.T) {
	lines, note := unifiedDiff("a\nb\nc\n", "a\nc\nd\n")
	assert.Empty(t, note)
	var kinds []string
	for _, l := range lines {
		kinds = append(kinds, l.Kind+":"+l.Text)
	}
	assert.Equal(t, []string{"ctx:a", "del:b", "ctx:c", "add:d"}, kinds)

	_, note = unifiedDiff("same\n", "same\n")
	assert.Equal(t, "No changes.", note)
}
