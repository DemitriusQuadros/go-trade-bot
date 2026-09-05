package modules

import (
	repository "go-trade-bot/app/repository/signal"
	usecase "go-trade-bot/app/usecase/signal"

	"go.uber.org/fx"
)

var SignalModule = fx.Module("signal",
	fx.Provide(
		repository.NewSignalRepository,
		usecase.NewSignalUseCase,
		func(s repository.SignalRepository) usecase.SignalRepository { return s },
	),
)
