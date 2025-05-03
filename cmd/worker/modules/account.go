package modules

import (
	repository "go-trade-bot/app/repository/account"
	usecase "go-trade-bot/app/usecase/account"
	signal "go-trade-bot/app/usecase/signal"

	"go.uber.org/fx"
)

var AccountModule = fx.Module("account",
	fx.Provide(
		repository.NewAccountRepository,
		usecase.NewAccountUseCase,
		func(s repository.AccountRepository) usecase.AccountRepository { return s },
		func(s *usecase.AccountUseCase) signal.AccountUseCase { return s },
	),
)
