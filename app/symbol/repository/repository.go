package repository

import (
	"context"
	"go-trade-bot/app/symbol/entities"

	"go.mongodb.org/mongo-driver/mongo"
)

type SymbolRepository struct {
	client *mongo.Client
}

func NewSymbolRepository(client *mongo.Client) SymbolRepository {
	return SymbolRepository{
		client: client,
	}
}

func (r SymbolRepository) Save(ctx context.Context, symbol entities.Symbol) error {
	collection := r.client.Database("TradeBot").Collection("SymbolPrices")
	_, err := collection.InsertOne(ctx, symbol)
	return err
}
