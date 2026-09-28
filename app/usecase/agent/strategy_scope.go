package agent

import (
	"context"
	"fmt"

	"go-trade-bot/app/entities"
)

// inWriteScope enforces the Phase B write scope (B-01 §6): an agent's
// strategy-writing tools only act on
//   - strategies bound to it,
//   - challengers whose champion is bound to it,
//   - strategies it created (Strategy.CreatedByAgentID).
//
// It fails closed: without the platform repository (legacy wiring) or for
// an unsaved persona (ID 0) nothing existing is in scope.
func (u AgentUseCase) inWriteScope(ctx context.Context, agent entities.Agent, s entities.Strategy) error {
	outOfScope := fmt.Errorf("strategy %d is outside agent %q's scope - agents may only change strategies bound to them, challengers of bound strategies, or strategies they created", s.ID, agent.Name)
	if u.Platform == nil || agent.ID == 0 || s.ID == 0 {
		return outOfScope
	}
	if s.CreatedByAgentID != nil && *s.CreatedByAgentID == agent.ID {
		return nil
	}
	bindings, err := u.Platform.ListBindingsByAgent(ctx, agent.ID)
	if err != nil {
		return fmt.Errorf("could not check agent %q's bindings: %w", agent.Name, err)
	}
	bound := make(map[uint]bool, len(bindings))
	for _, b := range bindings {
		bound[b.StrategyID] = true
	}
	if StrategyInScope(agent.ID, bound, s) {
		return nil
	}
	return outOfScope
}

func (u AgentUseCase) isBound(ctx context.Context, agentID, strategyID uint) (bool, error) {
	bindings, err := u.Platform.ListBindingsByAgent(ctx, agentID)
	if err != nil {
		return false, err
	}
	for _, b := range bindings {
		if b.StrategyID == strategyID {
			return true, nil
		}
	}
	return false, nil
}
