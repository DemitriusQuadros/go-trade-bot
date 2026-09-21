package agent

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"context"

	"go-trade-bot/app/entities"
	"go-trade-bot/app/strategies"
	backtestusecase "go-trade-bot/app/usecase/backtest"
	"go-trade-bot/internal/metrics_provider"
	"go-trade-bot/internal/modelprovider"
)

// Tool is one entry in the closed tool registry: a provider-agnostic
// definition plus its Go executor.
type Tool struct {
	Def     modelprovider.ToolDefinition
	Execute func(ctx context.Context, args json.RawMessage) (result string, err error)
}

// buildToolRegistry is the complete, closed list of everything the model
// may call. There is no generic "invoke any usecase method by name"
// dispatch - every tool is hand-registered here, precisely so a new write
// capability cannot be added by accident (e.g. by a future usecase method
// getting picked up via reflection). Per Backend Spec 03 AC#9: there is no
// tool capable of setting Status = Productive, placing a real order, or
// changing Testnet/exchange credentials - these are absent entirely, not
// merely blocked by a runtime check.
func (u AgentUseCase) buildToolRegistry() []Tool {
	return []Tool{
		u.listStrategiesTool(),          // read
		u.getStrategyTool(),             // read
		u.listBacktestsTool(),           // read
		u.getBacktestTool(),             // read
		u.getOpenPositionsTool(),        // read
		u.getPerformanceSnapshotsTool(), // read
		u.runBacktestTool(),             // writes a BacktestRun only, never a Strategy row
		u.saveStrategyScriptTool(),      // WRITE - the only tool that can touch entities.Strategy
	}
}

var emptySchema = json.RawMessage(`{"type":"object","properties":{}}`)

func (u AgentUseCase) listStrategiesTool() Tool {
	return Tool{
		Def: modelprovider.ToolDefinition{
			Name:        "list_strategies",
			Description: "List every strategy the platform knows about, with id, name, status, and mode.",
			InputSchema: emptySchema,
		},
		Execute: func(ctx context.Context, _ json.RawMessage) (string, error) {
			all, err := u.Strategy.GetAll(ctx)
			if err != nil {
				return "", err
			}
			var b strings.Builder
			for _, s := range all {
				fmt.Fprintf(&b, "strategy_id=%d name=%q strategy_name=%q status=%s mode=%s symbols=%v\n",
					s.ID, s.Name, s.StrategyName, s.Status, s.Mode, []string(s.MonitoredSymbols))
			}
			if b.Len() == 0 {
				return "no strategies exist yet", nil
			}
			return b.String(), nil
		},
	}
}

var getStrategySchema = json.RawMessage(`{"type":"object","properties":{"strategy_id":{"type":"integer"}},"required":["strategy_id"]}`)

func (u AgentUseCase) getStrategyTool() Tool {
	return Tool{
		Def: modelprovider.ToolDefinition{
			Name:        "get_strategy",
			Description: "Get one strategy's full detail, including its script_source, by id.",
			InputSchema: getStrategySchema,
		},
		Execute: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var in struct {
				StrategyID uint `json:"strategy_id"`
			}
			if err := json.Unmarshal(raw, &in); err != nil {
				return "", fmt.Errorf("get_strategy: invalid args: %w", err)
			}
			s, err := u.Strategy.GetByID(ctx, in.StrategyID)
			if err != nil {
				return "", err
			}
			return summarizeStrategyForModel(s), nil
		},
	}
}

func (u AgentUseCase) listBacktestsTool() Tool {
	return Tool{
		Def: modelprovider.ToolDefinition{
			Name:        "list_backtests",
			Description: "List every backtest run recorded for a given strategy_id.",
			InputSchema: getStrategySchema,
		},
		Execute: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var in struct {
				StrategyID uint `json:"strategy_id"`
			}
			if err := json.Unmarshal(raw, &in); err != nil {
				return "", fmt.Errorf("list_backtests: invalid args: %w", err)
			}
			runs, err := u.Backtest.ListByStrategy(ctx, in.StrategyID)
			if err != nil {
				return "", err
			}
			var b strings.Builder
			for _, r := range runs {
				fmt.Fprintf(&b, "backtest_id=%d symbol=%s sharpe=%.2f max_drawdown_pct=%.2f total_trades=%d passed=%v\n",
					r.ID, r.Symbol, r.Sharpe, r.MaxDrawdownPct, r.TotalTrades, r.Passed)
			}
			if b.Len() == 0 {
				return "no backtest runs exist yet for this strategy", nil
			}
			return b.String(), nil
		},
	}
}

