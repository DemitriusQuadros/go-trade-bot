package modules

import (
	handler "go-trade-bot/app/handler/web/signal"
	repository "go-trade-bot/app/repository/signal"
	account "go-trade-bot/app/usecase/account"
	usecase "go-trade-bot/app/usecase/signal"
	"go-trade-bot/internal/broker"

	"go.uber.org/fx"
)

var SignalModule = fx.Module("signal",
	fx.Provide(
		repository.NewSignalRepository,
		usecase.NewSignalUseCase,
		func(b broker.Broker) usecase.Broker { return b },
		func(a *account.AccountUseCase) usecase.AccountUseCase { return a },
		func(s repository.SignalRepository) usecase.SignalRepository { return s },
		func(s usecase.SignalUseCase) handler.UseCase { return s },
	),
)
