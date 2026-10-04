package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"go-trade-bot/app/entities"
	agentworker "go-trade-bot/app/workers/agent"
	"go-trade-bot/internal/modelprovider"
)

// ChainLauncher starts a chained agent run behind the shared chain guards
// (depth <= 3, no agent twice in a chain, one run per (target, source run)).
// Satisfied by *app/workers/agent.ChainLauncher.
type ChainLauncher interface {
	LaunchChain(ctx context.Context, req agentworker.ChainRequest) (string, error)
}

const (
	triggerAgentToolName   = "trigger_agent"
	maxTriggerMessageChars = 2000
)

var triggerAgentSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"agent_id": {"type": "integer", "description": "the agent to start"},
		"message": {"type": "string", "description": "what you want it to look at (becomes its prompt), at most 2000 characters"}
	},
	"required": ["agent_id", "message"]
}`)

// triggerAgentTool (permission chain, C-01 §4) queues a run of another
// agent with a message. It only STARTS a run: the target runs unattended
// under its own permissions, scope, budget and every Phase A/B guard. The
// target must exist, not be paused, not be the caller and not already be in
// this run's chain; at most 3 calls per run; depth/path/dedupe guards are
// re-applied by the launcher.
func (u AgentUseCase) triggerAgentTool() Tool {
	return Tool{
		Def: modelprovider.ToolDefinition{
			Name: triggerAgentToolName,
			Description: "Start another agent's run with a message (it runs later, unattended, under its own permissions). " +
				"At most 3 per run; chains stop after 3 hops and never revisit an agent. Returns {enqueued, task_id}.",
			InputSchema: triggerAgentSchema,
		},
		Execute: func(ctx context.Context, raw json.RawMessage) (string, error) {
			if err := u.requirePlatform(triggerAgentToolName); err != nil {
				return "", err
			}
			if u.Chain == nil {
				return "", fmt.Errorf("%s: chaining agents is not available on this server", triggerAgentToolName)
			}
			scope, ok := scopeFrom(ctx)
			if !ok || scope.runID == 0 || scope.agent.ID == 0 {
				return "", fmt.Errorf("%s: only available inside a saved agent's run", triggerAgentToolName)
			}
			var in struct {
				AgentID uint   `json:"agent_id"`
				Message string `json:"message"`
			}
			if err := json.Unmarshal(raw, &in); err != nil {
				return "", fmt.Errorf("%s: invalid args: %w", triggerAgentToolName, err)
			}
			in.Message = strings.TrimSpace(in.Message)
			if in.Message == "" || len([]rune(in.Message)) > maxTriggerMessageChars {
				return "", fmt.Errorf("%s: message is required (at most %d characters)", triggerAgentToolName, maxTriggerMessageChars)
			}
			if in.AgentID == 0 {
				return "", fmt.Errorf("%s: agent_id is required", triggerAgentToolName)
			}
			if in.AgentID == scope.agent.ID {
				return "", fmt.Errorf("%s: an agent cannot trigger itself", triggerAgentToolName)
			}
			for _, id := range scope.chainPath {
				if id == in.AgentID {
					return "", fmt.Errorf("%s: agent %d is already in this chain %v", triggerAgentToolName, in.AgentID, scope.chainPath)
				}
			}
			target, err := u.Platform.GetAgent(ctx, in.AgentID)
			if err != nil || target.ID == 0 {
				return "", fmt.Errorf("%s: agent %d does not exist", triggerAgentToolName, in.AgentID)
			}
			if target.Paused {
				return "", fmt.Errorf("%s: agent %q is paused", triggerAgentToolName, target.Name)
			}
			if !scope.takeChainSlot() {
				return "", fmt.Errorf("%s: limit reached (%d per run)", triggerAgentToolName, maxChainCallsPerRun)
			}
			taskID, err := u.Chain.LaunchChain(ctx, agentworker.ChainRequest{
				TargetAgentID: target.ID,
				SourceAgentID: scope.agent.ID,
				SourceRunID:   scope.runID,
				SourceDepth:   scope.chainDepth,
				SourcePath:    scope.chainPath,
				On:            agentworker.ChainOnTool,
				Message:       in.Message,
			})
			if err != nil {
				return "", fmt.Errorf("%s: %w", triggerAgentToolName, err)
			}
			return compactJSON(map[string]any{"enqueued": true, "task_id": taskID, "agent_id": target.ID, "agent_name": target.Name})
		},
	}
}

// StrategyInScope is the Phase B write scope as a pure predicate (the
// definition inWriteScope enforces): s is bound to agentID, is a challenger
// of a bound champion, or was created by agentID. Event triggers (C-01 §2)
// fire for exactly these strategies.
func StrategyInScope(agentID uint, boundIDs map[uint]bool, s entities.Strategy) bool {
	if agentID == 0 || s.ID == 0 {
		return false
	}
	if s.CreatedByAgentID != nil && *s.CreatedByAgentID == agentID {
		return true
	}
	if boundIDs[s.ID] {
		return true
	}
	return s.ChallengerOfID != nil && boundIDs[*s.ChallengerOfID]
}
