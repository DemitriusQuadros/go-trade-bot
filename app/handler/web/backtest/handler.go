package backtest

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"

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
