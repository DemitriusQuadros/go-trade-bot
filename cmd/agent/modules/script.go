package modules

import (
	scriptstate_repo "go-trade-bot/app/repository/scriptstate"
	"go-trade-bot/app/strategies/script"
	"go-trade-bot/internal/metrics"

	"go.uber.org/fx"
	"gorm.io/gorm"
)

// ScriptModule mirrors cmd/mcp/modules/script.go: the "script" strategy
// must be registered (main.go's RegisterScriptStrategy) so
// save_strategy_script/run_backtest pass strategies.Exists("script").
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
