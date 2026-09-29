package agent_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	handler "go-trade-bot/app/handler/web/agent"
	"go-trade-bot/app/usecase/agent/deploygate"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// compactJSON mirrors app/usecase/agent's compactJSON (json.Marshal of the
// tool's result map), so these cases are byte-for-byte what the tools emit.
func compactJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return string(b)
}

// strategySummary is summarizeStrategyForModel's exact format (tools.go) -
// the result of save_strategy_script, get_strategy and create_strategy.
func strategySummary(id uint, source string) string {
	return fmt.Sprintf("strategy_id=%d name=%q strategy_name=%q status=%s mode=%s symbols=%v cycle_minutes=%d\nscript_source:\n%s",
		id, "EMA Cross", "script", "testing", "backtest", []string{"BTCUSDT"}, 15, source)
}

// backtestSummary is summarizeBacktestForModel's exact first lines
// (tools.go) - the result of run_backtest and get_backtest.
func backtestSummary(backtestID, strategyID uint) string {
	return fmt.Sprintf("backtest_id=%d strategy_id=%d\nsymbol=%s\nsharpe=%.4f max_drawdown_pct=%.2f win_rate_pct=%.2f profit_factor=%s total_trades=%d total_return_pct=%.2f passed=%v initial_capital=%.2f\n",
		backtestID, strategyID, "BTCUSDT", 1.2345, 12.5, 55.0, "1.8000", 42, 18.2, true, 1000.0) +
		"trade log (last 1 of 1 trades):\n  2026-01-01T00:00:00Z entry=100.0000 exit=110.0000 qty=0.100000 profit=1.0000 reason=take_profit\n"
}

func gateResult() deploygate.GateResult {
	return deploygate.GateResult{
		Passed:         false,
		Checks:         []deploygate.Check{{Name: deploygate.CheckSharpe, Passed: false, Candidate: 0.8, Baseline: 1.1, Threshold: 1.1, Detail: "sharpe below baseline"}},
		BaselineRunID:  31,
		CandidateRunID: 32,
	}
}

var gateContext = map[string]any{
	"symbol": "BTCUSDT", "timeframe": "15m", "from": "2026-03-01T00:00:00Z", "to": "2026-09-01T00:00:00Z",
	"config": map[string]any{"min_sharpe_delta": 0, "max_drawdown_ratio": 1.1, "min_trades": 20, "min_profit_factor": 1, "lookback_months": 6, "train_months": 3, "test_months": 1, "timeframe": ""},
}

func ref(kind string, id uint, role ...string) handler.ToolRef {
	r := handler.ToolRef{Kind: kind, ID: id}
	if len(role) > 0 {
		r.Role = role[0]
	}
	return r
}

