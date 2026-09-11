package main

import (
	"context"
	"encoding/json"
	"fmt"
	"go-trade-bot/app/engine"
	"go-trade-bot/app/entities"
	internalapi "go-trade-bot/app/handler/internalapi"
	candleimport_tasks "go-trade-bot/app/handler/tasks/candleimport"
	optimize_tasks "go-trade-bot/app/handler/tasks/optimize"
	performance_tasks "go-trade-bot/app/handler/tasks/performancehistory"
	handler "go-trade-bot/app/handler/tasks/strategy"
	candle_repo "go-trade-bot/app/repository/candle"
	candleimport_repo "go-trade-bot/app/repository/candleimport"
	settings_repo "go-trade-bot/app/repository/settings"
	repository "go-trade-bot/app/repository/strategy"
	"go-trade-bot/app/strategies"
	settings_usecase "go-trade-bot/app/usecase/settings"
	"go-trade-bot/internal/exchange"
	"time"

	// Blank-imported for their init() side effect only: each package
	// self-registers a Strategy factory into app/strategies' registry
	// (Spec 06 AC#1) - neither package's exported API is otherwise used here.
	// mlgrpc (backend-04, Phase 4) registers "mlgrpc_dummy", which dials its
	// gRPC target lazily (grpc.NewClient does not connect eagerly) - a
	// worker with no ML strategy configured pays no cost for this import
	// beyond the registry entry itself.

	_ "go-trade-bot/app/strategies/mlgrpc"

	"go-trade-bot/app/strategies/script"

	candleimport_worker "go-trade-bot/app/workers/candleimport"
	optimize_worker "go-trade-bot/app/workers/optimize"
	tasks "go-trade-bot/app/workers/strategy"
	"go-trade-bot/cmd/worker/modules"
	config "go-trade-bot/internal/configuration"
	"go-trade-bot/internal/metrics"
	"go-trade-bot/internal/middleware"
	"go-trade-bot/internal/notifier"
	"log"
	"net/http"
	"os"

	"github.com/gorilla/mux"
	"github.com/hibiken/asynq"
	"github.com/hibiken/asynqmon"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"go.uber.org/fx"
	"gorm.io/gorm"
)

// assertModeGuard is Spec 10's startup gate (AC#1/#2), checked once before
// the fx.App is even constructed - a hard startup failure, not a
// runtime-skipped cycle. cfg.Mode is expected to already be normalized by
// internal/configuration (unset -> "dryrun", Spec 10 AC#8), so any parse
// error here means a genuinely unrecognized value, not an absent one.
func assertModeGuard(cfg *config.Configuration) {
	mode, err := strategies.ParseExecutionMode(cfg.Mode)
	if err != nil {
		fmt.Fprintf(os.Stderr, "go-trade-bot: refusing to start: invalid MODE %q: %v\n", cfg.Mode, err)
		os.Exit(1)
	}

	if mode == strategies.ModeLive && !cfg.ConfirmLive {
		fmt.Fprintln(os.Stderr, "go-trade-bot: refusing to start with MODE=live without explicit confirmation; "+
			"set CONFIRM_LIVE=true to acknowledge this worker will place real orders with real capital")
		os.Exit(1)
	}

	// Spec 01 AC#9: Testnet=true + process MODE=live is a distinct guard from
	// the confirm-live check above - live mode must never resolve to a
	// testnet adapter, regardless of confirmation. The mirror case (some
	// strategy's effective mode is "paper" while Testnet=false) can't be
	// checked here since Mode is per-strategy, not process-wide - see
	// gateMode in app/handler/tasks/strategy/handler.go (Spec 10 AC#6).
	if cfg.Testnet && mode == strategies.ModeLive {
		fmt.Fprintln(os.Stderr, "go-trade-bot: refusing to start: Testnet=true is incompatible with MODE=live; "+
			"live mode must never resolve to a testnet adapter")
		os.Exit(1)
	}
}

// RegisterScriptStrategy wires the "script" strategy into the global registry
// (backend-04). Unlike native Go strategies (self-registered via init()), the
// script strategy's factory closes over fx-constructed dependencies (the
// shared *script.Runner and the DB-backed ScriptStateStore), so registration
// happens here at startup, once, rather than at package-init time.
func RegisterScriptStrategy(runner *script.Runner, store script.ScriptStateStore) {
	strategies.Register("script", func(dbStrategy entities.Strategy) strategies.Strategy {
		return script.NewScriptStrategy(dbStrategy, store, runner)
	})
}

type RedisConfiguration struct {
	Addr string
}

func NewRedisClient(cfg *config.Configuration) *asynq.RedisClientOpt {
	return &asynq.RedisClientOpt{
		Addr: cfg.Redis.Addr,
	}
}

func NewAsynqServer(client *asynq.RedisClientOpt) *asynq.Server {
	return asynq.NewServer(
		*client,
		asynq.Config{
			Concurrency: 10,
		},
	)
}

