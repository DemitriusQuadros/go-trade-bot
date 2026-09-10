// Package script (web handler) exposes the Lua REPL and fast-rerun endpoints
// (backend-08): POST /api/script/repl and POST /api/script/fast-rerun.
package script

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	usecase "go-trade-bot/app/usecase/script"
	"go-trade-bot/internal/handler"
)

// UseCase is this handler's local consumer-side interface (never the concrete
// usecase type), per this codebase's convention.
type UseCase interface {
	Eval(ctx context.Context, req usecase.EvalRequest) (usecase.EvalResponse, error)
	FastRerun(ctx context.Context, req usecase.FastRerunRequest) (usecase.FastRerunResponse, error)
}

type ScriptHandler struct {
	UseCase UseCase
}

func NewScriptHandler(u UseCase) *ScriptHandler {
	return &ScriptHandler{UseCase: u}
}

func (h *ScriptHandler) Handlers() []handler.Configuration {
	return []handler.Configuration{
		{
			Pattern: "/api/script/repl",
			Action:  h.Repl,
			Method:  http.MethodPost,
		},
		{
			Pattern: "/api/script/fast-rerun",
			Action:  h.FastRerun,
			Method:  http.MethodPost,
		},
	}
}

// Repl handles POST /api/script/repl. A malformed body is 400; an infra-level
// usecase error is 500; a script-level error is 200 with the response's Error
// field populated (a deliberate spec decision - a bad snippet is normal REPL
// output, not a transport failure).
func (h *ScriptHandler) Repl(w http.ResponseWriter, r *http.Request) {
	var req usecase.EvalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Symbol == "" {
		http.Error(w, "symbol is required", http.StatusBadRequest)
		return
	}

	resp, err := h.UseCase.Eval(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, resp)
}

// FastRerun handles POST /api/script/fast-rerun. A strategy-not-found lookup is
// mapped to 404; any other infra-level usecase error is 500; a script-level
// (parse) error is 200 with the response's Error field populated.
func (h *ScriptHandler) FastRerun(w http.ResponseWriter, r *http.Request) {
	var req usecase.FastRerunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Symbol == "" {
		http.Error(w, "symbol is required", http.StatusBadRequest)
		return
	}

	resp, err := h.UseCase.FastRerun(r.Context(), req)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, errNotFound) || isNotFound(err) {
			status = http.StatusNotFound
		}
		http.Error(w, err.Error(), status)
		return
	}

	writeJSON(w, resp)
}

var errNotFound = errors.New("not found")

// isNotFound recognizes the fast-rerun strategy-lookup failure so it maps to
// 404 rather than 500.
func isNotFound(err error) bool {
	return err != nil && containsNotFound(err.Error())
}

func containsNotFound(s string) bool {
	for i := 0; i+9 <= len(s); i++ {
		if s[i:i+9] == "not found" {
			return true
		}
	}
	return false
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(v)
}
