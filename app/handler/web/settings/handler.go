package settings

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"go-trade-bot/app/entities"
	usecase "go-trade-bot/app/usecase/settings"
	"go-trade-bot/internal/handler"
)

// UseCase is the local interface this handler needs (this package's own
// consumer-side interface, per this codebase's convention - never the
// concrete usecase.UseCase type directly).
type UseCase interface {
	Get(ctx context.Context) (entities.Settings, error)
	Apply(ctx context.Context, next entities.Settings, confirmLive bool) (entities.Settings, error)
}

type SettingsHandler struct {
	UseCase UseCase
}

func NewSettingsHandler(u UseCase) *SettingsHandler {
	return &SettingsHandler{UseCase: u}
}

func (h *SettingsHandler) Handlers() []handler.Configuration {
	return []handler.Configuration{
		{
			Pattern: "/settings",
			Action:  h.GetSettings,
			Method:  http.MethodGet,
		},
		{
			Pattern: "/settings",
			Action:  h.PutSettings,
			Method:  http.MethodPut,
		},
	}
}

func (h *SettingsHandler) GetSettings(w http.ResponseWriter, r *http.Request) {
	s, err := h.UseCase.Get(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ToSettingsResponse(s))
}

func (h *SettingsHandler) PutSettings(w http.ResponseWriter, r *http.Request) {
	var req PlatformSettingsUpdateRequestDTO
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	existing, err := h.UseCase.Get(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	next := req.MergeInto(existing)

	applied, err := h.UseCase.Apply(r.Context(), next, req.ConfirmLive)
	if err != nil {
		h.writeApplyError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(PlatformSettingsUpdateResponseDTO{
		Settings: ToSettingsResponse(applied),
		Applied:  true,
	})
}

// writeApplyError maps usecase.UseCase.Apply's sentinel errors to the exact
// status codes/response bodies Spec backend-05's API surface specifies.
func (h *SettingsHandler) writeApplyError(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json")
	switch {
	case errors.Is(err, usecase.ErrConfirmLiveRequired):
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
	case errors.Is(err, usecase.ErrDrainTimeout):
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(DrainTimeoutErrorBody{
			Error:   "drain_timeout",
			Message: "timed out waiting for in-flight strategy cycles to drain; settings were not applied",
		})
	case errors.Is(err, usecase.ErrSwapFailed):
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
	default:
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
	}
}