// NewAsynqScheduler provides backend-05's periodic-task infrastructure: the
// same "already-present asynq infrastructure" the coordinator's spec calls
// for, just asynq's cron-based Scheduler component (distinct from the
// Server/ServeMux pair used for on-demand tasks like strategy cycles and
// optimize:execute) rather than a bespoke timer.
func NewAsynqScheduler(client *asynq.RedisClientOpt) *asynq.Scheduler {
	return asynq.NewScheduler(*client, nil)
}

func RegisterHandlers(
	lc fx.Lifecycle,
	server *asynq.Server,
	scheduler *asynq.Scheduler,
	cfg *config.Configuration,
	collector *metrics.MetricsCollector,
	worker tasks.StrategyWorker,
	candleRepo candle_repo.Repository,
	repository repository.StrategyRepository,
	eng *engine.Engine,
	notifySender notifier.NotificationSender,
	optimizeProcessor *optimize_tasks.OptimizeProcessor,
	snapshotProcessor *performance_tasks.SnapshotProcessor,
	candleImportTaskHandler *candleimport_tasks.TaskHandler,
	candleImportRepo candleimport_repo.Repository,
	db *gorm.DB,
	swappableExchange *exchange.SwappableExchangeClient,
	swappableNotifier *notifier.SwappableNotifier,
) {
	// cfg.Mode is guaranteed parseable here: assertModeGuard already validated
	// it in main() before fx.New() was even called, and this ParseExecutionMode
	// call reconstructs the same value from the same (re-normalized) config.
	processCeiling, err := strategies.ParseExecutionMode(cfg.Mode)
	if err != nil {
		processCeiling = strategies.ModeDryRun
	}

	// processor is constructed here (not inside OnStart) because Spec
	// backend-05 needs a reference to it before the server starts, to wire
	// it as the settings usecase's ProcessorGate (drain admission
	// gate/in-flight counter) and mount the internal settings-apply route
	// on the metrics server below.
	processor := handler.NewStrategyProcessor(collector, worker, repository, eng, notifySender, processCeiling, cfg.Testnet)

	// Spec backend-05 (ADR-016): this process's own settings usecase
	// instance, with a real ProcessorGate (processor) and no WorkerClient
	// (this process IS the worker - see app/usecase/settings's package doc).
	// Exposed only via the internal, non-public /internal/settings/apply
	// route cmd/api's PUT /settings handler forwards risk-bearing (and, for
	// consistency, safe-tier) changes to.
	settingsRepo := settings_repo.NewRepository(db)
	settingsUseCase := settings_usecase.NewUseCase(settingsRepo, swappableExchange, swappableNotifier, processor, nil)
	if cfg.InternalBridgeSecret == "" {
		log.Printf("WARNING: INTERNAL_BRIDGE_SECRET is not set - the internal settings-apply bridge (%s) "+
			"is protected only by its loopback-only bind, with no shared-secret defense-in-depth. Set "+
			"INTERNAL_BRIDGE_SECRET in config.yml/env if cmd/api and cmd/worker do not share full "+
			"network isolation from other processes on the host.", cfg.InternalBridgeAddr)
	}
	internalSettingsHandler := internalapi.NewSettingsHandler(settingsUseCase, cfg.InternalBridgeSecret)

	StartMetricsServer(cfg)
	StartInternalBridgeServer(cfg, internalSettingsHandler)

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			mux := asynq.NewServeMux()

			mux.Handle(tasks.StrategyTask, middleware.AsynqConfigMiddleware(
				asynq.HandlerFunc(processor.HandleStrategyTask),
				cfg,
				collector,
			))

			// backend-01: this codebase's first async job pattern, reusing the
			// exact same asynq server/mux, not a second queue mechanism.
			mux.Handle(optimize_worker.OptimizeTask, middleware.AsynqConfigMiddleware(
				asynq.HandlerFunc(optimizeProcessor.ProcessTask),
				cfg,
				collector,
			))

			// backend-05: daily strategy performance snapshot job, driven by
			// asynq's cron Scheduler rather than an on-demand Enqueue call -
			// this task type has no client-side "worker" wrapper since nothing
			// ever enqueues it directly.
			mux.Handle(performance_tasks.SnapshotTask, middleware.AsynqConfigMiddleware(
				asynq.HandlerFunc(snapshotProcessor.ProcessTask),
				cfg,
				collector,
			))

			if _, err := scheduler.Register("0 0 * * *", asynq.NewTask(performance_tasks.SnapshotTask, nil)); err != nil {
				log.Printf("failed to register daily performance snapshot cron entry: %v", err)
			}

			// Candle import (Spec backend-04/platform-self-service): one-off
			// import jobs and recurring cron-scheduled imports, on the same
			// shared mux/scheduler as everything else - this module used to
			// wire itself onto a shared *asynq.ServeMux that never existed as
			// an fx-provided type; moved here to match the one place this
			// codebase actually constructs and wires its asynq mux.
			mux.Handle(candleimport_worker.TaskImportExecute, middleware.AsynqConfigMiddleware(
				asynq.HandlerFunc(candleImportTaskHandler.HandleImportExecute),
				cfg,
				collector,
			))
			mux.Handle(candleimport_worker.TaskRecurringImport, middleware.AsynqConfigMiddleware(
				asynq.HandlerFunc(candleImportTaskHandler.HandleRecurringImport),
				cfg,
				collector,
			))
			if schedules, err := candleImportRepo.ListEnabledSchedules(ctx); err != nil {
				log.Printf("failed to load enabled candle import schedules: %v", err)
			} else {
				for _, sched := range schedules {
					payload, _ := json.Marshal(sched)
					if _, err := scheduler.Register(sched.CronSpec, asynq.NewTask(candleimport_worker.TaskRecurringImport, payload)); err != nil {
						log.Printf("failed to register candle import schedule %q: %v", sched.CronSpec, err)
					}
				}
			}

			go func() {
				if err := scheduler.Run(); err != nil {
					log.Printf("asynq scheduler stopped: %v", err)
				}
			}()

			go server.Run(mux)
			go startCandleLagMonitor(ctx, candleRepo, repository, collector)
			return nil
		},
		OnStop: func(ctx context.Context) error {
			scheduler.Shutdown()
			server.Shutdown()
			return nil
		},
	})
}

