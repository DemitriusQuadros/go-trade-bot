// Package proposals is the REST transport for agents-platform Phase B-01
// §5: strategy change proposals (list, detail, approve, reject, pending
// count) and the deploy gate thresholds. Approval is ONLY possible here
// (authenticated REST, cmd/api) - never via webhook or an LLM tool.
package proposals

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"go-trade-bot/app/entities"
	agentusecase "go-trade-bot/app/usecase/agent"
	usecase "go-trade-bot/app/usecase/proposal"
	"go-trade-bot/internal/customerror"
	"go-trade-bot/internal/handler"

	"github.com/gorilla/mux"
)

// UseCase is the slice of app/usecase/proposal this handler needs.
type UseCase interface {
	List(ctx context.Context, f usecase.ListFilter) ([]usecase.View, error)
	PendingCount(ctx context.Context) (int64, error)
	Get(ctx context.Context, id uint) (usecase.View, error)
	Approve(ctx context.Context, id uint, note string) (usecase.View, error)
	Reject(ctx context.Context, id uint, note string) (usecase.View, error)
	GateConfig(ctx context.Context) (entities.DeployGateConfig, error)
	UpdateGateConfig(ctx context.Context, c entities.DeployGateConfig) (entities.DeployGateConfig, error)
}

// Handler serves the proposal and deploy-gate endpoints.
type Handler struct {
	useCase UseCase
}

// NewProposalsHandler builds the handler.
func NewProposalsHandler(u UseCase) *Handler {
	return &Handler{useCase: u}
}

// Handlers implements the Route interface (cmd/api adds /api). The {id}
// patterns are numeric-only, so /proposals/pending-count never collides.
func (h *Handler) Handlers() []handler.Configuration {
	return []handler.Configuration{
		{Pattern: "/proposals", Method: http.MethodGet, Action: h.List},
		{Pattern: "/proposals/pending-count", Method: http.MethodGet, Action: h.PendingCount},
		{Pattern: "/proposals/{id:[0-9]+}", Method: http.MethodGet, Action: h.Get},
		{Pattern: "/proposals/{id:[0-9]+}/approve", Method: http.MethodPost, Action: h.Approve},
		{Pattern: "/proposals/{id:[0-9]+}/reject", Method: http.MethodPost, Action: h.Reject},
		{Pattern: "/deploy-gate", Method: http.MethodGet, Action: h.GetGate},
		{Pattern: "/deploy-gate", Method: http.MethodPut, Action: h.PutGate},
	}
}

const maxNoteChars = 2000

// --- helpers -----------------------------------------------------------------

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

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErrorBody(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]string{"error": code, "message": msg})
}

// writeError maps usecase errors: SupersededError -> 409 {"error":"superseded"},
// CustomError -> its code, anything else -> 500.
func writeError(w http.ResponseWriter, err error) {
	var sup *usecase.SupersededError
	if errors.As(err, &sup) {
		writeErrorBody(w, http.StatusConflict, "superseded", sup.Message)
		return
	}
	var ce *customerror.CustomError
	if errors.As(err, &ce) {
		writeErrorBody(w, ce.Code, errorCode(ce.Code), ce.Message)
		return
	}
	writeErrorBody(w, http.StatusInternalServerError, "internal_error", err.Error())
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

func queryUintPtr(r *http.Request, name string) (*uint, error) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return nil, nil
	}
	v, err := strconv.ParseUint(raw, 10, 32)
	if err != nil || v == 0 {
		return nil, errors.New("invalid " + name)
	}
	u := uint(v)
	return &u, nil
}

// --- proposals ---------------------------------------------------------------

// List serves GET /proposals?status=a,b&strategy_id=&agent_id=&limit=&before_id=.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	statuses, err := agentusecase.ParseProposalStatuses(r.URL.Query().Get("status"))
	if err != nil {
		writeErrorBody(w, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	f := usecase.ListFilter{Statuses: statuses}
	for name, dst := range map[string]**uint{"strategy_id": &f.StrategyID, "agent_id": &f.AgentID, "before_id": &f.BeforeID} {
		v, err := queryUintPtr(r, name)
		if err != nil {
			writeErrorBody(w, http.StatusBadRequest, "validation_error", err.Error())
			return
		}
		*dst = v
	}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			writeErrorBody(w, http.StatusBadRequest, "validation_error", "invalid limit")
			return
		}
		if n > 200 {
			n = 200
		}
		f.Limit = n
	}
	views, err := h.useCase.List(r.Context(), f)
	if err != nil {
		writeError(w, err)
		return
	}
	out := make([]ListItem, 0, len(views))
	for _, v := range views {
		out = append(out, ToListItem(v))
	}
	writeJSON(w, http.StatusOK, out)
}

// PendingCount serves GET /proposals/pending-count.
func (h *Handler) PendingCount(w http.ResponseWriter, r *http.Request) {
	n, err := h.useCase.PendingCount(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"count": n})
}

// Get serves GET /proposals/{id}.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeErrorBody(w, http.StatusBadRequest, "validation_error", "invalid id")
		return
	}
	v, err := h.useCase.Get(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ToDetail(v))
}

type decisionRequest struct {
	Note string `json:"note"`
}

func (h *Handler) decide(w http.ResponseWriter, r *http.Request, fn func(ctx context.Context, id uint, note string) (usecase.View, error)) {
	id, ok := pathID(r)
	if !ok {
		writeErrorBody(w, http.StatusBadRequest, "validation_error", "invalid id")
		return
	}
	var req decisionRequest
	if err := decodeBody(r, &req); err != nil {
		writeErrorBody(w, http.StatusBadRequest, "validation_error", "invalid request json: "+err.Error())
		return
	}
	req.Note = strings.TrimSpace(req.Note)
	if len([]rune(req.Note)) > maxNoteChars {
		writeErrorBody(w, http.StatusBadRequest, "validation_error", "note must be at most 2000 characters")
		return
	}
	v, err := fn(r.Context(), id, req.Note)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ToDetail(v))
}

// Approve serves POST /proposals/{id}/approve.
func (h *Handler) Approve(w http.ResponseWriter, r *http.Request) { h.decide(w, r, h.useCase.Approve) }

// Reject serves POST /proposals/{id}/reject.
func (h *Handler) Reject(w http.ResponseWriter, r *http.Request) { h.decide(w, r, h.useCase.Reject) }

// --- deploy gate ---------------------------------------------------------------

// GetGate serves GET /deploy-gate.
func (h *Handler) GetGate(w http.ResponseWriter, r *http.Request) {
	c, err := h.useCase.GateConfig(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ToGateConfig(c))
}

// PutGate serves PUT /deploy-gate (full replace, validated).
func (h *Handler) PutGate(w http.ResponseWriter, r *http.Request) {
	var req GateConfigRequest
	if err := decodeBody(r, &req); err != nil {
		writeErrorBody(w, http.StatusBadRequest, "validation_error", "invalid request json: "+err.Error())
		return
	}
	c, err := req.ToEntity()
	if err != nil {
		writeErrorBody(w, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	saved, err := h.useCase.UpdateGateConfig(r.Context(), c)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ToGateConfig(saved))
}
