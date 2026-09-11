package modules

import (
	scripthandler "go-trade-bot/app/handler/web/script"
	candle_repo "go-trade-bot/app/repository/candle"
	scriptstate_repo "go-trade-bot/app/repository/scriptstate"
	strategy_repo "go-trade-bot/app/repository/strategy"
	strategyscript "go-trade-bot/app/strategies/script"
	scriptusecase "go-trade-bot/app/usecase/script"
	"go-trade-bot/internal/exchange"
	"go-trade-bot/internal/metrics"

	"go.uber.org/fx"
	"gorm.io/gorm"
)

// ScriptModule wires the REPL/fast-rerun usecase and handler (backend-08),
// bridging the existing cmd/api DI graph (ExchangeClient, candle repo, strategy
// repo) into the usecase's narrow source interfaces. It also provides the
// same *strategyscript.Runner and DB-backed ScriptStateStore cmd/worker's
// ScriptModule provides, so main.go can register the "script" strategy into
// the registry the same way cmd/worker/main.go does (RegisterScriptStrategy)
// - without this, saving a script-type strategy fails validation on the API
// side even though the worker could execute it.
var ScriptModule = fx.Module("script",
	fx.Provide(
		func(collector *metrics.MetricsCollector) *strategyscript.Runner {
			return strategyscript.NewRunner(strategyscript.DefaultHookTimeout, collector)
		},
		func(db *gorm.DB) strategyscript.ScriptStateStore {
			return strategyscript.NewScriptStateStore(scriptstate_repo.NewRepository(db))
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
