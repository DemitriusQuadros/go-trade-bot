package modules

import (
	"context"
	"sort"
	"time"

	"go-trade-bot/app/entities"
	repoagent "go-trade-bot/app/repository/agent"
	snapshot_repo "go-trade-bot/app/repository/performancesnapshot"
	strategy_repo "go-trade-bot/app/repository/strategy"
	agentusecase "go-trade-bot/app/usecase/agent"
	backtestusecase "go-trade-bot/app/usecase/backtest"
	signalusecase "go-trade-bot/app/usecase/signal"
	strategyusecase "go-trade-bot/app/usecase/strategy"
	"go-trade-bot/internal/configuration"
	"go-trade-bot/internal/modelprovider"

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
		func(
			model modelprovider.ModelProvider,
			repo repoagent.Repository,
			strategy agentusecase.StrategyUseCase,
			backtest agentusecase.BacktestUseCase,
			signal agentusecase.SignalUseCase,
			snapshot agentusecase.PerformanceSnapshotUseCase,
			cfg *configuration.Configuration,
		) *agentusecase.AgentUseCase {
			uc := agentusecase.NewAgentUseCase(model, repo, strategy, backtest, signal, snapshot)
			uc.Provider = cfg.Agent.Provider
			if cfg.Agent.Provider == "anthropic" {
				uc.ModelName = cfg.Agent.AnthropicModel
			} else if cfg.Agent.Provider == "gemini" {
				uc.ModelName = cfg.Agent.GeminiModel
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
