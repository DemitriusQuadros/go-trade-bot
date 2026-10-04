// Package agents is the REST transport for agent personas (agents-platform
// A-02 §5): CRUD, pause, manual runs, run history, usage, the global kill
// switch, and the per-strategy shared memory endpoints.
package agents

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"go-trade-bot/app/entities"
	agenthandler "go-trade-bot/app/handler/web/agent"
	usecase "go-trade-bot/app/usecase/agentplatform"
	"go-trade-bot/internal/authz"
	"go-trade-bot/internal/customerror"
	"go-trade-bot/internal/handler"

	"github.com/gorilla/mux"
)

// UseCase is the slice of app/usecase/agentplatform this handler needs.
type UseCase interface {
	ListAgents(ctx context.Context) ([]usecase.AgentView, error)
	GetAgent(ctx context.Context, id uint) (usecase.AgentView, error)
	CreateAgent(ctx context.Context, in usecase.AgentInput) (usecase.AgentView, error)
	UpdateAgent(ctx context.Context, id uint, in usecase.AgentInput) (usecase.AgentView, error)
	DeleteAgent(ctx context.Context, id uint) error
	SetPaused(ctx context.Context, id uint, paused bool) (usecase.AgentView, error)
	EnqueueManualRun(ctx context.Context, id uint, prompt string) (string, error)
	ListRuns(ctx context.Context, agentID uint, limit int, beforeID *uint) ([]entities.AgentRun, entities.Agent, error)
	Usage(ctx context.Context, agentID uint, days int) (usecase.UsageDay, []usecase.UsageDay, error)
	SetKillSwitch(ctx context.Context, paused bool) (bool, error)
	KillSwitch(ctx context.Context) (bool, error)
	ListMemory(ctx context.Context, strategyID uint, kinds []string, limit int, beforeID *uint) ([]entities.StrategyMemoryEntry, error)
	AddOperatorNote(ctx context.Context, strategyID uint, content string) (entities.StrategyMemoryEntry, error)
	AgentNames(ctx context.Context) map[uint]string
	MarketSymbols(ctx context.Context) (usecase.MarketSymbolsView, error)
}

// Handler serves the agents endpoints.
type Handler struct {
	useCase UseCase
}

// NewAgentsHandler builds the handler.
func NewAgentsHandler(u UseCase) *Handler {
	return &Handler{useCase: u}
}

// Handlers implements the Route interface. Patterns are bare (cmd/api adds
// /api). The {id} patterns are numeric-only, so /agents/kill-switch never
// collides with /agents/{id}.
func (h *Handler) Handlers() []handler.Configuration {
	return []handler.Configuration{
		{Pattern: "/agents", Method: http.MethodGet, Action: h.List, Capability: authz.CapView},
		{Pattern: "/agents", Method: http.MethodPost, Action: h.Create, Capability: authz.CapAdmin},
		{Pattern: "/agents/kill-switch", Method: http.MethodGet, Action: h.GetKillSwitch, Capability: authz.CapView},
		{Pattern: "/agents/kill-switch", Method: http.MethodPut, Action: h.KillSwitch, Capability: authz.CapAdmin},
		{Pattern: "/agents/market-symbols", Method: http.MethodGet, Action: h.MarketSymbols, Capability: authz.CapView},
		{Pattern: "/agents/{id:[0-9]+}", Method: http.MethodGet, Action: h.Get, Capability: authz.CapView},
		{Pattern: "/agents/{id:[0-9]+}", Method: http.MethodPut, Action: h.Update, Capability: authz.CapAdmin},
		{Pattern: "/agents/{id:[0-9]+}", Method: http.MethodDelete, Action: h.Delete, Capability: authz.CapAdmin},
		{Pattern: "/agents/{id:[0-9]+}/pause", Method: http.MethodPost, Action: h.Pause, Capability: authz.CapAdmin},
		{Pattern: "/agents/{id:[0-9]+}/run", Method: http.MethodPost, Action: h.Run, Capability: authz.CapAdmin},
		{Pattern: "/agents/{id:[0-9]+}/runs", Method: http.MethodGet, Action: h.Runs, Capability: authz.CapView},
		{Pattern: "/agents/{id:[0-9]+}/usage", Method: http.MethodGet, Action: h.Usage, Capability: authz.CapView},
		{Pattern: "/strategies/{id:[0-9]+}/memory", Method: http.MethodGet, Action: h.ListMemory, Capability: authz.CapView},
		{Pattern: "/strategies/{id:[0-9]+}/memory", Method: http.MethodPost, Action: h.AddMemory, Capability: authz.CapEditDrafts},
	}
}

