package usecase

import (
	"context"
	"go-trade-bot/app/entities"
	"go-trade-bot/internal/customerror"
	"net/http"
	"time"

	"github.com/google/uuid"
)

type StrategyRepository interface {
	Save(ctx context.Context, symbol entities.Strategy) error
}

type StrategyWorker interface {
	EnqueueStrategyTask(strategy entities.Strategy) error
}

type StrategyUseCase struct {
	Repository StrategyRepository
	Worker     StrategyWorker
}

func NewStrategyUseCase(repository StrategyRepository, worker StrategyWorker) StrategyUseCase {
	return StrategyUseCase{
		Repository: repository,
		Worker:     worker,
	}
}

func (u StrategyUseCase) Save(ctx context.Context, strategy entities.Strategy) error {
	if err := u.validateStrategy(strategy); err != nil {
		return err
	}
	strategy.ID = uuid.New()
	strategy.CreatedAt = time.Now()
	strategy.UpdatedAt = time.Now()
	u.Repository.Save(ctx, strategy)

	if err := u.Worker.EnqueueStrategyTask(strategy); err != nil {
		return err
	}
	return nil
}

func (u StrategyUseCase) validateStrategy(strategy entities.Strategy) error {
	if strategy.Name == "" {
		return customerror.New(http.StatusBadRequest, "Strategy has to have a name")
	}
	if strategy.Description == "" {
		return customerror.New(http.StatusBadRequest, "Description must be filled")
	}

	if len(strategy.MonitoredSymbols) == 0 {
		return customerror.New(http.StatusBadRequest, "Please define a set of symbols to monitor")
	}

	if strategy.Algorithm == "" {
		return customerror.New(http.StatusBadRequest, "Please define a altorigthm to be used")
	}

	if !entities.IsValidAlgorithm(string(strategy.Algorithm)) {
		return customerror.New(http.StatusBadRequest, "Invalid algorithm option")
	}

	if strategy.StrategyConfiguration.Cycle == 0 {
		return customerror.New(http.StatusBadRequest, "Cycle can't be zero")
	}

	if !entities.IsValidCycle(int(strategy.StrategyConfiguration.Cycle)) {
		return customerror.New(http.StatusBadRequest, "Invalid cycle option")
	}
	return nil
}
