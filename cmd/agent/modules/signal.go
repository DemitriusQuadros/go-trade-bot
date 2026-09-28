package modules

import (
	repository "go-trade-bot/app/repository/signal"
	usecase "go-trade-bot/app/usecase/signal"

	"go.uber.org/fx"
)

// SignalModule mirrors cmd/mcp/modules/signal.go. cmd/agent only reaches
// this use case through app/usecase/agent's read-only get_open_positions
// tool (SignalUseCase.GetAllOpen). SignalUseCase is built against the
// READ-ONLY exchange client (see exchange.go): even if a future code path
// called GenerateBuySignal/GenerateSellSignal/Close here, PlaceOrder and
// CancelOrder would fail with exchange.ErrReadOnlyClient.
var SignalModule = fx.Module("signal",
	fx.Provide(
		repository.NewSignalRepository,
		usecase.NewDefaultPositionSizer,
		usecase.NewSignalUseCase,
		func(s repository.SignalRepository) usecase.SignalRepository { return s },
	),
)
