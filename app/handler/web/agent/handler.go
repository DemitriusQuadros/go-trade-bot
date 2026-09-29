// Package agent is the cmd/api HTTP transport onto app/usecase/agent.AgentUseCase
// (Frontend Spec 01) - a second transport onto the exact same usecase
// cmd/mcp already exposes over MCP (Backend Spec 04), not a second
// implementation. It reuses the same tool registry and Mode-clamp safety
// gate (Backend Spec 03): nothing here can set a strategy to "live" or
// "productive", because AgentUseCase.RunToolLoop's only write-capable tool
// (save_strategy_script) structurally cannot do that regardless of caller.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"go-trade-bot/app/entities"
	agentrepo "go-trade-bot/app/repository/agent"
	agentusecase "go-trade-bot/app/usecase/agent"
	"go-trade-bot/internal/customerror"
	"go-trade-bot/internal/handler"

	"github.com/gorilla/mux"
)

// UseCase is the narrow slice of app/usecase/agent.AgentUseCase this handler
// needs - matches the existing "usecase defines its own interface for its
// dependencies" convention.
type UseCase interface {
	RunToolLoop(ctx context.Context, trigger, userInput string, history []agentusecase.PriorTurn, knownStrategyID *uint) (entities.AgentRun, error)
}

// Repository is the narrow slice of app/repository/agent.Repository this
// handler needs for the read-only history endpoints (Frontend Spec 02).
type Repository interface {
	GetRun(ctx context.Context, id uint) (entities.AgentRun, error)
	ListRuns(ctx context.Context, limit int, strategyID *uint) ([]entities.AgentRun, error)
	ListRunsFiltered(ctx context.Context, f agentrepo.RunFilter) ([]entities.AgentRun, error)
}

// PersonaChat is the agents-platform half of the chat transport (A-02 §5
// "Chat"): resolve the requested persona, pre-flight its RunGuard, and run
// it. Optional - when nil the handler keeps its pre-platform behaviour
// (RunToolLoop with the default agent).
type PersonaChat interface {
	// ResolveAgent returns the agent with id, or the default agent for nil.
	ResolveAgent(ctx context.Context, id *uint) (entities.Agent, error)
	CheckGuard(ctx context.Context, agent entities.Agent) error
	Run(ctx context.Context, req agentusecase.RunRequest) (entities.AgentRun, error)
	AgentNames(ctx context.Context) map[uint]string
}

const (
	defaultListLimit = 20
	// maxListLimit caps GET /agent/runs?limit= (Phase D-01 §1); larger
	// values are clamped, not rejected.
	maxListLimit = 100
)

// validRunTriggers are the AgentRun.Trigger values GET /agent/runs?trigger=
// accepts (entities.AgentRun.Trigger's documented set).
var validRunTriggers = map[string]bool{
	"mcp_tool": true, "chat_ui": true, "monitor": true, "cron": true,
	"manual": true, "event": true, "market": true, "chain": true,
}

type AgentHandler struct {
	useCase    UseCase
	repository Repository
	personas   PersonaChat
}

func NewAgentHandler(u UseCase, r Repository) *AgentHandler {
	return &AgentHandler{useCase: u, repository: r}
}

// NewAgentHandlerWithPersonas is NewAgentHandler plus persona-aware chat
// (agent_id, 409 for a paused/halted agent, agent names on runs).
func NewAgentHandlerWithPersonas(u UseCase, r Repository, p PersonaChat) *AgentHandler {
	return &AgentHandler{useCase: u, repository: r, personas: p}
}

func (h *AgentHandler) names(ctx context.Context) map[uint]string {
	if h.personas == nil {
		return map[uint]string{}
	}
	return h.personas.AgentNames(ctx)
}

func writeJSONError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code, "message": message})
}

func (h *AgentHandler) Handlers() []handler.Configuration {
	return []handler.Configuration{
		{
			Pattern: "/agent/runs",
			Method:  http.MethodPost,
			Action:  h.SendMessage,
		},
		{
			Pattern: "/agent/runs",
			Method:  http.MethodGet,
			Action:  h.ListRuns,
		},
		{
			Pattern: "/agent/runs/{id:[0-9]+}",
			Method:  http.MethodGet,
			Action:  h.GetRun,
		},
	}
}

