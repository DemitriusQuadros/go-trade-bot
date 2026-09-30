// Package agent implements app/usecase/agent.AgentUseCase (Backend Spec 03)
// - the one place that turns a model's tool-call request into a real
// platform action, and the one place that guarantees an agent can never
// produce a Strategy.Mode == "live" row or place a real order.
//
// This package reuses app/usecase/strategy.StrategyUseCase.Save/Update
// UNCHANGED (via the narrow StrategyUseCase interface below) - no new write
// path into entities.Strategy is created here. THE safety gate lives in
// tools.go's saveStrategyScriptTool, as the last line of Go code before
// that call.
package agent

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"go-trade-bot/app/entities"
	"go-trade-bot/app/repository/agent"
	"go-trade-bot/app/repository/agentplatform"
	backtestusecase "go-trade-bot/app/usecase/backtest"
	optimizeusecase "go-trade-bot/app/usecase/optimize"
	"go-trade-bot/internal/i18n"
	"go-trade-bot/internal/modelprovider"
	"go-trade-bot/internal/notifier"
	"go-trade-bot/internal/report/agentreport"

	"gorm.io/datatypes"
)

// strategyAuthoringDoc is the Backend Spec 05 generated knowledge document,
// embedded once here so cmd/mcp/server/resources.go (Backend Spec 04) can
// import this package's exported StrategyAuthoringDoc and serve the exact
// same bytes as docs://strategy-authoring - one source of truth, not two
// independently-drifting copies (Backend Spec 05 AC#4).
//
//go:embed strategy_authoring_doc.md
var strategyAuthoringDoc string

// StrategyAuthoringDoc exposes the embedded doc for cmd/mcp's resource
// handler.
var StrategyAuthoringDoc = strategyAuthoringDoc

// DefaultMaxToolLoopIterations is the tool-loop cap for interactive triggers
// (chat_ui, mcp_tool), used when AgentUseCase.MaxToolLoopIterations is left at
// its zero value and the agent has no MaxIterations override.
const DefaultMaxToolLoopIterations = 8

// DefaultUnattendedMaxToolLoopIterations is the tool-loop cap for unattended
// triggers (cron, manual, event, market, chain), used when
// AgentUseCase.UnattendedMaxToolLoopIterations is zero and the agent has no
// MaxIterations override (fix-02 B1: a real analysis run - reads, backtests,
// journals, a report and a notify - needs well over 8 model turns).
const DefaultUnattendedMaxToolLoopIterations = 24

// iterationCapMessage is appended as a user message for the forced,
// tools-disabled final turn once the tool loop hits its cap.
const iterationCapMessage = "Iteration limit reached - give your final answer now, no more tool calls."

// isInteractiveTrigger reports whether trigger is a human-in-the-loop one
// (the low cap applies).
func isInteractiveTrigger(trigger string) bool {
	return trigger == "chat_ui" || trigger == "mcp_tool"
}

// maxIterationsFor resolves the tool-loop cap for one run: the agent's own
// MaxIterations when set, otherwise the trigger default.
func (u AgentUseCase) maxIterationsFor(agent entities.Agent, trigger string) int {
	if agent.MaxIterations > 0 {
		return agent.MaxIterations
	}
	if isInteractiveTrigger(trigger) {
		if u.MaxToolLoopIterations > 0 {
			return u.MaxToolLoopIterations
		}
		return DefaultMaxToolLoopIterations
	}
	if u.UnattendedMaxToolLoopIterations > 0 {
		return u.UnattendedMaxToolLoopIterations
	}
	return DefaultUnattendedMaxToolLoopIterations
}

// Tools exposes the closed tool registry (see tools.go's buildToolRegistry)
// to cmd/mcp's transport layer (Backend Spec 04), which maps each
// Tool.Def/Execute pair onto the chosen MCP SDK's tool-registration API.
// This is the ONLY exported way to reach the registry - there is no
// generic "invoke any usecase method by name" path.
//
// Agents platform (A-02 §5 "MCP"): MCP calls run with the DEFAULT agent's
// permissions - tools it isn't granted are not registered, and every
// permission-gated tool re-checks the default agent's current permissions
// at call time (the operator may edit them while cmd/mcp runs). The notify
// tool is never exposed over MCP, and neither is trigger_agent (it only
// exists inside an agent run). write_report/write_journal invoked over
// MCP attribute to the default agent (see tools_platform.go).
func (u AgentUseCase) Tools() []Tool {
	registered, lookupErr := u.DefaultAgent(context.Background())
	var out []Tool
	for _, t := range u.buildToolRegistry() {
		if t.Def.Name == notifyToolName || t.Def.Name == triggerAgentToolName {
			continue
		}
		if t.Permission == "" {
			out = append(out, t)
			continue
		}
		if lookupErr == nil && !registered.HasPermission(t.Permission) {
			continue
		}
		inner, perm, name := t.Execute, t.Permission, t.Def.Name
		t.Execute = func(ctx context.Context, args json.RawMessage) (string, error) {
			agent, err := u.DefaultAgent(ctx)
			if err != nil {
				return "", fmt.Errorf("%s: could not load the default agent's permissions: %w", name, err)
			}
			if !agent.HasPermission(perm) {
				return "", fmt.Errorf("%s: the default agent %q does not have permission %q", name, agent.Name, perm)
			}
			return inner(ctx, args)
		}
		out = append(out, t)
	}
	return out
}

