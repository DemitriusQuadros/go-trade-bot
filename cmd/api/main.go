package main

import (
	"context"
	"fmt"
	strategy "go-trade-bot/app/handler/web/strategy"
	"go-trade-bot/cmd/api/modules"
	config "go-trade-bot/internal/configuration"
	"go-trade-bot/internal/handler"
	"go-trade-bot/internal/middleware"
	"net"
	"net/http"

	"github.com/gorilla/mux"
	"go.uber.org/fx"
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
		fx.Provide(
			NewHTTPServer,
			AsRoute(strategy.NewStrategyHandler),
			fx.Annotate(
				NewServeMux,
				fx.ParamTags(`group:"routes"`),
			),
		),
		fx.Invoke(func(*http.Server) {}),
	).Run()
}

func NewHTTPServer(lc fx.Lifecycle, router *mux.Router, cfg *config.Configuration) *http.Server {
	wrappedMux := middleware.ConfigMiddleware(cfg)(router)
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
	return router
}

func AsRoute(f any) any {
	return fx.Annotate(
		f,
		fx.As(new(Route)),
		fx.ResultTags(`group:"routes"`),
	)
}
