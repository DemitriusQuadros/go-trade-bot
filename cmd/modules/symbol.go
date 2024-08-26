package modules

import (
	"go-trade-bot/app/symbol/handler"
	"go-trade-bot/app/symbol/repository"
	"go-trade-bot/app/symbol/usecase"

	"go.uber.org/fx"
)

var SymbolModule = fx.Module("symbol",
	fx.Provide(
		repository.NewSymbolRepository,
		usecase.NewSymbolUseCase,
		func(s repository.SymbolRepository) usecase.SymbolRepository { return s },
		func(s usecase.SymbolUseCase) handler.UseCase { return s },
	),
)
