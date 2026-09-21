// Command mcp is the fifth binary alongside cmd/api, cmd/worker,
// cmd/backtest, and cmd/candleimport (Backend Spec 04): an independent,
// long-running process hosting an MCP server that exposes the AI strategy
// agent's tools (Backend Spec 03/06) and knowledge resource (Backend Spec
// 05) to external MCP clients (Claude Desktop, other agent harnesses).
//
// It shares app/usecase/* the same way every other entry point does - it
// does not duplicate business logic, and cmd/api/cmd/worker never embed any
// MCP transport code (ADR-A2 in the architecture doc): a crash or resource
// spike in MCP handling can never affect the live-trading API or worker
// processes.
package main

import (
	"context"
	"flag"
	"log"

	"go-trade-bot/app/entities"
	"go-trade-bot/app/strategies"
	strategyscript "go-trade-bot/app/strategies/script"
	"go-trade-bot/cmd/mcp/modules"
	"go-trade-bot/cmd/mcp/server"

	"go.uber.org/fx"
	"gorm.io/gorm"
)

func main() {
	transport := flag.String("transport", "stdio", "MCP transport: stdio | http")
	flag.Parse()

	fx.New(
		modules.ConfigurationModule,
		modules.DbModule,
		modules.MetricsModule,
		modules.ExchangeModule,
		modules.NotifierModule,
		modules.AccountModule,
		modules.CandleModule,
		modules.SignalModule,
		modules.StrategyModule,
		modules.ScriptModule,
		modules.BacktestModule,
		modules.ModelProviderModule,
		modules.AgentModule,
		modules.McpServerModule,
		fx.Invoke(func(db *gorm.DB) {
			// Runs defensively on startup with the same entity list
			// cmd/api migrates (Backend Spec 04 AC#3), so cmd/mcp can be
			// started standalone against a fresh database without
			// depending on cmd/api having run first. AutoMigrate is
			// idempotent - calling it from multiple binaries against the
			// same DB is safe.
			if err := Migrate(db); err != nil {
				log.Fatalf("failed to migrate database: %v", err)
			}
		}),
		fx.Invoke(RegisterScriptStrategy),
		fx.Invoke(func(srv *server.Server, lc fx.Lifecycle) {
			lc.Append(fx.Hook{
				OnStart: func(ctx context.Context) error { return srv.Start(*transport) },
				OnStop:  func(ctx context.Context) error { return srv.Stop(ctx) },
			})
		}),
	).Run()
}

// RegisterScriptStrategy wires the "script" strategy into the global
// registry, mirroring cmd/api/main.go and cmd/worker/main.go's function of
// the same name - required so app/usecase/strategy.StrategyUseCase.Save's
// validateStrategy (called by save_strategy_script, Backend Spec 03) can
// resolve strategies.Exists("script").
func RegisterScriptStrategy(runner *strategyscript.Runner, store strategyscript.ScriptStateStore) {
	strategies.Register("script", func(dbStrategy entities.Strategy) strategies.Strategy {
		return strategyscript.NewScriptStrategy(dbStrategy, store, runner)
	})
}

// Migrate mirrors cmd/api/main.go's Migrate - the same entity list,
// including the Backend Spec 02 additions (AgentInstruction/AgentRun), so
// this binary never depends on cmd/api having started first.
func Migrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&entities.Strategy{},
		&entities.StrategyExecution{},
		&entities.Signal{},
		&entities.Order{},
		&entities.Account{},
		&entities.Candle{},
		&entities.BacktestRun{},
		&entities.OptimizationRun{},
		&entities.StrategyPerformanceSnapshot{},
		&entities.Settings{},
		&entities.ScriptVersion{},
		&entities.ImportJob{},
		&entities.ImportSchedule{},
		&entities.ScriptState{},
		&entities.AgentInstruction{},
		&entities.AgentRun{},
	)
}
