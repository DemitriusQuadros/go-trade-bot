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
	candledata_worker "go-trade-bot/app/workers/candledata"
	"go-trade-bot/internal/candlesource"
	"go-trade-bot/internal/exchange"

	"go.uber.org/fx"
	"gorm.io/gorm"
)

// MCPToolsModule wires the in-process MCP adapter (app/handler/mcptools) onto
// the same usecases cmd/api uses. Only the methods listed in that package's
// ports are reachable. Where cmd/api wires a collaborator that no allowed
// method touches (the agent run enqueuer, the webhook test sender) it is
// left nil here on purpose, so e.g. running an agent is not just unlisted but
// impossible from this process.
var MCPToolsModule = fx.Module("mcptools",
	fx.Provide(
		candledata_repo.NewRepository,
		candledata_worker.NewEnqueuer,
		newMCPCandleDataService,
		func(db *gorm.DB, strategies strategy_repo.StrategyRepository) *platformusecase.UseCase {
			return platformusecase.NewUseCase(
				agentplatform.NewGormRepository(db), strategies, repoagent.NewGormRepository(db),
				nil, // no run enqueuer: manual agent runs are REST-only
				settings_repo.NewRepository(db),
				nil, // no webhook test sender
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
		) mcptools.Deps {
			return mcptools.Deps{
				Settings:   settings_repo.NewRepository(db),
				Accounts:   acc,
				Signals:    sig,
				Versions:   strat,
				MonteCarlo: bt,
				Market:     ex,
				Datasets:   ds,
				Scripts:    sc,
				Agents:     ag,
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
