package modules

import (
	scriptstate_repo "go-trade-bot/app/repository/scriptstate"
	"go-trade-bot/app/strategies/script"
	"go-trade-bot/internal/metrics"

	"go.uber.org/fx"
	"gorm.io/gorm"
)

// ScriptModule mirrors cmd/api/modules/script.go / cmd/worker/modules/script.go.
// cmd/mcp needs the "script" strategy registered in the global registry
// (see main.go's RegisterScriptStrategy) for the exact same reason cmd/api
// does: app/usecase/strategy.StrategyUseCase.Save/Update's validateStrategy
// calls strategies.Exists("script") - without this, save_strategy_script
// would fail validation even though it always sets StrategyName: "script".
var ScriptModule = fx.Module("script",
	fx.Provide(
		func(collector *metrics.MetricsCollector) *script.Runner {
			return script.NewRunner(script.DefaultHookTimeout, collector)
		},
		func(db *gorm.DB) script.ScriptStateStore {
			return script.NewScriptStateStore(scriptstate_repo.NewRepository(db))
		},
	),
)
