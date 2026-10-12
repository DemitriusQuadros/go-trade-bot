package mcptools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"go-trade-bot/app/entities"
	agentplatform "go-trade-bot/app/usecase/agentplatform"
	scriptuc "go-trade-bot/app/usecase/script"
)

var errBadStatus = errors.New("list_signals: status must be open, closed or all")

// --- scripts -------------------------------------------------------------------

const (
	defaultTraceTail = 20
	maxTraceTail     = 100
)

func scriptTools(s ScriptRunner) []Tool {
	return []Tool{
		{
			Name:        "script_repl",
			Description: "Evaluate a Lua strategy snippet against the latest candles of a symbol (no orders, nothing persisted). Returns the result and the last trace_tail per-candle trace records (default 20, max 100). A script error is reported in `error`, not as a tool failure.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"source":{"type":"string"},"symbol":{"type":"string"},"timeframe":{"type":"string"},"window_candles":{"type":"integer"},"trace_tail":{"type":"integer"}},"required":["source","symbol","timeframe"]}`),
			Write:       true,
			Run: func(ctx context.Context, raw json.RawMessage) (any, error) {
				var in struct {
					scriptuc.EvalRequest
					TraceTail int `json:"trace_tail"`
				}
				if err := decode("script_repl", raw, &in); err != nil {
					return nil, err
				}
				if strings.TrimSpace(in.Symbol) == "" {
					return nil, fmt.Errorf("script_repl: symbol is required")
				}
				resp, err := s.Eval(ctx, in.EvalRequest)
				if err != nil {
					return nil, err
				}
				resp.Trace = tail(resp.Trace, clamp(in.TraceTail, defaultTraceTail, maxTraceTail))
				return resp, nil
			},
		},
		{
			Name:        "script_fast_rerun",
			Description: "Replay a Lua script (source) over a bounded candle window and return the last trace_tail per-candle trace records (default 20, max 100). strategy_id supplies the strategy's stored configuration. No orders are placed and nothing is persisted.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"strategy_id":{"type":"integer"},"source":{"type":"string"},"symbol":{"type":"string"},"timeframe":{"type":"string"},"end_time":{"type":"string","description":"RFC3339"},"window_candles":{"type":"integer"},"trace_tail":{"type":"integer"}},"required":["strategy_id","source","symbol","timeframe"]}`),
			Write:       true,
			Run: func(ctx context.Context, raw json.RawMessage) (any, error) {
				var in struct {
					scriptuc.FastRerunRequest
					TraceTail int `json:"trace_tail"`
				}
				if err := decode("script_fast_rerun", raw, &in); err != nil {
					return nil, err
				}
				if strings.TrimSpace(in.Symbol) == "" {
					return nil, fmt.Errorf("script_fast_rerun: symbol is required")
				}
				resp, err := s.FastRerun(ctx, in.FastRerunRequest)
				if err != nil {
					return nil, err
				}
				resp.Trace = tail(resp.Trace, clamp(in.TraceTail, defaultTraceTail, maxTraceTail))
				return resp, nil
			},
		},
	}
}

