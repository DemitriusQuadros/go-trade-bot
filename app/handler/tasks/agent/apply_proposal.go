package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	proposalusecase "go-trade-bot/app/usecase/proposal"
	agentworker "go-trade-bot/app/workers/agent"

	"github.com/hibiken/asynq"
)

// TaskApplyProposal re-exports the task type.
const TaskApplyProposal = agentworker.TaskApplyProposal

// Applier is the slice of app/usecase/proposal.Applier this processor needs.
type Applier interface {
	Apply(ctx context.Context, proposalID uint) (proposalusecase.ApplyResult, error)
}

// ApplyEnqueuer schedules the next attempt (app/workers/agent.AgentWorker).
type ApplyEnqueuer interface {
	EnqueueApplyProposal(ctx context.Context, proposalID uint, delay time.Duration) error
}

// ApplyProcessor handles agent:apply_proposal (cmd/agent only). This task
// is enqueued only by the operator's authenticated REST approve (and by
// itself while waiting for the target to be flat) - no LLM tool can
// enqueue it.
type ApplyProcessor struct {
	applier  Applier
	enqueuer ApplyEnqueuer
}

// NewApplyProcessor builds an ApplyProcessor.
func NewApplyProcessor(a Applier, e ApplyEnqueuer) *ApplyProcessor {
	return &ApplyProcessor{applier: a, enqueuer: e}
}

// ProcessTask runs one apply attempt. A "not flat yet" outcome schedules a
// NEW task (ProcessIn) instead of retrying this one, so asynq's retry
// counter only ever counts infrastructure failures.
func (p *ApplyProcessor) ProcessTask(ctx context.Context, t *asynq.Task) error {
	var payload agentworker.ApplyProposalPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil || payload.ProposalID == 0 {
		log.Printf("agent:apply_proposal: invalid payload %q: %v", string(t.Payload()), err)
		return nil
	}
	res, err := p.applier.Apply(ctx, payload.ProposalID)
	if err != nil {
		return fmt.Errorf("agent:apply_proposal %d: %w", payload.ProposalID, err)
	}
	log.Printf("agent:apply_proposal %d: %s %s", payload.ProposalID, res.Outcome, res.Reason)
	if res.Outcome == proposalusecase.OutcomeRescheduled {
		if err := p.enqueuer.EnqueueApplyProposal(ctx, payload.ProposalID, res.RetryIn); err != nil {
			return fmt.Errorf("agent:apply_proposal %d: could not schedule the next check: %w", payload.ProposalID, err)
		}
	}
	return nil
}
