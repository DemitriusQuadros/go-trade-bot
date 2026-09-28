// Package webhooktargets is the REST transport for agent notification
// targets (agents-platform A-02 §5). url and secret are always masked in
// responses; a masked value sent back on PUT means "keep the stored value".
package webhooktargets

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"go-trade-bot/app/entities"
	settingshandler "go-trade-bot/app/handler/web/settings"
	usecase "go-trade-bot/app/usecase/agentplatform"
	"go-trade-bot/internal/customerror"
	"go-trade-bot/internal/handler"

	"github.com/gorilla/mux"
)

// UseCase is the slice of app/usecase/agentplatform this handler needs.
type UseCase interface {
	ListWebhookTargets(ctx context.Context) ([]entities.WebhookTarget, error)
	CreateWebhookTarget(ctx context.Context, in usecase.WebhookTargetInput) (entities.WebhookTarget, error)
	UpdateWebhookTarget(ctx context.Context, id uint, in usecase.WebhookTargetInput) (entities.WebhookTarget, error)
	DeleteWebhookTarget(ctx context.Context, id uint) error
	TestWebhookTarget(ctx context.Context, id uint) error
}

// Handler serves /webhook-targets.
type Handler struct {
	useCase UseCase
}

// NewWebhookTargetsHandler builds the handler.
func NewWebhookTargetsHandler(u UseCase) *Handler {
	return &Handler{useCase: u}
}

// Handlers implements the Route interface.
func (h *Handler) Handlers() []handler.Configuration {
	return []handler.Configuration{
		{Pattern: "/webhook-targets", Method: http.MethodGet, Action: h.List},
		{Pattern: "/webhook-targets", Method: http.MethodPost, Action: h.Create},
		{Pattern: "/webhook-targets/{id:[0-9]+}", Method: http.MethodPut, Action: h.Update},
		{Pattern: "/webhook-targets/{id:[0-9]+}", Method: http.MethodDelete, Action: h.Delete},
		{Pattern: "/webhook-targets/{id:[0-9]+}/test", Method: http.MethodPost, Action: h.Test},
	}
}

// WebhookTargetDTO is both the request and (masked) response shape.
type WebhookTargetDTO struct {
	ID      uint   `json:"id,omitempty"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	URL     string `json:"url"`
	Secret  string `json:"secret"`
	ChatID  string `json:"chat_id"`
	Enabled bool   `json:"enabled"`
}

// ToResponse masks url and secret with settings.MaskSecret.
func ToResponse(t entities.WebhookTarget) WebhookTargetDTO {
	return WebhookTargetDTO{
		ID: t.ID, Name: t.Name, Kind: string(t.Kind),
		URL:    settingshandler.MaskSecret(t.URL),
		Secret: settingshandler.MaskSecret(t.Secret),
		ChatID: t.ChatID, Enabled: t.Enabled,
	}
}

// keep mirrors settings' resolveSecret: empty or a masked placeholder means
// "keep the existing stored value".
func keep(v string) bool {
	return v == "" || settingshandler.IsMaskedPlaceholder(v)
}

func (d WebhookTargetDTO) toInput(isUpdate bool) usecase.WebhookTargetInput {
	in := usecase.WebhookTargetInput{Name: d.Name, Kind: d.Kind, URL: d.URL, Secret: d.Secret, ChatID: d.ChatID, Enabled: d.Enabled}
	if isUpdate {
		in.KeepURL = keep(d.URL)
		in.KeepSecret = keep(d.Secret)
	} else {
		// On create there is nothing to keep: a masked value is not a real
		// secret, so it is dropped (and validation reports it missing).
		in.KeepURL = settingshandler.IsMaskedPlaceholder(d.URL)
		in.KeepSecret = settingshandler.IsMaskedPlaceholder(d.Secret)
	}
	return in
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, err error) {
	var ref *usecase.ReferencedError
	if errors.As(err, &ref) {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "conflict", "message": ref.Error(), "agents": ref.AgentNames})
		return
	}
	status, msg := http.StatusInternalServerError, err.Error()
	var ce *customerror.CustomError
	if errors.As(err, &ce) {
		status, msg = ce.Code, ce.Message
	}
	code := map[int]string{http.StatusBadRequest: "validation_error", http.StatusNotFound: "not_found", http.StatusConflict: "conflict"}[status]
	if code == "" {
		code = "internal_error"
	}
	writeJSON(w, status, map[string]string{"error": code, "message": msg})
}

func pathID(w http.ResponseWriter, r *http.Request) (uint, bool) {
	id, err := strconv.ParseUint(mux.Vars(r)["id"], 10, 32)
	if err != nil || id == 0 {
		writeError(w, &customerror.CustomError{Code: http.StatusBadRequest, Message: "invalid id"})
		return 0, false
	}
	return uint(id), true
}

func decode(w http.ResponseWriter, r *http.Request) (WebhookTargetDTO, bool) {
	var d WebhookTargetDTO
	body, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err == nil {
		err = json.Unmarshal(body, &d)
	}
	if err != nil {
		writeError(w, &customerror.CustomError{Code: http.StatusBadRequest, Message: "invalid request json"})
		return d, false
	}
	return d, true
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	targets, err := h.useCase.ListWebhookTargets(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	out := make([]WebhookTargetDTO, 0, len(targets))
	for _, t := range targets {
		out = append(out, ToResponse(t))
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	d, ok := decode(w, r)
	if !ok {
		return
	}
	t, err := h.useCase.CreateWebhookTarget(r.Context(), d.toInput(false))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, ToResponse(t))
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	d, ok := decode(w, r)
	if !ok {
		return
	}
	t, err := h.useCase.UpdateWebhookTarget(r.Context(), id, d.toInput(true))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ToResponse(t))
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := h.useCase.DeleteWebhookTarget(r.Context(), id); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type testResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// Test is POST /webhook-targets/{id}/test: 200 {"ok": bool, "error"?}.
// Delivery failures are reported in the body (never containing the URL or
// token), not as an HTTP error.
func (h *Handler) Test(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := h.useCase.TestWebhookTarget(r.Context(), id); err != nil {
		var ce *customerror.CustomError
		if errors.As(err, &ce) && ce.Code == http.StatusNotFound {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, testResponse{OK: false, Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, testResponse{OK: true})
}
