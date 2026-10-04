package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"go-trade-bot/app/entities"
	"go-trade-bot/internal/modelprovider"

	"github.com/hibiken/asynq"
)

// AsynqInspector defines the subset of asynq.Inspector needed for queue and worker health monitoring.
type AsynqInspector interface {
	Servers() ([]*asynq.ServerInfo, error)
	Queues() ([]string, error)
	GetQueueInfo(queue string) (*asynq.QueueInfo, error)
	SchedulerEntries() ([]*asynq.SchedulerEntry, error)
}

// StrategyExecutionReader returns the latest execution record for each strategy ID.
type StrategyExecutionReader interface {
	GetLatestExecutions(ctx context.Context) (map[uint]entities.StrategyExecution, error)
}

var schedulerHealthSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"strategy_id": {
			"type": "integer",
			"description": "Optional strategy ID to focus on. If omitted, checks all active strategies, queues, and schedulers."
		}
	}
}`)

func (u AgentUseCase) getSchedulerHealthTool() Tool {
	return Tool{
		Def: modelprovider.ToolDefinition{
			Name:        "get_scheduler_health",
			Description: "Inspect the health and status of background schedulers and task queues: Asynq worker & agent process heartbeats, queue backlogs/latencies, per-strategy execution cycles (last run time, status, overdue alerts), and agent cron schedules.",
			InputSchema: schedulerHealthSchema,
		},
		Execute: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var in struct {
				StrategyID uint `json:"strategy_id"`
			}
			if len(raw) > 0 && string(raw) != "{}" {
				_ = json.Unmarshal(raw, &in)
			}

			now := time.Now().UTC()
			if u.Clock != nil {
				now = u.Clock().UTC()
			}

			var b strings.Builder
			var issues []string
			var schedulerEntries []*asynq.SchedulerEntry

			// 1. Asynq / Worker Process Health
			b.WriteString("=== 1. WORKER PROCESSES & ASYNQ QUEUES ===\n")
			if u.Inspector == nil {
				b.WriteString("Asynq inspector: not wired on this transport.\n\n")
			} else {
				servers, err := u.Inspector.Servers()
				if err != nil {
					b.WriteString(fmt.Sprintf("Failed to list Asynq servers: %v\n", err))
					issues = append(issues, "Could not inspect Asynq servers")
				} else {
					var hasWorker, hasAgent bool
					b.WriteString(fmt.Sprintf("Active Asynq Server Instances (%d):\n", len(servers)))
					for _, s := range servers {
						var qNames []string
						for qname, weight := range s.Queues {
							qNames = append(qNames, fmt.Sprintf("%s:%d", qname, weight))
							if qname == "default" {
								hasWorker = true
							}
							if qname == "agents" {
								hasAgent = true
							}
						}
						sort.Strings(qNames)
						b.WriteString(fmt.Sprintf("  • PID %d on %s | status=%s concurrency=%d queues=[%s] active_workers=%d\n",
							s.PID, s.Host, s.Status, s.Concurrency, strings.Join(qNames, ", "), len(s.ActiveWorkers)))
					}

					if !hasWorker {
						b.WriteString("  ⚠️ CRITICAL: No active worker server consuming the \"default\" queue! Strategy trading cycles will NOT execute.\n")
						issues = append(issues, "Strategy worker process is DOWN (no default queue consumer)")
					}
					if !hasAgent {
						b.WriteString("  ⚠️ WARNING: No active agent runner consuming the \"agents\" queue! Scheduled agent crons will NOT trigger.\n")
						issues = append(issues, "Agent runner process is DOWN (no agents queue consumer)")
					}
				}

				b.WriteString("\nQueue Health:\n")
				queues, err := u.Inspector.Queues()
				if err != nil {
					b.WriteString(fmt.Sprintf("Failed to list queues: %v\n", err))
				} else {
					for _, q := range queues {
						info, err := u.Inspector.GetQueueInfo(q)
						if err != nil {
							b.WriteString(fmt.Sprintf("  • %s: error getting info: %v\n", q, err))
							continue
						}
						role := "general / trading tasks"
						if q == "agents" {
							role = "AI agent runs & proposals"
						} else if q == "default" {
							role = "strategy cycles & backtests"
						}
						b.WriteString(fmt.Sprintf("  • Queue %q (%s):\n", q, role))
						b.WriteString(fmt.Sprintf("      size=%d active=%d pending=%d scheduled=%d retry=%d latency=%s paused=%t\n",
							info.Size, info.Active, info.Pending, info.Scheduled, info.Retry, info.Latency.Round(time.Millisecond), info.Paused))

						if info.Pending > 20 || info.Latency > 2*time.Minute {
							b.WriteString(fmt.Sprintf("      ⚠️ High latency/backlog in queue %q (latency=%s, pending=%d)\n", q, info.Latency, info.Pending))
							issues = append(issues, fmt.Sprintf("High queue latency on %q (%s)", q, info.Latency))
						}
						if info.Retry > 0 {
							b.WriteString(fmt.Sprintf("      ⚠️ %d task(s) currently failing and waiting for retry in %q\n", info.Retry, q))
							issues = append(issues, fmt.Sprintf("%d tasks in retry state on %q", info.Retry, q))
						}
					}
				}

				// Periodic scheduler entries
				entries, err := u.Inspector.SchedulerEntries()
				if err == nil {
					schedulerEntries = entries
					if len(entries) > 0 {
						b.WriteString(fmt.Sprintf("\nPeriodic / Cron Scheduler Entries (%d):\n", len(entries)))
						for _, e := range entries {
							taskType := "unknown"
							if e.Task != nil {
								taskType = e.Task.Type()
							}
							var nextStr string
							if !e.Next.IsZero() {
								nextStr = e.Next.UTC().Format("15:04:05 UTC")
							} else {
								nextStr = "n/a"
							}
							b.WriteString(fmt.Sprintf("  • %s | spec=%q | next=%s\n", taskType, e.Spec, nextStr))
						}
					}
				}
				b.WriteString("\n")
			}

			// 2. Strategy Cycle Execution Health
			b.WriteString("=== 2. STRATEGY CYCLE EXECUTION HEALTH ===\n")
			strategies, err := u.Strategy.GetAll(ctx)
			if err != nil {
				b.WriteString(fmt.Sprintf("Failed to list strategies: %v\n", err))
				issues = append(issues, "Could not fetch strategies from database")
			} else {
				var execMap map[uint]entities.StrategyExecution
				if u.ExecutionReader != nil {
					execMap, _ = u.ExecutionReader.GetLatestExecutions(ctx)
				}
				if execMap == nil {
					execMap = make(map[uint]entities.StrategyExecution)
				}

				var activeCount int
				for _, s := range strategies {
					if in.StrategyID > 0 && s.ID != in.StrategyID {
						continue
					}
					if s.Status == entities.Disabled && in.StrategyID == 0 {
						continue
					}
					activeCount++

					cycleMins := int(s.StrategyConfiguration.Cycle)
					if cycleMins <= 0 {
						cycleMins = 5
					}

					exec, hasExec := execMap[s.ID]
					b.WriteString(fmt.Sprintf("  • [ID %d] %s\n", s.ID, s.Name))
					b.WriteString(fmt.Sprintf("      status=%s mode=%s cycle=%dm monitored=%s\n",
						s.Status, s.Mode, cycleMins, strings.Join(s.MonitoredSymbols, ",")))

					if !hasExec {
						age := now.Sub(s.CreatedAt)
						if age > time.Duration(cycleMins*2+2)*time.Minute {
							b.WriteString(fmt.Sprintf("      ⚠️ NEVER EXECUTED (created %s ago, expected every %dm)\n",
								formatDurationShort(age), cycleMins))
							issues = append(issues, fmt.Sprintf("Strategy %q (ID %d) has never executed (created %s ago)", s.Name, s.ID, formatDurationShort(age)))
						} else {
							b.WriteString("      ⏳ PENDING FIRST RUN (newly created, waiting for first scheduled cycle)\n")
						}
					} else {
						elapsed := now.Sub(exec.ExecutedAt)
						maxAllowed := time.Duration(cycleMins*2+3) * time.Minute

						var healthLabel string
						if elapsed > maxAllowed {
							healthLabel = fmt.Sprintf("⚠️ STALLED / OVERDUE (expected every %dm, last ran %s ago)", cycleMins, formatDurationShort(elapsed))
							issues = append(issues, fmt.Sprintf("Strategy %q (ID %d) is stalled (no run in %s)", s.Name, s.ID, formatDurationShort(elapsed)))
						} else if exec.Status == entities.ExecutionStatus(entities.Error) {
							healthLabel = fmt.Sprintf("⚠️ RUNNING WITH ERROR (last cycle failed %s ago)", formatDurationShort(elapsed))
							issues = append(issues, fmt.Sprintf("Strategy %q (ID %d) error on last cycle: %s", s.Name, s.ID, exec.Message))
						} else {
							healthLabel = fmt.Sprintf("✅ HEALTHY (last ran %s ago, cycle %dm)", formatDurationShort(elapsed), cycleMins)
						}

						b.WriteString(fmt.Sprintf("      last_executed: %s (%s ago) | status=%s\n",
							exec.ExecutedAt.UTC().Format("2006-01-02 15:04:05 UTC"), formatDurationShort(elapsed), exec.Status))
						if exec.Status == entities.ExecutionStatus(entities.Error) && exec.Message != "" {
							b.WriteString(fmt.Sprintf("      error_message: %s\n", exec.Message))
						}
						b.WriteString(fmt.Sprintf("      schedule_status: %s\n", healthLabel))
					}
				}

				if activeCount == 0 {
					b.WriteString("  No active strategies found matching criteria.\n")
				}
				b.WriteString("\n")
			}

			// 3. Agent Cron Schedules
			b.WriteString("=== 3. AGENT CRON SCHEDULE HEALTH ===\n")
			if u.Platform == nil {
				b.WriteString("Agent platform repository: not wired on this transport.\n\n")
			} else {
				// Map next runs per agent from schedulerEntries
				nextAgentRuns := make(map[uint]time.Time)
				for _, e := range schedulerEntries {
					if e != nil && e.Task != nil {
						var p struct {
							AgentID uint `json:"agent_id"`
						}
						if err := json.Unmarshal(e.Task.Payload(), &p); err == nil && p.AgentID > 0 {
							if curr, exists := nextAgentRuns[p.AgentID]; !exists || (!e.Next.IsZero() && e.Next.Before(curr)) {
								nextAgentRuns[p.AgentID] = e.Next
							}
						}
					}
				}

				agents, err := u.Platform.ListAgents(ctx)
				if err != nil {
					b.WriteString(fmt.Sprintf("Failed to list agents: %v\n", err))
				} else {
					var cronAgentsCount int
					for _, a := range agents {
						parsed := a.ParsedTriggers()
						if len(parsed.Cron) == 0 {
							continue
						}
						cronAgentsCount++

						b.WriteString(fmt.Sprintf("  • [Agent %d] %s\n", a.ID, a.Name))
						b.WriteString(fmt.Sprintf("      paused=%t crons=[%s]\n", a.Paused, strings.Join(parsed.Cron, ", ")))

						bindings, _ := u.Platform.ListBindingsByAgent(ctx, a.ID)
						if len(bindings) == 0 {
							b.WriteString("      ⚠️ UNSCHEDULED: agent has no strategy bindings (attach a strategy to activate cron)\n")
						}

						if u.Repository != nil {
							lastRun, err := u.Repository.LastRunByAgent(ctx, a.ID)
							if err == nil && lastRun != nil && !lastRun.StartedAt.IsZero() {
								elapsed := now.Sub(lastRun.StartedAt)
								b.WriteString(fmt.Sprintf("      last_run: %s (%s ago) | trigger=%s status=%s\n",
									lastRun.StartedAt.UTC().Format("2006-01-02 15:04:05 UTC"),
									formatDurationShort(elapsed),
									lastRun.Trigger,
									lastRun.Status))
							} else {
								b.WriteString("      last_run: never\n")
							}
						}

						if nextTime, hasNext := nextAgentRuns[a.ID]; hasNext && !nextTime.IsZero() {
							b.WriteString(fmt.Sprintf("      next_scheduled_run: %s\n", nextTime.UTC().Format("2006-01-02 15:04:05 UTC")))
						}
					}

					if cronAgentsCount == 0 {
						b.WriteString("  No agents with cron schedules configured.\n")
					}
					b.WriteString("\n")
				}
			}

			// 4. Overall Assessment Summary
			b.WriteString("=== OVERALL HEALTH ASSESSMENT ===\n")
			if len(issues) == 0 {
				b.WriteString("STATUS: ✅ ALL SCHEDULERS & WORKERS HEALTHY\n")
				b.WriteString("All queues clear, workers heartbeat active, and trading/agent cycles running within normal tolerances.\n")
			} else {
				b.WriteString(fmt.Sprintf("STATUS: ⚠️ ATTENTION REQUIRED (%d issue(s) detected)\n", len(issues)))
				for _, iss := range issues {
					b.WriteString(fmt.Sprintf("  • %s\n", iss))
				}
			}

			return b.String(), nil
		},
	}
}

func formatDurationShort(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		h := int(d.Hours())
		m := int(d.Minutes()) % 60
		return fmt.Sprintf("%dh%dm", h, m)
	}
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	return fmt.Sprintf("%dd%dh", days, hours)
}
