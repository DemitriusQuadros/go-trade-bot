package modules

import (
	repository "go-trade-bot/app/repository/signal"
	usecase "go-trade-bot/app/usecase/signal"
	"go-trade-bot/internal/broker"

	"go.uber.org/fx"
)

var SignalModule = fx.Module("signal",
	fx.Provide(
		repository.NewSignalRepository,
		usecase.NewSignalUseCase,
		func(s broker.Broker) usecase.Broker { return s },
		func(s repository.SignalRepository) usecase.SignalRepository { return s },
	),
)
