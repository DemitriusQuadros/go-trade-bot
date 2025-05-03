package usecase_test

import (
	"context"
	"errors"
	"go-trade-bot/app/entities"
	usecase "go-trade-bot/app/usecase/strategy"
	"go-trade-bot/app/usecase/strategy/mocks"

	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestStrategyUseCase_GetAll(t *testing.T) {
	mockRepo := new(mocks.StrategyRepository)
	strategyUC := usecase.NewStrategyUseCase(mockRepo, nil)

	ctx := context.Background()
	strategies := []entities.Strategy{
		{
			ID:               1,
			Name:             "Test Strategy",
			Description:      "A test strategy",
			MonitoredSymbols: []string{"BTCUSDT", "ETHUSDT"},
			Algorithm:        entities.Grid,
			StrategyConfiguration: entities.StrategyConfiguration{
				Cycle: 10,
			},
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
	}

	t.Run("should return all strategies", func(t *testing.T) {
		mockRepo.On("GetAll", ctx).Return(strategies, nil).Once()

		result, err := strategyUC.GetAll(ctx)
		assert.NoError(t, err)
		assert.Equal(t, strategies, result)

		mockRepo.AssertExpectations(t)
	})

	t.Run("should return error when repository fails", func(t *testing.T) {
		mockRepo.On("GetAll", ctx).Return(nil, errors.New("database error")).Once()

		result, err := strategyUC.GetAll(ctx)
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "database error")

		mockRepo.AssertExpectations(t)
	})
}

func TestStrategyUseCase_Enqueue(t *testing.T) {
	mockRepo := new(mocks.StrategyRepository)
	mockWorker := new(mocks.StrategyWorker)
	strategyUC := usecase.NewStrategyUseCase(mockRepo, mockWorker)

	ctx := context.Background()
	strategy := entities.Strategy{
		ID:               1,
		Name:             "Test Strategy",
		Description:      "A test strategy",
		MonitoredSymbols: []string{"BTCUSDT", "ETHUSDT"},
		Algorithm:        entities.Grid,
		StrategyConfiguration: entities.StrategyConfiguration{
			Cycle: 10,
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	t.Run("should enqueue strategy task successfully", func(t *testing.T) {
		mockWorker.On("EnqueueStrategyTask", strategy).Return(nil).Once()
		mockRepo.On("GetAll", ctx).Return([]entities.Strategy{strategy}, nil).Once()

		err := strategyUC.Enqueue(ctx)
		assert.NoError(t, err)

		mockWorker.AssertExpectations(t)
	})

	t.Run("should return error when worker fails", func(t *testing.T) {
		mockWorker.On("EnqueueStrategyTask", strategy).Return(errors.New("worker error")).Once()
		mockRepo.On("GetAll", ctx).Return([]entities.Strategy{strategy}, nil).Once()
		err := strategyUC.Enqueue(ctx)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "worker error")

		mockWorker.AssertExpectations(t)
	})
}
func TestStrategyUseCase_Save(t *testing.T) {
	mockRepo := new(mocks.StrategyRepository)
	mockWorker := new(mocks.StrategyWorker)
	strategyUC := usecase.NewStrategyUseCase(mockRepo, mockWorker)

	ctx := context.Background()
	strategy := entities.Strategy{
		ID:               1,
		Name:             "Test Strategy",
		Description:      "A test strategy",
		MonitoredSymbols: []string{"BTCUSDT", "ETHUSDT"},
		Algorithm:        entities.Grid,
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
