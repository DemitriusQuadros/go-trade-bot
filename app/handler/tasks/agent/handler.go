// Package agent is cmd/agent's asynq task handling for agent runs
// (agents-platform A-02 §2-3): the agent:run processor and the DB-backed
// cron schedule provider.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"go-trade-bot/app/entities"
	agentusecase "go-trade-bot/app/usecase/agent"
	agentworker "go-trade-bot/app/workers/agent"

	"github.com/hibiken/asynq"
)

// TaskAgentRun re-exports the task type.
const TaskAgentRun = agentworker.TaskAgentRun

// RunPayload re-exports the task payload.
type RunPayload = agentworker.RunPayload

// UseCase is the narrow slice of app/usecase/agent this processor needs.
type UseCase interface {
	Run(ctx context.Context, req agentusecase.RunRequest) (entities.AgentRun, error)
}

// AgentRepository is the narrow slice of app/repository/agentplatform
// this processor needs.
type AgentRepository interface {
	GetAgent(ctx context.Context, id uint) (entities.Agent, error)
	ListBindingsByAgent(ctx context.Context, agentID uint) ([]entities.AgentStrategyBinding, error)
}

// MetricsRecorder is satisfied by *metrics.MetricsCollector.
type MetricsRecorder interface {
	IncrementCounter(name string, labels map[string]string)
	AddCounter(name string, labels map[string]string, value float64)
	ObserveHistogram(name string, labels map[string]string, value float64)
}

// Metric names (registered by cmd/agent/modules/metrics.go).
const (
	MetricRunsTotal     = "agent_runs_total"
	MetricRunDuration   = "agent_run_duration_seconds"
	MetricCostUSDTotal  = "agent_cost_usd_total"
	MetricTokensTotal   = "agent_tokens_total"
	MetricBlockedOrders = "agent_blocked_order_attempts_total"
)

// Processor handles agent:run tasks.
type Processor struct {
	useCase UseCase
	agents  AgentRepository
	metrics MetricsRecorder // may be nil
	chains  *ChainDispatcher
}

// SetChainDispatcher enables declarative ChainFrom triggers (C-01 §4):
// after an ok run, the dispatcher starts every agent chained from it. nil =
// no chaining.
func (p *Processor) SetChainDispatcher(c *ChainDispatcher) { p.chains = c }

// NewProcessor builds a Processor.
func NewProcessor(uc UseCase, agents AgentRepository, m MetricsRecorder) *Processor {
	return &Processor{useCase: uc, agents: agents, metrics: m}
}

// ProcessTask runs one agent. Flow (A-02 §2):
//  1. load the agent (missing -> log, return nil: never retried);
//  2. the RunGuard (kill switch / pause / budget) is enforced by
//     AgentUseCase.Run itself BEFORE its first model call - a blocked run is
//     still persisted as an AgentRun with status error "halted: ...";
//  3. build the input (evaluation prompt + optional operator request);
//  4. Run with StrategyID = the single binding when there is exactly one;
//  5. record metrics.
//
// Errors recorded on an AgentRun return nil (the run is the record; the
// task has MaxRetry(0) anyway). Only a failure that left no AgentRun row is
// returned to asynq, so it shows up as archived in asynqmon.
func (p *Processor) ProcessTask(ctx context.Context, t *asynq.Task) error {
	var payload RunPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		log.Printf("agent:run: invalid payload: %v", err)
		return nil
	}

	agent, err := p.agents.GetAgent(ctx, payload.AgentID)
	if err != nil {
		log.Printf("agent:run: agent %d not found, dropping task: %v", payload.AgentID, err)
		return nil
	}

	var strategyID *uint
	var boundIDs []uint
	if bindings, err := p.agents.ListBindingsByAgent(ctx, agent.ID); err == nil {
		for _, b := range bindings {
			boundIDs = append(boundIDs, b.StrategyID)
		}
		if len(bindings) == 1 {
			id := bindings[0].StrategyID
			strategyID = &id
		}
	}
	if payload.StrategyID != nil && *payload.StrategyID != 0 {
		id := *payload.StrategyID
		strategyID = &id
	}

	detail := payload.Detail
	if len(detail) == 0 && payload.Trigger == "manual" {
		detail, _ = json.Marshal(map[string]string{"requested_by": "operator", "prompt": payload.Prompt})
	}

	trigger := payload.Trigger
	if trigger == "" {
		trigger = "manual"
	}

	start := time.Now()
	run, runErr := p.useCase.Run(ctx, agentusecase.RunRequest{
		Agent:         agent,
		Trigger:       trigger,
		TriggerDetail: detail,
		UserInput:     p.buildInput(ctx, trigger, payload, detail),
		StrategyID:    strategyID,
		ChainDepth:    payload.ChainDepth,
		ChainPath:     payload.ChainPath,
		ParentRunID:   payload.ParentRunID,
	})
	p.record(agent, trigger, run, runErr, time.Since(start))

	if runErr != nil {
		if run.ID == 0 {
			return fmt.Errorf("agent:run: agent %d: %w", agent.ID, runErr)
		}
		log.Printf("agent:run: agent %d run %d finished with error: %v", agent.ID, run.ID, runErr)
		return nil
	}
	if p.chains != nil && run.ID != 0 && run.Status == entities.AgentRunOK {
		p.chains.AfterRun(ctx, agent, payload, run)
	}
	return nil
}

// buildInput phrases the run's instruction from its trigger (C-01 §5).
func (p *Processor) buildInput(ctx context.Context, trigger string, payload RunPayload, detail json.RawMessage) string {
	in := agentusecase.TriggerInput{Trigger: trigger, Detail: detail, Prompt: payload.Prompt}
	switch trigger {
	case entities.TriggerKindChain:
		var d struct {
			SourceAgentID uint `json:"source_agent_id"`
		}
		if json.Unmarshal(detail, &d) == nil && d.SourceAgentID != 0 {
			if src, err := p.agents.GetAgent(ctx, d.SourceAgentID); err == nil {
				in.SourceAgent = src.Name
			}
		}
	case entities.TriggerKindMarket:
		var d struct {
			Monitored bool `json:"monitored_by_bound_strategy"`
		}
		_ = json.Unmarshal(detail, &d)
		in.BoundStrategy = d.Monitored
	}
	return agentusecase.BuildTriggerInput(in)
}

func (p *Processor) record(agent entities.Agent, trigger string, run entities.AgentRun, runErr error, d time.Duration) {
	if p.metrics == nil {
		return
	}
	status := string(run.Status)
	if runErr != nil && status == "" {
		status = string(entities.AgentRunError)
	}
	name := agent.Name
	p.metrics.IncrementCounter(MetricRunsTotal, map[string]string{"agent": name, "trigger": trigger, "status": status})
	p.metrics.ObserveHistogram(MetricRunDuration, map[string]string{"agent": name}, d.Seconds())
	p.metrics.AddCounter(MetricCostUSDTotal, map[string]string{"agent": name}, run.CostUSD)
	p.metrics.AddCounter(MetricTokensTotal, map[string]string{"agent": name, "direction": "input"}, float64(run.InputTokens))
	p.metrics.AddCounter(MetricTokensTotal, map[string]string{"agent": name, "direction": "output"}, float64(run.OutputTokens))
}
