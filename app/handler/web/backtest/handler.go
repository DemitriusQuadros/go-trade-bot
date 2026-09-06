package backtest

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"go-trade-bot/app/engine"
	"go-trade-bot/app/entities"
	usecase "go-trade-bot/app/usecase/backtest"
	"go-trade-bot/internal/handler"

	"github.com/gorilla/mux"
)

type UseCase interface {
	Run(ctx context.Context, req usecase.RunRequest) (entities.BacktestRun, error)
	RunWalkForward(ctx context.Context, req usecase.WalkForwardRequest) (entities.BacktestRun, error)
	GetByID(ctx context.Context, id uint) (entities.BacktestRun, error)
	ListByStrategy(ctx context.Context, strategyID uint) ([]entities.BacktestRun, error)
	RunMonteCarlo(ctx context.Context, runID uint, iterations int) (engine.MonteCarloResult, error)
	GetMonteCarlo(ctx context.Context, runID uint) (engine.MonteCarloResult, error)
}

type BacktestHandler struct {
	useCase UseCase
}

func NewBacktestHandler(u UseCase) *BacktestHandler {
	return &BacktestHandler{
		useCase: u,
	}
}

func (h *BacktestHandler) Handlers() []handler.Configuration {
	return []handler.Configuration{
		{
			Pattern: "/backtest",
			Action:  h.RunBacktest,
			Method:  http.MethodPost,
		},
		{
			Pattern: "/backtest/walkforward",
			Action:  h.RunWalkForward,
			Method:  http.MethodPost,
		},
		{
			Pattern: "/backtest/{id:[0-9]+}",
			Action:  h.GetByID,
			Method:  http.MethodGet,
		},
		{
			Pattern: "/backtest",
			Action:  h.List,
			Method:  http.MethodGet,
		},
		{
			Pattern: "/backtest/{id:[0-9]+}/montecarlo",
			Action:  h.RunMonteCarlo,
			Method:  http.MethodPost,
		},
		{
			Pattern: "/backtest/{id:[0-9]+}/montecarlo",
			Action:  h.GetMonteCarlo,
			Method:  http.MethodGet,
		},
	}
}

func (h *BacktestHandler) RunBacktest(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body: "+err.Error(), http.StatusBadRequest)
		return
	}

	var req RunRequestDTO
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid request json: "+err.Error(), http.StatusBadRequest)
		return
	}

	run, err := h.useCase.Run(r.Context(), req.ToUseCase())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := ToRunResponse(run, true)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *BacktestHandler) RunWalkForward(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body: "+err.Error(), http.StatusBadRequest)
		return
	}

	var req WalkForwardRequestDTO
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid request json: "+err.Error(), http.StatusBadRequest)
		return
	}

	run, err := h.useCase.RunWalkForward(r.Context(), req.ToUseCase())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := ToRunResponse(run, true)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *BacktestHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	idStr, ok := vars["id"]
	if !ok {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}

	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	run, err := h.useCase.GetByID(r.Context(), uint(id))
	if err != nil {
		http.Error(w, "backtest run not found", http.StatusNotFound)
		return
	}

	resp := ToRunResponse(run, true)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *BacktestHandler) List(w http.ResponseWriter, r *http.Request) {
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

	respList := make([]BacktestRunResponse, len(runs))
	for i, r := range runs {
		respList[i] = ToRunResponse(r, false)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(respList)
}

type monteCarloRequestDTO struct {
	Iterations int `json:"iterations"`
}

// RunMonteCarlo implements backend-03's POST /backtest/{id}/montecarlo:
// 404 (run not found), 400 (iterations <= 0 or > cap), 422 (fewer than 2
// trades - reordering is meaningless), 200 with the computed distribution
// otherwise. The use case itself persists the result onto the run row so a
// subsequent GET doesn't recompute it.
func (h *BacktestHandler) RunMonteCarlo(w http.ResponseWriter, r *http.Request) {
	id, ok := parseBacktestID(w, r)
	if !ok {
		return
	}

	var dto monteCarloRequestDTO
	if r.ContentLength != 0 {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "failed to read body: "+err.Error(), http.StatusBadRequest)
			return
		}
		if len(body) > 0 {
			if err := json.Unmarshal(body, &dto); err != nil {
				http.Error(w, "invalid request json: "+err.Error(), http.StatusBadRequest)
				return
			}
		}
	}

	result, err := h.useCase.RunMonteCarlo(r.Context(), id, dto.Iterations)
	if err != nil {
		writeMonteCarloError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

// GetMonteCarlo implements backend-03's GET /backtest/{id}/montecarlo:
// returns the cached result, 404 if the run doesn't exist or Monte Carlo has
// never been computed for it.
func (h *BacktestHandler) GetMonteCarlo(w http.ResponseWriter, r *http.Request) {
	id, ok := parseBacktestID(w, r)
	if !ok {
		return
	}

	result, err := h.useCase.GetMonteCarlo(r.Context(), id)
	if err != nil {
		if errors.Is(err, usecase.ErrMonteCarloNotYetComputed) || errors.Is(err, usecase.ErrBacktestRunNotFound) {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func writeMonteCarloError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, usecase.ErrInvalidMonteCarloIterations):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, usecase.ErrInsufficientTradesForMonteCarlo):
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
	case errors.Is(err, usecase.ErrBacktestRunNotFound):
		http.Error(w, "backtest run not found", http.StatusNotFound)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func parseBacktestID(w http.ResponseWriter, r *http.Request) (uint, bool) {
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
