// Package internalapi holds HTTP handlers that are never part of the public
// REST surface (no auth-middleware wrapping, no frontend consumer) - they
// exist purely for host-internal, process-to-process coordination. Currently
// just cmd/worker's settings-apply bridge (Spec backend-05); see
// internal/settingsbridge's package doc for why this exists at all and why
// it must only ever be served on a dedicated loopback-only listener, never
// on cmd/worker's public :9191 monitoring server.
package internalapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"

	"go-trade-bot/app/entities"
	usecase "go-trade-bot/app/usecase/settings"
	"go-trade-bot/internal/settingsbridge"
)

// SettingsApplier is the local interface this handler needs - just
// usecase.UseCase.Apply, not the concrete type.
type SettingsApplier interface {
	Apply(ctx context.Context, next entities.Settings, confirmLive bool) (entities.Settings, error)
}

type SettingsHandler struct {
	UseCase SettingsApplier
	// Secret, when non-empty, must match the settingsbridge.InternalSecretHeader
	// value on every incoming request (defense-in-depth on top of the
	// loopback-only bind this handler is meant to be served behind - see
	// cmd/worker/main.go's StartInternalBridgeServer). Left empty by default
	// for backward compatibility with config.yml files that predate this
	// field; a startup warning is logged in that case (main.go), since the
	// loopback bind is then the only protection.
	Secret string
}

func NewSettingsHandler(u SettingsApplier, secret string) *SettingsHandler {
	return &SettingsHandler{UseCase: u, Secret: secret}
}

// Apply handles POST /internal/settings/apply - see internal/settingsbridge
// for the request/response contract.
func (h *SettingsHandler) Apply(w http.ResponseWriter, r *http.Request) {
	if h.Secret != "" {
		got := r.Header.Get(settingsbridge.InternalSecretHeader)
		// constant-time compare: this guards live-trading credentials/Mode,
		// worth the trivial cost of avoiding a timing side-channel.
		if subtle.ConstantTimeCompare([]byte(got), []byte(h.Secret)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}

	var req settingsbridge.ApplyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if _, err := h.UseCase.Apply(r.Context(), req.Settings, req.ConfirmLive); err != nil {
		h.writeError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(settingsbridge.SuccessResponse{Applied: true})
}

func (h *SettingsHandler) writeError(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json")
	switch {
	case errors.Is(err, usecase.ErrConfirmLiveRequired):
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(settingsbridge.ErrorResponse{Error: "confirm_live_required", Message: err.Error()})
	case errors.Is(err, usecase.ErrDrainTimeout):
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(settingsbridge.ErrorResponse{Error: "drain_timeout", Message: err.Error()})
	case errors.Is(err, usecase.ErrSwapFailed):
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(settingsbridge.ErrorResponse{Error: "swap_failed", Message: err.Error()})
	default:
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(settingsbridge.ErrorResponse{Error: "internal_error", Message: err.Error()})
	}
}