func startCandleLagMonitor(
	ctx context.Context,
	candleRepo candle_repo.Repository,
	stratRepo repository.StrategyRepository,
	collector *metrics.MetricsCollector,
) {
	ticker := time.NewTicker(2 * time.Minute)
	for {
		select {
		case <-ctx.Done():
			ticker.Stop()
			return
		case <-ticker.C:
			strats, err := stratRepo.GetAll(ctx)
			if err != nil {
				continue
			}
			for _, s := range strats {
				for _, sym := range s.MonitoredSymbols {
					tf := s.GetBrokerInterval()
					if tf == "" {
						tf = "1m"
					}
					latest, err := candleRepo.LatestOpenTime(ctx, sym, tf)
					if err == nil && !latest.IsZero() {
						lag := time.Since(latest).Seconds()
						collector.SetGauge("candle_import_lag_seconds", map[string]string{"symbol": sym, "timeframe": tf}, lag)
					}
				}
			}
		}
	}
}

// StartInternalBridgeServer serves Spec backend-05's settings-apply bridge on
// its own dedicated listener, bound to cfg.InternalBridgeAddr (loopback-only
// by default - see internal/settingsbridge's package doc). This is
// deliberately never mounted on StartMetricsServer's :9191 server below,
// which binds all interfaces (":9191" has no host prefix) - an
// unauthenticated write endpoint that can change broker credentials or flip
// Mode to "live" must never be reachable from the network.
func StartInternalBridgeServer(cfg *config.Configuration, internalSettingsHandler *internalapi.SettingsHandler) {
	r := mux.NewRouter()
	r.HandleFunc("/internal/settings/apply", internalSettingsHandler.Apply).Methods(http.MethodPost)

	srv := &http.Server{
		Handler: r,
		Addr:    cfg.InternalBridgeAddr,
	}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("internal settings-bridge server stopped: %v", err)
		}
	}()
	log.Printf("internal settings-bridge listening on %s (loopback-only)", cfg.InternalBridgeAddr)
}

func StartMetricsServer(cfg *config.Configuration) {
	h := asynqmon.New(asynqmon.Options{
		RootPath:          "/tasks/monitoring",
		RedisConnOpt:      asynq.RedisClientOpt{Addr: cfg.Redis.Addr},
		PrometheusAddress: cfg.Prometheus.Address,
	})

	r := mux.NewRouter()
	r.PathPrefix(h.RootPath()).Handler(h)
	// Spec 11: this /metrics route never existed on the worker process despite
	// prometheus.yml's "go-worker" job scraping :9191 since it was written -
	// every worker-side metric (total_strategy_task, asynq task counters, the
	// new order/WS/feed metrics from Specs 02-04) was invisible to Prometheus
	// until this line.
	r.Handle("/metrics", promhttp.Handler())

	srv := &http.Server{
		Handler: r,
		Addr:    ":9191",
	}
	go func() {
		srv.ListenAndServe()
	}()

}

func main() {
	// Spec 10 AC#1/#2: the mode guard is checked before the fx.App is even
	// constructed, using a standalone Configuration built the same way
	// ConfigurationModule builds it for the DI graph - a hard startup
	// failure here must happen regardless of what fx would have wired.
	assertModeGuard(config.NewConfiguration())

	app := fx.New(
		modules.ConfigurationModule,
		modules.DbModule,
		modules.MetricsModule,
		modules.CacheModule,
		modules.StrategyModule,
		modules.SignalModule,
		modules.ExchangeModule,
		modules.IndicatorsModule,
		modules.NotifierModule,
		modules.EngineModule,
		modules.AccountModule,
		modules.CandleModule,
		modules.CandleImportModule,
		modules.BacktestModule,
		modules.OptimizeModule,
		modules.PerformanceHistoryModule,
		modules.ScriptModule,
		fx.Provide(
			NewRedisClient,
			NewAsynqServer,
			NewAsynqScheduler,
		),
		fx.Invoke(RegisterScriptStrategy),
		fx.Invoke(RegisterHandlers),
	)

	app.Run()
}