// Narrow interfaces, each scoped to exactly what the agent needs - matches
// the existing "usecase defines its own interface for its dependencies"
// convention already used throughout app/usecase/*.
type StrategyUseCase interface {
	Save(ctx context.Context, s entities.Strategy) (entities.Strategy, error)
	Update(ctx context.Context, s entities.Strategy) error
	GetByID(ctx context.Context, id uint) (entities.Strategy, error)
	GetAll(ctx context.Context) ([]entities.Strategy, error)
}

type BacktestUseCase interface {
	Run(ctx context.Context, req backtestusecase.RunRequest) (entities.BacktestRun, error)
	GetByID(ctx context.Context, id uint) (entities.BacktestRun, error)
	ListByStrategy(ctx context.Context, strategyID uint) ([]entities.BacktestRun, error)
}

type SignalUseCase interface {
	GetOpenSignals(ctx context.Context) ([]entities.Signal, error) // read-only in Phase 1
}

type PerformanceSnapshotUseCase interface {
	ListByStrategy(ctx context.Context, strategyID uint, limit int) ([]entities.StrategyPerformanceSnapshot, error)
}

// OptimizeUseCase and OptimizeWorker close the gap the platform's own
// original design called for ("via the MCP be possible to query results of
// every state of the platform") - hyperparameter grid-search runs were
// fully persisted (entities.OptimizationRun) and REST-queryable from day
// one, but never reachable from the agent/MCP tool registry. Both narrow
// interfaces mirror BacktestUseCase's pattern: Create starts an async job
// (mirrors the HTTP handler's create+enqueue, Run() itself can take
// minutes for a large grid - unlike run_backtest, this cannot block a tool
// call to completion), GetByID/ListByStrategy read back what's already
// persisted.
type OptimizeUseCase interface {
	Create(ctx context.Context, req optimizeusecase.CreateRequest) (entities.OptimizationRun, error)
	GetByID(ctx context.Context, id uint) (entities.OptimizationRun, error)
	ListByStrategy(ctx context.Context, strategyID uint) ([]entities.OptimizationRun, error)
}

type OptimizeWorker interface {
	EnqueueOptimizeTask(runID uint) error
}