// sendMessageHistoryToolCall/sendMessageHistoryTurn are the request-side
// shapes of app/usecase/agent.PriorToolCall/PriorTurn - the copilot widget
// (web/src/components/domain/AgentCopilotWidget.tsx) sends every earlier
// successful turn of the CURRENT chat session on each new message, so
// RunToolLoop can replay real conversational memory instead of treating
// every message as the start of a brand new, context-free conversation
// (the bug that made iterating on a script - "draft this, now tighten the
// stop loss" - impossible: the second message had no idea what script the
// first one produced). The widget keeps this history client-side only
// (Frontend Spec 01's non-persistence scope); nothing new is stored
// server-side to support this.
type sendMessageHistoryToolCall struct {
	Tool   string          `json:"tool"`
	Args   json.RawMessage `json:"args,omitempty"`
	Result string          `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

type sendMessageHistoryTurn struct {
	Input        string                       `json:"input"`
	ToolCalls    []sendMessageHistoryToolCall `json:"tool_calls,omitempty"`
	ResponseText string                       `json:"response_text,omitempty"`
}

type sendMessageRequest struct {
	Input string `json:"input"`
	// StrategyID is an optional additive hint from the floating copilot
	// widget: when the operator has the strategy workbench open, the
	// widget sends the strategy currently being edited so the agent's
	// answers can be strategy-aware without the operator repeating "for
	// strategy #N" every message. This is folded into the plain-text
	// prompt below rather than threaded through RunToolLoop's signature -
	// RunToolLoop already takes a free-form userInput string, so this
	// stays additive and doesn't touch the safety-gated tool-loop plumbing
	// at all.
	StrategyID *uint                    `json:"strategy_id,omitempty"`
	History    []sendMessageHistoryTurn `json:"history,omitempty"`
	// AgentID selects the persona that answers (agents-platform A-02 §5);
	// omitted = the default "Copilot" agent.
	AgentID *uint `json:"agent_id,omitempty"`
}

// strategyContextMarker prefixes every user input that carries the
// StrategyID hint (see SendMessage below) - stripped back off in
// historyFromPersistedRuns before replay so it doesn't compound turn after
// turn (each of N historical turns would otherwise re-inject its own copy
// of this marker into the replayed history, wasting context for no
// benefit - the CURRENT turn already re-adds it once, which is enough for
// the model to know the strategy is still in scope).
const strategyContextMarker = "[Context: the operator currently has strategy #"

// historyLookbackLimit bounds how many of a strategy's past AgentRun rows
// get loaded and replayed as memory for a new turn - mirrors
// agentusecase.maxHistoryTurnsReplayed's cost/size reasoning; the usecase
// re-applies its own cap regardless, so this is a first line of defense
// against querying an unbounded number of rows for a strategy with a very
// long history.
const historyLookbackLimit = 20

func (req sendMessageRequest) toUseCaseHistory() []agentusecase.PriorTurn {
	out := make([]agentusecase.PriorTurn, len(req.History))
	for i, t := range req.History {
		calls := make([]agentusecase.PriorToolCall, len(t.ToolCalls))
		for j, c := range t.ToolCalls {
			calls[j] = agentusecase.PriorToolCall{Tool: c.Tool, Args: c.Args, Result: c.Result, Error: c.Error}
		}
		out[i] = agentusecase.PriorTurn{Input: t.Input, ToolCalls: calls, ResponseText: t.ResponseText}
	}
	return out
}

// historyFromPersistedRuns builds RunToolLoop's replay history straight
// from the database instead of trusting whatever the browser tab's
// in-memory transcript happens to still hold. Found missing entirely
// during manual verification: once a strategy actually exists, memory
// should belong to THAT STRATEGY, durably - reopening its editor tomorrow,
// in a different tab, after a reload, should continue the same
// conversation instead of starting blank. Client-sent history (see
// sendMessageRequest.History) remains the only source of memory for a chat
// that ISN'T yet strategy-scoped (general questions, or drafting a brand
// new strategy before it's ever been saved) - there is no natural DB key
// for that case yet.
//
// runs is repository.ListRuns' order (newest-first, "id DESC") - reversed
// here for chronological replay. Only Status == AgentRunOK runs are
// replayed: an errored turn (e.g. a transient model failure) has nothing
// coherent to feed back to the model and would just be noise.
func historyFromPersistedRuns(runs []entities.AgentRun) []agentusecase.PriorTurn {
	out := make([]agentusecase.PriorTurn, 0, len(runs))
	for i := len(runs) - 1; i >= 0; i-- {
		run := runs[i]
		if run.Status != entities.AgentRunOK {
			continue
		}

		var persisted []sendMessageHistoryToolCall
		if len(run.ToolCallsJSON) > 0 {
			_ = json.Unmarshal(run.ToolCallsJSON, &persisted)
		}
		toolCalls := make([]agentusecase.PriorToolCall, len(persisted))
		for j, c := range persisted {
			toolCalls[j] = agentusecase.PriorToolCall{Tool: c.Tool, Args: c.Args, Result: c.Result, Error: c.Error}
		}

		out = append(out, agentusecase.PriorTurn{
			Input:        stripStrategyContextMarker(run.InputSummary),
			ToolCalls:    toolCalls,
			ResponseText: run.ResponseText,
		})
	}
	return out
}

// stripStrategyContextMarker removes the "[Context: ...]\n\n" prefix
// SendMessage adds to every strategy-scoped input (see strategyContextMarker)
// before that input is replayed as history - the CURRENT turn already adds
// its own copy, so replaying N historical copies on top would just waste
// context budget for no benefit.
func stripStrategyContextMarker(input string) string {
	if !strings.HasPrefix(input, strategyContextMarker) {
		return input
	}
	if idx := strings.Index(input, "]\n\n"); idx != -1 {
		return input[idx+len("]\n\n"):]
	}
	return input
}

// SendMessage drives one full agent turn (Frontend Spec 01's chat panel).
// This is a synchronous, non-streaming call - RunToolLoop can run for
// several seconds (it may execute a backtest mid-loop), matching Backend
// Spec 01's non-streaming scope; the frontend shows a loading state for the
// duration.
//
// A run that finishes with entities.AgentRunError (a model failure, an
// exhausted tool-loop, or - most commonly in a freshly-deployed
// environment - no model provider configured) is still a normal HTTP 200
// with status:"error" in the body: the operator needs to SEE the failed
// run and its error_message in the transcript (Frontend Spec 01 AC#4), not
// receive an opaque 500. Only a request-shape problem (empty input) or a
// total inability to even create the audit row is a genuine HTTP error.
func (h *AgentHandler) SendMessage(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body: "+err.Error(), http.StatusBadRequest)
		return
	}

	var req sendMessageRequest
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "invalid request json: "+err.Error(), http.StatusBadRequest)
			return
		}
	}
	if req.Input == "" {
		http.Error(w, "input is required", http.StatusBadRequest)
		return
	}

	userInput := req.Input
	// history defaults to whatever the client sent (today's behavior for a
	// chat that isn't strategy-scoped yet). Once a strategy exists, memory
	// belongs to it durably in the database instead - see
	// historyFromPersistedRuns's doc comment for why.
	history := req.toUseCaseHistory()
	if req.StrategyID != nil {
		userInput = fmt.Sprintf("%s%d open in the workbench editor. Assume questions refer to it unless stated otherwise.]\n\n%s", strategyContextMarker, *req.StrategyID, req.Input)

		if priorRuns, err := h.repository.ListRuns(r.Context(), historyLookbackLimit, req.StrategyID); err == nil {
			history = historyFromPersistedRuns(priorRuns)
		}
		// A lookup failure here is not fatal to the turn itself - falling
		// back to the client-sent history (still set above) means the
		// operator loses cross-reload continuity for this one message
		// rather than the whole request failing.
	}

	if h.personas != nil {
		h.sendPersonaMessage(w, r, req, userInput, history)
		return
	}

	run, err := h.useCase.RunToolLoop(r.Context(), "chat_ui", userInput, history, req.StrategyID)
	if err != nil && run.ID == 0 {
		// RunToolLoop only returns a zero-ID run alongside an error when it
		// failed before ever persisting an AgentRun row (e.g. couldn't load
		// AgentInstruction) - there is nothing to render as a chat turn, so
		// this really is a 500.
		customerror.WriteHTTPError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(ToRunResponse(run))
}

// sendPersonaMessage is SendMessage's agents-platform path: the requested
// persona (default if agent_id is omitted) answers; a paused agent, the
// global kill switch or an exhausted budget is a 409 before any model call.
func (h *AgentHandler) sendPersonaMessage(w http.ResponseWriter, r *http.Request, req sendMessageRequest, userInput string, history []agentusecase.PriorTurn) {
	ctx := r.Context()
	agent, err := h.personas.ResolveAgent(ctx, req.AgentID)
	if err != nil {
		var ce *customerror.CustomError
		if errors.As(err, &ce) && ce.Code == http.StatusNotFound {
			writeJSONError(w, http.StatusNotFound, "not_found", ce.Message)
			return
		}
		customerror.WriteHTTPError(w, err)
		return
	}
	if gErr := h.personas.CheckGuard(ctx, agent); gErr != nil {
		writeJSONError(w, http.StatusConflict, "agent_unavailable", "agent "+strconv.Quote(agent.Name)+" cannot run: "+gErr.Error())
		return
	}

	run, err := h.personas.Run(ctx, agentusecase.RunRequest{
		Agent:        agent,
		Trigger:      "chat_ui",
		UserInput:    userInput,
		DisplayInput: req.Input,
		History:      history,
		StrategyID:   req.StrategyID,
	})
	if err != nil && run.ID == 0 {
		customerror.WriteHTTPError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(ToRunResponseWithNames(run, map[uint]string{agent.ID: agent.Name}))
}

// ListRuns implements GET /agent/runs (Frontend Specs 01/02, Phase D-01 §1):
//   - limit: default 20, capped at 100;
//   - strategy_id: a number, or "none" for runs without a strategy;
//   - trigger: comma-separated AgentRun triggers (unknown -> 400 invalid_trigger);
//   - agent_id: one persona's runs;
//   - before_id: cursor, only runs with id < before_id.
//
// Always newest first ("id DESC").
func (h *AgentHandler) ListRuns(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := agentrepo.RunFilter{Limit: defaultListLimit}
	if limitStr := q.Get("limit"); limitStr != "" {
		parsed, err := strconv.Atoi(limitStr)
		if err != nil || parsed <= 0 {
			http.Error(w, "invalid limit", http.StatusBadRequest)
			return
		}
		f.Limit = min(parsed, maxListLimit)
	}

	if idStr := q.Get("strategy_id"); idStr != "" {
		if idStr == "none" {
			f.NoStrategy = true
		} else {
			id, ok := parseUintParam(idStr)
			if !ok {
				http.Error(w, "invalid strategy_id", http.StatusBadRequest)
				return
			}
			f.StrategyID = &id
		}
	}

	if trigStr := q.Get("trigger"); trigStr != "" {
		for _, t := range strings.Split(trigStr, ",") {
			t = strings.TrimSpace(t)
			if t == "" {
				continue
			}
			if !validRunTriggers[t] {
				writeJSONError(w, http.StatusBadRequest, "invalid_trigger",
					"unknown trigger "+strconv.Quote(t)+" (valid: mcp_tool, chat_ui, monitor, cron, manual, event, market, chain)")
				return
			}
			f.Triggers = append(f.Triggers, t)
		}
	}

	if idStr := q.Get("agent_id"); idStr != "" {
		id, ok := parseUintParam(idStr)
		if !ok {
			http.Error(w, "invalid agent_id", http.StatusBadRequest)
			return
		}
		f.AgentID = &id
	}

	if idStr := q.Get("before_id"); idStr != "" {
		id, ok := parseUintParam(idStr)
		if !ok {
			http.Error(w, "invalid before_id", http.StatusBadRequest)
			return
		}
		f.BeforeID = &id
	}

	runs, err := h.repository.ListRunsFiltered(r.Context(), f)
	if err != nil {
		customerror.WriteHTTPError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(ToRunListResponseWithNames(runs, h.names(r.Context())))
}

func parseUintParam(s string) (uint, bool) {
	parsed, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return 0, false
	}
	return uint(parsed), true
}

func (h *AgentHandler) GetRun(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.ParseUint(vars["id"], 10, 32)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	run, err := h.repository.GetRun(r.Context(), uint(id))
	if err != nil {
		http.Error(w, "agent run not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(ToRunResponseWithNames(run, h.names(r.Context())))
}
