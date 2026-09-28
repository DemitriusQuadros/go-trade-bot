// Package agent enqueues agent:run tasks onto the dedicated "agents" asynq
// queue, which only cmd/agent consumes (agents-platform A-02 §2).
package agent

import (
	"context"
	"encoding/json"
	"time"

	"go-trade-bot/internal/configuration"

	"github.com/hibiken/asynq"
)

// TaskAgentRun is the asynq task type for one agent run.
const TaskAgentRun = "agent:run"

// Queue is the asynq queue agent runs are enqueued on; only cmd/agent
// serves it (never cmd/worker).
const Queue = "agents"

// Task options (A-02 §2).
const (
	RunTimeout = 10 * time.Minute
	// CronUniqueTTL prevents cron pile-ups when a run overruns its interval.
	CronUniqueTTL = 2 * time.Minute
)

// RunPayload is the agent:run task payload. Trigger is "cron" | "manual"
// today; the shape leaves room for event/chain triggers (Phase C).
type RunPayload struct {
	AgentID    uint            `json:"agent_id"`
	Trigger    string          `json:"trigger"`          // "cron" | "manual"
	Detail     json.RawMessage `json:"detail,omitempty"` // cron spec or {"requested_by":"operator","prompt":...}
	Prompt     string          `json:"prompt,omitempty"` // manual runs may carry an operator instruction
	ChainDepth int             `json:"chain_depth"`
}

// Options returns the asynq options for a payload: agents queue, no
// retries (LLM runs are not idempotent), 10 minute timeout, and uniqueness
// for cron runs.
func Options(p RunPayload) []asynq.Option {
	opts := []asynq.Option{asynq.Queue(Queue), asynq.MaxRetry(0), asynq.Timeout(RunTimeout)}
	if p.Trigger == "cron" {
		opts = append(opts, asynq.Unique(CronUniqueTTL))
	}
	return opts
}

// NewTask builds the asynq task for a payload.
func NewTask(p RunPayload) (*asynq.Task, error) {
	b, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	return asynq.NewTask(TaskAgentRun, b), nil
}

// AgentWorker is the asynq client wrapper cmd/api uses for manual runs.
type AgentWorker struct {
	client *asynq.Client
}

// NewAgentWorker builds an AgentWorker against the configured Redis.
func NewAgentWorker(cfg *configuration.Configuration) AgentWorker {
	return AgentWorker{client: asynq.NewClient(asynq.RedisClientOpt{Addr: cfg.Redis.Addr})}
}

// EnqueueRun enqueues an agent run and returns the asynq task id.
func (w AgentWorker) EnqueueRun(ctx context.Context, p RunPayload) (string, error) {
	task, err := NewTask(p)
	if err != nil {
		return "", err
	}
	info, err := w.client.EnqueueContext(ctx, task, Options(p)...)
	if err != nil {
		return "", err
	}
	return info.ID, nil
}

// TaskApplyProposal applies an operator-approved strategy change proposal
// (agents-platform Phase B-01 §5). Enqueued by cmd/api's approve endpoint
// and re-enqueued by cmd/agent itself while the target is not flat.
const TaskApplyProposal = "agent:apply_proposal"

// ApplyProposalTimeout bounds one apply attempt.
const ApplyProposalTimeout = 2 * time.Minute

// ApplyProposalPayload is the agent:apply_proposal payload.
type ApplyProposalPayload struct {
	ProposalID uint `json:"proposal_id"`
}

// ApplyProposalOptions: agents queue, a few retries for infrastructure
// errors (an apply is idempotent: it re-checks status and base source),
// and an optional delay. Each flat re-check is a NEW task, never a retry.
func ApplyProposalOptions(delay time.Duration) []asynq.Option {
	opts := []asynq.Option{asynq.Queue(Queue), asynq.MaxRetry(3), asynq.Timeout(ApplyProposalTimeout)}
	if delay > 0 {
		opts = append(opts, asynq.ProcessIn(delay))
	}
	return opts
}

// NewApplyProposalTask builds the task.
func NewApplyProposalTask(proposalID uint) (*asynq.Task, error) {
	b, err := json.Marshal(ApplyProposalPayload{ProposalID: proposalID})
	if err != nil {
		return nil, err
	}
	return asynq.NewTask(TaskApplyProposal, b), nil
}

// EnqueueApplyProposal enqueues agent:apply_proposal for proposalID after
// delay (0 = now).
func (w AgentWorker) EnqueueApplyProposal(ctx context.Context, proposalID uint, delay time.Duration) error {
	task, err := NewApplyProposalTask(proposalID)
	if err != nil {
		return err
	}
	_, err = w.client.EnqueueContext(ctx, task, ApplyProposalOptions(delay)...)
	return err
}
