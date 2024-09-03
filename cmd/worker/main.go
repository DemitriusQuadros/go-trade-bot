package main

import (
	"context"
	"log"

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
			mux.HandleFunc("email:send", HandleSendEmailTask)
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

	if err := app.Start(context.Background()); err != nil {
		log.Fatal(err)
	}
	defer app.Stop(context.Background())
}
