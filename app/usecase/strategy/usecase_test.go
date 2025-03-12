package usecase_test

import (
	"context"
	"errors"
	"go-trade-bot/app/entities"
	usecase "go-trade-bot/app/usecase/strategy"
	"go-trade-bot/app/usecase/strategy/mocks"

	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestStrategyUseCase_Save(t *testing.T) {
	mockRepo := new(mocks.StrategyRepository)
	mockWorker := new(mocks.StrategyWorker)
	strategyUC := usecase.NewStrategyUseCase(mockRepo, mockWorker)

	ctx := context.Background()
	strategy := entities.Strategy{
		ID:               uuid.New(),
		Name:             "Test Strategy",
		Description:      "A test strategy",
		MonitoredSymbols: []string{"BTCUSDT", "ETHUSDT"},
		Algorithm:        entities.Heikenashi,
		StrategyConfiguration: entities.StrategyConfiguration{
			Cycle: 10,
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	t.Run("should save strategy successfully", func(t *testing.T) {
		mockRepo.On("Save", ctx, mock.AnythingOfType("entities.Strategy")).Return(nil).Once()
		mockWorker.On("EnqueueStrategyTask", mock.AnythingOfType("entities.Strategy")).Return(nil).Once()

		err := strategyUC.Save(ctx, strategy)
		assert.NoError(t, err)

		mockRepo.AssertExpectations(t)
		mockWorker.AssertExpectations(t)
	})

	t.Run("should return error when strategy name is empty", func(t *testing.T) {
		invalidStrategy := strategy
		invalidStrategy.Name = ""

		err := strategyUC.Save(ctx, invalidStrategy)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "Strategy has to have a name")
	})

	t.Run("should return error when repository fails", func(t *testing.T) {
		mockRepo.On("Save", ctx, mock.AnythingOfType("entities.Strategy")).Return(errors.New("database error")).Once()

		err := strategyUC.Save(ctx, strategy)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "database error")

		mockRepo.AssertExpectations(t)
	})

	t.Run("should return error when worker fails", func(t *testing.T) {
		mockRepo.On("Save", ctx, mock.AnythingOfType("entities.Strategy")).Return(nil).Once()
		mockWorker.On("EnqueueStrategyTask", mock.AnythingOfType("entities.Strategy")).Return(errors.New("worker error")).Once()

		err := strategyUC.Save(ctx, strategy)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "worker error")

		mockRepo.AssertExpectations(t)
		mockWorker.AssertExpectations(t)
	})
}
