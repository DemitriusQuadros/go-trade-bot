package main

import (
	"context"
	"fmt"
	"go-trade-bot/app/entities"
	account "go-trade-bot/app/handler/web/account"
	backtest "go-trade-bot/app/handler/web/backtest"
	broker "go-trade-bot/app/handler/web/broker"
	candleimport "go-trade-bot/app/handler/web/candleimport"
	optimize "go-trade-bot/app/handler/web/optimize"
	performancehistory "go-trade-bot/app/handler/web/performancehistory"
	realtime "go-trade-bot/app/handler/web/realtime"
	scripthandler "go-trade-bot/app/handler/web/script"
	settings "go-trade-bot/app/handler/web/settings"
	signal "go-trade-bot/app/handler/web/signal"
	strategy "go-trade-bot/app/handler/web/strategy"
	_ "go-trade-bot/app/strategies/mlgrpc"
	"go-trade-bot/cmd/api/modules"
	"go-trade-bot/cmd/api/webui"
	config "go-trade-bot/internal/configuration"
	"go-trade-bot/internal/handler"
	"go-trade-bot/internal/metrics"
	"go-trade-bot/internal/middleware"
	"log"
	"net"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/fx"
	"gorm.io/gorm"
)

type Route interface {
	Handlers() []handler.Configuration
}

func main() {
	fx.New(
		modules.ConfigurationModule,
		modules.DbModule,
		modules.ExchangeModule,
		modules.NotifierModule,
		modules.StrategyModule,
		modules.MetricsModule,
		modules.AccountModule,
		modules.SignalModule,
		modules.CandleModule,
		modules.BacktestModule,
		modules.OptimizeModule,
		modules.PerformanceHistoryModule,
		modules.RealtimeModule,
		modules.CandleImportModule,
		modules.SettingsModule,
		modules.ScriptModule,
		fx.Provide(
			NewHTTPServer,
			AsRoute(strategy.NewStrategyHandler),
			AsRoute(broker.NewBrokerHandler),
			AsRoute(account.NewAccountHandler),
			AsRoute(signal.NewSignalHandler),
			AsRoute(backtest.NewBacktestHandler),
			AsRoute(optimize.NewOptimizeHandler),
			AsRoute(performancehistory.NewHandler),
			AsRoute(realtime.NewRealtimeHandler),
			AsRoute(candleimport.NewCandleImportHandler),
			AsRoute(settings.NewSettingsHandler),
			AsRoute(scripthandler.NewScriptHandler),
			fx.Annotate(
				NewServeMux,
				fx.ParamTags(`group:"routes"`, ``),
			),
		),
		fx.Invoke(func(db *gorm.DB) {
			if err := Migrate(db); err != nil {
				log.Fatalf("failed to migrate database: %v", err)
			}
		}),
		fx.Invoke(func(*http.Server) {}),
	).Run()
}

func NewHTTPServer(
	lc fx.Lifecycle,
	router *mux.Router,
	cfg *config.Configuration,
	collector *metrics.MetricsCollector,
) *http.Server {
	wrappedMux := middleware.ConfigMiddleware(cfg, collector)(router)
	srv := &http.Server{Addr: ":8080", Handler: wrappedMux}

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			ln, err := net.Listen("tcp", srv.Addr)
			if err != nil {
				return err
			}
			fmt.Println("Starting HTTP server at", srv.Addr)
			go srv.Serve(ln)
			return nil
		},
		OnStop: func(ctx context.Context) error {
			return srv.Shutdown(ctx)
		},
	})
	return srv
}

func NewServeMux(routes []Route, cfg *config.Configuration) *mux.Router {
	router := mux.NewRouter()
	for _, route := range routes {
		for _, h := range route.Handlers() {
			router.HandleFunc(h.Pattern, middleware.RequireAuth(cfg, h.Action)).Methods(h.Method)
		}
	}

	router.Handle("/metrics", promhttp.Handler())
	router.PathPrefix("/").Handler(webui.Handler())
	return router
}

func AsRoute(f any) any {
	return fx.Annotate(
		f,
		fx.As(new(Route)),
		fx.ResultTags(`group:"routes"`),
	)
}

func Migrate(db *gorm.DB) error {
	if err := db.AutoMigrate(
		&entities.Strategy{},
		&entities.StrategyExecution{},
		&entities.Signal{},
		&entities.Order{},
		&entities.Account{},
		&entities.Candle{},
		&entities.BacktestRun{},
		&entities.OptimizationRun{},
		&entities.StrategyPerformanceSnapshot{},
		&entities.Settings{},
		&entities.ScriptVersion{},
		&entities.ImportJob{},
		&entities.ImportSchedule{},
		&entities.ScriptState{},
	); err != nil {
		return err
	}

	// Legacy backfill (Spec 06): older DBs may still carry rows that only set
	// the now-removed `algorithm` column (backend-05 dropped the Strategy.
	// Algorithm field/enum entirely). If that column still physically exists
	// on this database (AutoMigrate never drops columns), backfill
	// strategy_name from it once. On a fresh database the column was never
	// created, so this step is skipped rather than failing on an unknown
	// column reference.
	if db.Migrator().HasColumn(&entities.Strategy{}, "algorithm") {
		if err := db.Exec(`
			UPDATE strategies
			SET strategy_name = algorithm
			WHERE (strategy_name IS NULL OR strategy_name = '') AND algorithm IS NOT NULL AND algorithm <> ''
		`).Error; err != nil {
			return err
		}
	}

	return nil
}
