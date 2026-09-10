package modules

import (
	scripthandler "go-trade-bot/app/handler/web/script"
	candle_repo "go-trade-bot/app/repository/candle"
	strategy_repo "go-trade-bot/app/repository/strategy"
	strategyscript "go-trade-bot/app/strategies/script"
	scriptusecase "go-trade-bot/app/usecase/script"
	"go-trade-bot/internal/exchange"
	"go-trade-bot/internal/metrics"

	"go.uber.org/fx"
)

// ScriptModule wires the REPL/fast-rerun usecase and handler (backend-08),
// bridging the existing cmd/api DI graph (ExchangeClient, candle repo, strategy
// repo) into the usecase's narrow source interfaces.
var ScriptModule = fx.Module("script",
	fx.Provide(
		func(collector *metrics.MetricsCollector) *strategyscript.Runner {
			return strategyscript.NewRunner(strategyscript.DefaultHookTimeout, collector)
		},
		scriptusecase.NewUseCase,
		// Interface adapters: concrete DI types -> the usecase's narrow sources.
		func(c exchange.ExchangeClient) scriptusecase.LiveCandleSource { return c },
		func(r candle_repo.Repository) scriptusecase.HistoricalCandleSource { return r },
		func(r strategy_repo.StrategyRepository) scriptusecase.StrategyRepository { return r },
		// Usecase -> handler's local UseCase interface.
		func(u *scriptusecase.UseCase) scripthandler.UseCase { return u },
	),
)
