package optimize

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"go-trade-bot/app/entities"
	usecase "go-trade-bot/app/usecase/optimize"
	"go-trade-bot/internal/handler"

	"github.com/gorilla/mux"
)

type UseCase interface {
	Create(ctx context.Context, req usecase.CreateRequest) (entities.OptimizationRun, error)
	GetByID(ctx context.Context, id uint) (entities.OptimizationRun, error)
	ListByStrategy(ctx context.Context, strategyID uint) ([]entities.OptimizationRun, error)
}

// Worker is the narrow enqueue-only slice of app/workers/optimize.OptimizeWorker
// this handler needs.
type Worker interface {
	EnqueueOptimizeTask(runID uint) error
}

type OptimizeHandler struct {
	useCase UseCase
	worker  Worker
}

func NewOptimizeHandler(u UseCase, w Worker) *OptimizeHandler {
	return &OptimizeHandler{useCase: u, worker: w}
}

func (h *OptimizeHandler) Handlers() []handler.Configuration {
	return []handler.Configuration{
		{
			Pattern: "/optimize",
			Action:  h.Create,
			Method:  http.MethodPost,
		},
		{
			Pattern: "/optimize/{id:[0-9]+}",
			Action:  h.GetByID,
			Method:  http.MethodGet,
		},
		{
			Pattern: "/optimize/{id:[0-9]+}/results",
			Action:  h.GetResults,
			Method:  http.MethodGet,
		},
		{
			Pattern: "/optimize",
			Action:  h.List,
			Method:  http.MethodGet,
		},
	}
}

// Create implements POST /optimize (backend-01): validates + persists a
// pending OptimizationRun, enqueues the asynq job, and returns 202
// immediately - it must NOT block for the grid search itself (spec AC#1).
func (h *OptimizeHandler) Create(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body: "+err.Error(), http.StatusBadRequest)
		return
	}

	var dto CreateOptimizationRequestDTO
	if err := json.Unmarshal(body, &dto); err != nil {
		http.Error(w, "invalid request json: "+err.Error(), http.StatusBadRequest)
		return
	}

	run, err := h.useCase.Create(r.Context(), dto.ToUseCase())
	if err != nil {
		writeCreateError(w, err)
		return
	}

	if err := h.worker.EnqueueOptimizeTask(run.ID); err != nil {
		http.Error(w, "failed to enqueue optimization job: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(ToCreateResponse(run))
}

func writeCreateError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, usecase.ErrGridTooLarge):
		http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
	case errors.Is(err, usecase.ErrMissingStrategyID),
		errors.Is(err, usecase.ErrMissingSymbol),
		errors.Is(err, usecase.ErrEmptyParamGrid),
		errors.Is(err, usecase.ErrInvalidStep) || errors.Is(err, usecase.ErrNoCandleData):
		http.Error(w, err.Error(), http.StatusBadRequest)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// GetByID implements GET /optimize/{id} - the poll target for the TUI's
// progress bar (spec AC#4).
func (h *OptimizeHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}

	run, err := h.useCase.GetByID(r.Context(), id)
	if err != nil {
		http.Error(w, "optimization run not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(ToStatusResponse(run))
}

// GetResults implements GET /optimize/{id}/results - 404 if the run doesn't
// exist, 409 if it exists but hasn't completed yet (spec AC#6), 200 with the
// full per-combination grid otherwise (spec AC#5).
func (h *OptimizeHandler) GetResults(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}

	run, err := h.useCase.GetByID(r.Context(), id)
	if err != nil {
		http.Error(w, "optimization run not found", http.StatusNotFound)
		return
	}

	if run.Status != entities.OptimizationCompleted {
		http.Error(w, "optimization run is not completed yet; keep polling GET /optimize/{id}", http.StatusConflict)
		return
	}

	resp, err := ToResultsResponse(run)
	if err != nil {
		http.Error(w, "failed to build results response: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// List implements GET /optimize?strategy_id= - the history list, mirroring
// GET /backtest?strategy_id='s shape/purpose.
func (h *OptimizeHandler) List(w http.ResponseWriter, r *http.Request) {
	stratIDStr := r.URL.Query().Get("strategy_id")
	if stratIDStr == "" {
		http.Error(w, "strategy_id query param is required", http.StatusBadRequest)
		return
	}

	stratID, err := strconv.ParseUint(stratIDStr, 10, 32)
	if err != nil {
		http.Error(w, "invalid strategy_id", http.StatusBadRequest)
		return
	}

	runs, err := h.useCase.ListByStrategy(r.Context(), uint(stratID))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	respList := make([]ListItemResponse, len(runs))
	for i, run := range runs {
		respList[i] = ToListItemResponse(run)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(respList)
}

func parseID(w http.ResponseWriter, r *http.Request) (uint, bool) {
	vars := mux.Vars(r)
	idStr, ok := vars["id"]
	if !ok {
		http.Error(w, "missing id", http.StatusBadRequest)
		return 0, false
	}
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return 0, false
	}
	return uint(id), true
}
