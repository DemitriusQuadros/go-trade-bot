package db

import (
	"context"
	"go-trade-bot/internal/configuration"
	"log"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.uber.org/fx"
)

var (
	clientInstance      *mongo.Client
	clientInstanceError error
	mongoOnce           sync.Once
)

func NewMongoClient(cfg *configuration.Configuration, lc fx.Lifecycle) (*mongo.Client, error) {
	mongoOnce.Do(func() {
		clientOptions := options.Client().ApplyURI(cfg.DB.URI)
		clientOptions.SetMaxPoolSize(uint64(cfg.DB.PoolSize))
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		client, err := mongo.Connect(ctx, clientOptions)
		if err != nil {
			clientInstanceError = err
			return
		}
		err = client.Ping(ctx, nil)
		if err != nil {
			clientInstanceError = err
			return
		}

		clientInstance = client

		lc.Append(fx.Hook{
			OnStop: func(ctx context.Context) error {
				return client.Disconnect(ctx)
			},
		})
	})

	return clientInstance, clientInstanceError
}

func DisconnectMongoClient() error {
	if clientInstance == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := clientInstance.Disconnect(ctx)
	if err != nil {
		log.Printf("Erro ao desconectar do MongoDB: %v", err)
		return err
	}

	return nil
}
