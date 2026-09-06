package main

import (
	"context"
	"fmt"
	"go-trade-bot/app/entities"
	account "go-trade-bot/app/handler/web/account"
	backtest "go-trade-bot/app/handler/web/backtest"
	broker "go-trade-bot/app/handler/web/broker"
	optimize "go-trade-bot/app/handler/web/optimize"
	performancehistory "go-trade-bot/app/handler/web/performancehistory"
	signal "go-trade-bot/app/handler/web/signal"
	strategy "go-trade-bot/app/handler/web/strategy"
	_ "go-trade-bot/app/strategies/bollinger"
	_ "go-trade-bot/app/strategies/grid"
	_ "go-trade-bot/app/strategies/mlgrpc"
	_ "go-trade-bot/app/strategies/scalping"
	"go-trade-bot/cmd/api/modules"
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
		fx.Provide(
			NewHTTPServer,
			AsRoute(strategy.NewStrategyHandler),
			AsRoute(broker.NewBrokerHandler),
			AsRoute(account.NewAccountHandler),
			AsRoute(signal.NewSignalHandler),
			AsRoute(backtest.NewBacktestHandler),
			AsRoute(optimize.NewOptimizeHandler),
			AsRoute(performancehistory.NewHandler),
			fx.Annotate(
				NewServeMux,
				fx.ParamTags(`group:"routes"`),
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

func NewServeMux(routes []Route) *mux.Router {
	router := mux.NewRouter()
	for _, route := range routes {
		for _, handler := range route.Handlers() {
			router.HandleFunc(handler.Pattern, handler.Action).Methods(handler.Method)
		}
	}

	router.Handle("/metrics", promhttp.Handler())
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
	); err != nil {
		return err
	}

	// Spec 06 migration step 2: backfill StrategyName from the legacy
	// Algorithm enum for every pre-existing row - additive, not destructive,
	// since Algorithm is kept (not dropped) per Spec 06 migration step 5.
	// Spec 10 AC#5: Mode itself is backfilled to "dryrun" via the column's
	// `gorm:"default:'dryrun'"` tag, which Postgres applies to existing rows
	// when the column is added - never left NULL, never "live".
	if err := db.Exec(`
		UPDATE strategies
		SET strategy_name = algorithm
		WHERE (strategy_name IS NULL OR strategy_name = '') AND algorithm IS NOT NULL AND algorithm <> ''
	`).Error; err != nil {
		return err
	}

	return nil
}
