package modules

import (
	repository "go-trade-bot/app/repository/signal"
	usecase "go-trade-bot/app/usecase/signal"

	"go.uber.org/fx"
)

// SignalModule mirrors cmd/worker/modules/signal.go's wiring. cmd/mcp only
// ever reaches this use case through app/usecase/agent's read-only
// get_open_positions tool (SignalUseCase.GetAllOpen) - no code in cmd/mcp
// calls GenerateBuySignal/GenerateSellSignal/Close, which are the methods
// that place real orders.
var SignalModule = fx.Module("signal",
	fx.Provide(
		repository.NewSignalRepository,
		usecase.NewDefaultPositionSizer,
		usecase.NewSignalUseCase,
		func(s repository.SignalRepository) usecase.SignalRepository { return s },
	),
)
