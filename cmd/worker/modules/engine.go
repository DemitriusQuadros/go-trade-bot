package modules

import (
	"go-trade-bot/app/engine"
	accountusecase "go-trade-bot/app/usecase/account"
	signalusecase "go-trade-bot/app/usecase/signal"

	"go.uber.org/fx"
)

// EngineModule provides app/engine.Engine (Spec 05/08's hook-loop driver)
// and the interface adapters it needs from the already-provided concrete
// usecase types.
var EngineModule = fx.Module("engine",
	fx.Provide(
		engine.NewEngine,
		func(s signalusecase.SignalUseCase) engine.SignalUseCase { return s },
		func(a *accountusecase.AccountUseCase) engine.AccountReader { return a },
	),
)
