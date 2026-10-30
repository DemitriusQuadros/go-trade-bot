package mcptools

import (
	"context"
	"time"

	"go-trade-bot/app/engine"
	"go-trade-bot/app/entities"
	agentplatform "go-trade-bot/app/usecase/agentplatform"
	candledata "go-trade-bot/app/usecase/candledata"
	scriptuc "go-trade-bot/app/usecase/script"
	"go-trade-bot/internal/exchange"
)

// The interfaces below are the complete surface this adapter may use. A
// method that is not listed cannot be called from an MCP tool, so e.g. there
// is no way to reach a settings write, an account write or a signal close
// from here.

// SettingsReader is app/repository/settings.Repository's read half.
type SettingsReader interface {
	Get(ctx context.Context) (*entities.Settings, error)
}

// AccountReader is app/usecase/account.AccountUseCase's read half.
type AccountReader interface {
	GetAllAccounts() ([]entities.Account, error)
}

// SignalReader is app/usecase/signal.SignalUseCase's read half (no Close).
type SignalReader interface {
	GetAll(ctx context.Context) ([]entities.Signal, error)
	GetAllOpen(ctx context.Context) ([]entities.Signal, error)
	GetAllClosed(ctx context.Context) ([]entities.Signal, error)
	GetByID(ctx context.Context, id uint) (entities.Signal, error)
}

// VersionReader is app/usecase/strategy.StrategyUseCase's version listing
// (no revert).
type VersionReader interface {
	GetScriptVersions(ctx context.Context, strategyID uint) ([]entities.ScriptVersion, error)
}

// MonteCarlo is app/usecase/backtest.BacktestUseCase's Monte Carlo pair. Run
// only writes the result onto the finished run row.
type MonteCarlo interface {
	RunMonteCarlo(ctx context.Context, runID uint, iterations int) (engine.MonteCarloResult, error)
	GetMonteCarlo(ctx context.Context, runID uint) (engine.MonteCarloResult, error)
}

// MarketData is the exchange's read-only market data.
type MarketData interface {
	ListTickerPrices(ctx context.Context, symbol string) ([]exchange.TickerPrice, error)
	ListKline(ctx context.Context, symbol, interval string, limit int) ([]exchange.Candle, error)
}

// DatasetManager is app/usecase/candledata.Service without Delete.
type DatasetManager interface {
	List(ctx context.Context) ([]*candledata.DatasetStatus, error)
	Status(ctx context.Context, id uint) (*candledata.DatasetStatus, error)
	Create(ctx context.Context, symbol, timeframe string, start *time.Time, keepLive bool) (*entities.CandleDataset, error)
	Pause(ctx context.Context, id uint) error
	Resume(ctx context.Context, id uint) error
	RetryFailed(ctx context.Context, id uint) (int, error)
	Reconcile(ctx context.Context, id uint) (int, error)
	Chunks(ctx context.Context, id uint, status entities.ChunkStatus, limit int) ([]entities.CandleChunk, error)
}

// ScriptRunner is app/usecase/script.UseCase: it never places an order.
type ScriptRunner interface {
	Eval(ctx context.Context, req scriptuc.EvalRequest) (scriptuc.EvalResponse, error)
	FastRerun(ctx context.Context, req scriptuc.FastRerunRequest) (scriptuc.FastRerunResponse, error)
}

// AgentManager is the agents platform management usecase: full agent
// lifecycle (create/update/delete/pause), manual runs, usage and the global
// kill switch. Webhook TARGETS are only readable (WebhookTargetReader): a
// target's URL is where trade events get sent, so creating one stays a
// human, authenticated-REST action.
type AgentManager interface {
	ListAgents(ctx context.Context) ([]agentplatform.AgentView, error)
	GetAgent(ctx context.Context, id uint) (agentplatform.AgentView, error)
	CreateAgent(ctx context.Context, in agentplatform.AgentInput) (agentplatform.AgentView, error)
	UpdateAgent(ctx context.Context, id uint, in agentplatform.AgentInput) (agentplatform.AgentView, error)
	DeleteAgent(ctx context.Context, id uint) error
	SetPaused(ctx context.Context, id uint, paused bool) (agentplatform.AgentView, error)
	EnqueueManualRun(ctx context.Context, id uint, prompt string) (string, error)
	ListRuns(ctx context.Context, agentID uint, limit int, beforeID *uint) ([]entities.AgentRun, entities.Agent, error)
	Usage(ctx context.Context, agentID uint, days int) (agentplatform.UsageDay, []agentplatform.UsageDay, error)
	KillSwitch(ctx context.Context) (bool, error)
	SetKillSwitch(ctx context.Context, paused bool) (bool, error)
}

// WebhookTargetReader lists notification targets so agents can be wired to
// them by id. Results never include the URL or secret.
type WebhookTargetReader interface {
	ListWebhookTargets(ctx context.Context) ([]entities.WebhookTarget, error)
}