// --- helpers -----------------------------------------------------------------

// WriteError writes {"error","message"} with the status carried by err.
func WriteError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	msg := err.Error()
	if usecase.IsChainCycle(err) {
		// C-01 §1: the frontend matches on this exact error code.
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "chain_cycle", "message": err.Error()})
		return
	}
	var ce *customerror.CustomError
	if errors.As(err, &ce) {
		if ce.ErrorCode != "" {
			writeJSON(w, ce.Code, map[string]string{"error": ce.ErrorCode, "message": ce.Message})
			return
		}
		status, msg = ce.Code, ce.Message
	}
	writeErrorBody(w, status, msg)
}

// MarketSymbols serves GET /agents/market-symbols (C-01 §6).
func (h *Handler) MarketSymbols(w http.ResponseWriter, r *http.Request) {
	v, err := h.useCase.MarketSymbols(r.Context())
	if err != nil {
		WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ToMarketSymbolsResponse(v))
}

func errorCode(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "validation_error"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusConflict:
		return "conflict"
	}
	return "internal_error"
}

func writeErrorBody(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": errorCode(status), "message": msg})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func pathID(r *http.Request) (uint, bool) {
	id, err := strconv.ParseUint(mux.Vars(r)["id"], 10, 32)
	return uint(id), err == nil && id > 0
}

func decodeBody(r *http.Request, v any) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return nil
	}
	return json.Unmarshal(body, v)
}

// queryInt parses an optional positive integer query param.
func queryInt(r *http.Request, name string) (int, bool, error) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return 0, false, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		return 0, false, errors.New("invalid " + name)
	}
	return v, true, nil
}

func queryUintPtr(r *http.Request, name string) (*uint, error) {
	v, ok, err := queryInt(r, name)
	if err != nil || !ok {
		return nil, err
	}
	u := uint(v)
	return &u, nil
}

