package agent

import (
	"context"
	"log"

	"go-trade-bot/app/entities"
	agentworker "go-trade-bot/app/workers/agent"
)

// ChainLauncher starts a chained run behind the shared guards
// (*app/workers/agent.ChainLauncher).
type ChainLauncher interface {
	LaunchChain(ctx context.Context, req agentworker.ChainRequest) (string, error)
}

// ChainDispatcher implements declarative ChainFrom triggers (C-01 §4): after
// a source run finishes ok, every non-paused agent whose ChainFrom contains
// {agent_id: source, on} with a matching `on` gets a chained run.
type ChainDispatcher struct {
	agents   TriggerAgentSource
	settings SettingsReader
	launcher ChainLauncher
}

// NewChainDispatcher builds a ChainDispatcher.
func NewChainDispatcher(agents TriggerAgentSource, settings SettingsReader, launcher ChainLauncher) *ChainDispatcher {
	return &ChainDispatcher{agents: agents, settings: settings, launcher: launcher}
}

// ChainOnMatches reports whether a ChainFrom condition matches the run.
func ChainOnMatches(on string, run entities.AgentRun) bool {
	switch on {
	case entities.ChainOnReport, "":
		return len(run.ReportIDs) > 0
	case entities.ChainOnNotify:
		return run.NotificationsSent > 0
	case entities.ChainOnSuccess:
		return true
	}
	return false
}

// AfterRun launches the chained runs. It never fails the source run: every
// error or guard refusal is logged (and counted by the launcher).
func (d *ChainDispatcher) AfterRun(ctx context.Context, source entities.Agent, payload RunPayload, run entities.AgentRun) {
	if s, err := d.settings.Get(ctx); err != nil || (s != nil && s.AgentsPaused) {
		return
	}
	agents, err := d.agents.ListAgents(ctx)
	if err != nil {
		log.Printf("agent chain: cannot list agents after run %d: %v", run.ID, err)
		return
	}
	for _, target := range agents {
		if target.Paused || target.ID == source.ID {
			continue
		}
		for _, c := range target.ParsedTriggers().ChainFrom {
			if c.AgentID != source.ID || !ChainOnMatches(c.On, run) {
				continue
			}
			on := c.On
			if on == "" {
				on = entities.ChainOnReport
			}
			_, err := d.launcher.LaunchChain(ctx, agentworker.ChainRequest{
				TargetAgentID: target.ID,
				SourceAgentID: source.ID,
				SourceRunID:   run.ID,
				SourceDepth:   payload.ChainDepth,
				SourcePath:    payload.ChainPath,
				On:            on,
				ReportIDs:     run.ReportIDs,
			})
			if err != nil && !agentworker.IsChainSuppressed(err) {
				log.Printf("agent chain: run %d -> agent %d: %v", run.ID, target.ID, err)
			}
			break // one chained run per target per source run
		}
	}
}