// AgentUseCase is the orchestration layer described above. Repository is
// the exact app/repository/agent.Repository interface (Backend Spec 02) -
// not re-declared locally, since that package already defines the minimal
// interface this usecase needs and nothing more.
type AgentUseCase struct {
	Model      modelprovider.ModelProvider
	Repository agent.Repository
	Strategy   StrategyUseCase
	Backtest   BacktestUseCase
	Signal     SignalUseCase
	Snapshot   PerformanceSnapshotUseCase
	// Optimize/OptimizeWorker are optional (nil-safe: buildToolRegistry
	// always registers the tools, but a nil check inside each Execute
	// closure reports a clear "not available" error rather than panicking)
	// so existing callers/tests that construct an AgentUseCase without
	// wiring optimization keep working unchanged - set post-construction,
	// same as Provider/ModelName below, rather than widening
	// NewAgentUseCase's signature.
	Optimize              OptimizeUseCase
	OptimizeWorker        OptimizeWorker
	MaxToolLoopIterations int // interactive-trigger cap (chat_ui, mcp_tool); 0 = DefaultMaxToolLoopIterations
	// UnattendedMaxToolLoopIterations is the cap for every other trigger;
	// 0 = DefaultUnattendedMaxToolLoopIterations. Agent.MaxIterations > 0
	// overrides both.
	UnattendedMaxToolLoopIterations int
	// Provider/ModelName are recorded onto every AgentRun for audit
	// purposes (Backend Spec 02's AgentRun.Provider/Model) - set once at
	// construction from configuration.Agent, not per-call.
	Provider  string
	ModelName string

	// Agents platform (A-01/A-02) dependencies - all optional and set
	// post-construction like Optimize, so legacy wiring/tests keep working:
	// nil Platform = synthetic default persona, no memory/usage/reports;
	// nil Providers = always u.Model; nil Guard = never halted; nil Lock =
	// no strategy writer locking (cmd/mcp); nil Notifier = notify tool
	// reports "not available"; nil Reports = write_report "not available".
	Platform   agentplatform.Repository
	Providers  modelprovider.ProviderFactory
	Notifier   notifier.AgentNotifier
	Reports    ReportRenderer
	Guard      RunGuard
	Lock       StrategyLock
	APIBaseURL string // for report/proposal deep links (<APIBaseURL>/agents/reports/<id>, /agents/proposals/<id>)

	// Locales is Settings.DefaultLocale (i18n-02): the language of
	// unattended runs and the RenderedHTML snapshot of write_report. nil =
	// always en.
	Locales i18n.Source

	// Coverage backs get_candle_coverage (fix-02 B2). Set by WirePhaseB
	// when the candle repository supports it; nil = the tool reports "not
	// available".
	Coverage CandleCoverageReader

	// Chain (C-01 §4) starts chained runs for the trigger_agent tool behind
	// the shared chain guards (app/workers/agent.ChainLauncher). nil =
	// trigger_agent reports "not available" (cmd/api chat, cmd/mcp).
	Chain ChainLauncher

	// Agents platform Phase B-01 dependencies - optional like the above:
	// nil Gate/Proposals = deploy_to_testing / propose_promotion /
	// list_proposals / get_deploy_gate_config report "not available" (they
	// never fall back to an ungated write); nil ForwardTest = no forward-
	// test evidence; nil Validator = the script runner's compile check;
	// nil Clock = time.Now.
	Gate        DeployGate
	Proposals   ProposalStore
	ForwardTest ClosedSignalReader
	Validator   ScriptValidator
	Clock       func() time.Time

	// Schedulers & queue health monitoring (get_scheduler_health tool).
	Inspector       AsynqInspector
	ExecutionReader StrategyExecutionReader
}

// ReportRenderer validates and renders report blocks (internal/report/agentreport).
type ReportRenderer interface {
	Validate(blocks []agentreport.Block) error
	// RenderLocales renders one snapshot per locale (i18n-02 §3), resolving
	// data once for all of them.
	RenderLocales(ctx context.Context, meta agentreport.ReportMeta, blocks []agentreport.Block, locales []i18n.Locale) (map[i18n.Locale]string, error)
}

func NewAgentUseCase(
	model modelprovider.ModelProvider,
	repository agent.Repository,
	strategy StrategyUseCase,
	backtest BacktestUseCase,
	signal SignalUseCase,
	snapshot PerformanceSnapshotUseCase,
) *AgentUseCase {
	return &AgentUseCase{
		Model:                 model,
		Repository:            repository,
		Strategy:              strategy,
		Backtest:              backtest,
		Signal:                signal,
		Snapshot:              snapshot,
		MaxToolLoopIterations: DefaultMaxToolLoopIterations,
	}
}

// toolCallRecord is one entry of AgentRun.ToolCallsJSON - the audit trail:
// every tool the agent called, its arguments, and a summary of the result,
// in call order.
type toolCallRecord struct {
	Tool      string          `json:"tool"`
	Args      json.RawMessage `json:"args"`
	Result    string          `json:"result,omitempty"`
	Error     string          `json:"error,omitempty"`
	Timestamp time.Time       `json:"timestamp"`
}

