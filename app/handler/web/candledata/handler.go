// Package candledata serves the candle-dataset management REST API
// (docs/specs/candle-data/candle-dataset-reconciler-design.md section 7).
package candledata

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go-trade-bot/app/entities"
	usecase "go-trade-bot/app/usecase/candledata"
	"go-trade-bot/internal/authz"
	"go-trade-bot/internal/handler"

	"github.com/gorilla/mux"
	"gorm.io/gorm"
)

// Manager is what the handler needs from the reconciler service.
type Manager interface {
	List(ctx context.Context) ([]*usecase.DatasetStatus, error)
	Status(ctx context.Context, id uint) (*usecase.DatasetStatus, error)
	Create(ctx context.Context, symbol, timeframe string, start *time.Time, keepLive bool) (*entities.CandleDataset, error)
	Pause(ctx context.Context, id uint) error
	Resume(ctx context.Context, id uint) error
	RetryFailed(ctx context.Context, id uint) (int, error)
	Reconcile(ctx context.Context, id uint) (int, error)
	Delete(ctx context.Context, id uint) error
	Chunks(ctx context.Context, id uint, status entities.ChunkStatus, limit int) ([]entities.CandleChunk, error)
}

type Handler struct{ mgr Manager }

func NewHandler(svc *usecase.Service) *Handler { return &Handler{mgr: svc} }

func (h *Handler) Handlers() []handler.Configuration {
	return []handler.Configuration{
		{Pattern: "/candle-datasets", Action: h.List, Method: http.MethodGet, Capability: authz.CapView},
		{Pattern: "/candle-datasets", Action: h.Create, Method: http.MethodPost, Capability: authz.CapAdmin},
		{Pattern: "/candle-datasets/{id}", Action: h.Get, Method: http.MethodGet, Capability: authz.CapView},
		{Pattern: "/candle-datasets/{id}", Action: h.Delete, Method: http.MethodDelete, Capability: authz.CapAdmin},
		{Pattern: "/candle-datasets/{id}/chunks", Action: h.Chunks, Method: http.MethodGet, Capability: authz.CapView},
		{Pattern: "/candle-datasets/{id}/pause", Action: h.Pause, Method: http.MethodPost, Capability: authz.CapAdmin},
		{Pattern: "/candle-datasets/{id}/resume", Action: h.Resume, Method: http.MethodPost, Capability: authz.CapAdmin},
		{Pattern: "/candle-datasets/{id}/retry-failed", Action: h.RetryFailed, Method: http.MethodPost, Capability: authz.CapAdmin},
		{Pattern: "/candle-datasets/{id}/reconcile", Action: h.Reconcile, Method: http.MethodPost, Capability: authz.CapAdmin},
	}
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	all, err := h.mgr.List(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	out := make([]DatasetResponse, 0, len(all))
	for _, st := range all {
		out = append(out, toResponse(st))
	}
	writeJSON(w, http.StatusOK, out)
}

type createRequest struct {
	Symbol    string  `json:"symbol"`
	Timeframe string  `json:"timeframe"`
	Start     *string `json:"start"` // RFC3339 or YYYY-MM-DD; absent = from listing
	KeepLive  *bool   `json:"keep_live"`
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if err := decodeBody(r, &req); err != nil {
		writeErrorBody(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	var start *time.Time
	if req.Start != nil && strings.TrimSpace(*req.Start) != "" {
		t, err := parseTime(*req.Start)
		if err != nil {
			writeErrorBody(w, http.StatusBadRequest, "bad_request", "start must be RFC3339 or YYYY-MM-DD")
			return
		}
		start = &t
	}
	keepLive := req.KeepLive == nil || *req.KeepLive
	ds, err := h.mgr.Create(r.Context(), strings.ToUpper(strings.TrimSpace(req.Symbol)), strings.TrimSpace(req.Timeframe), start, keepLive)
	if err != nil {
		writeError(w, err)
		return
	}
	h.respondStatus(w, r, ds.ID, http.StatusCreated)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	h.respondStatus(w, r, id, http.StatusOK)
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := h.mgr.Delete(r.Context(), id); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) Pause(w http.ResponseWriter, r *http.Request) {
	h.act(w, r, func(ctx context.Context, id uint) error { return h.mgr.Pause(ctx, id) })
}

func (h *Handler) Resume(w http.ResponseWriter, r *http.Request) {
	h.act(w, r, func(ctx context.Context, id uint) error { return h.mgr.Resume(ctx, id) })
}

func (h *Handler) RetryFailed(w http.ResponseWriter, r *http.Request) {
	h.act(w, r, func(ctx context.Context, id uint) error { _, err := h.mgr.RetryFailed(ctx, id); return err })
}

func (h *Handler) Reconcile(w http.ResponseWriter, r *http.Request) {
	h.act(w, r, func(ctx context.Context, id uint) error { _, err := h.mgr.Reconcile(ctx, id); return err })
}

// act runs a dataset action and returns the refreshed status.
func (h *Handler) act(w http.ResponseWriter, r *http.Request, fn func(context.Context, uint) error) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := fn(r.Context(), id); err != nil {
		writeError(w, err)
		return
	}
	h.respondStatus(w, r, id, http.StatusOK)
}

func (h *Handler) Chunks(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	chunks, err := h.mgr.Chunks(r.Context(), id, entities.ChunkStatus(r.URL.Query().Get("status")), limit)
	if err != nil {
		writeError(w, err)
		return
	}
	out := make([]ChunkResponse, 0, len(chunks))
	for _, c := range chunks {
		out = append(out, toChunkResponse(c))
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) respondStatus(w http.ResponseWriter, r *http.Request, id uint, status int) {
	st, err := h.mgr.Status(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, status, toResponse(st))
}

func parseTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	t, err := time.Parse("2006-01-02", s)
	return t.UTC(), err
}

func pathID(w http.ResponseWriter, r *http.Request) (uint, bool) {
	id, err := strconv.ParseUint(mux.Vars(r)["id"], 10, 32)
	if err != nil || id == 0 {
		writeErrorBody(w, http.StatusBadRequest, "bad_request", "invalid dataset id")
		return 0, false
	}
	return uint(id), true
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

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErrorBody(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]string{"error": code, "message": msg})
}

func writeError(w http.ResponseWriter, err error) {
	var inv usecase.ErrInvalid
	switch {
	case errors.As(err, &inv):
		writeErrorBody(w, http.StatusBadRequest, "invalid", inv.Msg)
	case errors.Is(err, gorm.ErrRecordNotFound):
		writeErrorBody(w, http.StatusNotFound, "not_found", "dataset not found")
	default:
		writeErrorBody(w, http.StatusInternalServerError, "internal_error", err.Error())
	}
}
