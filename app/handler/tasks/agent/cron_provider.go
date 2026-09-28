package agent

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"go-trade-bot/app/entities"
	agentworker "go-trade-bot/app/workers/agent"
	"go-trade-bot/internal/cronspec"

	"github.com/hibiken/asynq"
)

// CronSource is the narrow slice of app/repository/agentplatform the cron
// provider needs.
type CronSource interface {
	ListAgents(ctx context.Context) ([]entities.Agent, error)
	ListBindingsByAgent(ctx context.Context, agentID uint) ([]entities.AgentStrategyBinding, error)
}

// SettingsReader reads the global kill switch.
type SettingsReader interface {
	Get(ctx context.Context) (*entities.Settings, error)
}

// CronProvider implements asynq.PeriodicTaskConfigProvider from the DB: one
// config per (agent, cron spec) for every agent that is not Paused and has
// at least one binding, and nothing at all while Settings.AgentsPaused is
// set. The PeriodicTaskManager re-reads it every SyncInterval, so edits in
// the UI take effect without restarting cmd/agent.
//
// Fail-closed: if the settings or agents cannot be read, it returns NO
// configs (unscheduling everything until the next successful sync) rather
// than keeping stale schedules alive.
type CronProvider struct {
	agents   CronSource
	settings SettingsReader
	timeout  time.Duration
	sweep    bool
}

// WithEventSweep also schedules the agent:sweep_events task (every 5 min,
// C-01 §2.2) whenever at least one non-paused agent has a drawdown or
// no_signal trigger.
func (p *CronProvider) WithEventSweep() *CronProvider {
	p.sweep = true
	return p
}

// NewCronProvider builds a CronProvider.
func NewCronProvider(agents CronSource, settings SettingsReader) *CronProvider {
	return &CronProvider{agents: agents, settings: settings, timeout: 10 * time.Second}
}

var _ asynq.PeriodicTaskConfigProvider = (*CronProvider)(nil)

// GetConfigs implements asynq.PeriodicTaskConfigProvider.
func (p *CronProvider) GetConfigs() ([]*asynq.PeriodicTaskConfig, error) {
	ctx, cancel := context.WithTimeout(context.Background(), p.timeout)
	defer cancel()

	configs := []*asynq.PeriodicTaskConfig{}
	s, err := p.settings.Get(ctx)
	if err != nil {
		log.Printf("agent cron: cannot read settings, scheduling nothing: %v", err)
		return configs, nil
	}
	if s != nil && s.AgentsPaused {
		return configs, nil
	}
	agents, err := p.agents.ListAgents(ctx)
	if err != nil {
		log.Printf("agent cron: cannot list agents, scheduling nothing: %v", err)
		return configs, nil
	}

	needSweep := false
	for _, a := range agents {
		if a.Paused {
			continue
		}
		parsed := a.ParsedTriggers()
		for _, e := range parsed.Events {
			if entities.IsSweptEventType(e.Type) {
				needSweep = true
			}
		}
		specs := parsed.Cron
		if len(specs) == 0 {
			continue
		}
		bindings, err := p.agents.ListBindingsByAgent(ctx, a.ID)
		if err != nil {
			log.Printf("agent cron: cannot list bindings for agent %d, skipping: %v", a.ID, err)
			continue
		}
		if len(bindings) == 0 {
			continue
		}
		for _, spec := range specs {
			if err := cronspec.Validate(spec); err != nil {
				log.Printf("agent cron: agent %d: skipping %v", a.ID, err)
				continue
			}
			detail, _ := json.Marshal(map[string]string{"cron": spec})
			payload := agentworker.RunPayload{AgentID: a.ID, Trigger: "cron", Detail: detail}
			task, err := agentworker.NewTask(payload)
			if err != nil {
				continue
			}
			configs = append(configs, &asynq.PeriodicTaskConfig{
				Cronspec: spec,
				Task:     task,
				Opts:     agentworker.Options(payload),
			})
		}
	}
	if p.sweep && needSweep {
		task, opts := NewSweepTask()
		configs = append(configs, &asynq.PeriodicTaskConfig{Cronspec: SweepCronspec, Task: task, Opts: opts})
	}
	return configs, nil
}
