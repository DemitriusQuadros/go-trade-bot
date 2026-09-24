package modules

import (
	repository "go-trade-bot/app/repository/strategy"
	usecase "go-trade-bot/app/usecase/strategy"
	worker "go-trade-bot/app/workers/strategy"

	"go.uber.org/fx"
)

// StrategyModule mirrors cmd/api/modules/strategy.go's wiring exactly -
// app/usecase/agent's save_strategy_script tool is a thin wrapper over this
// SAME *usecase.StrategyUseCase instance (Backend Spec 03 AC#4), never a
// duplicate write path.
var StrategyModule = fx.Module("strategy",
	fx.Provide(
		repository.NewStrategyRepository,
		usecase.NewStrategyUseCase,
		worker.NewStrategyWorker,
		func(s repository.StrategyRepository) usecase.StrategyRepository { return s },
		func(s worker.StrategyWorker) usecase.StrategyWorker { return s },
	),
)
