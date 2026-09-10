package modules

import (
	scriptstate_repo "go-trade-bot/app/repository/scriptstate"
	"go-trade-bot/app/strategies/script"
	"go-trade-bot/internal/metrics"

	"go.uber.org/fx"
	"gorm.io/gorm"
)

// ScriptModule provides the Lua scripting engine's fx-constructed dependencies
// (backend-04): the shared *script.Runner (sandboxed VM factory) and the
// DB-backed script.ScriptStateStore. The registry entry for the "script"
// strategy is NOT an init() (unlike native Go strategies) because these two
// dependencies must be fx-constructed first - cmd/worker/main.go wires the
// strategies.Register("script", ...) call via an fx.Invoke over this module's
// outputs.
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
