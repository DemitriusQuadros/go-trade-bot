package mcptools

import (
	"context"
	"encoding/json"
	"fmt"

	"go-trade-bot/app/entities"
	agentplatform "go-trade-bot/app/usecase/agentplatform"
)

// detailedAgentView is a full agent view: the goal prompt (long) and the
// complete trigger definition instead of the counts.
func detailedAgentView(v agentplatform.AgentView) agentView {
	out := toAgentView(v, 20000)
	out.Triggers = v.Agent.ParsedTriggers()
	out.WebhookTargetIDs = append([]uint{}, v.Agent.WebhookTargetIDs...)
	return out
}

const agentFieldsSchema = `
		"name": {"type":"string"},
		"goal": {"type":"string","description":"the persona's system prompt"},
		"provider": {"type":"string","description":"optional: anthropic | gemini (empty = process default)"},
		"model": {"type":"string","description":"optional model override (empty = provider default)"},
		"permissions": {"type":"array","items":{"type":"string","enum":["read","backtest","optimize","edit_testing","create_strategy","propose_live","notify","chain"]},"description":"what the agent may do. None of them can change a live or productive strategy: live changes only land through a proposal an operator approves over REST."},
		"triggers": {"type":"object","description":"{cron:[\"0 * * * *\"], events:[{type,threshold_pct?,window_hours?,cooldown_minutes?}], market:[{id,symbol,kind:pct_move|volatility_spike,window_minutes,threshold_pct?,multiplier?,cooldown_minutes?}], chain_from:[{agent_id,on:report|notify|success}]}. Validated server-side (cron syntax, chain cycles)."},
		"strategy_ids": {"type":"array","items":{"type":"integer"},"description":"strategies the agent is bound to (its write scope)"},
		"webhook_target_ids": {"type":"array","items":{"type":"integer"},"description":"existing notification targets, see list_webhook_targets"},
		"daily_budget_usd": {"type":"number","description":"0 = no limit"},
		"max_auto_deploys_per_day": {"type":"integer","description":"0 = auto-deploy disabled"},
		"max_iterations": {"type":"integer","description":"tool-loop cap, 0 = trigger default, else 4..50"}`

// agentFields are the editable fields. Pointers/nil mean "not provided", so
// update_agent can patch instead of requiring the whole definition.
type agentFields struct {
	Name                 *string                 `json:"name"`
	Goal                 *string                 `json:"goal"`
	Provider             *string                 `json:"provider"`
	Model                *string                 `json:"model"`
	Permissions          *[]string               `json:"permissions"`
	Triggers             *entities.AgentTriggers `json:"triggers"`
	StrategyIDs          *[]uint                 `json:"strategy_ids"`
	WebhookTargetIDs     *[]uint                 `json:"webhook_target_ids"`
	DailyBudgetUSD       *float64                `json:"daily_budget_usd"`
	MaxAutoDeploysPerDay *int                    `json:"max_auto_deploys_per_day"`
	MaxIterations        *int                    `json:"max_iterations"`
}

// apply overlays the provided fields onto in.
func (f agentFields) apply(in *agentplatform.AgentInput) {
	if f.Name != nil {
		in.Name = *f.Name
	}
	if f.Goal != nil {
		in.Goal = *f.Goal
	}
	if f.Provider != nil {
		in.Provider = *f.Provider
	}
	if f.Model != nil {
		in.Model = *f.Model
	}
	if f.Permissions != nil {
		in.Permissions = *f.Permissions
	}
	if f.Triggers != nil {
		in.Triggers = *f.Triggers
	}
	if f.StrategyIDs != nil {
		in.StrategyIDs = *f.StrategyIDs
	}
	if f.WebhookTargetIDs != nil {
		in.WebhookTargetIDs = *f.WebhookTargetIDs
	}
	if f.DailyBudgetUSD != nil {
		in.DailyBudgetUSD = *f.DailyBudgetUSD
	}
	if f.MaxAutoDeploysPerDay != nil {
		in.MaxAutoDeploysPerDay = *f.MaxAutoDeploysPerDay
	}
	if f.MaxIterations != nil {
		in.MaxIterations = *f.MaxIterations
	}
}

// inputFromView is the current definition as an AgentInput (the base a patch
// is applied to).
func inputFromView(v agentplatform.AgentView) agentplatform.AgentInput {
	a := v.Agent
	return agentplatform.AgentInput{
		Name: a.Name, Goal: a.Goal, Provider: a.Provider, Model: a.Model,
		Permissions: append([]string{}, a.Permissions...), Triggers: a.ParsedTriggers(),
		StrategyIDs: append([]uint{}, v.StrategyIDs...), WebhookTargetIDs: append([]uint{}, a.WebhookTargetIDs...),
		DailyBudgetUSD: a.DailyBudgetUSD, MaxAutoDeploysPerDay: a.MaxAutoDeploysPerDay, MaxIterations: a.MaxIterations,
	}
}

