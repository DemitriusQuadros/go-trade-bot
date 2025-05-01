package modules

import (
	repository "go-trade-bot/app/repository/strategy"
	worker "go-trade-bot/app/workers/strategy"

	"go.uber.org/fx"
)

var StrategyModule = fx.Module("strategy",
	fx.Provide(
		worker.NewStrategyWorker,
		repository.NewStrategyRepository,
	),
)
