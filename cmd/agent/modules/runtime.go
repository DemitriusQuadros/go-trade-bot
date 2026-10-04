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
	signalrepo "go-trade-bot/app/repository/signal"
	strategy_repo "go-trade-bot/app/repository/strategy"
	agentusecase "go-trade-bot/app/usecase/agent"
	proposalusecase "go-trade-bot/app/usecase/proposal"
	strategyusecase "go-trade-bot/app/usecase/strategy"
	agentworker "go-trade-bot/app/workers/agent"
	"go-trade-bot/internal/configuration"
	"go-trade-bot/internal/cooldown"
	"go-trade-bot/internal/exchange"
	"go-trade-bot/internal/feed"
	"go-trade-bot/internal/indicators"
	"go-trade-bot/internal/lock"
	"go-trade-bot/internal/marketstatus"
	"go-trade-bot/internal/metrics"
	"go-trade-bot/internal/notifier"

	"github.com/hibiken/asynq"
	"github.com/hibiken/asynqmon"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/fx"
	"gorm.io/gorm"
)

// RuntimeModule is cmd/agent's asynq server (queue "agents" ONLY - never
// the default queue cmd/worker consumes), the DB-backed cron
// PeriodicTaskManager, and the /metrics server.
var RuntimeModule = fx.Module("runtime",
	fx.Provide(
		func(uc *agentusecase.AgentUseCase, platform agentplatform.Repository, collector *metrics.MetricsCollector, chains *taskagent.ChainDispatcher) *taskagent.Processor {
			p := taskagent.NewProcessor(uc, platform, collector)
			p.SetChainDispatcher(chains)
			return p
		},
		func(platform agentplatform.Repository, settings settings_repo.Repository) *taskagent.CronProvider {
			return taskagent.NewCronProvider(platform, settings).WithEventSweep()
		},
		agentworker.NewAgentWorker,
		// Agents platform C-01: event / market / chain triggers. Every one of
		// them only ENQUEUES agent:run tasks; the runs themselves are ordinary
		// unattended runs under every Phase A/B guard.
		func(cfg *configuration.Configuration) cooldown.Store {
			return cooldown.NewRedisStoreFromAddr(cfg.Redis.Addr)
		},
		func(w agentworker.AgentWorker, collector *metrics.MetricsCollector) *agentworker.ChainLauncher {
			l := agentworker.NewChainLauncher(w)
			l.OnFired = func() {
				collector.IncrementCounter(taskagent.MetricTriggersFired, map[string]string{"kind": "chain"})
			}
			l.OnSuppressed = func(string) {
				collector.IncrementCounter(taskagent.MetricTriggersSuppressed, map[string]string{"kind": "chain"})
			}
			return l
		},
		func(platform agentplatform.Repository, settings settings_repo.Repository, l *agentworker.ChainLauncher) *taskagent.ChainDispatcher {
			return taskagent.NewChainDispatcher(platform, settings, l)
		},
		func(platform agentplatform.Repository, strategies strategy_repo.StrategyRepository, settings settings_repo.Repository,
			store cooldown.Store, w agentworker.AgentWorker, collector *metrics.MetricsCollector) *taskagent.EventDispatcher {
			return taskagent.NewEventDispatcher(platform, strategies, settings, store, w, collector)
		},
		func(db *gorm.DB, platform agentplatform.Repository, strategies strategy_repo.StrategyRepository, d *taskagent.EventDispatcher) *taskagent.Sweeper {
			return taskagent.NewSweeper(platform, strategies, signalrepo.NewSignalRepository(db), d)
		},
		func(cfg *configuration.Configuration, platform agentplatform.Repository, strategies strategy_repo.StrategyRepository,
			settings settings_repo.Repository, client exchange.ExchangeClient, store cooldown.Store, w agentworker.AgentWorker,
			collector *metrics.MetricsCollector) *taskagent.MarketWatcher {
			// client is cmd/agent's exchange.ReadOnlyClient (ExchangeModule):
			// klines only, order placement impossible.
			newFeed := func(symbol string) (taskagent.KlineFeed, error) {
				return feed.NewLiveFeed(client, symbol, "1m", nil)
			}
			return taskagent.NewMarketWatcher(platform, strategies, settings, client, newFeed, indicators.NewTalibAdapter(),
				store, w, marketstatus.NewRedisStoreFromAddr(cfg.Redis.Addr), collector, cfg.AgentRuntime.WithDefaults().SyncInterval)
		},
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

// AsynqmonRootPath is where cmd/agent serves the Asynqmon UI (fix-02 B5),
// the same path cmd/worker uses on :9191.
const AsynqmonRootPath = "/tasks/monitoring"

// NewMonitoringHandler serves /metrics and the Asynqmon UI on
// AGENT_RUNTIME.METRICS_PORT (default 9194), mounted like cmd/worker's
// StartMetricsServer. It reads the same Redis, so it shows every queue -
// including "agents", which the worker's Asynqmon also lists but which only
// this process consumes.
func NewMonitoringHandler(cfg *configuration.Configuration) http.Handler {
	mon := asynqmon.New(asynqmon.Options{
		RootPath:          AsynqmonRootPath,
		RedisConnOpt:      asynq.RedisClientOpt{Addr: cfg.Redis.Addr},
		PrometheusAddress: cfg.Prometheus.Address,
	})
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, AsynqmonRootPath, http.StatusTemporaryRedirect)
			return
		}
		http.NotFound(w, r)
	})
	mux.Handle("/metrics", promhttp.Handler())
	mux.Handle(mon.RootPath(), mon)
	mux.Handle(mon.RootPath()+"/", mon)
	return mux
}

// StartRuntime wires the lifecycle hooks.
func StartRuntime(lc fx.Lifecycle, cfg *configuration.Configuration, processor *taskagent.Processor, provider *taskagent.CronProvider,
	applyProcessor *taskagent.ApplyProcessor, events *taskagent.EventDispatcher, sweeper *taskagent.Sweeper, watcher *taskagent.MarketWatcher) error {
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
	mux.HandleFunc(taskagent.TaskAgentEvent, events.ProcessTask)   // C-01 §2.3: worker bridge
	mux.HandleFunc(taskagent.TaskSweepEvents, sweeper.ProcessTask) // C-01 §2.2: drawdown / no_signal

	metricsSrv := &http.Server{Addr: ":" + rt.MetricsPort, Handler: NewMonitoringHandler(cfg)}

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			if err := server.Start(mux); err != nil {
				return fmt.Errorf("cmd/agent: asynq server: %w", err)
			}
			if err := manager.Start(); err != nil {
				return fmt.Errorf("cmd/agent: cron manager: %w", err)
			}
			watcher.Start() // C-01 §3
			go func() {
				if err := metricsSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					log.Printf("cmd/agent: metrics server stopped: %v", err)
				}
			}()
			log.Printf("cmd/agent: consuming queue %q (concurrency %d), cron sync every %s, /metrics and %s on :%s",
				agentworker.Queue, rt.Concurrency, rt.SyncInterval, AsynqmonRootPath, rt.MetricsPort)
			return nil
		},
		OnStop: func(ctx context.Context) error {
			watcher.Stop()
			manager.Shutdown()
			server.Shutdown()
			return metricsSrv.Shutdown(ctx)
		},
	})
	return nil
}
