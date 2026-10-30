package modules

import (
	"os"

	"go-trade-bot/app/handler/mcptools"
	repoagent "go-trade-bot/app/repository/agent"
	"go-trade-bot/app/repository/agentplatform"
	candle_repo "go-trade-bot/app/repository/candle"
	candledata_repo "go-trade-bot/app/repository/candledata"
	settings_repo "go-trade-bot/app/repository/settings"
	strategy_repo "go-trade-bot/app/repository/strategy"
	strategyscript "go-trade-bot/app/strategies/script"
	accountusecase "go-trade-bot/app/usecase/account"
	platformusecase "go-trade-bot/app/usecase/agentplatform"
	backtestusecase "go-trade-bot/app/usecase/backtest"
	candledatausecase "go-trade-bot/app/usecase/candledata"
	scriptusecase "go-trade-bot/app/usecase/script"
	signalusecase "go-trade-bot/app/usecase/signal"
	strategyusecase "go-trade-bot/app/usecase/strategy"
	agentworker "go-trade-bot/app/workers/agent"
	backtestworker "go-trade-bot/app/workers/backtest"
	candledata_worker "go-trade-bot/app/workers/candledata"
	"go-trade-bot/internal/candlesource"
	"go-trade-bot/internal/configuration"
	"go-trade-bot/internal/exchange"

	"go.uber.org/fx"
	"gorm.io/gorm"
)

// MCPToolsModule wires the in-process MCP adapter (app/handler/mcptools) onto
// the same usecases cmd/api uses. Only the methods listed in that package's
// ports are reachable. The webhook test sender is left nil on purpose:
// webhook targets are read-only over MCP.
var MCPToolsModule = fx.Module("mcptools",
	fx.Provide(
		candledata_repo.NewRepository,
		candledata_worker.NewEnqueuer,
		newMCPCandleDataService,
		agentworker.NewAgentWorker,
		func(db *gorm.DB, strategies strategy_repo.StrategyRepository, worker agentworker.AgentWorker) *platformusecase.UseCase {
			return platformusecase.NewUseCase(
				agentplatform.NewGormRepository(db), strategies, repoagent.NewGormRepository(db),
				worker, // run_agent enqueues agent:run on the "agents" queue (consumed by cmd/agent)
				settings_repo.NewRepository(db),
				nil, // no webhook test sender: webhook targets are read-only here
			)
		},
		func(c exchange.ExchangeClient, r candle_repo.Repository, s strategy_repo.StrategyRepository, runner *strategyscript.Runner) *scriptusecase.UseCase {
			return scriptusecase.NewUseCase(c, r, s, runner)
		},
		func(
			db *gorm.DB,
			acc *accountusecase.AccountUseCase,
			sig signalusecase.SignalUseCase,
			strat strategyusecase.StrategyUseCase,
			bt *backtestusecase.BacktestUseCase,
			ex exchange.ExchangeClient,
			ds *candledatausecase.Service,
			sc *scriptusecase.UseCase,
			ag *platformusecase.UseCase,
			bw backtestworker.BacktestWorker,
			cfg *configuration.Configuration,
		) mcptools.Deps {
			return mcptools.Deps{
				Settings:    settings_repo.NewRepository(db),
				Accounts:    acc,
				Signals:     sig,
				Versions:    strat,
				MonteCarlo:  bt,
				Market:      ex,
				Datasets:    ds,
				Scripts:     sc,
				Agents:      ag,
				Webhooks:    ag,
				WalkForward: bt,
				Tasks:       bw,
				Reports:     ag,
				Backtests:   bt,
				BaseURL:     cfg.APIBaseURL,
			}
		},
	),
)

// newMCPCandleDataService mirrors cmd/api's NewCandleDataService: it only
// plans and enqueues (chunks execute in cmd/worker), so there is no source
// runner here.
func newMCPCandleDataService(
	store candledata_repo.Repository,
	enq *candledata_worker.Enqueuer,
	client exchange.ExchangeClient,
	db *gorm.DB,
) *candledatausecase.Service {
	fetcher, _ := client.(exchange.HistoricalKlineFetcher)
	host, _ := os.Hostname()
	svc := candledatausecase.NewService(store, nil, candlesource.NewListingResolver(nil, fetcher), enq, "mcp-"+host)
	svc.Scanner = candle_repo.NewCandleRepository(db)
	return svc
}
