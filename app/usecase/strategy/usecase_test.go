package usecase_test

import (
	"context"
	"errors"
	"go-trade-bot/app/entities"
	"go-trade-bot/app/strategies"
	usecase "go-trade-bot/app/usecase/strategy"
	"go-trade-bot/app/usecase/strategy/mocks"

	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// This test package doesn't blank-import the real app/strategies/grid
// package (that would couple usecase tests to the concrete Spec 08 ports),
// so it registers a stand-in "grid" factory here purely to exercise the
// registry-backed validation path (Spec 06) with the same "grid" name the
// fixtures below use via entities.Grid.
func init() {
	strategies.Register("grid", func(_ entities.Strategy) strategies.Strategy { return nil })
	// "script" is registered as a stand-in (nil factory - validateStrategy
	// never constructs, only checks Exists + ScriptSource) so the script
	// empty-source validation branch (backend-04) can be exercised here.
	strategies.Register("script", func(_ entities.Strategy) strategies.Strategy { return nil })
}

func TestStrategyUseCase_Save_ScriptEmptySourceRejected(t *testing.T) {
	mockRepo := new(mocks.StrategyRepository)
	strategyUC := usecase.NewStrategyUseCase(mockRepo, nil)

	strat := entities.Strategy{
		Name:             "My Script",
		Description:      "A scripted strategy",
		MonitoredSymbols: []string{"BTCUSDT"},
		StrategyName:     "script",
		ScriptSource:     "   ",
		StrategyConfiguration: entities.StrategyConfiguration{
			Cycle: 10,
		},
	}

	err := strategyUC.Save(context.Background(), strat)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Script source cannot be empty")
	mockRepo.AssertNotCalled(t, "Save", mock.Anything, mock.Anything)
}

func TestStrategyUseCase_Save_ScriptWithSourceAccepted(t *testing.T) {
	mockRepo := new(mocks.StrategyRepository)
	mockWorker := new(mocks.StrategyWorker)
	strategyUC := usecase.NewStrategyUseCase(mockRepo, mockWorker)

	strat := entities.Strategy{
		Name:             "My Script",
		Description:      "A scripted strategy",
		MonitoredSymbols: []string{"BTCUSDT"},
		StrategyName:     "script",
		ScriptSource:     "function should_long(ctx) return false end",
		StrategyConfiguration: entities.StrategyConfiguration{
			Cycle: 10,
		},
	}

	mockRepo.On("Save", mock.Anything, mock.Anything).Return(nil).Once()
	mockWorker.On("EnqueueStrategyTask", mock.Anything).Return(nil).Once()
	mockRepo.On("SaveScriptVersion", mock.Anything, mock.Anything).Return(nil).Once()

	err := strategyUC.Save(context.Background(), strat)
	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockWorker.AssertExpectations(t)
}

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
			StrategyName:     "grid",
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
		StrategyName:     "grid",
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
		StrategyName:     "grid",
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
func TestStrategyUseCase_GetByID(t *testing.T) {
	mockRepo := new(mocks.StrategyRepository)
	strategyUC := usecase.NewStrategyUseCase(mockRepo, nil)

	ctx := context.Background()
	strategy := entities.Strategy{
		ID:               1,
		Name:             "Test Strategy",
		Description:      "A test strategy",
		MonitoredSymbols: []string{"BTCUSDT", "ETHUSDT"},
		StrategyName:     "grid",
		StrategyConfiguration: entities.StrategyConfiguration{
			Cycle: 10,
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	t.Run("should return strategy by ID", func(t *testing.T) {
		mockRepo.On("GetByID", ctx, uint(1)).Return(strategy, nil).Once()

		result, err := strategyUC.GetByID(ctx, 1)
		assert.NoError(t, err)
		assert.Equal(t, strategy, result)

		mockRepo.AssertExpectations(t)
	})

	t.Run("should return error when repository fails", func(t *testing.T) {
		mockRepo.On("GetByID", ctx, uint(1)).Return(entities.Strategy{}, errors.New("database error")).Once()

		result, err := strategyUC.GetByID(ctx, 1)
		assert.Error(t, err)
		assert.Equal(t, entities.Strategy{}, result)
		assert.Contains(t, err.Error(), "database error")

		mockRepo.AssertExpectations(t)
	})
}

func TestStrategyUseCase_Update(t *testing.T) {
	mockRepo := new(mocks.StrategyRepository)
	strategyUC := usecase.NewStrategyUseCase(mockRepo, nil)

	ctx := context.Background()
	strategy := entities.Strategy{
		ID:               1,
		Name:             "Test Strategy",
		Description:      "A test strategy",
		MonitoredSymbols: []string{"BTCUSDT", "ETHUSDT"},
		StrategyName:     "grid",
		StrategyConfiguration: entities.StrategyConfiguration{
			Cycle: 10,
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	t.Run("should update strategy successfully", func(t *testing.T) {
		mockRepo.On("Update", ctx, mock.AnythingOfType("entities.Strategy")).Return(nil).Once()

		err := strategyUC.Update(ctx, strategy)
		assert.NoError(t, err)

		mockRepo.AssertExpectations(t)
	})

	t.Run("should return error when strategy name is empty", func(t *testing.T) {
		invalidStrategy := strategy
		invalidStrategy.Name = ""

		err := strategyUC.Update(ctx, invalidStrategy)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "Strategy has to have a name")
	})

	t.Run("should return error when repository fails", func(t *testing.T) {
		mockRepo.On("Update", ctx, mock.AnythingOfType("entities.Strategy")).Return(errors.New("database error")).Once()

		err := strategyUC.Update(ctx, strategy)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "database error")

		mockRepo.AssertExpectations(t)
	})
}

func TestStrategyUseCase_UpdateStatus(t *testing.T) {
	mockRepo := new(mocks.StrategyRepository)
	strategyUC := usecase.NewStrategyUseCase(mockRepo, nil)
	ctx := context.Background()

	t.Run("should update status successfully", func(t *testing.T) {
		current := entities.Strategy{ID: 1, Name: "Test", StrategyName: "grid", Status: entities.Productive}
		mockRepo.On("GetByID", ctx, uint(1)).Return(current, nil).Once()
		mockRepo.On("Update", ctx, mock.MatchedBy(func(s entities.Strategy) bool {
			return s.ID == 1 && s.Status == entities.Disabled
		})).Return(nil).Once()

		updated, err := strategyUC.UpdateStatus(ctx, 1, entities.Disabled)
		assert.NoError(t, err)
		assert.Equal(t, entities.Disabled, updated.Status)
		mockRepo.AssertExpectations(t)
	})

	t.Run("should return error on invalid status", func(t *testing.T) {
		_, err := strategyUC.UpdateStatus(ctx, 1, entities.StrategyStatus("invalid_status"))
		assert.Error(t, err)
	})

	t.Run("should return error when strategy not found", func(t *testing.T) {
		mockRepo.On("GetByID", ctx, uint(99)).Return(entities.Strategy{}, errors.New("not found")).Once()
		_, err := strategyUC.UpdateStatus(ctx, 99, entities.Disabled)
		assert.Error(t, err)
		mockRepo.AssertExpectations(t)
	})

	// Fix 3 (registry-existence check): UpdateStatus was bypassing the same
	// StrategyName registry check Save/Update enforce - a strategy persisted
	// under a StrategyName that's no longer registered (e.g. deleted from
	// the codebase) must not be allowed to silently transition status.
	t.Run("should return error when strategy_name is no longer registered", func(t *testing.T) {
		freshRepo := new(mocks.StrategyRepository)
		freshUC := usecase.NewStrategyUseCase(freshRepo, nil)
		current := entities.Strategy{ID: 2, Name: "Stale", StrategyName: "no-longer-registered", Status: entities.Productive}
		freshRepo.On("GetByID", ctx, uint(2)).Return(current, nil).Once()

		_, err := freshUC.UpdateStatus(ctx, 2, entities.Disabled)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "must be one of:")
		freshRepo.AssertNotCalled(t, "Update", mock.Anything, mock.Anything)
	})
}

func TestStrategyUseCase_UpdateMode(t *testing.T) {
	mockRepo := new(mocks.StrategyRepository)
	strategyUC := usecase.NewStrategyUseCase(mockRepo, nil)
	ctx := context.Background()

	t.Run("should update mode successfully", func(t *testing.T) {
		current := entities.Strategy{ID: 1, Name: "Test", Mode: "dryrun"}
		mockRepo.On("GetByID", ctx, uint(1)).Return(current, nil).Once()
		mockRepo.On("Update", ctx, mock.MatchedBy(func(s entities.Strategy) bool {
			return s.ID == 1 && s.Mode == "paper"
		})).Return(nil).Once()

		updated, err := strategyUC.UpdateMode(ctx, 1, "paper")
		assert.NoError(t, err)
		assert.Equal(t, "paper", updated.Mode)
		mockRepo.AssertExpectations(t)
	})

	t.Run("should return error on unparseable mode", func(t *testing.T) {
		_, err := strategyUC.UpdateMode(ctx, 1, "unsupported_mode")
		assert.Error(t, err)
	})
}

func TestStrategyUseCase_GetPerformance(t *testing.T) {
	mockRepo := new(mocks.StrategyRepository)
	strategyUC := usecase.NewStrategyUseCase(mockRepo, nil)
	ctx := context.Background()

	perfs := []entities.StrategyPerformance{
		{Name: "grid-btc", Symbol: "BTCUSDT", Profit: 50.0, Trades: 4},
	}
	mockRepo.On("GetStrategyPerformanceBySymbol", ctx).Return(perfs).Once()

	res, err := strategyUC.GetPerformance(ctx)
	assert.NoError(t, err)
	assert.Equal(t, perfs, res)
	mockRepo.AssertExpectations(t)
}
