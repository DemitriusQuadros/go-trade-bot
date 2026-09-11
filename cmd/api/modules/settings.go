package modules

import (
	handler "go-trade-bot/app/handler/web/settings"
	repository "go-trade-bot/app/repository/settings"
	usecase "go-trade-bot/app/usecase/settings"
	"go-trade-bot/internal/exchange"
	"go-trade-bot/internal/notifier"

	"go.uber.org/fx"
)

// SettingsModule wires Spec backend-05 (ADR-016) for the API process:
// cmd/api runs no strategy cycles, so its ProcessorGate is nil (drain is a
// no-op there); the WorkerClient forwards risk-bearing (and, for
// consistency, safe-tier) changes to cmd/worker's internal endpoint so its
// separate in-memory Swappables/StrategyProcessor actually observe the
// change too - see internal/settingsbridge's package doc.
var SettingsModule = fx.Module("settings",
	fx.Provide(
		repository.NewRepository,
		asSettingsRepository,
		provideNilProcessorGate,
		NewWorkerSettingsClient,
		asExchangeSwapper,
		asNotifierSwapper,
		usecase.NewUseCase,
		asSettingsHandlerUseCase,
		handler.NewSettingsHandler,
	),
)

func asSettingsRepository(r repository.Repository) usecase.Repository { return r }

func provideNilProcessorGate() usecase.ProcessorGate { return nil }

func asExchangeSwapper(e *exchange.SwappableExchangeClient) usecase.ExchangeSwapper { return e }

func asNotifierSwapper(n *notifier.SwappableNotifier) usecase.NotifierSwapper { return n }

func asSettingsHandlerUseCase(u *usecase.UseCase) handler.UseCase { return u }