// --- agents ------------------------------------------------------------------

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	views, err := h.useCase.ListAgents(r.Context())
	if err != nil {
		WriteError(w, err)
		return
	}
	out := make([]AgentResponse, 0, len(views))
	for _, v := range views {
		out = append(out, ToAgentResponse(v))
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeErrorBody(w, http.StatusBadRequest, "invalid id")
		return
	}
	v, err := h.useCase.GetAgent(r.Context(), id)
	if err != nil {
		WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ToAgentResponse(v))
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var req AgentRequest
	if err := decodeBody(r, &req); err != nil {
		writeErrorBody(w, http.StatusBadRequest, "invalid request json: "+err.Error())
		return
	}
	v, err := h.useCase.CreateAgent(r.Context(), req.ToInput())
	if err != nil {
		WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, ToAgentResponse(v))
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeErrorBody(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req AgentRequest
	if err := decodeBody(r, &req); err != nil {
		writeErrorBody(w, http.StatusBadRequest, "invalid request json: "+err.Error())
		return
	}
	v, err := h.useCase.UpdateAgent(r.Context(), id, req.ToInput())
	if err != nil {
		WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ToAgentResponse(v))
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeErrorBody(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.useCase.DeleteAgent(r.Context(), id); err != nil {
		WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type pauseRequest struct {
	Paused *bool `json:"paused"`
}

func (h *Handler) Pause(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeErrorBody(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req pauseRequest
	if err := decodeBody(r, &req); err != nil || req.Paused == nil {
		writeErrorBody(w, http.StatusBadRequest, `body must be {"paused": bool}`)
		return
	}
	v, err := h.useCase.SetPaused(r.Context(), id, *req.Paused)
	if err != nil {
		WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ToAgentResponse(v))
}

type runRequest struct {
	Prompt string `json:"prompt"`
}

type runEnqueuedResponse struct {
	Enqueued bool   `json:"enqueued"`
	TaskID   string `json:"task_id"`
}

func (h *Handler) Run(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeErrorBody(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req runRequest
	if err := decodeBody(r, &req); err != nil {
		writeErrorBody(w, http.StatusBadRequest, "invalid request json: "+err.Error())
		return
	}
	taskID, err := h.useCase.EnqueueManualRun(r.Context(), id, req.Prompt)
	if err != nil {
		WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, runEnqueuedResponse{Enqueued: true, TaskID: taskID})
}

func (h *Handler) Runs(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeErrorBody(w, http.StatusBadRequest, "invalid id")
		return
	}
	limit, _, err := queryInt(r, "limit")
	if err != nil {
		writeErrorBody(w, http.StatusBadRequest, err.Error())
		return
	}
	beforeID, err := queryUintPtr(r, "before_id")
	if err != nil {
		writeErrorBody(w, http.StatusBadRequest, err.Error())
		return
	}
	runs, agent, err := h.useCase.ListRuns(r.Context(), id, limit, beforeID)
	if err != nil {
		WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, agenthandler.ToRunListResponseWithNames(runs, map[uint]string{agent.ID: agent.Name}))
}

func (h *Handler) Usage(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeErrorBody(w, http.StatusBadRequest, "invalid id")
		return
	}
	days, _, err := queryInt(r, "days")
	if err != nil {
		writeErrorBody(w, http.StatusBadRequest, err.Error())
		return
	}
	today, all, err := h.useCase.Usage(r.Context(), id, days)
	if err != nil {
		WriteError(w, err)
		return
	}
	resp := UsageResponse{Today: toUsageDay(today), Days: make([]UsageDayResponse, 0, len(all))}
	for _, d := range all {
		resp.Days = append(resp.Days, toUsageDay(d))
	}
	writeJSON(w, http.StatusOK, resp)
}

type killSwitchRequest struct {
	Paused *bool `json:"paused"`
}

type killSwitchResponse struct {
	AgentsPaused bool `json:"agents_paused"`
}

// KillSwitch is PUT /agents/kill-switch {"paused": bool} -> {"agents_paused"}.
// It writes ONLY Settings.AgentsPaused - never the settings drain-and-swap
// path, never mode/confirm_live validation.
func (h *Handler) KillSwitch(w http.ResponseWriter, r *http.Request) {
	var req killSwitchRequest
	if err := decodeBody(r, &req); err != nil || req.Paused == nil {
		writeErrorBody(w, http.StatusBadRequest, `body must be {"paused": bool}`)
		return
	}
	paused, err := h.useCase.SetKillSwitch(r.Context(), *req.Paused)
	if err != nil {
		WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, killSwitchResponse{AgentsPaused: paused})
}

// GetKillSwitch is GET /agents/kill-switch -> {"agents_paused": bool}
// (capability view, so non-admins - who can't read GET /settings - can
// still show the state read-only).
func (h *Handler) GetKillSwitch(w http.ResponseWriter, r *http.Request) {
	paused, err := h.useCase.KillSwitch(r.Context())
	if err != nil {
		WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, killSwitchResponse{AgentsPaused: paused})
}

// --- strategy memory ---------------------------------------------------------

func (h *Handler) ListMemory(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeErrorBody(w, http.StatusBadRequest, "invalid id")
		return
	}
	var kinds []string
	if raw := strings.TrimSpace(r.URL.Query().Get("kinds")); raw != "" {
		for _, k := range strings.Split(raw, ",") {
			if k = strings.TrimSpace(k); k != "" {
				kinds = append(kinds, k)
			}
		}
	}
	limit, _, err := queryInt(r, "limit")
	if err != nil {
		writeErrorBody(w, http.StatusBadRequest, err.Error())
		return
	}
	beforeID, err := queryUintPtr(r, "before_id")
	if err != nil {
		writeErrorBody(w, http.StatusBadRequest, err.Error())
		return
	}
	entries, err := h.useCase.ListMemory(r.Context(), id, kinds, limit, beforeID)
	if err != nil {
		WriteError(w, err)
		return
	}
	names := h.useCase.AgentNames(r.Context())
	users := h.userNames(r.Context())
	out := make([]MemoryEntryResponse, 0, len(entries))
	for _, e := range entries {
		out = append(out, ToMemoryEntryResponseWithUsers(e, names, users))
	}
	writeJSON(w, http.StatusOK, out)
}

type addMemoryRequest struct {
	Content string `json:"content"`
}

func (h *Handler) AddMemory(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeErrorBody(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req addMemoryRequest
	if err := decodeBody(r, &req); err != nil {
		writeErrorBody(w, http.StatusBadRequest, "invalid request json: "+err.Error())
		return
	}
	entry, err := h.useCase.AddOperatorNote(r.Context(), id, req.Content)
	if err != nil {
		WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, ToMemoryEntryResponseWithUsers(entry, nil, h.userNames(r.Context())))
}

// userNamer is optionally implemented by the usecase (auth-01 §7).
type userNamer interface {
	UserNames(ctx context.Context) map[uint]string
}

func (h *Handler) userNames(ctx context.Context) map[uint]string {
	if n, ok := h.useCase.(userNamer); ok {
		return n.UserNames(ctx)
	}
	return nil
}
