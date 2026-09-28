package agent

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"context"

	"go-trade-bot/app/entities"
	"go-trade-bot/app/strategies"
	backtestusecase "go-trade-bot/app/usecase/backtest"
	optimizeusecase "go-trade-bot/app/usecase/optimize"
	"go-trade-bot/internal/metrics_provider"
	"go-trade-bot/internal/modelprovider"
)

// Tool is one entry in the closed tool registry: a provider-agnostic
// definition plus its Go executor.
type Tool struct {
	Def     modelprovider.ToolDefinition
	Execute func(ctx context.Context, args json.RawMessage) (result string, err error)
	// Permission is what an agent persona must be granted to be offered
	// (and to execute) this tool. "" = granted to every agent.
	Permission entities.AgentPermission
}

// withPermission tags a tool with its required permission.
func withPermission(t Tool, p entities.AgentPermission) Tool {
	t.Permission = p
	return t
}

// buildToolRegistry is the complete, closed list of everything the model
// may call. There is no generic "invoke any usecase method by name"
// dispatch - every tool is hand-registered here, precisely so a new write
// capability cannot be added by accident (e.g. by a future usecase method
// getting picked up via reflection). Per Backend Spec 03 AC#9: there is no
// tool capable of setting Status = Productive, placing a real order, or
// changing Testnet/exchange credentials - these are absent entirely, not
// merely blocked by a runtime check.
//
// Agents platform (A-01 §4.2): every tool is tagged with the permission an
// agent persona needs to be offered it. The new platform tools (memory,
// reports, notify - tools_platform.go) write only their own tables and can
// never modify entities.Strategy, place/cancel orders, or touch settings.
func (u AgentUseCase) buildToolRegistry() []Tool {
	return []Tool{
		withPermission(u.listStrategiesTool(), entities.PermRead),
		withPermission(u.getStrategyTool(), entities.PermRead),
		withPermission(u.listBacktestsTool(), entities.PermRead),
		withPermission(u.getBacktestTool(), entities.PermRead),
		withPermission(u.getOpenPositionsTool(), entities.PermRead),
		withPermission(u.getPerformanceSnapshotsTool(), entities.PermRead),
		withPermission(u.getCandleCoverageTool(), entities.PermRead),         // read - fix-02 B2
		withPermission(u.runBacktestTool(), entities.PermBacktest),           // writes a BacktestRun only, never a Strategy row
		withPermission(u.listOptimizationsTool(), entities.PermRead),         // read
		withPermission(u.runOptimizationTool(), entities.PermOptimize),       // writes an OptimizationRun only, never a Strategy row
		withPermission(u.getOptimizationResultsTool(), entities.PermRead),    // read
		withPermission(u.saveStrategyScriptTool(), entities.PermEditTesting), // WRITE - creates strategies / updates backtest-mode drafts only
		u.readMemoryTool(),   // always granted
		u.writeJournalTool(), // always granted
		u.listReportsTool(),  // always granted
		u.writeReportTool(),  // always granted
		withPermission(u.notifyTool(), entities.PermNotify),
		// Phase B-01 (tools_phaseb.go). None can change a live-mode or
		// productive strategy's source, mode or status.
		withPermission(u.createChallengerTool(), entities.PermEditTesting),  // WRITE - creates a dryrun/testing clone; the champion is only read
		withPermission(u.deployToTestingTool(), entities.PermEditTesting),   // WRITE - non-live, non-productive, in-scope strategies only, behind the Go deploy gate
		withPermission(u.proposePromotionTool(), entities.PermProposeLive),  // files a pending proposal only; applying needs operator approval over REST
		withPermission(u.createStrategyTool(), entities.PermCreateStrategy), // WRITE - new testing strategies (backtest/dryrun), max 3/day
		u.listProposalsTool(),       // always granted, read-only
		u.getDeployGateConfigTool(), // always granted, read-only
		// Phase C-01 §4: starts another agent's (unattended, fully guarded)
		// run; it cannot act on the exchange or on any strategy itself.
		withPermission(u.triggerAgentTool(), entities.PermChain),
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

			// Agents platform (A-02 §4): one writer per existing strategy.
			// Taken before the write; the gate above is unchanged.
			if in.StrategyID != 0 {
				release, lockErr := u.acquireStrategyLock(ctx, in.StrategyID)
				if lockErr != nil {
					return "", lockErr
				}
				defer release()

				// Live/productive strategies are never edited by any agent
				// path (chat, cron/manual, MCP) - the Mode/Status clamp above
				// would otherwise replace a live strategy's script and silently
				// take it out of live trading. Live changes go through an
				// operator-approved proposal (agents platform Phase B).
				existing, getErr := u.Strategy.GetByID(ctx, in.StrategyID)
				if getErr != nil {
					return "", fmt.Errorf("save_strategy_script: could not load strategy %d: %w", in.StrategyID, getErr)
				}
				if existing.IsLiveOrProductive() {
					return "", fmt.Errorf("save_strategy_script: strategy %d is %s/%s - agents may only edit non-productive, non-live strategies; use create_challenger + propose_promotion, or record a finding or write a report instead", in.StrategyID, existing.Status, existing.Mode)
				}
				// Phase B-01 §4: one rule for changing code that runs - only
				// backtest-mode drafts are updated here; everything else goes
				// through the gated deploy_to_testing.
				if existing.Mode != strategies.ModeBacktest.String() {
					return "", fmt.Errorf("save_strategy_script: strategy %d is in %s mode - use deploy_to_testing (gated) to change it; save_strategy_script only updates backtest-mode drafts", in.StrategyID, existing.Mode)
				}
				agent, _, agentErr := u.toolAgent(ctx)
				if agentErr != nil {
					return "", agentErr
				}
				if scopeErr := u.inWriteScope(ctx, agent, existing); scopeErr != nil {
					return "", fmt.Errorf("save_strategy_script: %w", scopeErr)
				}
			} else if agent, _, agentErr := u.toolAgent(ctx); agentErr == nil && agent.ID != 0 {
				// Record the creator so the new draft stays in this agent's
				// write scope (Phase B-01 §6).
				agentID := agent.ID
				s.CreatedByAgentID = &agentID
			}

			// Creating a strategy from an unattended run (cron, manual,
			// event, market, chain) is governed exactly like create_strategy:
			// it needs that permission and counts toward the same daily cap,
			// and the new strategy is bound to the agent. Otherwise
			// save_strategy_script's create path bypassed both (E2E run #58
			// created a dryrun "scratch" strategy with only edit_testing).
			// Chat and MCP drafting is unchanged: an operator is driving it.
			var unattendedAgent *entities.Agent
			if in.StrategyID == 0 {
				if sc, ok := scopeFrom(ctx); ok && isScheduledTrigger(sc.trigger) && sc.agent.ID != 0 {
					if !sc.agent.HasPermission(entities.PermCreateStrategy) {
						return "", fmt.Errorf("save_strategy_script: creating a new strategy from an unattended run needs the create_strategy permission; improve an existing strategy with deploy_to_testing instead")
					}
					if u.Platform == nil {
						return "", fmt.Errorf("save_strategy_script: strategy creation is not available here")
					}
					ok, limErr := u.Platform.TryIncStrategiesCreated(ctx, sc.agent.ID, u.now(), MaxStrategiesCreatedPerDay)
					if limErr != nil {
						return "", fmt.Errorf("save_strategy_script: could not check the daily creation limit: %w", limErr)
					}
					if !ok {
						return "", fmt.Errorf("save_strategy_script: agent %q already created %d strategies today (UTC) - the limit is %d per day", sc.agent.Name, MaxStrategiesCreatedPerDay, MaxStrategiesCreatedPerDay)
					}
					a := sc.agent
					unattendedAgent = &a
				}
			}

			var saved entities.Strategy
			var err error
			if in.StrategyID == 0 {
				saved, err = u.Strategy.Save(ctx, s)
				if err == nil && unattendedAgent != nil {
					if bErr := u.Platform.AddBinding(ctx, unattendedAgent.ID, saved.ID); bErr != nil {
						log.Printf("agent: strategy %d created by agent %q but binding failed: %v", saved.ID, unattendedAgent.Name, bErr)
					}
				}
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

// formatProfitFactorForModel renders the no-losing-trades case as "inf".
// The backtest usecase persists +Inf as math.MaxFloat64 (Postgres can't
// store Inf); printed with %.4f that is a 309-digit number the model then
// has to reason about (E2E run #58). Same sentinel rule as
// app/handler/web/backtest/dto.go.
func formatProfitFactorForModel(pf float64) string {
	if math.IsInf(pf, 1) || pf >= 1e15 {
		return "inf"
	}
	return strconv.FormatFloat(pf, 'f', 4, 64)
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
	fmt.Fprintf(&b, "backtest_id=%d strategy_id=%d\nsymbol=%s\nsharpe=%.4f max_drawdown_pct=%.2f win_rate_pct=%.2f profit_factor=%s total_trades=%d total_return_pct=%.2f passed=%v initial_capital=%.2f\n",
		run.ID, run.StrategyID, run.Symbol, run.Sharpe, run.MaxDrawdownPct, run.WinRatePct, formatProfitFactorForModel(run.ProfitFactor), run.TotalTrades, run.TotalReturnPct, run.Passed, run.InitialCapital)

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

// --- Optimization (hyperparameter grid search) --------------------------
//
// Closes a real gap in the platform's original MCP-exposure goal: grid
// search runs were fully persisted (entities.OptimizationRun) and
// REST-queryable from day one, but never reachable through the agent/MCP
// tool registry - list_optimizations/run_optimization/get_optimization_results
// bring it to parity with the backtest tools above.

var listOptimizationsSchema = getStrategySchema

func (u AgentUseCase) listOptimizationsTool() Tool {
	return Tool{
		Def: modelprovider.ToolDefinition{
			Name:        "list_optimizations",
			Description: "List every optimization (hyperparameter grid search) run recorded for a given strategy_id, with status and best Sharpe ratio if completed.",
			InputSchema: listOptimizationsSchema,
		},
		Execute: func(ctx context.Context, raw json.RawMessage) (string, error) {
			if u.Optimize == nil {
				return "", fmt.Errorf("list_optimizations: optimization is not available on this server")
			}
			var in struct {
				StrategyID uint `json:"strategy_id"`
			}
			if err := json.Unmarshal(raw, &in); err != nil {
				return "", fmt.Errorf("list_optimizations: invalid args: %w", err)
			}
			runs, err := u.Optimize.ListByStrategy(ctx, in.StrategyID)
			if err != nil {
				return "", err
			}
			var b strings.Builder
			for _, r := range runs {
				fmt.Fprintf(&b, "optimization_run_id=%d status=%s progress=%d/%d", r.ID, r.Status, r.Progress, r.TotalCombinations)
				if r.Status == entities.OptimizationCompleted {
					var best metrics_provider.BacktestMetrics
					_ = json.Unmarshal(r.BestMetricsJSON, &best)
					fmt.Fprintf(&b, " best_sharpe=%.4f", best.SharpeRatio)
				}
				b.WriteString("\n")
			}
			if b.Len() == 0 {
				return "no optimization runs exist yet for this strategy", nil
			}
			return b.String(), nil
		},
	}
}

var runOptimizationSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"strategy_id": {"type": "integer"},
		"symbol": {"type": "string"},
		"timeframe": {"type": "string", "description": "e.g. 1m, 5m, 15m, 1h"},
		"start_date": {"type": "string", "description": "RFC3339 timestamp"},
		"end_date": {"type": "string", "description": "RFC3339 timestamp"},
		"initial_capital": {"type": "number"},
		"param_grid": {
			"type": "object",
			"description": "Maps a strategy config field name - read by the Lua script as ctx.config.<name>, NOT a hardcoded literal in the script source - to a numeric sweep range. A name the script never reads produces identical results for every combination.",
			"additionalProperties": {
				"type": "object",
				"properties": {
					"min": {"type": "number"},
					"max": {"type": "number"},
					"step": {"type": "number"}
				},
				"required": ["min", "max", "step"]
			}
		}
	},
	"required": ["strategy_id", "symbol", "timeframe", "start_date", "end_date", "param_grid"]
}`)

// runOptimizationTool is read/simulation-only, same reasoning as
// runBacktestTool - it only ever writes an OptimizationRun row, never a
// Strategy row, so it carries no live-trading risk. Unlike run_backtest,
// it does NOT block until completion: a grid search runs sequentially
// (app/usecase/optimize's own homelab-memory-motivated design) and can
// take minutes, far too long for one tool-call turn - it starts the async
// job (mirroring the HTTP handler's create+enqueue) and returns
// immediately, so the model is expected to call get_optimization_results
// again later to check progress.
func (u AgentUseCase) runOptimizationTool() Tool {
	return Tool{
		Def: modelprovider.ToolDefinition{
			Name:        "run_optimization",
			Description: "Start a hyperparameter grid search for an existing strategy: sweeps one or more numeric config fields across a range, backtesting every combination and scoring by Sharpe ratio (capped combination count). Runs asynchronously - a large grid can take minutes - so this returns the pending run's id immediately rather than waiting; call get_optimization_results with that id to check progress and read results once complete.",
			InputSchema: runOptimizationSchema,
		},
		Execute: func(ctx context.Context, raw json.RawMessage) (string, error) {
			if u.Optimize == nil || u.OptimizeWorker == nil {
				return "", fmt.Errorf("run_optimization: optimization is not available on this server")
			}
			var in struct {
				StrategyID     uint                      `json:"strategy_id"`
				Symbol         string                    `json:"symbol"`
				Timeframe      string                    `json:"timeframe"`
				StartDate      string                    `json:"start_date"`
				EndDate        string                    `json:"end_date"`
				InitialCapital float64                   `json:"initial_capital"`
				ParamGrid      optimizeusecase.ParamGrid `json:"param_grid"`
			}
			if err := json.Unmarshal(raw, &in); err != nil {
				return "", fmt.Errorf("run_optimization: invalid args: %w", err)
			}
			start, err := time.Parse(time.RFC3339, in.StartDate)
			if err != nil {
				return "", fmt.Errorf("run_optimization: invalid start_date: %w", err)
			}
			end, err := time.Parse(time.RFC3339, in.EndDate)
			if err != nil {
				return "", fmt.Errorf("run_optimization: invalid end_date: %w", err)
			}

			run, err := u.Optimize.Create(ctx, optimizeusecase.CreateRequest{
				StrategyID:     in.StrategyID,
				Symbol:         in.Symbol,
				Timeframe:      in.Timeframe,
				StartDate:      start,
				EndDate:        end,
				InitialCapital: in.InitialCapital,
				ParamGrid:      in.ParamGrid,
			})
			if err != nil {
				return "", err // e.g. grid too large, no candle data, strategy not registered - fed back as-is so the model can adjust
			}
			if err := u.OptimizeWorker.EnqueueOptimizeTask(run.ID); err != nil {
				return "", fmt.Errorf("run_optimization: created run %d but failed to enqueue it: %w", run.ID, err)
			}
			return fmt.Sprintf(
				"optimization_run_id=%d status=%s total_combinations=%d\nThis runs asynchronously - call get_optimization_results with this id to check progress and see results once complete.",
				run.ID, run.Status, run.TotalCombinations,
			), nil
		},
	}
}

var getOptimizationResultsSchema = json.RawMessage(`{"type":"object","properties":{"optimization_run_id":{"type":"integer"}},"required":["optimization_run_id"]}`)

func (u AgentUseCase) getOptimizationResultsTool() Tool {
	return Tool{
		Def: modelprovider.ToolDefinition{
			Name:        "get_optimization_results",
			Description: "Get one optimization (grid search) run's status by id, and once completed, its best parameter combination and the top results by Sharpe ratio.",
			InputSchema: getOptimizationResultsSchema,
		},
		Execute: func(ctx context.Context, raw json.RawMessage) (string, error) {
			if u.Optimize == nil {
				return "", fmt.Errorf("get_optimization_results: optimization is not available on this server")
			}
			var in struct {
				OptimizationRunID uint `json:"optimization_run_id"`
			}
			if err := json.Unmarshal(raw, &in); err != nil {
				return "", fmt.Errorf("get_optimization_results: invalid args: %w", err)
			}
			run, err := u.Optimize.GetByID(ctx, in.OptimizationRunID)
			if err != nil {
				return "", err
			}
			return summarizeOptimizationForModel(run), nil
		},
	}
}

// maxOptimizationSummaryPoints bounds summarizeOptimizationForModel's
// output the same way maxBacktestSummaryTradeLines bounds a backtest
// summary: only the top N combinations by Sharpe, never the full grid
// (which can hold up to DefaultMaxCombinations/500 entries).
const maxOptimizationSummaryPoints = 10

func summarizeOptimizationForModel(run entities.OptimizationRun) string {
	var b strings.Builder
	fmt.Fprintf(&b, "optimization_run_id=%d strategy_id=%d symbol=%s status=%s progress=%d/%d\n",
		run.ID, run.StrategyID, run.Symbol, run.Status, run.Progress, run.TotalCombinations)
	if run.ErrorMessage != "" {
		fmt.Fprintf(&b, "error=%s\n", run.ErrorMessage)
	}
	if run.Status != entities.OptimizationCompleted {
		b.WriteString("Not completed yet - call this tool again later to check progress.\n")
		return b.String()
	}

	var bestConfig map[string]float64
	_ = json.Unmarshal(run.BestConfigJSON, &bestConfig)
	var bestMetrics metrics_provider.BacktestMetrics
	_ = json.Unmarshal(run.BestMetricsJSON, &bestMetrics)
	fmt.Fprintf(&b, "best_params=%v best_sharpe=%.4f best_max_drawdown_pct=%.2f best_total_return_pct=%.2f\n",
		bestConfig, bestMetrics.SharpeRatio, bestMetrics.MaxDrawdownPct, bestMetrics.TotalReturnPct)

	var grid []optimizeusecase.GridPoint
	_ = json.Unmarshal(run.ResultsGridJSON, &grid)
	sort.Slice(grid, func(i, j int) bool {
		if grid[i].Metrics == nil {
			return false
		}
		if grid[j].Metrics == nil {
			return true
		}
		return grid[i].Metrics.SharpeRatio > grid[j].Metrics.SharpeRatio
	})
	top := grid
	if len(top) > maxOptimizationSummaryPoints {
		top = top[:maxOptimizationSummaryPoints]
	}
	fmt.Fprintf(&b, "top %d of %d combinations by Sharpe:\n", len(top), len(grid))
	for _, p := range top {
		if p.Metrics != nil {
			fmt.Fprintf(&b, "  params=%v sharpe=%.4f max_drawdown_pct=%.2f total_return_pct=%.2f\n",
				p.Params, p.Metrics.SharpeRatio, p.Metrics.MaxDrawdownPct, p.Metrics.TotalReturnPct)
		} else {
			fmt.Fprintf(&b, "  params=%v error=%s\n", p.Params, p.Error)
		}
	}

	summary := b.String()
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
	if toolName != "save_strategy_script" && toolName != "create_strategy" {
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