func TestExtractToolRefs(t *testing.T) {
	var listBacktests strings.Builder
	for i := 1; i <= 25; i++ {
		fmt.Fprintf(&listBacktests, "backtest_id=%d symbol=%s sharpe=%.2f max_drawdown_pct=%.2f total_trades=%d passed=%v\n", i, "BTCUSDT", 1.0, 10.0, 30, true)
	}
	wantCapped := make([]handler.ToolRef, 0, 20)
	for i := uint(1); i <= 20; i++ {
		wantCapped = append(wantCapped, ref("backtest", i))
	}

	cases := []struct {
		name    string
		tool    string
		result  string
		isError bool
		want    []handler.ToolRef
	}{
		{
			name:   "save_strategy_script text, ids inside the script source are ignored",
			tool:   "save_strategy_script",
			result: strategySummary(7, "-- see strategy_id=99\nfunction on_candle(ctx) end"),
			want:   []handler.ToolRef{ref("strategy", 7)},
		},
		{
			name:   "get_strategy text",
			tool:   "get_strategy",
			result: strategySummary(5, "function on_candle(ctx) end"),
			want:   []handler.ToolRef{ref("strategy", 5)},
		},
		{
			name:   "create_strategy text marks the created strategy",
			tool:   "create_strategy",
			result: strategySummary(12, "function on_candle(ctx) end"),
			want:   []handler.ToolRef{ref("strategy", 12, "created")},
		},
		{
			name: "list_strategies lines",
			tool: "list_strategies",
			result: fmt.Sprintf("strategy_id=%d name=%q strategy_name=%q status=%s mode=%s symbols=%v\n", 1, "A", "script", "testing", "dryrun", []string{"BTCUSDT"}) +
				fmt.Sprintf("strategy_id=%d name=%q strategy_name=%q status=%s mode=%s symbols=%v\n", 2, "B", "script", "productive", "live", []string{"ETHUSDT"}),
			want: []handler.ToolRef{ref("strategy", 1), ref("strategy", 2)},
		},
		{
			name:   "run_backtest text",
			tool:   "run_backtest",
			result: backtestSummary(44, 7),
			want:   []handler.ToolRef{ref("backtest", 44), ref("strategy", 7)},
		},
		{
			name:   "get_backtest text",
			tool:   "get_backtest",
			result: backtestSummary(45, 7),
			want:   []handler.ToolRef{ref("backtest", 45), ref("strategy", 7)},
		},
		{
			name:   "list_backtests is capped at 20 refs",
			tool:   "list_backtests",
			result: listBacktests.String(),
			want:   wantCapped,
		},
		{
			name:   "get_open_positions: signal ids are not refs, strategy ids are deduped",
			tool:   "get_open_positions",
			result: "signal_id=3 strategy_id=5 symbol=BTCUSDT status=open entry_price=100.0000\nsignal_id=4 strategy_id=5 symbol=ETHUSDT status=open entry_price=10.0000\n",
			want:   []handler.ToolRef{ref("strategy", 5)},
		},
		{
			name:   "run_optimization text",
			tool:   "run_optimization",
			result: "optimization_run_id=4 status=pending total_combinations=12\nThis runs asynchronously - call get_optimization_results with this id to check progress and see results once complete.",
			want:   []handler.ToolRef{ref("optimization", 4)},
		},
		{
			name:   "get_optimization_results text",
			tool:   "get_optimization_results",
			result: "optimization_run_id=4 strategy_id=7 symbol=BTCUSDT status=running progress=3/12\nNot completed yet - call this tool again later to check progress.\n",
			want:   []handler.ToolRef{ref("optimization", 4), ref("strategy", 7)},
		},
		{
			name:   "list_optimizations text",
			tool:   "list_optimizations",
			result: "optimization_run_id=4 status=completed progress=12/12 best_sharpe=1.2000\noptimization_run_id=5 status=pending progress=0/8\n",
			want:   []handler.ToolRef{ref("optimization", 4), ref("optimization", 5)},
		},
		{
			name:   "write_report",
			tool:   "write_report",
			result: compactJSON(t, map[string]any{"report_id": uint(3), "url": "http://localhost:8080/agents/reports/3"}),
			want:   []handler.ToolRef{ref("report", 3)},
		},
		{
			name:   "write_journal",
			tool:   "write_journal",
			result: compactJSON(t, map[string]any{"memory_id": uint(8), "strategy_id": uint(5), "kind": "journal"}),
			want:   []handler.ToolRef{ref("strategy", 5), ref("memory", 8)},
		},
		{
			name:   "create_challenger (new)",
			tool:   "create_challenger",
			result: compactJSON(t, map[string]any{"challenger_strategy_id": uint(12), "champion_strategy_id": uint(5), "created": true}),
			want:   []handler.ToolRef{ref("strategy", 12, "challenger"), ref("strategy", 5, "champion")},
		},
		{
			name: "create_challenger (already exists)",
			tool: "create_challenger",
			result: compactJSON(t, map[string]any{
				"challenger_strategy_id": uint(12), "champion_strategy_id": uint(5), "created": false,
				"note": "an active challenger already exists for this champion; iterate on it with deploy_to_testing",
			}),
			want: []handler.ToolRef{ref("strategy", 12, "challenger"), ref("strategy", 5, "champion")},
		},
		{
			name: "deploy_to_testing (deployed)",
			tool: "deploy_to_testing",
			result: func() string {
				g := gateResult()
				g.Passed = true
				return compactJSON(t, map[string]any{"deployed": true, "strategy_id": uint(7), "gate": &g, "gate_context": gateContext})
			}(),
			want: []handler.ToolRef{ref("strategy", 7), ref("backtest", 31, "baseline"), ref("backtest", 32, "candidate")},
		},
		{
			name: "deploy_to_testing (gate failed -> proposal)",
			tool: "deploy_to_testing",
			result: func() string {
				g := gateResult()
				return compactJSON(t, map[string]any{"deployed": false, "proposal_id": uint(9), "gate": &g, "gate_context": gateContext, "url": "http://localhost:8080/agents/proposals/9"})
			}(),
			want: []handler.ToolRef{ref("proposal", 9), ref("backtest", 31, "baseline"), ref("backtest", 32, "candidate")},
		},
		{
			name: "deploy_to_testing with insufficient history (zero run ids are not refs)",
			tool: "deploy_to_testing",
			result: func() string {
				g := deploygate.GateResult{Checks: []deploygate.Check{{Name: deploygate.CheckInsufficientHistory, Detail: "not enough candles"}}}
				return compactJSON(t, map[string]any{"deployed": false, "proposal_id": uint(10), "gate": &g, "gate_context": gateContext, "url": "u"})
			}(),
			want: []handler.ToolRef{ref("proposal", 10)},
		},
		{
			// propose_promotion's result carries only the proposal id (the
			// challenger/champion ids live in the proposal itself).
			name: "propose_promotion",
			tool: "propose_promotion",
			result: compactJSON(t, map[string]any{
				"proposal_id": uint(11), "status": "pending", "early": false, "gate_passed": true,
				"superseded": int64(1), "notified": true, "url": "http://localhost:8080/agents/proposals/11",
			}),
			want: []handler.ToolRef{ref("proposal", 11)},
		},
		{
			name:   "trigger_agent",
			tool:   "trigger_agent",
			result: compactJSON(t, map[string]any{"enqueued": true, "task_id": "agent-chain:4:77", "agent_id": uint(4), "agent_name": "Risk Watcher"}),
			want:   []handler.ToolRef{ref("agent", 4)},
		},
		{
			name:   "list_proposals array",
			tool:   "list_proposals",
			result: `[{"id":9,"kind":"gate_failed_change","target_strategy_id":7,"agent_id":2,"status":"pending","early":false,"gate_passed":false,"rationale":"r","created_at":"2026-09-01T00:00:00Z"},{"id":8,"kind":"promote_challenger","target_strategy_id":5,"challenger_strategy_id":12,"agent_id":2,"status":"rejected","early":false,"gate_passed":true,"rationale":"r","created_at":"2026-08-01T00:00:00Z"}]`,
			want:   []handler.ToolRef{ref("proposal", 9), ref("proposal", 8)},
		},
		{
			name:   "list_reports array",
			tool:   "list_reports",
			result: `[{"id":3,"title":"Weekly","severity":"info","created_at":"2026-09-01T00:00:00Z","agent":"Copilot","strategy_ids":[5]}]`,
			want:   []handler.ToolRef{ref("report", 3)},
		},
		{
			name:   "read_memory array",
			tool:   "read_memory",
			result: `[{"id":21,"author":"operator","kind":"journal","content":"strategy_id=5 looks fine","created_at":"2026-09-01T00:00:00Z"}]`,
			want:   []handler.ToolRef{ref("memory", 21)},
		},
		{
			name:   "notify has no refs",
			tool:   "notify",
			result: `{"sent_to_targets":2}`,
			want:   []handler.ToolRef{},
		},
		{
			name:   "get_deploy_gate_config has no refs",
			tool:   "get_deploy_gate_config",
			result: `{"min_sharpe_delta":0,"max_drawdown_ratio":1.1,"min_trades":20,"min_profit_factor":1,"lookback_months":6,"train_months":3,"test_months":1,"timeframe":""}`,
			want:   []handler.ToolRef{},
		},
		{
			name:   "run_id outside optimization tools is ignored",
			tool:   "some_other_tool",
			result: `{"run_id":5}`,
			want:   []handler.ToolRef{},
		},
		{
			name:   "run_id on an optimization tool is an optimization",
			tool:   "get_optimization_results",
			result: `{"run_id":5}`,
			want:   []handler.ToolRef{ref("optimization", 5)},
		},
		{
			name:    "errored call has no refs even with ids in the result",
			tool:    "run_backtest",
			result:  backtestSummary(44, 7),
			isError: true,
			want:    []handler.ToolRef{},
		},
		{
			name:   "plain non-JSON result without ids",
			tool:   "get_candle_coverage",
			result: "no candles stored for any symbol - import history first (POST /api/candles/import)",
			want:   []handler.ToolRef{},
		},
		{
			name:   "empty result",
			tool:   "list_strategies",
			result: "",
			want:   []handler.ToolRef{},
		},
		{
			name:   "malformed JSON falls back to the text rules",
			tool:   "whatever",
			result: `{broken strategy_id=3`,
			want:   []handler.ToolRef{ref("strategy", 3)},
		},
		{
			name:   "an id glued to another word is not a ref",
			tool:   "whatever",
			result: "xstrategy_id=3",
			want:   []handler.ToolRef{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := handler.ExtractToolRefs(tc.tool, tc.result, tc.isError)
			require.NotNil(t, got, "refs must never be nil")
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestExtractToolRefs_JSONNeverNull pins the wire shape: an empty result is
// "refs":[] and the role key is omitted when unset.
func TestExtractToolRefs_JSONNeverNull(t *testing.T) {
	b, err := json.Marshal(handler.ExtractToolRefs("notify", "", false))
	require.NoError(t, err)
	assert.Equal(t, "[]", string(b))

	b, err = json.Marshal(handler.ExtractToolRefs("run_backtest", backtestSummary(44, 7), false))
	require.NoError(t, err)
	assert.JSONEq(t, `[{"kind":"backtest","id":44},{"kind":"strategy","id":7}]`, string(b))
}