var getBacktestSchema = json.RawMessage(`{"type":"object","properties":{"backtest_id":{"type":"integer"}},"required":["backtest_id"]}`)

func (u AgentUseCase) getBacktestTool() Tool {
	return Tool{
		Def: modelprovider.ToolDefinition{
			Name:        "get_backtest",
			Description: "Get one backtest run's full result summary by id.",
			InputSchema: getBacktestSchema,
		},
		Execute: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var in struct {
				BacktestID uint `json:"backtest_id"`
			}
			if err := json.Unmarshal(raw, &in); err != nil {
				return "", fmt.Errorf("get_backtest: invalid args: %w", err)
			}
			run, err := u.Backtest.GetByID(ctx, in.BacktestID)
			if err != nil {
				return "", err
			}
			return summarizeBacktestForModel(run), nil
		},
	}
}

func (u AgentUseCase) getOpenPositionsTool() Tool {
	return Tool{
		Def: modelprovider.ToolDefinition{
			Name:        "get_open_positions",
			Description: "List every currently open signal/position across all strategies. Read-only.",
			InputSchema: emptySchema,
		},
		Execute: func(ctx context.Context, _ json.RawMessage) (string, error) {
			signals, err := u.Signal.GetOpenSignals(ctx)
			if err != nil {
				return "", err
			}
			var b strings.Builder
			for _, s := range signals {
				var entryPrice float32
				if len(s.Orders) > 0 {
					entryPrice = s.Orders[0].EntryPrice
				}
				fmt.Fprintf(&b, "signal_id=%d strategy_id=%d symbol=%s status=%s entry_price=%.4f\n",
					s.ID, s.StrategyID, s.Symbol, s.Status, entryPrice)
			}
			if b.Len() == 0 {
				return "no open positions", nil
			}
			return b.String(), nil
		},
	}
}

var getPerformanceSchema = json.RawMessage(`{"type":"object","properties":{"strategy_id":{"type":"integer"},"limit":{"type":"integer"}},"required":["strategy_id"]}`)

func (u AgentUseCase) getPerformanceSnapshotsTool() Tool {
	return Tool{
		Def: modelprovider.ToolDefinition{
			Name:        "get_performance_snapshots",
			Description: "Get recent daily P&L snapshots for a strategy_id (default limit 30).",
			InputSchema: getPerformanceSchema,
		},
		Execute: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var in struct {
				StrategyID uint `json:"strategy_id"`
				Limit      int  `json:"limit"`
			}
			if err := json.Unmarshal(raw, &in); err != nil {
				return "", fmt.Errorf("get_performance_snapshots: invalid args: %w", err)
			}
			if in.Limit <= 0 {
				in.Limit = 30
			}
			snapshots, err := u.Snapshot.ListByStrategy(ctx, in.StrategyID, in.Limit)
			if err != nil {
				return "", err
			}
			var b strings.Builder
			for _, s := range snapshots {
				fmt.Fprintf(&b, "period_start=%s symbol=%s profit=%.4f trades=%d\n",
					s.PeriodStart.Format(time.RFC3339), s.Symbol, s.Profit, s.Trades)
			}
			if b.Len() == 0 {
				return "no performance snapshots recorded yet for this strategy", nil
			}
			return b.String(), nil
		},
	}
}

// saveStrategyArgs is the model-facing shape of save_strategy_script.
// Deliberately has no `status` field - the model has no channel to request
// Productive (Backend Spec 03 AC#3).
type saveStrategyArgs struct {
	StrategyID   uint     `json:"strategy_id"` // 0 = create
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	ScriptSource string   `json:"script_source"`
	Symbols      []string `json:"symbols"`
	CycleMinutes int      `json:"cycle_minutes"`
	Mode         string   `json:"mode"` // model-supplied; see the clamp below - this value is advisory only
}

var saveStrategySchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"strategy_id": {"type": "integer", "description": "0 to create a new strategy, non-zero to update an existing one"},
		"name": {"type": "string"},
		"description": {"type": "string"},
		"script_source": {"type": "string", "description": "Lua source implementing the Strategy hook contract"},
		"symbols": {"type": "array", "items": {"type": "string"}},
		"cycle_minutes": {"type": "integer", "description": "one of 1, 5, 10, 15, 30, 60"},
		"mode": {"type": "string", "description": "advisory only - always clamped to backtest or dryrun server-side, see tool description"}
	},
	"required": ["name", "description", "script_source", "symbols", "cycle_minutes"]
}`)

// saveStrategyScriptTool is the ONE tool that can create/modify a Strategy
// row, and it is a thin wrapper over the *exact same*
// u.Strategy.Save/Update the web handler calls. THE GATE lives here, as the
// last line of Go code before the usecase call - not as middleware wrapping
// modelprovider.Complete, and not as a check the model could reason its way
// around, because the model's own output (a tool-call argument) is the
// untrusted input this gate defends against.
func (u AgentUseCase) saveStrategyScriptTool() Tool {
	return Tool{
		Def: modelprovider.ToolDefinition{
			Name:        "save_strategy_script",
			Description: "Create or update a script strategy. Mode is always forced to \"backtest\" or \"dryrun\" regardless of what is requested - this tool cannot set live mode or Status=productive.",
			InputSchema: saveStrategySchema,
		},
		Execute: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var in saveStrategyArgs
			if err := json.Unmarshal(raw, &in); err != nil {
				return "", fmt.Errorf("save_strategy_script: invalid args: %w", err)
			}

			// THE GATE: any value other than exactly "backtest" is forced to
			// "dryrun" - including "paper", "live", typos, or an empty
			// string. This is a hard-coded allowlist-of-one-plus-default,
			// not a denylist of "live".
			mode := strategies.ModeDryRun.String()
			if in.Mode == strategies.ModeBacktest.String() {
				mode = in.Mode
			}

			s := entities.Strategy{
				ID:               in.StrategyID,
				Name:             in.Name,
				Description:      in.Description,
				StrategyName:     "script",
				ScriptSource:     in.ScriptSource,
				Status:           entities.Testing, // never Productive - see Acceptance Criterion #3
				Mode:             mode,
				MonitoredSymbols: in.Symbols,
				StrategyConfiguration: entities.StrategyConfiguration{
					Cycle: entities.Cycle(in.CycleMinutes),
				},
			}

			var saved entities.Strategy
			var err error
			if in.StrategyID == 0 {
				saved, err = u.Strategy.Save(ctx, s)
			} else {
				err = u.Strategy.Update(ctx, s)
				saved = s
			}
			if err != nil {
				return "", err
			}
			return summarizeStrategyForModel(saved), nil
		},
	}
}

// summarizeStrategyForModel's first line is machine-parseable
// (strategy_id=N) so RunToolLoop can populate AgentRun.StrategyID after a
// successful save_strategy_script call, without changing the Tool.Execute
// signature.
func summarizeStrategyForModel(s entities.Strategy) string {
	return fmt.Sprintf("strategy_id=%d name=%q strategy_name=%q status=%s mode=%s symbols=%v cycle_minutes=%d\nscript_source:\n%s",
		s.ID, s.Name, s.StrategyName, s.Status, s.Mode, []string(s.MonitoredSymbols), int(s.StrategyConfiguration.Cycle), s.ScriptSource)
}

// runBacktestArgs is the model-facing shape of run_backtest (Backend Spec 06).
type runBacktestArgs struct {
	StrategyID     uint    `json:"strategy_id"`
	Symbol         string  `json:"symbol"`
	Timeframe      string  `json:"timeframe"`
	StartDate      string  `json:"start_date"` // RFC3339
	EndDate        string  `json:"end_date"`
	InitialCapital float64 `json:"initial_capital"`
}

var runBacktestSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"strategy_id": {"type": "integer"},
		"symbol": {"type": "string"},
		"timeframe": {"type": "string", "description": "e.g. 1m, 5m, 15m, 1h"},
		"start_date": {"type": "string", "description": "RFC3339 timestamp"},
		"end_date": {"type": "string", "description": "RFC3339 timestamp"},
		"initial_capital": {"type": "number"}
	},
	"required": ["strategy_id", "symbol", "timeframe", "start_date", "end_date"]
}`)

