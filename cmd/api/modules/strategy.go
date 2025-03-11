package modules

import (
	handler "go-trade-bot/app/handler/web/strategy"
	repository "go-trade-bot/app/repository/strategy"
	usecase "go-trade-bot/app/usecase/strategy"
	worker "go-trade-bot/app/workers/strategy"

	"go.uber.org/fx"
)

var StrategyModule = fx.Module("strategy",
	fx.Provide(
		repository.NewStrategyRepository,
		usecase.NewStrategyUseCase,
		worker.NewStrategyWorker,
		func(s repository.StrategyRepository) usecase.StrategyRepository { return s },
		func(s usecase.StrategyUseCase) handler.UseCase { return s },
		func(s worker.StrategyWorker) usecase.StrategyWorker { return s },
	),
)
