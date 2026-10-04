package modules

import (
	candle_repo "go-trade-bot/app/repository/candle"

	optimize_repo "go-trade-bot/app/repository/optimize"
	strategy_repo "go-trade-bot/app/repository/strategy"
	backtest_usecase "go-trade-bot/app/usecase/backtest"
	usecase "go-trade-bot/app/usecase/optimize"
	worker "go-trade-bot/app/workers/optimize"

	"go.uber.org/fx"
	"gorm.io/gorm"
)

// OptimizeModule wires backend-01/02's async grid-search job into cmd/mcp,
// mirroring cmd/api/modules/optimize.go exactly - previously absent here
// entirely, which is why the agent/MCP tool registry had no way to reach
// optimization runs even though they were already fully persisted and
// REST-queryable. cmd/mcp only needs the same enqueue/read half cmd/api
// does; cmd/worker's OptimizeProcessor is still the only place a grid
// search actually executes.
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
	),
)
