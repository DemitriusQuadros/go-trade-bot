package modules

import (
	repository "go-trade-bot/app/repository/candleimport"
	usecase "go-trade-bot/app/usecase/candleimport"
	worker "go-trade-bot/app/workers/candleimport"
	handler "go-trade-bot/app/handler/web/candleimport"

	"go.uber.org/fx"
)

var CandleImportModule = fx.Module("candleimport",
	fx.Provide(
		repository.NewRepository,
		worker.NewWorker,
		usecase.NewCandleImportUseCase,
		handler.NewCandleImportHandler,
	),
)
