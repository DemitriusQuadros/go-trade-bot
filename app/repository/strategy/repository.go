package repository

import (
	"context"
	"go-trade-bot/app/entities"

	"go.mongodb.org/mongo-driver/mongo"
)

type StrategyRepository struct {
	client *mongo.Client
}

func NewStrategyRepository(client *mongo.Client) StrategyRepository {
	return StrategyRepository{
		client: client,
	}
}

func (r StrategyRepository) Save(ctx context.Context, strategy entities.Strategy) error {
	collection := r.client.Database("TradeBot").Collection("Strategies")
	_, err := collection.InsertOne(ctx, strategy)
	return err
}
