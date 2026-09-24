package modules

import (
	"context"
	"fmt"
	"sort"
	"time"

	"go-trade-bot/app/entities"
	agenthandler "go-trade-bot/app/handler/web/agent"
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

// AgentModule is cmd/api's transport onto app/usecase/agent.AgentUseCase
// (Frontend Spec 01) - wired against the same strategy/backtest/signal/
// snapshot usecases cmd/api already constructs elsewhere, mirroring
// cmd/mcp/modules/agent.go's wiring so both transports drive the exact same
// usecase, tool registry, and safety gate (Backend Spec 03).
//
// Deviation from a literal "wired identically to cmd/mcp" reading: unlike
// cmd/mcp/modules.ModelProviderModule (whose entire job is being an AI
// agent process, so failing fast on missing AGENT.* config is correct),
// cmd/api is the live-trading REST API - it must keep serving every other
// route even when no model provider is configured. So this module never
// fails fx construction on missing/invalid AGENT config; instead it wires
// an unconfiguredProvider that turns every chat request into a normal
// AgentRun row with Status="error" and a clear ErrorMessage, surfaced by
// the chat panel like any other failed run (Frontend Spec 01 AC#4) instead
// of taking the whole API process down.
var AgentModule = fx.Module("agent",
	fx.Provide(
		func(db *gorm.DB) repoagent.Repository {
			return repoagent.NewGormRepository(db)
		},
		func(db *gorm.DB) snapshot_repo.Repository {
			return snapshot_repo.NewSnapshotRepository(db)
		},
		func(s strategyusecase.StrategyUseCase) agentusecase.StrategyUseCase { return s },
		func(u *backtestusecase.BacktestUseCase) agentusecase.BacktestUseCase { return u },
		func(s signalusecase.SignalUseCase) agentusecase.SignalUseCase { return agentSignalAdapter{s} },
		func(strategyRepo strategy_repo.StrategyRepository, snapshotRepo snapshot_repo.Repository) agentusecase.PerformanceSnapshotUseCase {
			return agentSnapshotAdapter{strategyRepo: strategyRepo, snapshotRepo: snapshotRepo}
		},
		func(cfg *configuration.Configuration) modelprovider.ModelProvider {
			switch cfg.Agent.Provider {
			case "anthropic":
				if cfg.Agent.AnthropicKey == "" {
					return modelprovider.UnconfiguredProvider{Reason: "AI agent is not configured on this server (AGENT.PROVIDER=anthropic but AGENT.ANTHROPIC_KEY is empty)"}
				}
				return modelprovider.NewAnthropicAdapter(cfg.Agent.AnthropicKey, cfg.Agent.AnthropicModel)
			case "gemini":
				if cfg.Agent.GeminiKey == "" {
					return modelprovider.UnconfiguredProvider{Reason: "AI agent is not configured on this server (AGENT.PROVIDER=gemini but AGENT.GEMINI_KEY is empty)"}
				}
				return modelprovider.NewGeminiAdapter(cfg.Agent.GeminiKey, cfg.Agent.GeminiModel)
			default:
				return modelprovider.UnconfiguredProvider{Reason: fmt.Sprintf("AI agent is not configured on this server (unrecognized AGENT.PROVIDER %q)", cfg.Agent.Provider)}
			}
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
		func(uc *agentusecase.AgentUseCase) agenthandler.UseCase { return uc },
		func(repo repoagent.Repository) agenthandler.Repository { return repo },
	),
)

// agentSignalAdapter mirrors cmd/mcp/modules/agent.go's signalUseCaseAdapter
// - bridges SignalUseCase.GetAllOpen onto agentusecase.SignalUseCase's
// GetOpenSignals method name.
type agentSignalAdapter struct {
	inner signalusecase.SignalUseCase
}

func (a agentSignalAdapter) GetOpenSignals(ctx context.Context) ([]entities.Signal, error) {
	return a.inner.GetAllOpen(ctx)
}

// agentSnapshotAdapter mirrors cmd/mcp/modules/agent.go's
// snapshotUseCaseAdapter - implements agentusecase.PerformanceSnapshotUseCase
// on top of the existing per-symbol ListDaily repository method by merging
// each of the strategy's monitored symbols' daily rows.
type agentSnapshotAdapter struct {
	strategyRepo strategy_repo.StrategyRepository
	snapshotRepo snapshot_repo.Repository
}

func (a agentSnapshotAdapter) ListByStrategy(ctx context.Context, strategyID uint, limit int) ([]entities.StrategyPerformanceSnapshot, error) {
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