func tail[T any](s []T, n int) []T {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

// --- agents --------------------------------------------------------------------

type agentView struct {
	ID                   uint     `json:"id"`
	Name                 string   `json:"name"`
	Goal                 string   `json:"goal,omitempty"`
	Provider             string   `json:"provider,omitempty"`
	Model                string   `json:"model,omitempty"`
	Permissions          []string `json:"permissions"`
	StrategyIDs          []uint   `json:"strategy_ids"`
	Paused               bool     `json:"paused"`
	IsDefault            bool     `json:"is_default"`
	DailyBudgetUSD       float64  `json:"daily_budget_usd"`
	MaxAutoDeploysPerDay int      `json:"max_auto_deploys_per_day"`
	MaxIterations        int      `json:"max_iterations"`
	TodayCostUSD         float64  `json:"today_cost_usd"`
	Triggers             any      `json:"triggers"`
	WebhookTargetIDs     []uint   `json:"webhook_target_ids,omitempty"`
	LastRun              any      `json:"last_run,omitempty"`
	NextRunAt            string   `json:"next_run_at,omitempty"`
}

func toAgentView(v agentplatform.AgentView, goalChars int) agentView {
	a := v.Agent
	out := agentView{
		ID: a.ID, Name: a.Name, Goal: truncate(a.Goal, goalChars), Provider: a.Provider, Model: a.Model,
		Permissions: append([]string{}, a.Permissions...), StrategyIDs: append([]uint{}, v.StrategyIDs...),
		Paused: a.Paused, IsDefault: a.IsDefault, DailyBudgetUSD: a.DailyBudgetUSD,
		MaxAutoDeploysPerDay: a.MaxAutoDeploysPerDay, MaxIterations: a.MaxIterations, TodayCostUSD: v.TodayCostUSD,
		Triggers: a.ParsedTriggers().Summary(), NextRunAt: tsPtr(v.NextRunAt),
	}
	if v.LastRun != nil {
		out.LastRun = map[string]any{"id": v.LastRun.ID, "status": v.LastRun.Status, "trigger": v.LastRun.Trigger, "started_at": ts(v.LastRun.StartedAt)}
	}
	return out
}

// runView is a run WITHOUT its transcript or tool-call payloads (those can be
// large and, for chat runs, belong to the user who started them).
type runView struct {
	ID              uint    `json:"id"`
	Status          string  `json:"status"`
	Trigger         string  `json:"trigger"`
	StartedAt       string  `json:"started_at"`
	FinishedAt      string  `json:"finished_at,omitempty"`
	StrategyID      *uint   `json:"strategy_id,omitempty"`
	InputTokens     int64   `json:"input_tokens"`
	OutputTokens    int64   `json:"output_tokens"`
	CostUSD         float64 `json:"cost_usd"`
	HitIterationCap bool    `json:"hit_iteration_cap,omitempty"`
	ChainDepth      int     `json:"chain_depth,omitempty"`
	ParentRunID     *uint   `json:"parent_run_id,omitempty"`
	Error           string  `json:"error,omitempty"`
}

func toRunView(r entities.AgentRun) runView {
	return runView{
		ID: r.ID, Status: string(r.Status), Trigger: r.Trigger, StartedAt: ts(r.StartedAt), FinishedAt: ts(r.FinishedAt),
		StrategyID: r.StrategyID, InputTokens: r.InputTokens, OutputTokens: r.OutputTokens, CostUSD: r.CostUSD,
		HitIterationCap: r.HitIterationCap, ChainDepth: r.ChainDepth, ParentRunID: r.ParentRunID, Error: truncate(r.ErrorMessage, 300),
	}
}

type usageDayView struct {
	Day          string  `json:"day"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	CostUSD      float64 `json:"cost_usd"`
	Runs         int     `json:"runs"`
}

func toUsageDay(d agentplatform.UsageDay) usageDayView {
	return usageDayView{Day: d.Day.UTC().Format("2006-01-02"), InputTokens: d.InputTokens, OutputTokens: d.OutputTokens, CostUSD: d.CostUSD, Runs: d.Runs}
}

var agentIDSchema = json.RawMessage(`{"type":"object","properties":{"agent_id":{"type":"integer"}},"required":["agent_id"]}`)

func agentTools(m AgentManager) []Tool {
	return append(agentWriteTools(m), []Tool{
		{
			Name:        "list_agents",
			Description: "All agent personas: permissions, bound strategies, paused flag, daily budget and today's cost, trigger counts, last run and next scheduled run. The goal prompt is shortened; use get_agent for it.",
			InputSchema: json.RawMessage(emptyObject),
			Run: func(ctx context.Context, _ json.RawMessage) (any, error) {
				all, err := m.ListAgents(ctx)
				if err != nil {
					return nil, err
				}
				out := make([]agentView, 0, len(all))
				for _, v := range all {
					out = append(out, toAgentView(v, 0))
				}
				return map[string]any{"agents": out}, nil
			},
		},
		{
			Name:        "get_agent",
			Description: "One agent persona by id, including its goal prompt.",
			InputSchema: agentIDSchema,
			Run: func(ctx context.Context, raw json.RawMessage) (any, error) {
				id, err := agentID("get_agent", raw)
				if err != nil {
					return nil, err
				}
				v, err := m.GetAgent(ctx, id)
				if err != nil {
					return nil, err
				}
				return detailedAgentView(v), nil
			},
		},
		{
			Name:        "list_agent_run_history",
			Description: "Recent runs of one agent (status, trigger, tokens, cost, errors) WITHOUT transcripts or tool-call payloads. limit defaults to 20 (max 100).",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"agent_id":{"type":"integer"},"limit":{"type":"integer"}},"required":["agent_id"]}`),
			Run: func(ctx context.Context, raw json.RawMessage) (any, error) {
				var in struct {
					AgentID uint `json:"agent_id"`
					Limit   int  `json:"limit"`
				}
				if err := decode("list_agent_run_history", raw, &in); err != nil {
					return nil, err
				}
				if err := requireID("list_agent_run_history", "agent_id", in.AgentID); err != nil {
					return nil, err
				}
				runs, _, err := m.ListRuns(ctx, in.AgentID, clamp(in.Limit, 20, 100), nil)
				if err != nil {
					return nil, err
				}
				out := make([]runView, 0, len(runs))
				for _, r := range runs {
					out = append(out, toRunView(r))
				}
				return map[string]any{"runs": out}, nil
			},
		},
		{
			Name:        "get_agent_usage",
			Description: "Daily token and cost usage of one agent: today plus the last `days` UTC days (default 30).",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"agent_id":{"type":"integer"},"days":{"type":"integer"}},"required":["agent_id"]}`),
			Run: func(ctx context.Context, raw json.RawMessage) (any, error) {
				var in struct {
					AgentID uint `json:"agent_id"`
					Days    int  `json:"days"`
				}
				if err := decode("get_agent_usage", raw, &in); err != nil {
					return nil, err
				}
				if err := requireID("get_agent_usage", "agent_id", in.AgentID); err != nil {
					return nil, err
				}
				today, days, err := m.Usage(ctx, in.AgentID, clamp(in.Days, 30, 90))
				if err != nil {
					return nil, err
				}
				out := make([]usageDayView, 0, len(days))
				for _, d := range days {
					out = append(out, toUsageDay(d))
				}
				return map[string]any{"today": toUsageDay(today), "days": out}, nil
			},
		},
		{
			Name:        "get_kill_switch",
			Description: "Whether the global agents kill switch is on (read only; it can only be changed over the authenticated REST API).",
			InputSchema: json.RawMessage(emptyObject),
			Run: func(ctx context.Context, _ json.RawMessage) (any, error) {
				on, err := m.KillSwitch(ctx)
				if err != nil {
					return nil, err
				}
				return map[string]any{"agents_paused": on}, nil
			},
		},
		{
			Name:        "pause_agent",
			Description: "Pause (paused=true) or resume (paused=false) one agent persona. Pausing is the safe direction; it stops the agent's cron/event/market triggers.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"agent_id":{"type":"integer"},"paused":{"type":"boolean"}},"required":["agent_id","paused"]}`),
			Write:       true,
			Run: func(ctx context.Context, raw json.RawMessage) (any, error) {
				var in struct {
					AgentID uint  `json:"agent_id"`
					Paused  *bool `json:"paused"`
				}
				if err := decode("pause_agent", raw, &in); err != nil {
					return nil, err
				}
				if err := requireID("pause_agent", "agent_id", in.AgentID); err != nil {
					return nil, err
				}
				if in.Paused == nil {
					return nil, fmt.Errorf("pause_agent: paused (boolean) is required")
				}
				v, err := m.SetPaused(ctx, in.AgentID, *in.Paused)
				if err != nil {
					return nil, err
				}
				return toAgentView(v, 0), nil
			},
		},
	}...)
}

func agentID(name string, raw json.RawMessage) (uint, error) {
	var in struct {
		AgentID uint `json:"agent_id"`
	}
	if err := decode(name, raw, &in); err != nil {
		return 0, err
	}
	return in.AgentID, requireID(name, "agent_id", in.AgentID)
}
