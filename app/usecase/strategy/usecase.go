package usecase

import (
	"context"
	"fmt"
	"go-trade-bot/app/entities"
	"go-trade-bot/app/strategies"
	"go-trade-bot/internal/customerror"
	"net/http"
	"strings"
	"time"
)

type StrategyRepository interface {
	Save(ctx context.Context, strategy entities.Strategy) error
	GetAll(ctx context.Context) ([]entities.Strategy, error)
	GetByID(ctx context.Context, id uint) (entities.Strategy, error)
	Update(ctx context.Context, strategy entities.Strategy) error
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
	strategy = applyStrategyNameFallback(strategy)
	if err := u.validateStrategy(strategy); err != nil {
		return err
	}
	strategy.CreatedAt = time.Now()
	strategy.UpdatedAt = time.Now()

	if err := u.Repository.Save(ctx, strategy); err != nil {
		return err
	}

	if err := u.Worker.EnqueueStrategyTask(strategy); err != nil {
		return err
	}
	return nil
}

func (u StrategyUseCase) Update(ctx context.Context, strategy entities.Strategy) error {
	strategy = applyStrategyNameFallback(strategy)
	if err := u.validateStrategy(strategy); err != nil {
		return err
	}
	strategy.UpdatedAt = time.Now()

	if err := u.Repository.Update(ctx, strategy); err != nil {
		return err
	}

	return nil
}

func (u StrategyUseCase) Enqueue(ctx context.Context) error {
	strategies, err := u.Repository.GetAll(ctx)
	if err != nil {
		return err
	}

	for _, strategy := range strategies {
		if err := u.Worker.EnqueueStrategyTask(strategy); err != nil {
			return err
		}
	}
	return nil
}

func (u StrategyUseCase) GetByID(ctx context.Context, id uint) (entities.Strategy, error) {
	if id == 0 {
		return entities.Strategy{}, customerror.New(http.StatusBadRequest, "Input a valid ID")
	}

	return u.Repository.GetByID(ctx, id)
}

func (u StrategyUseCase) GetAll(ctx context.Context) ([]entities.Strategy, error) {
	return u.Repository.GetAll(ctx)
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

	// Validation cutover (Spec 06 ADR-005): the closed Algorithm enum switch
	// (entities.IsValidAlgorithm) is replaced by a runtime registry lookup on
	// StrategyName, so adding a new strategy no longer requires a recompile
	// of the entities package.
	if strategy.StrategyName == "" {
		return customerror.New(http.StatusBadRequest, "Strategy has to have a strategy_name")
	}

	if !strategies.Exists(strategy.StrategyName) {
		return customerror.New(http.StatusBadRequest, fmt.Sprintf(
			"Invalid strategy name %q, must be one of: %s", strategy.StrategyName, strings.Join(strategies.Names(), ", "),
		))
	}

	if strategy.StrategyConfiguration.Cycle == 0 {
		return customerror.New(http.StatusBadRequest, "Cycle can't be zero")
	}

	if !entities.IsValidCycle(int(strategy.StrategyConfiguration.Cycle)) {
		return customerror.New(http.StatusBadRequest, "Invalid cycle option")
	}
	return nil
}

// applyStrategyNameFallback mirrors the DTO-layer backward-compat mapping
// (app/handler/web/strategy/dto.go) for callers that construct
// entities.Strategy directly (bypassing the DTO), so validation behaves
// consistently regardless of entry point during the deprecation window.
func applyStrategyNameFallback(strategy entities.Strategy) entities.Strategy {
	if strategy.StrategyName == "" && strategy.Algorithm != "" {
		strategy.StrategyName = string(strategy.Algorithm)
	}
	return strategy
}
