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
	agentworker "go-trade-bot/app/workers/agent"
	optimizeworker "go-trade-bot/app/workers/optimize"
	"go-trade-bot/internal/configuration"
	"go-trade-bot/internal/i18n"
	"go-trade-bot/internal/lock"
	"go-trade-bot/internal/modelprovider"
	"go-trade-bot/internal/notifier"
	"go-trade-bot/internal/report/agentreport"

	"github.com/hibiken/asynq"
	"go.uber.org/fx"
	"gorm.io/gorm"
)

// AgentModule wires app/usecase/agent.AgentUseCase for cmd/agent with every
// agents-platform dependency: persona repository, per-persona provider
// factory, report renderer, multi-target webhook notifier, RunGuard (kill
// switch / pause / budget) and the Redis strategy writer lock.
var AgentModule = fx.Module("agent",
	fx.Provide(
		func(db *gorm.DB) repoagent.Repository { return repoagent.NewGormRepository(db) },
		func(db *gorm.DB) snapshot_repo.Repository { return snapshot_repo.NewSnapshotRepository(db) },
		func(db *gorm.DB) agentplatform.Repository { return agentplatform.NewGormRepository(db) },
		func(db *gorm.DB) settings_repo.Repository { return settings_repo.NewRepository(db) },
		func(s strategyusecase.StrategyUseCase) agentusecase.StrategyUseCase { return s },
		func(u *backtestusecase.BacktestUseCase) agentusecase.BacktestUseCase { return u },
		func(s signalusecase.SignalUseCase) agentusecase.SignalUseCase { return signalUseCaseAdapter{s} },
		func(strategyRepo strategy_repo.StrategyRepository, snapshotRepo snapshot_repo.Repository) agentusecase.PerformanceSnapshotUseCase {
			return snapshotUseCaseAdapter{strategyRepo: strategyRepo, snapshotRepo: snapshotRepo}
		},
		func(o *optimizeusecase.OptimizeUseCase) agentusecase.OptimizeUseCase { return o },
		func(w optimizeworker.OptimizeWorker) agentusecase.OptimizeWorker { return w },
		func(cfg *configuration.Configuration) *modelprovider.ConfigProviderFactory {
			return modelprovider.NewConfigProviderFactory(cfg.Agent)
		},
		// The process-default model: unlike cmd/mcp, a missing key must not
		// crash startup - the run records a clear error instead.
		func(f *modelprovider.ConfigProviderFactory) modelprovider.ModelProvider {
			p, err := f.For("", "")
			if err != nil {
				return modelprovider.UnconfiguredProvider{Reason: "AI agent is not configured on this server: " + err.Error()}
			}
			return p
		},
		// i18n-02: Settings.DefaultLocale (cached 60 s) for unattended runs,
		// report snapshots and notification words.
		func(settings settings_repo.Repository) i18n.Source {
			return settings_repo.NewDefaultLocaleSource(settings)
		},
		func(src i18n.Source) *notifier.MultiTargetNotifier {
			n := notifier.NewMultiTargetNotifier()
			n.SetLocaleSource(src)
			return n
		},
		func(db *gorm.DB) agentusecase.ReportRenderer {
			return agentreport.NewHTMLRenderer(agentplatform.NewReportDataSource(db))
		},
		func(cfg *configuration.Configuration) agentusecase.StrategyLock {
			return lock.NewRedisStrategyLockFromAddr(cfg.Redis.Addr)
		},
		func(
			model modelprovider.ModelProvider,
			repo repoagent.Repository,
			strategy agentusecase.StrategyUseCase,
			backtest agentusecase.BacktestUseCase,
			signal agentusecase.SignalUseCase,
			snapshot agentusecase.PerformanceSnapshotUseCase,
			optimize agentusecase.OptimizeUseCase,
			optimizeWorker agentusecase.OptimizeWorker,
			platform agentplatform.Repository,
			settings settings_repo.Repository,
			factory *modelprovider.ConfigProviderFactory,
			n *notifier.MultiTargetNotifier,
			renderer agentusecase.ReportRenderer,
			strategyLock agentusecase.StrategyLock,
			cfg *configuration.Configuration,
			db *gorm.DB,
			bt *backtestusecase.BacktestUseCase,
			runner *strategyscript.Runner,
			chain *agentworker.ChainLauncher,
			locales i18n.Source,
		) *agentusecase.AgentUseCase {
			uc := agentusecase.NewAgentUseCase(model, repo, strategy, backtest, signal, snapshot)
			uc.Optimize = optimize
			uc.OptimizeWorker = optimizeWorker
			uc.Provider, uc.ModelName = factory.Resolve("", "")
			uc.Platform = platform
			uc.Providers = factory
			uc.Notifier = n
			uc.Reports = renderer
			uc.Guard = agentusecase.NewDefaultGuard(settings, platform, n)
			uc.Lock = strategyLock
			uc.APIBaseURL = cfg.APIBaseURL
			uc.Locales = locales
			// C-01 §4: trigger_agent (permission chain) enqueues through the
			// same guarded launcher the declarative ChainFrom path uses.
			uc.Chain = chain
			uc.WirePhaseB(bt, candle_repo.NewCandleRepository(db), proposalrepo.NewGormRepository(db), signalrepo.NewSignalRepository(db), runner)
			uc.Inspector = asynq.NewInspector(asynq.RedisClientOpt{Addr: cfg.Redis.Addr})
			uc.ExecutionReader = strategy_repo.NewStrategyRepository(db)
			return uc
		},
	),
)

// signalUseCaseAdapter mirrors cmd/mcp/modules/agent.go's adapter.
type signalUseCaseAdapter struct {
	inner signalusecase.SignalUseCase
}

func (a signalUseCaseAdapter) GetOpenSignals(ctx context.Context) ([]entities.Signal, error) {
	return a.inner.GetAllOpen(ctx)
}

// snapshotUseCaseAdapter mirrors cmd/mcp/modules/agent.go's adapter.
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
	from := to.AddDate(-1, 0, -1)
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
