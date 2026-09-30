package modules

import (
	"context"
	"sort"
	"time"

	"go-trade-bot/app/entities"
	repoagent "go-trade-bot/app/repository/agent"
	"go-trade-bot/app/repository/agentplatform"
	candle_repo "go-trade-bot/app/repository/candle"
	snapshot_repo "go-trade-bot/app/repository/performancesnapshot"
	proposalrepo "go-trade-bot/app/repository/proposal"
	settings_repo "go-trade-bot/app/repository/settings"
	signalrepo "go-trade-bot/app/repository/signal"
	strategy_repo "go-trade-bot/app/repository/strategy"
	strategyscript "go-trade-bot/app/strategies/script"
	agentusecase "go-trade-bot/app/usecase/agent"
	backtestusecase "go-trade-bot/app/usecase/backtest"
	optimizeusecase "go-trade-bot/app/usecase/optimize"
	signalusecase "go-trade-bot/app/usecase/signal"
	strategyusecase "go-trade-bot/app/usecase/strategy"
	optimizeworker "go-trade-bot/app/workers/optimize"
	"go-trade-bot/internal/configuration"
	"go-trade-bot/internal/modelprovider"
	"go-trade-bot/internal/report/agentreport"

	"github.com/hibiken/asynq"
	"go.uber.org/fx"
	"gorm.io/gorm"
)

// AgentModule wires app/usecase/agent.AgentUseCase (Backend Spec 03) against
// the SAME strategy/backtest/signal/snapshot usecase constructors cmd/api
// uses (via this package's StrategyModule/BacktestModule/SignalModule) -
// exactly as Backend Spec 04 requires.
var AgentModule = fx.Module("agent",
	fx.Provide(
		func(db *gorm.DB) repoagent.Repository {
			return repoagent.NewGormRepository(db)
		},
		func(db *gorm.DB) snapshot_repo.Repository {
			return snapshot_repo.NewSnapshotRepository(db)
		},
		// Interface adapters - convert concrete/other-shaped usecases into
		// the narrow local interfaces app/usecase/agent defines for itself.
		func(s strategyusecase.StrategyUseCase) agentusecase.StrategyUseCase { return s },
		func(u *backtestusecase.BacktestUseCase) agentusecase.BacktestUseCase { return u },
		func(s signalusecase.SignalUseCase) agentusecase.SignalUseCase { return signalUseCaseAdapter{s} },
		func(strategyRepo strategy_repo.StrategyRepository, snapshotRepo snapshot_repo.Repository) agentusecase.PerformanceSnapshotUseCase {
			return snapshotUseCaseAdapter{strategyRepo: strategyRepo, snapshotRepo: snapshotRepo}
		},
		func(o *optimizeusecase.OptimizeUseCase) agentusecase.OptimizeUseCase { return o },
		func(w optimizeworker.OptimizeWorker) agentusecase.OptimizeWorker { return w },
		func(
			model modelprovider.ModelProvider,
			repo repoagent.Repository,
			strategy agentusecase.StrategyUseCase,
			backtest agentusecase.BacktestUseCase,
			signal agentusecase.SignalUseCase,
			snapshot agentusecase.PerformanceSnapshotUseCase,
			optimize agentusecase.OptimizeUseCase,
			optimizeWorker agentusecase.OptimizeWorker,
			cfg *configuration.Configuration,
			db *gorm.DB,
			bt *backtestusecase.BacktestUseCase,
			runner *strategyscript.Runner,
		) *agentusecase.AgentUseCase {
			uc := agentusecase.NewAgentUseCase(model, repo, strategy, backtest, signal, snapshot)
			uc.Optimize = optimize
			uc.OptimizeWorker = optimizeWorker
			// Agents platform (A-02 §5 "MCP"): the memory/report tools are
			// exposed over MCP and act as the DEFAULT agent (its permissions
			// filter the registered tools; write_report attributes to it).
			// Notify is never exposed over MCP, so no notifier is wired.
			// Lock is deliberately nil here: no strategy writer locking for
			// MCP-originated save_strategy_script calls (documented choice -
			// cmd/mcp has no Redis dependency today). No Guard either: MCP
			// tool calls never run the model loop.
			uc.Platform = agentplatform.NewGormRepository(db)
			uc.Reports = agentreport.NewHTMLRenderer(agentplatform.NewReportDataSource(db))
			uc.APIBaseURL = cfg.APIBaseURL
			// i18n-02: write_report's RenderedHTML is the DefaultLocale snapshot.
			uc.Locales = settings_repo.NewDefaultLocaleSource(settings_repo.NewRepository(db))
			// Phase B-01: gated deploys, challengers, proposals (acting as
			// the default agent, like every MCP tool call).
			uc.WirePhaseB(bt, candle_repo.NewCandleRepository(db), proposalrepo.NewGormRepository(db), signalrepo.NewSignalRepository(db), runner)
			uc.Inspector = asynq.NewInspector(asynq.RedisClientOpt{Addr: cfg.Redis.Addr})
			uc.ExecutionReader = strategy_repo.NewStrategyRepository(db)
			uc.Provider = cfg.Agent.Provider
			if cfg.Agent.Provider == "anthropic" {
				uc.ModelName = cfg.Agent.AnthropicModel
			} else if cfg.Agent.Provider == "gemini" {
				uc.ModelName = cfg.Agent.GeminiModel
			}
			if uc.ModelName == "" {
				uc.ModelName = modelprovider.DefaultModelFor(uc.Provider)
			}
			return uc
		},
	),
)

// signalUseCaseAdapter bridges app/usecase/signal.SignalUseCase's existing
// GetAllOpen method onto the agent package's GetOpenSignals interface
// method name - no new signal-layer code, just a naming adapter.
type signalUseCaseAdapter struct {
	inner signalusecase.SignalUseCase
}

func (a signalUseCaseAdapter) GetOpenSignals(ctx context.Context) ([]entities.Signal, error) {
	return a.inner.GetAllOpen(ctx)
}

// snapshotUseCaseAdapter implements agentusecase.PerformanceSnapshotUseCase
// on top of the existing performancesnapshot.Repository (which only
// exposes a per-symbol ListDaily, not a per-strategy query) by fetching the
// strategy's MonitoredSymbols and merging each symbol's daily rows. No
// repository signature changes - this is read-only aggregation in the
// caller.
type snapshotUseCaseAdapter struct {
	strategyRepo strategy_repo.StrategyRepository
	snapshotRepo snapshot_repo.Repository
}

func (a snapshotUseCaseAdapter) ListByStrategy(ctx context.Context, strategyID uint, limit int) ([]entities.StrategyPerformanceSnapshot, error) {
	strat, err := a.strategyRepo.GetByID(ctx, strategyID)
	if err != nil {
		return nil, err
	}

	to := time.Now().UTC().AddDate(0, 0, 1)
	from := to.AddDate(-1, 0, -1) // roughly a trailing year, ample for a "recent snapshots" tool

	var all []entities.StrategyPerformanceSnapshot
	for _, symbol := range strat.MonitoredSymbols {
		rows, err := a.snapshotRepo.ListDaily(ctx, strategyID, symbol, from, to)
		if err != nil {
			return nil, err
		}
		all = append(all, rows...)
	}

	sort.Slice(all, func(i, j int) bool { return all[i].PeriodStart.After(all[j].PeriodStart) })
	if limit > 0 && len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}
