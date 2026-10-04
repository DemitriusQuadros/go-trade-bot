// Command agent is the isolated agents-platform runtime (agents-platform
// A-02): it consumes ONLY the "agents" asynq queue (agent:run tasks,
// enqueued manually by cmd/api or by its own DB-backed cron scheduler) and
// runs persona-aware agent tool loops. It never runs strategy cycles
// (that is cmd/worker) and never serves the REST API (cmd/api).
//
// Safety invariant: this binary's only exchange client is
// exchange.ReadOnlyClient - PlaceOrder/CancelOrder always fail. It ignores
// MODE/CONFIRM_LIVE entirely (see modules/exchange.go).
package main

import (
	"context"
	"log"
	"time"

	"go-trade-bot/app/entities"
	agentrepo "go-trade-bot/app/repository/agent"
	accountrepo "go-trade-bot/app/repository/account"
	"go-trade-bot/app/repository/agentplatform"
	proposalrepo "go-trade-bot/app/repository/proposal"
	"go-trade-bot/app/strategies"
	strategyscript "go-trade-bot/app/strategies/script"
	agentworker "go-trade-bot/app/workers/agent"
	"go-trade-bot/cmd/agent/modules"

	"go.uber.org/fx"
	"gorm.io/gorm"
)

func main() {
	fx.New(appOptions()).Run()
}

// appOptions is the full fx graph (extracted so a test can fx.ValidateApp it).
func appOptions() fx.Option {
	return fx.Options(
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
		modules.OptimizeModule,
		modules.AgentModule,
		fx.Invoke(func(db *gorm.DB) {
			if err := Migrate(db); err != nil {
				log.Fatalf("failed to migrate database: %v", err)
			}
			// Runs orphaned by a previous crash/deadline show as in progress
			// forever otherwise. The cutoff is older than any live run can
			// be, so another replica's in-flight runs are never touched.
			cutoff := time.Now().Add(-(agentworker.RunTimeout + 5*time.Minute))
			if n, err := agentrepo.MarkOrphanedRuns(context.Background(), db, cutoff); err != nil {
				log.Printf("cmd/agent: failed to mark orphaned agent runs: %v", err)
			} else if n > 0 {
				log.Printf("cmd/agent: marked %d orphaned agent run(s) as interrupted", n)
			}
		}),
		fx.Invoke(RegisterScriptStrategy),
		modules.RuntimeModule,
	)
}

// RegisterScriptStrategy mirrors cmd/mcp/main.go's function of the same
// name (save_strategy_script/run_backtest need strategies.Exists("script")).
func RegisterScriptStrategy(runner *strategyscript.Runner, store strategyscript.ScriptStateStore) {
	strategies.Register("script", func(dbStrategy entities.Strategy) strategies.Strategy {
		return strategyscript.NewScriptStrategy(dbStrategy, store, runner)
	})
}

// Migrate mirrors cmd/api/main.go's entity list (keep all three binaries in
// sync) and seeds the default "Copilot" agent.
func Migrate(db *gorm.DB) error {
	if err := db.AutoMigrate(
		&entities.Strategy{},
		&entities.StrategyExecution{},
		&entities.Signal{},
		&entities.Order{},
		&entities.Account{},
		&entities.Candle{},
		&entities.CandleDataset{},
		&entities.CandleSegment{},
		&entities.CandleChunk{},
		&entities.BacktestRun{},
		&entities.OptimizationRun{},
		&entities.StrategyPerformanceSnapshot{},
		&entities.Settings{},
		&entities.ScriptVersion{},
		&entities.ScriptState{},
		&entities.AgentInstruction{},
		&entities.AgentRun{},
		&entities.Agent{},
		&entities.AgentStrategyBinding{},
		&entities.StrategyMemoryEntry{},
		&entities.AgentReport{},
		&entities.WebhookTarget{},
		&entities.AgentUsage{},
		// Agents platform Phase B-01.
		&entities.StrategyChangeProposal{},
		&entities.DeployGateConfig{},
		// Auth-01 (multi-user) - keep in sync with cmd/api.
		&entities.User{},
		&entities.Session{},
		&entities.UserUsage{},
	); err != nil {
		return err
	}
	// Seed the deploy gate thresholds (idempotent).
	if err := proposalrepo.NewGormRepository(db).EnsureGateConfig(context.Background()); err != nil {
		return err
	}
	if _, err := agentplatform.NewGormRepository(db).EnsureDefaultAgent(context.Background()); err != nil {
		return err
	}
	if err := accountrepo.NewAccountRepository(db).EnsureDefaultAccounts(); err != nil {
		return err
	}
	// Seed the empty house-rules row (idempotent, fix-02 B4).
	return agentrepo.NewGormRepository(db).EnsureInstruction(context.Background())
}
