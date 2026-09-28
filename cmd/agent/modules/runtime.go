package modules

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	taskagent "go-trade-bot/app/handler/tasks/agent"
	"go-trade-bot/app/repository/agentplatform"
	proposalrepo "go-trade-bot/app/repository/proposal"
	settings_repo "go-trade-bot/app/repository/settings"
	strategy_repo "go-trade-bot/app/repository/strategy"
	agentusecase "go-trade-bot/app/usecase/agent"
	proposalusecase "go-trade-bot/app/usecase/proposal"
	strategyusecase "go-trade-bot/app/usecase/strategy"
	agentworker "go-trade-bot/app/workers/agent"
	"go-trade-bot/internal/configuration"
	"go-trade-bot/internal/lock"
	"go-trade-bot/internal/metrics"
	"go-trade-bot/internal/notifier"

	"github.com/hibiken/asynq"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/fx"
	"gorm.io/gorm"
)

// RuntimeModule is cmd/agent's asynq server (queue "agents" ONLY - never
// the default queue cmd/worker consumes), the DB-backed cron
// PeriodicTaskManager, and the /metrics server.
var RuntimeModule = fx.Module("runtime",
	fx.Provide(
		func(uc *agentusecase.AgentUseCase, platform agentplatform.Repository, collector *metrics.MetricsCollector) *taskagent.Processor {
			return taskagent.NewProcessor(uc, platform, collector)
		},
		func(platform agentplatform.Repository, settings settings_repo.Repository) *taskagent.CronProvider {
			return taskagent.NewCronProvider(platform, settings)
		},
		agentworker.NewAgentWorker,
		// Phase B-01 §5: agent:apply_proposal. The ONLY code path that
		// changes a live strategy's source (strategy_repo.ReplaceScriptSource),
		// reached only after an operator approve over authenticated REST.
		func(
			db *gorm.DB,
			strategies strategy_repo.StrategyRepository,
			strategyUC strategyusecase.StrategyUseCase,
			platform agentplatform.Repository,
			n *notifier.MultiTargetNotifier,
			cfg *configuration.Configuration,
		) *proposalusecase.Applier {
			return &proposalusecase.Applier{
				Repo:       proposalrepo.NewGormRepository(db),
				Strategies: strategies,
				Status:     strategyUC,
				Lock:       lock.NewRedisCycleLockFromAddr(cfg.Redis.Addr),
				Platform:   platform,
				Notifier:   n,
				APIBaseURL: cfg.APIBaseURL,
			}
		},
		func(a *proposalusecase.Applier, w agentworker.AgentWorker) *taskagent.ApplyProcessor {
			return taskagent.NewApplyProcessor(a, w)
		},
	),
	fx.Invoke(StartRuntime),
)

// StartRuntime wires the lifecycle hooks.
func StartRuntime(lc fx.Lifecycle, cfg *configuration.Configuration, processor *taskagent.Processor, provider *taskagent.CronProvider, applyProcessor *taskagent.ApplyProcessor) error {
	rt := cfg.AgentRuntime.WithDefaults()
	redisOpt := asynq.RedisClientOpt{Addr: cfg.Redis.Addr}

	server := asynq.NewServer(redisOpt, asynq.Config{
		Concurrency: rt.Concurrency,
		Queues:      map[string]int{agentworker.Queue: 1},
	})
	manager, err := asynq.NewPeriodicTaskManager(asynq.PeriodicTaskManagerOpts{
		RedisConnOpt:               redisOpt,
		PeriodicTaskConfigProvider: provider,
		SyncInterval:               rt.SyncInterval,
		SchedulerOpts:              &asynq.SchedulerOpts{Location: time.UTC},
	})
	if err != nil {
		return fmt.Errorf("cmd/agent: periodic task manager: %w", err)
	}

	mux := asynq.NewServeMux()
	mux.HandleFunc(agentworker.TaskAgentRun, processor.ProcessTask)
	mux.HandleFunc(agentworker.TaskApplyProposal, applyProcessor.ProcessTask)

	metricsMux := http.NewServeMux()
	metricsMux.Handle("/metrics", promhttp.Handler())
	metricsSrv := &http.Server{Addr: ":" + rt.MetricsPort, Handler: metricsMux}

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			if err := server.Start(mux); err != nil {
				return fmt.Errorf("cmd/agent: asynq server: %w", err)
			}
			if err := manager.Start(); err != nil {
				return fmt.Errorf("cmd/agent: cron manager: %w", err)
			}
			go func() {
				if err := metricsSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					log.Printf("cmd/agent: metrics server stopped: %v", err)
				}
			}()
			log.Printf("cmd/agent: consuming queue %q (concurrency %d), cron sync every %s, /metrics on :%s",
				agentworker.Queue, rt.Concurrency, rt.SyncInterval, rt.MetricsPort)
			return nil
		},
		OnStop: func(ctx context.Context) error {
			manager.Shutdown()
			server.Shutdown()
			return metricsSrv.Shutdown(ctx)
		},
	})
	return nil
}
