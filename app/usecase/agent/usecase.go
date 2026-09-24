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
	"strings"
	"time"

	"go-trade-bot/app/entities"
	"go-trade-bot/app/repository/agent"
	backtestusecase "go-trade-bot/app/usecase/backtest"
	"go-trade-bot/internal/modelprovider"

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

// DefaultMaxToolLoopIterations is used when AgentUseCase.MaxToolLoopIterations
// is left at its zero value.
const DefaultMaxToolLoopIterations = 8

// Tools exposes the closed tool registry (see tools.go's buildToolRegistry)
// to cmd/mcp's transport layer (Backend Spec 04), which maps each
// Tool.Def/Execute pair onto the chosen MCP SDK's tool-registration API.
// This is the ONLY exported way to reach the registry - there is no
// generic "invoke any usecase method by name" path.
func (u AgentUseCase) Tools() []Tool {
	return u.buildToolRegistry()
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

// AgentUseCase is the orchestration layer described above. Repository is
// the exact app/repository/agent.Repository interface (Backend Spec 02) -
// not re-declared locally, since that package already defines the minimal
// interface this usecase needs and nothing more.
type AgentUseCase struct {
	Model                 modelprovider.ModelProvider
	Repository            agent.Repository
	Strategy              StrategyUseCase
	Backtest              BacktestUseCase
	Signal                SignalUseCase
	Snapshot              PerformanceSnapshotUseCase
	MaxToolLoopIterations int // e.g. 8; hard cap, not configurable per-call
	// Provider/ModelName are recorded onto every AgentRun for audit
	// purposes (Backend Spec 02's AgentRun.Provider/Model) - set once at
	// construction from configuration.Agent, not per-call.
	Provider  string
	ModelName string
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
	maxIterations := u.MaxToolLoopIterations
	if maxIterations <= 0 {
		maxIterations = DefaultMaxToolLoopIterations
	}

	instruction, err := u.Repository.GetInstruction(ctx)
	if err != nil {
		return entities.AgentRun{}, fmt.Errorf("agent: failed to load instruction: %w", err)
	}
	system := instruction.Content
	if system != "" {
		system += "\n\n"
	}
	system += strategyAuthoringDoc

	run, err := u.Repository.CreateRun(ctx, entities.AgentRun{
		Provider:     u.Provider,
		Model:        u.ModelName,
		Trigger:      trigger,
		Status:       entities.AgentRunOK,
		InputSummary: truncate(userInput, 4000),
		StrategyID:   knownStrategyID,
		StartedAt:    time.Now(),
	})
	if err != nil {
		return entities.AgentRun{}, fmt.Errorf("agent: failed to create run: %w", err)
	}

	registry := u.buildToolRegistry()
	toolDefs := make([]modelprovider.ToolDefinition, 0, len(registry))
	tools := make(map[string]Tool, len(registry))
	for _, t := range registry {
		toolDefs = append(toolDefs, t.Def)
		tools[t.Def.Name] = t
	}

	messages := append(buildHistoryMessages(history), modelprovider.Message{Role: "user", Content: userInput})
	var records []toolCallRecord

	finish := func(status entities.AgentRunStatus, errMsg string) (entities.AgentRun, error) {
		run.Status = status
		run.ErrorMessage = errMsg
		run.FinishedAt = time.Now()
		if b, mErr := json.Marshal(records); mErr == nil {
			run.ToolCallsJSON = datatypes.JSON(b)
		}
		if uErr := u.Repository.UpdateRun(ctx, run); uErr != nil {
			return run, fmt.Errorf("agent: failed to persist final run state: %w", uErr)
		}
		if status == entities.AgentRunError {
			return run, fmt.Errorf("agent: %s", errMsg)
		}
		return run, nil
	}

	for i := 0; i < maxIterations; i++ {
		result, err := u.Model.Complete(ctx, modelprovider.CompletionRequest{
			System:   system,
			Messages: messages,
			Tools:    toolDefs,
		})
		if err != nil {
			return finish(entities.AgentRunError, fmt.Sprintf("model completion failed: %v", err))
		}

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

		if result.StopReason != "tool_use" || len(result.ToolCalls) == 0 {
			run.Status = entities.AgentRunOK
			// This is the model's actual answer - previously computed and
			// then silently discarded here, so the copilot UI had nothing
			// to show but tool-call cards. See entities.AgentRun.ResponseText.
			run.ResponseText = result.Text
			return finish(entities.AgentRunOK, "")
		}

		for _, call := range result.ToolCalls {
			record := toolCallRecord{Tool: call.Name, Args: call.Args, Timestamp: time.Now()}

			tool, ok := tools[call.Name]
			var toolResultText string
			if !ok {
				toolResultText = fmt.Sprintf("error: unknown tool %q", call.Name)
				record.Error = toolResultText
			} else {
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
			if uErr := u.Repository.UpdateRun(ctx, run); uErr != nil {
				return run, fmt.Errorf("agent: failed to persist incremental run state: %w", uErr)
			}
		}
	}

	// AC#6: exceeding the iteration cap without end_turn is an explicit
	// error, never a silently-returned partial/misleading success.
	return finish(entities.AgentRunError, fmt.Sprintf("tool loop exceeded %d iterations", maxIterations))
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "...(truncated)"
}