func agentWriteTools(m AgentManager) []Tool {
	return []Tool{
		{
			Name:        "create_agent",
			Description: "Create an agent persona (name, goal prompt, permissions, triggers, bound strategies, webhook targets, budget). Validation errors (cron syntax, chain cycles, unknown strategy ids, ...) come back as the tool error. Starts unpaused.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{` + agentFieldsSchema + `},"required":["name","goal"]}`),
			Write:       true,
			Run: func(ctx context.Context, raw json.RawMessage) (any, error) {
				var f agentFields
				if err := decode("create_agent", raw, &f); err != nil {
					return nil, err
				}
				var in agentplatform.AgentInput
				f.apply(&in)
				v, err := m.CreateAgent(ctx, in)
				if err != nil {
					return nil, err
				}
				return detailedAgentView(v), nil
			},
		},
		{
			Name:        "update_agent",
			Description: "Change an agent persona. Only the fields you pass are changed; everything else keeps its current value (arrays and triggers are replaced as a whole when passed). Same validation as create_agent.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"agent_id":{"type":"integer"},` + agentFieldsSchema + `},"required":["agent_id"]}`),
			Write:       true,
			Run: func(ctx context.Context, raw json.RawMessage) (any, error) {
				var in struct {
					AgentID uint `json:"agent_id"`
					agentFields
				}
				if err := decode("update_agent", raw, &in); err != nil {
					return nil, err
				}
				if err := requireID("update_agent", "agent_id", in.AgentID); err != nil {
					return nil, err
				}
				cur, err := m.GetAgent(ctx, in.AgentID)
				if err != nil {
					return nil, err
				}
				next := inputFromView(cur)
				in.agentFields.apply(&next)
				v, err := m.UpdateAgent(ctx, in.AgentID, next)
				if err != nil {
					return nil, err
				}
				return detailedAgentView(v), nil
			},
		},
		{
			Name:        "delete_agent",
			Description: "Delete an agent persona. Its runs and reports stay; the default Copilot persona cannot be deleted (the server refuses).",
			InputSchema: agentIDSchema,
			Write:       true,
			Run: func(ctx context.Context, raw json.RawMessage) (any, error) {
				id, err := agentID("delete_agent", raw)
				if err != nil {
					return nil, err
				}
				if err := m.DeleteAgent(ctx, id); err != nil {
					return nil, err
				}
				return map[string]any{"deleted": true, "agent_id": id}, nil
			},
		},
		{
			Name:        "run_agent",
			Description: "Enqueue a manual run of one agent now (optional prompt). It spends that agent's model budget and runs unattended under all of its normal guards (kill switch, pause, budget, permissions, write scope). Returns the task id; watch list_agent_run_history.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"agent_id":{"type":"integer"},"prompt":{"type":"string","description":"optional instruction for this run"}},"required":["agent_id"]}`),
			Write:       true,
			Run: func(ctx context.Context, raw json.RawMessage) (any, error) {
				var in struct {
					AgentID uint   `json:"agent_id"`
					Prompt  string `json:"prompt"`
				}
				if err := decode("run_agent", raw, &in); err != nil {
					return nil, err
				}
				if err := requireID("run_agent", "agent_id", in.AgentID); err != nil {
					return nil, err
				}
				taskID, err := m.EnqueueManualRun(ctx, in.AgentID, in.Prompt)
				if err != nil {
					return nil, err
				}
				return map[string]any{"enqueued": true, "task_id": taskID}, nil
			},
		},
		{
			Name:        "set_kill_switch",
			Description: "Turn the global agents kill switch on (paused=true: no agent run starts and in-flight runs stop before their next model call) or off (paused=false: all non-paused agents may run again). Turning it ON is the emergency stop; turning it OFF re-enables every agent, so do it deliberately.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"paused":{"type":"boolean"}},"required":["paused"]}`),
			Write:       true,
			Run: func(ctx context.Context, raw json.RawMessage) (any, error) {
				var in struct {
					Paused *bool `json:"paused"`
				}
				if err := decode("set_kill_switch", raw, &in); err != nil {
					return nil, err
				}
				if in.Paused == nil {
					return nil, fmt.Errorf("set_kill_switch: paused (boolean) is required")
				}
				on, err := m.SetKillSwitch(ctx, *in.Paused)
				if err != nil {
					return nil, err
				}
				return map[string]any{"agents_paused": on}, nil
			},
		},
	}
}

// webhookView never carries the URL or secret.
type webhookView struct {
	ID      uint   `json:"id"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Enabled bool   `json:"enabled"`
}

func webhookTools(r WebhookTargetReader) []Tool {
	return []Tool{{
		Name:        "list_webhook_targets",
		Description: "Notification targets (id, name, kind, enabled) that agents can be wired to via webhook_target_ids. URLs and secrets are never shown, and targets can only be created or changed over the authenticated REST API.",
		InputSchema: json.RawMessage(emptyObject),
		Run: func(ctx context.Context, _ json.RawMessage) (any, error) {
			ts, err := r.ListWebhookTargets(ctx)
			if err != nil {
				return nil, err
			}
			out := make([]webhookView, 0, len(ts))
			for _, t := range ts {
				out = append(out, webhookView{ID: t.ID, Name: t.Name, Kind: string(t.Kind), Enabled: t.Enabled})
			}
			return map[string]any{"targets": out}, nil
		},
	}}
}