// PriorToolCall/PriorTurn let a caller (app/handler/web/agent) replay the
// conversation from earlier chat turns back into a fresh RunToolLoop call.
// Found missing entirely during manual verification: every message sent
// through the copilot widget started a brand new RunToolLoop with ONLY the
// latest input as Messages[0] - the agent had zero memory of anything
// discussed, drafted, or run in a previous turn of the same chat session.
// Asking it to draft a script and then, in the next message, "tighten the
// stop loss" simply couldn't work - there was no script in its context to
// tighten. This type pair is the caller-facing shape of that history;
// buildHistoryMessages below replays it into the same Message sequence a
// live multi-iteration loop would have produced, so a turn looks identical
// to the model whether it happened in this call or a previous one.
type PriorToolCall struct {
	Tool   string          `json:"tool"`
	Args   json.RawMessage `json:"args,omitempty"`
	Result string          `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

type PriorTurn struct {
	Input        string          `json:"input"`
	ToolCalls    []PriorToolCall `json:"tool_calls,omitempty"`
	ResponseText string          `json:"response_text,omitempty"`
}

// maxHistoryTurnsReplayed bounds how much prior conversation gets replayed
// into a fresh call - a defensive cap (mirroring MaxToolLoopIterations'
// cost/size bound) against an unbounded, ever-growing prompt in a very long
// chat session. Only the most recent turns are kept; older ones drop off
// the front, same as a normal chat context window would.
const maxHistoryTurnsReplayed = 20

// buildHistoryMessages replays prior turns into the same Message sequence
// the live loop below produces for a turn as it happens: one user message
// for the turn's input, one assistant message per model turn declaring any
// tool calls it made (mirroring the fix in the main loop - a tool call with
// no accompanying text still needs its own assistant turn, or the model
// loses track of having called it), one user message per tool result, and
// a final assistant message for the turn's concluding ResponseText.
func buildHistoryMessages(history []PriorTurn) []modelprovider.Message {
	if len(history) > maxHistoryTurnsReplayed {
		history = history[len(history)-maxHistoryTurnsReplayed:]
	}

	var messages []modelprovider.Message
	for _, turn := range history {
		messages = append(messages, modelprovider.Message{Role: "user", Content: turn.Input})

		if len(turn.ToolCalls) > 0 {
			var decl strings.Builder
			for _, call := range turn.ToolCalls {
				if decl.Len() > 0 {
					decl.WriteString("\n")
				}
				decl.WriteString(fmt.Sprintf("[called tool %s with args %s]", call.Tool, string(call.Args)))
			}
			messages = append(messages, modelprovider.Message{Role: "assistant", Content: decl.String()})

			for _, call := range turn.ToolCalls {
				result := call.Result
				if call.Error != "" {
					result = "error: " + call.Error
				}
				messages = append(messages, modelprovider.Message{
					Role:    "user",
					Content: fmt.Sprintf("[tool_result for %s]\n%s", call.Tool, result),
				})
			}
		}

		if turn.ResponseText != "" {
			messages = append(messages, modelprovider.Message{Role: "assistant", Content: turn.ResponseText})
		}
	}
	return messages
}

// RunToolLoop drives one agent turn end-to-end: builds the system prompt
// from AgentInstruction + the authoring-knowledge doc (Backend Spec 05),
// seeds the conversation with any prior turns via buildHistoryMessages so
// the agent has real memory of the chat so far, calls Model.Complete,
// executes any returned ToolCalls against the tool registry, feeds results
// back as the next message, and repeats until StopReason == "end_turn" or
// MaxToolLoopIterations is hit. Persists AgentRun incrementally via
// Repository.UpdateRun after every tool call - not only at the end - so a
// crash mid-loop leaves a partial audit trail.
//
// knownStrategyID pre-seeds run.StrategyID at creation, for a turn the
// caller already knows is scoped to an existing strategy (the
// app/handler/web/agent chat transport's strategy_id hint). Found missing
// during manual verification: without this, StrategyID was ONLY ever set
// opportunistically, from a tool call's own result (below) - so a turn
// that answered purely from replayed memory with no tool call at all (e.g.
// "what symbol did we use again?") persisted with StrategyID == nil. Since
// the DB-backed history lookup filters strictly on strategy_id, that
// silently invisible turn would then be excluded from EVERY future
// history replay for that strategy - a real, compounding memory gap that
// would only surface three or more turns into a conversation. Passing nil
// is correct for a turn that doesn't know its strategy yet (a brand-new
// draft, before its first save_strategy_script call) - the opportunistic
// path below still catches that case once the tool call resolves an ID.
func (u AgentUseCase) RunToolLoop(ctx context.Context, trigger, userInput string, history []PriorTurn, knownStrategyID *uint) (entities.AgentRun, error) {
	agent, err := u.DefaultAgent(ctx)
	if err != nil {
		return entities.AgentRun{}, fmt.Errorf("agent: failed to load default agent: %w", err)
	}
	return u.Run(ctx, RunRequest{
		Agent:      agent,
		Trigger:    trigger,
		UserInput:  userInput,
		History:    history,
		StrategyID: knownStrategyID,
	})
}

// RunRequest is one persona-aware agent run (agents-platform A-01 §4).
type RunRequest struct {
	Agent         entities.Agent // required; the persona
	Trigger       string         // "chat_ui" | "cron" | "manual" | "mcp_tool"
	TriggerDetail json.RawMessage
	UserInput     string
	// DisplayInput, when set, is what gets written to shared memory as the
	// chat_user entry instead of UserInput (the chat handler prefixes
	// UserInput with a strategy-context marker that should not be stored).
	DisplayInput string
	History      []PriorTurn // chat_ui only
	StrategyID   *uint       // context strategy (chat selection or the single binding being evaluated)
	ChainDepth   int
	ParentRunID  *uint
	// ChainPath is every agent already in this chain, source-first (chain
	// runs only, C-01 §4); trigger_agent refuses any agent in it.
	ChainPath []uint
	// UserID is the app user who started a chat run (auth-01 §6), recorded
	// on AgentRun.UserID. Nil for unattended and service-token runs.
	UserID *uint
	// Locale is the reply language for chat runs (i18n-02 §2: the user's
	// locale, else Accept-Language). Ignored by unattended runs, which use
	// Settings.DefaultLocale; empty or unsupported = the default locale.
	Locale string
}

// DefaultAgent returns the default "Copilot" persona. Without a Platform
// repository (legacy wiring/tests) it is a synthetic, unsaved persona with
// every Phase A permission - i.e. exactly the pre-platform behaviour.
func (u AgentUseCase) DefaultAgent(ctx context.Context) (entities.Agent, error) {
	if u.Platform == nil {
		return syntheticDefaultAgent(), nil
	}
	return u.Platform.GetDefaultAgent(ctx)
}

// ResolveAgent returns the agent with id, or the default agent when id is
// nil. Without a Platform only the default agent exists.
func (u AgentUseCase) ResolveAgent(ctx context.Context, id *uint) (entities.Agent, error) {
	if id == nil || *id == 0 {
		return u.DefaultAgent(ctx)
	}
	if u.Platform == nil {
		return entities.Agent{}, fmt.Errorf("agent %d not found: the agents platform is not configured", *id)
	}
	return u.Platform.GetAgent(ctx, *id)
}

// AgentNames maps agent id -> name (empty without a Platform).
func (u AgentUseCase) AgentNames(ctx context.Context) map[uint]string {
	return u.agentNames(ctx)
}

func syntheticDefaultAgent() entities.Agent {
	return entities.Agent{
		Name:        entities.DefaultAgentName,
		IsDefault:   true,
		Permissions: agentplatform.DefaultAgentPermissions(),
	}
}

// CheckGuard runs the RunGuard for agent (nil Guard = always allowed).
// Exposed so transports (the chat handler's 409, the manual-run endpoint)
// can pre-flight a run.
func (u AgentUseCase) CheckGuard(ctx context.Context, agent entities.Agent) error {
	if u.Guard == nil {
		return nil
	}
	return u.Guard.Check(ctx, agent)
}

// modelFor picks the ModelProvider and the provider/model names recorded
// on the run and used for pricing. An agent with no provider/model
// override uses the process default (u.Model) exactly as before.
func (u AgentUseCase) modelFor(agent entities.Agent) (modelprovider.ModelProvider, string, string, error) {
	if u.Providers == nil || (agent.Provider == "" && agent.Model == "") {
		return u.Model, u.Provider, u.ModelName, nil
	}
	provider, modelName := agent.Provider, agent.Model
	if resolver, ok := u.Providers.(interface {
		Resolve(provider, model string) (string, string)
	}); ok {
		provider, modelName = resolver.Resolve(provider, modelName)
	} else if provider == "" {
		provider = u.Provider
	}
	m, err := u.Providers.For(agent.Provider, agent.Model)
	return m, provider, modelName, err
}

// Run drives one agent turn end-to-end for a persona: builds the system
// prompt (house rules, persona, operating context, shared strategy memory,
// authoring doc), exposes only the tools the persona's permissions grant,
// re-checks the RunGuard (kill switch / pause / budget) before EVERY model
// call, accounts token usage and cost per call, executes tool calls
// (re-checking permissions at dispatch), and repeats until the model stops
// calling tools or the iteration cap (maxIterationsFor) is hit. At the cap
// the model gets one forced final turn with no tools; if that turn answers
// the run is ok with HitIterationCap set. AgentRun is persisted
// incrementally after every tool call so a crash mid-loop leaves a partial
// audit trail. Strategy writer locks acquired during the run are released
// when Run returns.
func (u AgentUseCase) Run(ctx context.Context, req RunRequest) (entities.AgentRun, error) {
	agent := req.Agent
	maxIterations := u.maxIterationsFor(agent, req.Trigger)

	instruction, err := u.Repository.GetInstruction(ctx)
	if err != nil {
		return entities.AgentRun{}, fmt.Errorf("agent: failed to load instruction: %w", err)
	}

	model, providerName, modelName, modelErr := u.modelFor(agent)

	newRun := entities.AgentRun{
		Provider:     providerName,
		Model:        modelName,
		Trigger:      req.Trigger,
		Status:       entities.AgentRunRunning,
		InputSummary: truncate(req.UserInput, 4000),
		StrategyID:   req.StrategyID,
		StartedAt:    time.Now(),
		ChainDepth:   req.ChainDepth,
		ParentRunID:  req.ParentRunID,
		UserID:       req.UserID,
	}
	if agent.ID != 0 {
		id := agent.ID
		newRun.AgentID = &id
	}
	if len(req.TriggerDetail) > 0 {
		newRun.TriggerDetail = datatypes.JSON(req.TriggerDetail)
	}
	run, err := u.Repository.CreateRun(ctx, newRun)
	if err != nil {
		return entities.AgentRun{}, fmt.Errorf("agent: failed to create run: %w", err)
	}

	scope := &runScope{agent: agent, runID: run.ID, trigger: req.Trigger, locks: map[uint]bool{}, chainDepth: req.ChainDepth, chainPath: append([]uint(nil), req.ChainPath...)}
	ctx = withRunScope(ctx, scope)
	defer u.releaseLocks(scope)

	var records []toolCallRecord
	finish := func(status entities.AgentRunStatus, errMsg string) (entities.AgentRun, error) {
		run.ReportIDs, run.NotificationsSent = scope.outcome()
		run.Status = status
		run.ErrorMessage = errMsg
		run.FinishedAt = time.Now()
		if b, mErr := json.Marshal(records); mErr == nil {
			run.ToolCallsJSON = datatypes.JSON(b)
		}
		// Persist on a context detached from the run's: when the run is
		// ended by its task deadline, ctx is already expired, and saving
		// with it failed - leaving the row "in progress" forever (E2E run
		// #61).
		pctx, cancel := persistContext(ctx)
		defer cancel()
		if uErr := u.Repository.UpdateRun(pctx, run); uErr != nil {
			return run, fmt.Errorf("agent: failed to persist final run state: %w", uErr)
		}
		if status == entities.AgentRunError {
			return run, fmt.Errorf("agent: %s", errMsg)
		}
		return run, nil
	}

	if modelErr != nil {
		return finish(entities.AgentRunError, fmt.Sprintf("model provider unavailable: %v", modelErr))
	}

	system := u.buildSystemPrompt(ctx, instruction.Content, req)

	fullRegistry := u.buildToolRegistry()
	toolDefs := make([]modelprovider.ToolDefinition, 0, len(fullRegistry))
	granted := make(map[string]Tool, len(fullRegistry))
	known := make(map[string]Tool, len(fullRegistry))
	for _, t := range fullRegistry {
		known[t.Def.Name] = t
		if toolGranted(agent, t) {
			toolDefs = append(toolDefs, t.Def)
			granted[t.Def.Name] = t
		}
	}

	messages := append(buildHistoryMessages(req.History), modelprovider.Message{Role: "user", Content: req.UserInput})

	truncations := 0  // consecutive max_tokens turns, see below
	emptyAnswers := 0 // turns with no text and no tool call, see below
	for i := 0; i < maxIterations; i++ {
		if ctx.Err() != nil {
			return finish(entities.AgentRunError, fmt.Sprintf("run timed out or was cancelled: %v", ctx.Err()))
		}
		// A-01 §4.3: kill switch / pause / budget re-checked before EVERY
		// model call, so an in-flight run halts at its next iteration.
		if gErr := u.CheckGuard(ctx, agent); gErr != nil {
			return finish(entities.AgentRunError, "halted: "+gErr.Error())
		}
		if i == 0 {
			u.onRunStarted(ctx, agent, req, run)
		}

		result, err := model.Complete(ctx, modelprovider.CompletionRequest{
			System:   system,
			Messages: messages,
			Tools:    toolDefs,
		})
		if err != nil {
			return finish(entities.AgentRunError, fmt.Sprintf("model completion failed: %v", err))
		}
		u.recordUsage(ctx, &run, agent, providerName, modelName, result.Usage)

		// Record this turn as a single assistant message covering BOTH any
		// text the model produced AND every tool call it made - not just
		// the text. Omitting the tool-call declaration here (the original
		// bug: only `if result.Text != ""` was appended) leaves a gap in
		// the replayed history where a tool result appears to materialize
		// between two "user" turns with no assistant turn ever having
		// asked for it. At least Gemini visibly did not cope with that:
		// with no record of its own prior function call, it re-issued the
		// identical tool call every iteration instead of answering from
		// the result already in front of it. Every provider-facing
		// adapter (internal/modelprovider) only ever sees this flattened
		// Message history, so the fix belongs here, once, rather than
		// duplicated per adapter.
		if result.Text != "" || len(result.ToolCalls) > 0 {
			var turn strings.Builder
			turn.WriteString(result.Text)
			for _, call := range result.ToolCalls {
				if turn.Len() > 0 {
					turn.WriteString("\n")
				}
				turn.WriteString(fmt.Sprintf("[called tool %s with args %s]", call.Name, string(call.Args)))
			}
			messages = append(messages, modelprovider.Message{Role: "assistant", Content: turn.String()})
		}

		// A turn cut off at the output-token limit is not an answer, and any
		// tool call in it may carry truncated arguments - never execute it,
		// and never record the run as ok on it (E2E run #57 finished "ok"
		// with an empty answer this way, mid-way through writing a script).
		// Ask the model to redo the step more concisely; two truncations in a
		// row end the run as an error.
		if result.StopReason == "max_tokens" {
			truncations++
			if truncations >= 2 {
				return finish(entities.AgentRunError, "model output truncated at the output-token limit twice in a row")
			}
			messages = append(messages, modelprovider.Message{Role: "user", Content: "Your previous response was cut off at the output-token limit, so none of its tool calls were executed. Redo that step more concisely (shorter text; keep tool arguments compact)."})
			continue
		}
		truncations = 0

		// A turn that ends with neither tool calls nor any text is not an
		// answer either: E2E run #58 stopped half-way through its task this
		// way (end_turn, empty content, right after a tool result) and was
		// recorded as ok. Nudge once; a second empty turn is an error.
		if len(result.ToolCalls) == 0 && strings.TrimSpace(result.Text) == "" {
			emptyAnswers++
			if emptyAnswers >= 2 {
				return finish(entities.AgentRunError, "model ended its turn twice without an answer or a tool call")
			}
			messages = append(messages, modelprovider.Message{Role: "user", Content: "You ended your turn without an answer. If the task is not finished, continue with the next step now; otherwise give a short final summary."})
			continue
		}

		if result.StopReason != "tool_use" || len(result.ToolCalls) == 0 {
			// This is the model's actual answer - previously computed and
			// then silently discarded here, so the copilot UI had nothing
			// to show but tool-call cards. See entities.AgentRun.ResponseText.
			run.ResponseText = result.Text
			return finish(entities.AgentRunOK, "")
		}

		for _, call := range result.ToolCalls {
			record := toolCallRecord{Tool: call.Name, Args: call.Args, Timestamp: time.Now()}

			var toolResultText string
			tool, ok := granted[call.Name]
			switch {
			case !ok:
				// Dispatch re-check (A-01 §4.2): a tool the model was never
				// shown, or an unknown name, is never executed.
				if t, exists := known[call.Name]; exists {
					toolResultText = fmt.Sprintf("error: tool %q is not available to agent %q (requires permission %q)", call.Name, agent.Name, t.Permission)
				} else {
					toolResultText = fmt.Sprintf("error: unknown tool %q", call.Name)
				}
				record.Error = toolResultText
			default:
				toolResultText, err = tool.Execute(ctx, call.Args)
				if err != nil {
					// Edge case (AC#7): malformed/failed tool calls are fed
					// back to the model as the tool result, not aborted -
					// the model can retry with corrected arguments.
					toolResultText = fmt.Sprintf("error: %v", err)
					record.Error = err.Error()
				} else {
					record.Result = truncate(toolResultText, 2000)
					if strategyID, ok := strategyIDFromToolResult(call.Name, call.Args, toolResultText); ok && run.StrategyID == nil {
						run.StrategyID = &strategyID
					}
				}
			}

			records = append(records, record)
			messages = append(messages, modelprovider.Message{Role: "user", Content: fmt.Sprintf("[tool_result for %s]\n%s", call.Name, toolResultText)})

			// AC#5: persisted incrementally after EVERY tool call, not only
			// at the end of the loop - a crash mid-loop still leaves a
			// partial audit row.
			if b, mErr := json.Marshal(records); mErr == nil {
				run.ToolCallsJSON = datatypes.JSON(b)
			}
			pctx, cancel := persistContext(ctx)
			uErr := u.Repository.UpdateRun(pctx, run)
			cancel()
			if uErr != nil {
				return finish(entities.AgentRunError, fmt.Sprintf("failed to persist incremental run state: %v", uErr))
			}
		}
	}

	// fix-02 B1: at the cap, give the model ONE final turn with tools
	// disabled so a run that finished its work can still deliver its answer.
	// The guard (kill switch / pause / budget) still applies before it. Only
	// if this turn also fails (or returns no text) is the run an error -
	// never a silently-returned partial/misleading success (AC#6).
	capMsg := fmt.Sprintf("tool loop exceeded %d iterations", maxIterations)
	if gErr := u.CheckGuard(ctx, agent); gErr != nil {
		return finish(entities.AgentRunError, "halted: "+gErr.Error())
	}
	messages = append(messages, modelprovider.Message{Role: "user", Content: iterationCapMessage})
	result, err := model.Complete(ctx, modelprovider.CompletionRequest{
		System:   system,
		Messages: messages,
		Tools:    nil,
	})
	if err != nil {
		return finish(entities.AgentRunError, fmt.Sprintf("%s; forced final answer failed: %v", capMsg, err))
	}
	u.recordUsage(ctx, &run, agent, providerName, modelName, result.Usage)
	if strings.TrimSpace(result.Text) == "" {
		return finish(entities.AgentRunError, capMsg+"; forced final answer returned no text")
	}
	run.ResponseText = result.Text
	run.HitIterationCap = true
	return finish(entities.AgentRunOK, "")
}

// persistContext returns a context for saving run state that survives the
// run's own cancellation/deadline (but keeps its values), bounded so a
// stuck database can't hang the task.
func persistContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
}

// toolGranted reports whether agent may use t ("" permission = always).
func toolGranted(agent entities.Agent, t Tool) bool {
	return t.Permission == "" || agent.HasPermission(t.Permission)
}

// onRunStarted runs once, after the first guard check passes: counts the
// run in today's AgentUsage. Chat turns are NOT written to shared strategy
// memory (Phase D follow-up): the chat has its own persisted transcript, and
// memory is for deliberate notes - operator notes (incl. "Save to notes" from
// the chat UI), write_journal findings and report refs - so chat can't crowd
// those out of the memory window every agent's prompt gets.
func (u AgentUseCase) onRunStarted(ctx context.Context, agent entities.Agent, req RunRequest, run entities.AgentRun) {
	if u.Platform == nil || agent.ID == 0 {
		return
	}
	if err := u.Platform.IncRuns(ctx, agent.ID, time.Now()); err != nil {
		log.Printf("agent: failed to count run for agent %d: %v", agent.ID, err)
	}
}

func (u AgentUseCase) appendMemory(ctx context.Context, e entities.StrategyMemoryEntry) {
	if _, err := u.Platform.AppendMemory(ctx, e); err != nil {
		log.Printf("agent: failed to append %s memory for strategy %d: %v", e.Kind, e.StrategyID, err)
	}
}

func runIDPtr(id uint) *uint {
	if id == 0 {
		return nil
	}
	return &id
}

// recordUsage adds one completion's usage + estimated cost to the run and
// to today's (UTC) AgentUsage row.
func (u AgentUseCase) recordUsage(ctx context.Context, run *entities.AgentRun, agent entities.Agent, provider, modelName string, usage modelprovider.Usage) {
	if usage.InputTokens == 0 && usage.OutputTokens == 0 {
		return
	}
	cost := modelprovider.EstimateCostUSD(provider, modelName, usage)
	run.InputTokens += usage.InputTokens
	run.OutputTokens += usage.OutputTokens
	run.CostUSD += cost
	if u.Platform != nil && agent.ID != 0 {
		if err := u.Platform.AddUsage(ctx, agent.ID, time.Now(), usage.InputTokens, usage.OutputTokens, cost); err != nil {
			log.Printf("agent: failed to record usage for agent %d: %v", agent.ID, err)
		}
	}
}

// releaseLocks releases every strategy writer lock the run acquired.
func (u AgentUseCase) releaseLocks(scope *runScope) {
	if u.Lock == nil {
		return
	}
	for _, id := range scope.heldLocks() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := u.Lock.Release(ctx, id, scope.holder()); err != nil {
			log.Printf("agent: failed to release strategy lock %d for %s: %v", id, scope.holder(), err)
		}
		cancel()
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "...(truncated)"
}