// runBacktestTool is read/simulation-only - running a backtest against any
// existing StrategyID carries no live-trading risk (Backend Spec 03 AC#8),
// so it needs no additional restriction beyond what BacktestUseCase.Run
// already enforces.
func (u AgentUseCase) runBacktestTool() Tool {
	return Tool{
		Def: modelprovider.ToolDefinition{
			Name:        "run_backtest",
			Description: "Run a backtest for an existing strategy and return a summary of results (trade count, PnL, Sharpe, max drawdown, and the tail of the trade log). Blocks until the backtest completes.",
			InputSchema: runBacktestSchema,
		},
		Execute: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var in runBacktestArgs
			if err := json.Unmarshal(raw, &in); err != nil {
				return "", fmt.Errorf("run_backtest: invalid args: %w", err)
			}
			start, err := time.Parse(time.RFC3339, in.StartDate)
			if err != nil {
				return "", fmt.Errorf("run_backtest: invalid start_date: %w", err)
			}
			end, err := time.Parse(time.RFC3339, in.EndDate)
			if err != nil {
				return "", fmt.Errorf("run_backtest: invalid end_date: %w", err)
			}

			run, err := u.Backtest.Run(ctx, backtestusecase.RunRequest{
				StrategyID:     in.StrategyID,
				Symbol:         in.Symbol,
				Timeframe:      in.Timeframe,
				StartDate:      start,
				EndDate:        end,
				InitialCapital: in.InitialCapital,
			})
			if err != nil {
				return "", err // a real backtest error (e.g. no candle data) is fed back to the model as-is, so it can adjust dates/symbol
			}
			return summarizeBacktestForModel(run), nil
		},
	}
}

// maxBacktestSummaryTradeLines bounds summarizeBacktestForModel's output
// (Backend Spec 06 AC#3): only the trailing N trades, never the full trace.
const maxBacktestSummaryTradeLines = 20

// summarizeBacktestForModel trims a potentially large trade log down to a
// bounded, model-friendly summary: aggregate stats (trade count, win rate,
// Sharpe, max drawdown, final equity) plus only the LAST 20 trade records -
// never the full trace, to stay within a reasonable tool-result size.
func summarizeBacktestForModel(run entities.BacktestRun) string {
	var b strings.Builder
	fmt.Fprintf(&b, "backtest_id=%d strategy_id=%d\nsymbol=%s\nsharpe=%.4f max_drawdown_pct=%.2f win_rate_pct=%.2f profit_factor=%.4f total_trades=%d total_return_pct=%.2f passed=%v initial_capital=%.2f\n",
		run.ID, run.StrategyID, run.Symbol, run.Sharpe, run.MaxDrawdownPct, run.WinRatePct, run.ProfitFactor, run.TotalTrades, run.TotalReturnPct, run.Passed, run.InitialCapital)

	var trades []metrics_provider.TradeLogEntry
	if len(run.TradeLogJSON) > 0 {
		_ = json.Unmarshal(run.TradeLogJSON, &trades)
	}
	if len(trades) > 0 {
		tail := trades
		if len(tail) > maxBacktestSummaryTradeLines {
			tail = tail[len(tail)-maxBacktestSummaryTradeLines:]
		}
		fmt.Fprintf(&b, "trade log (last %d of %d trades):\n", len(tail), len(trades))
		for _, t := range tail {
			fmt.Fprintf(&b, "  %s entry=%.4f exit=%.4f qty=%.6f profit=%.4f reason=%s\n",
				t.ExitTime.Format(time.RFC3339), t.EntryPrice, t.ExitPrice, t.Quantity, t.Profit, t.ExitReason)
		}
	}

	summary := b.String()
	// Hard bound (Backend Spec 06 AC#3): fall back to a further-truncated
	// tail if aggregate stats plus per-trade lines still somehow exceed 8KB
	// (e.g. an unusually verbose exit_reason).
	const maxBytes = 8 * 1024
	if len(summary) > maxBytes {
		summary = summary[:maxBytes] + "\n...(truncated)"
	}
	return summary
}

// strategyIDFromToolResult extracts the strategy_id a successful
// save_strategy_script call reported (see summarizeStrategyForModel's
// machine-parseable first line), so RunToolLoop can populate
// AgentRun.StrategyID without widening the Tool.Execute signature.
func strategyIDFromToolResult(toolName string, _ json.RawMessage, resultText string) (uint, bool) {
	if toolName != "save_strategy_script" {
		return 0, false
	}
	const prefix = "strategy_id="
	idx := strings.Index(resultText, prefix)
	if idx == -1 {
		return 0, false
	}
	rest := resultText[idx+len(prefix):]
	end := strings.IndexAny(rest, " \n")
	if end == -1 {
		end = len(rest)
	}
	id, err := strconv.ParseUint(rest[:end], 10, 64)
	if err != nil {
		return 0, false
	}
	return uint(id), true
}
