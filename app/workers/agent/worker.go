// Package agent enqueues agent:run tasks onto the dedicated "agents" asynq
// queue, which only cmd/agent consumes (agents-platform A-02 §2).
package agent

import (
	"context"
	"encoding/json"
	"fmt"
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

// RunPayload is the agent:run task payload. Trigger is "cron" | "manual" |
// "event" | "market" | "chain" (agents-platform C-01).
type RunPayload struct {
	AgentID    uint            `json:"agent_id"`
	Trigger    string          `json:"trigger"`          // "cron" | "manual" | "event" | "market" | "chain"
	Detail     json.RawMessage `json:"detail,omitempty"` // trigger detail (C-01 §2.3 / §3 / §4 shapes)
	Prompt     string          `json:"prompt,omitempty"` // manual runs: operator instruction; trigger_agent: the caller's message
	ChainDepth int             `json:"chain_depth"`
	// StrategyID is the run's context strategy when the trigger knows it
	// (event runs; market runs when exactly one bound strategy trades the
	// symbol). nil = the processor's default (the single binding, if any).
	StrategyID *uint `json:"strategy_id,omitempty"`
	// ChainPath is every agent id already in this chain, source-first
	// (chain runs only). ParentRunID is the run that triggered it.
	ChainPath   []uint `json:"chain_path,omitempty"`
	ParentRunID *uint  `json:"parent_run_id,omitempty"`
}

// ChainTaskRetention keeps a finished chain task's deterministic id
// reserved, so a redelivered parent cannot enqueue the same (target,
// source run) chain run twice.
const ChainTaskRetention = 24 * time.Hour

// ChainTaskID is the deterministic asynq task id of the chain run of
// targetAgentID triggered by sourceRunID.
func ChainTaskID(targetAgentID, sourceRunID uint) string {
	return fmt.Sprintf("agent-chain:%d:%d", targetAgentID, sourceRunID)
}

// Options returns the asynq options for a payload: agents queue, no
// retries (LLM runs are not idempotent), 10 minute timeout, uniqueness for
// cron runs, and a deterministic task id for chain runs (keyed by target
// agent and source run).
func Options(p RunPayload) []asynq.Option {
	opts := []asynq.Option{asynq.Queue(Queue), asynq.MaxRetry(0), asynq.Timeout(RunTimeout)}
	switch p.Trigger {
	case "cron":
		opts = append(opts, asynq.Unique(CronUniqueTTL))
	case "chain":
		if p.ParentRunID != nil {
			opts = append(opts, asynq.TaskID(ChainTaskID(p.AgentID, *p.ParentRunID)), asynq.Retention(ChainTaskRetention))
		}
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

// TaskEnqueuer is satisfied by *asynq.Client and AgentWorker.
type TaskEnqueuer interface {
	EnqueueContext(ctx context.Context, task *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error)
}

// EnqueueRunWith enqueues p on enq with Options(p).
func EnqueueRunWith(ctx context.Context, enq TaskEnqueuer, p RunPayload) (string, error) {
	task, err := NewTask(p)
	if err != nil {
		return "", err
	}
	info, err := enq.EnqueueContext(ctx, task, Options(p)...)
	if err != nil {
		return "", err
	}
	if info == nil {
		return "", nil
	}
	return info.ID, nil
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
	return EnqueueRunWith(ctx, w.client, p)
}

// EnqueueContext enqueues any task on the underlying client (TaskEnqueuer).
func (w AgentWorker) EnqueueContext(ctx context.Context, task *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error) {
	return w.client.EnqueueContext(ctx, task, opts...)
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
