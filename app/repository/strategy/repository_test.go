package repository_test

import (
	"context"
	"go-trade-bot/app/entities"
	repository "go-trade-bot/app/repository/strategy"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.mongodb.org/mongo-driver/mongo/integration/mtest"
)

func TestStrategyRepository_Save(t *testing.T) {
	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	mt.Run("should save strategy without errors", func(mt *mtest.T) {
		repo := repository.NewStrategyRepository(mt.Client)
		strategy := entities.Strategy{Name: "Test Strategy"}
		mt.AddMockResponses(mtest.CreateSuccessResponse())
		err := repo.Save(context.Background(), strategy)
		assert.NoError(t, err)
	})
}
