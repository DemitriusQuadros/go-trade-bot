package modules

import (
	agenthandler "go-trade-bot/app/handler/web/agent"
	agentreports "go-trade-bot/app/handler/web/agentreports"
	agentshandler "go-trade-bot/app/handler/web/agents"
	proposalshandler "go-trade-bot/app/handler/web/proposals"
	webhooktargets "go-trade-bot/app/handler/web/webhooktargets"
	repoagent "go-trade-bot/app/repository/agent"
	"go-trade-bot/app/repository/agentplatform"
	proposalrepo "go-trade-bot/app/repository/proposal"
	settings_repo "go-trade-bot/app/repository/settings"
	strategy_repo "go-trade-bot/app/repository/strategy"
	agentusecase "go-trade-bot/app/usecase/agent"
	platformusecase "go-trade-bot/app/usecase/agentplatform"
	proposalusecase "go-trade-bot/app/usecase/proposal"
	agentworker "go-trade-bot/app/workers/agent"
	"go-trade-bot/internal/configuration"
	"go-trade-bot/internal/lock"
	"go-trade-bot/internal/modelprovider"
	"go-trade-bot/internal/notifier"
	"go-trade-bot/internal/report/agentreport"

	"go.uber.org/fx"
	"gorm.io/gorm"
)

// AgentPlatformModule wires the agents platform (agents-platform A-01/A-02)
// into cmd/api: the persona repository and management usecase behind the
// /agents, /agent-reports, /webhook-targets, /strategies/{id}/memory and
// /agents/kill-switch routes, the manual-run enqueuer (asynq "agents"
// queue, consumed only by cmd/agent), and the dependencies the in-process
// chat runs need (renderer, notifier, provider factory, strategy lock).
var AgentPlatformModule = fx.Module("agentplatform",
	fx.Provide(
		func(db *gorm.DB) agentplatform.Repository { return agentplatform.NewGormRepository(db) },
		func() *notifier.MultiTargetNotifier { return notifier.NewMultiTargetNotifier() },
		func(db *gorm.DB) agentusecase.ReportRenderer {
			return agentreport.NewHTMLRenderer(agentplatform.NewReportDataSource(db))
		},
		func(cfg *configuration.Configuration) *modelprovider.ConfigProviderFactory {
			return modelprovider.NewConfigProviderFactory(cfg.Agent)
		},
		func(cfg *configuration.Configuration) agentusecase.StrategyLock {
			return lock.NewRedisStrategyLockFromAddr(cfg.Redis.Addr)
		},
		agentworker.NewAgentWorker,
		func(db *gorm.DB) platformusecase.RunReader { return repoagent.NewGormRepository(db) },
		func(
			platform agentplatform.Repository,
			strategies strategy_repo.StrategyRepository,
			runs platformusecase.RunReader,
			worker agentworker.AgentWorker,
			settings settings_repo.Repository,
			n *notifier.MultiTargetNotifier,
		) *platformusecase.UseCase {
			return platformusecase.NewUseCase(platform, strategies, runs, worker, settings, n)
		},
		func(u *platformusecase.UseCase) agentshandler.UseCase { return u },
		func(u *platformusecase.UseCase) agentreports.UseCase { return u },
		func(u *platformusecase.UseCase) webhooktargets.UseCase { return u },
		func(uc *agentusecase.AgentUseCase) agenthandler.PersonaChat { return uc },
		// Phase B-01 §5: proposals + deploy gate REST. Approve enqueues
		// agent:apply_proposal (queue "agents", cmd/agent); cmd/api itself
		// never applies a proposal.
		func(db *gorm.DB, platform agentplatform.Repository, strategies strategy_repo.StrategyRepository, worker agentworker.AgentWorker) *proposalusecase.UseCase {
			return proposalusecase.NewUseCase(proposalrepo.NewGormRepository(db), strategies, platform, worker)
		},
		func(u *proposalusecase.UseCase) proposalshandler.UseCase { return u },
	),
)
