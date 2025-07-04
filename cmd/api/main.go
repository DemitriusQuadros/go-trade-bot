package main

import (
	"context"
	"fmt"
	"go-trade-bot/app/entities"
	account "go-trade-bot/app/handler/web/account"
	broker "go-trade-bot/app/handler/web/broker"
	strategy "go-trade-bot/app/handler/web/strategy"
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
		modules.BrokerModule,
		modules.StrategyModule,
		modules.MetricsModule,
		modules.AccountModule,
		fx.Provide(
			NewHTTPServer,
			AsRoute(strategy.NewStrategyHandler),
			AsRoute(broker.NewBrokerHandler),
			AsRoute(account.NewAccountHandler),
			fx.Annotate(
				NewServeMux,
				fx.ParamTags(`group:"routes"`),
			),
		),
		fx.Invoke(func(db *gorm.DB) {
			if err := Migrate(db); err != nil {
				log.Fatalf("failed to migrate database: %w", err)
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
	return db.AutoMigrate(
		&entities.Strategy{},
		&entities.StrategyExecution{},
		&entities.Signal{},
		&entities.Order{},
		&entities.Account{},
	)
}
