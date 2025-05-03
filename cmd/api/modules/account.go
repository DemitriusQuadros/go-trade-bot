package modules

import (
	handler "go-trade-bot/app/handler/web/account"
	repository "go-trade-bot/app/repository/account"
	usecase "go-trade-bot/app/usecase/account"

	"go.uber.org/fx"
)

var AccountModule = fx.Module("account",
	fx.Provide(
		repository.NewAccountRepository,
		usecase.NewAccountUseCase,
		func(s repository.AccountRepository) usecase.AccountRepository { return s },
		func(s *usecase.AccountUseCase) handler.UseCase { return s },
	),
)
