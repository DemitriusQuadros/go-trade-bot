package modules

import (
	candle_repo "go-trade-bot/app/repository/candle"

	tasks "go-trade-bot/app/handler/tasks/optimize"
	optimize_repo "go-trade-bot/app/repository/optimize"
	strategy_repo "go-trade-bot/app/repository/strategy"
	backtest_usecase "go-trade-bot/app/usecase/backtest"
	usecase "go-trade-bot/app/usecase/optimize"
	worker "go-trade-bot/app/workers/optimize"

	"go.uber.org/fx"
	"gorm.io/gorm"
)

// OptimizeModule wires backend-01/02's grid-search job on the worker side:
// the asynq task processor that actually runs OptimizeUseCase.Run.
var OptimizeModule = fx.Module("optimize",
	fx.Provide(
		worker.NewOptimizeWorker,
		func(db *gorm.DB) usecase.OptimizationRepository {
			return optimize_repo.NewOptimizationRepository(db)
		},
		func(db *gorm.DB) usecase.StrategyRepository {
			return strategy_repo.NewStrategyRepository(db)
		},
		func(bt *backtest_usecase.BacktestUseCase) usecase.BacktestRunner {
			return bt
		},
		func(bt usecase.BacktestRunner, stratRepo usecase.StrategyRepository, optimizeRepo usecase.OptimizationRepository, c candle_repo.Repository) *usecase.OptimizeUseCase {
			return usecase.NewOptimizeUseCase(bt, stratRepo, optimizeRepo, c, usecase.DefaultMaxCombinations)
		},
		func(u *usecase.OptimizeUseCase) tasks.OptimizeUseCase { return u },
		tasks.NewOptimizeProcessor,
	),
)
