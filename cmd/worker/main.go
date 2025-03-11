package main

import (
	"context"
	handler "go-trade-bot/app/handler/tasks/strategy"
	tasks "go-trade-bot/app/workers/strategy"

	"github.com/hibiken/asynq"
	"go.uber.org/fx"
)

type RedisConfiguration struct {
	Addr string
}

func NewRedisClient() *asynq.RedisClientOpt {
	return &asynq.RedisClientOpt{
		Addr: "localhost:6379",
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

func RegisterHandlers(lc fx.Lifecycle, server *asynq.Server) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			mux := asynq.NewServeMux()
			processor := &handler.StrategyProcessor{}
			mux.HandleFunc(tasks.StrategyTask+"*", processor.HandleStrategyTask)
			go server.Run(mux)
			return nil
		},
		OnStop: func(ctx context.Context) error {
			server.Shutdown()
			return nil
		},
	})
}

func main() {
	app := fx.New(
		fx.Provide(
			NewRedisClient,
			NewAsynqServer,
		),
		fx.Invoke(RegisterHandlers),
	)

	app.Run()
}
