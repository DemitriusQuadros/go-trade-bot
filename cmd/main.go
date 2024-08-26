package main

import (
	"context"
	"fmt"
	symbol "go-trade-bot/app/symbol/handler"
	todo "go-trade-bot/app/todo"
	"go-trade-bot/cmd/modules"
	config "go-trade-bot/internal/configuration"
	"go-trade-bot/internal/middleware"
	"net"
	"net/http"

	"go.uber.org/fx"
)

type Route interface {
	http.Handler
	Pattern() string
}

func main() {
	fx.New(
		modules.ConfigurationModule,
		modules.DbModule,
		modules.BrokerModule,
		modules.SymbolModule,
		fx.Provide(
			NewHTTPServer,
			AsRoute(todo.NewTodoHandler),
			AsRoute(symbol.NewSymbolHandler),
			fx.Annotate(
				NewServeMux,
				fx.ParamTags(`group:"routes"`),
			),
		),
		fx.Invoke(func(*http.Server) {}),
	).Run()
}

func NewHTTPServer(lc fx.Lifecycle, mux *http.ServeMux, cfg *config.Configuration) *http.Server {
	wrappedMux := middleware.ConfigMiddleware(cfg)(mux)
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

func NewServeMux(routes []Route) *http.ServeMux {
	mux := http.NewServeMux()
	for _, route := range routes {
		mux.Handle(route.Pattern(), route)
	}
	return mux
}

func AsRoute(f any) any {
	return fx.Annotate(
		f,
		fx.As(new(Route)),
		fx.ResultTags(`group:"routes"`),
	)
}
