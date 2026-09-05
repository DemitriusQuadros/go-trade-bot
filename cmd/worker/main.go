package main

import (
	"context"
	"fmt"
	"go-trade-bot/app/engine"
	handler "go-trade-bot/app/handler/tasks/strategy"
	repository "go-trade-bot/app/repository/strategy"
	"go-trade-bot/app/strategies"

	// Blank-imported for their init() side effect only: each package
	// self-registers a Strategy factory into app/strategies' registry
	// (Spec 06 AC#1) - neither package's exported API is otherwise used here.
	_ "go-trade-bot/app/strategies/bollinger"
	_ "go-trade-bot/app/strategies/grid"
	_ "go-trade-bot/app/strategies/scalping"
	tasks "go-trade-bot/app/workers/strategy"
	"go-trade-bot/cmd/worker/modules"
	config "go-trade-bot/internal/configuration"
	"go-trade-bot/internal/metrics"
	"go-trade-bot/internal/middleware"
	"go-trade-bot/internal/notifier"
	"net/http"
	"os"

	"github.com/gorilla/mux"
	"github.com/hibiken/asynq"
	"github.com/hibiken/asynqmon"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"go.uber.org/fx"
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

func RegisterHandlers(
	lc fx.Lifecycle,
	server *asynq.Server,
	cfg *config.Configuration,
	collector *metrics.MetricsCollector,
	worker tasks.StrategyWorker,
	repository repository.StrategyRepository,
	eng *engine.Engine,
	notifySender notifier.NotificationSender,
) {
	StartMetricsServer(cfg)
	// cfg.Mode is guaranteed parseable here: assertModeGuard already validated
	// it in main() before fx.New() was even called, and this ParseExecutionMode
	// call reconstructs the same value from the same (re-normalized) config.
	processCeiling, err := strategies.ParseExecutionMode(cfg.Mode)
	if err != nil {
		processCeiling = strategies.ModeDryRun
	}
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			mux := asynq.NewServeMux()
			processor := handler.NewStrategyProcessor(collector, worker, repository, eng, notifySender, processCeiling, cfg.Testnet)

			mux.Handle(tasks.StrategyTask, middleware.AsynqConfigMiddleware(
				asynq.HandlerFunc(processor.HandleStrategyTask),
				cfg,
				collector,
			))

			go server.Run(mux)
			return nil
		},
		OnStop: func(ctx context.Context) error {
			server.Shutdown()
			return nil
		},
	})
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
		fx.Provide(
			NewRedisClient,
			NewAsynqServer,
		),
		fx.Invoke(RegisterHandlers),
	)

	app.Run()
}
