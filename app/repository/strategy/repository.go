package repository

import (
	"context"
	"go-trade-bot/app/entities"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

const (
	database   = "TradeBot"
	collection = "Strategies"
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
	collection := r.client.Database(database).Collection(collection)
	_, err := collection.InsertOne(ctx, strategy)
	return err
}

func (r StrategyRepository) GetByID(ctx context.Context, id string) (entities.Strategy, error) {
	collection := r.client.Database(database).Collection(collection)
	filter := bson.M{"id": id}

	cursor, err := collection.Find(ctx, filter)

	if err != nil {
		return entities.Strategy{}, err
	}

	defer cursor.Close(ctx)

	var strategy entities.Strategy
	for cursor.Next(ctx) {
		err := cursor.Decode(&strategy)
		if err != nil {
			return entities.Strategy{}, err
		}
	}

	return strategy, nil
}

func (r StrategyRepository) GetAll(ctx context.Context) ([]entities.Strategy, error) {
	collection := r.client.Database(database).Collection(collection)

	var strategies []entities.Strategy
	cursor, err := collection.Find(ctx, bson.M{})

	if err != nil {
		return nil, err
	}

	defer cursor.Close(ctx)

	for cursor.Next(ctx) {
		var strategy entities.Strategy
		err := cursor.Decode(&strategy)
		if err != nil {
			return nil, err
		}
		strategies = append(strategies, strategy)
	}

	return strategies, nil
}
