package performancehistory

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"go-trade-bot/app/entities"
	usecase "go-trade-bot/app/usecase/performancehistory"
	"go-trade-bot/internal/handler"

	"github.com/gorilla/mux"
)

type UseCase interface {
	GetHistory(ctx context.Context, strategyID uint, symbol string, bucket entities.PerformanceBucket, limit int) ([]usecase.HistoryPoint, error)
}

type Handler struct {
	useCase UseCase
}

func NewHandler(u UseCase) *Handler {
	return &Handler{useCase: u}
}

func (h *Handler) Handlers() []handler.Configuration {
	return []handler.Configuration{
		{
			Pattern: "/strategy/{id:[0-9]+}/performance/history",
			Action:  h.GetHistory,
			Method:  http.MethodGet,
		},
	}
}

// GetHistory implements backend-05's frozen contract:
// GET /strategy/{id}/performance/history?symbol=&bucket=daily|weekly|monthly&limit=
func (h *Handler) GetHistory(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.ParseUint(vars["id"], 10, 32)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	symbol := r.URL.Query().Get("symbol")
	if symbol == "" {
		http.Error(w, "symbol query param is required", http.StatusBadRequest)
		return
	}

	bucketStr := r.URL.Query().Get("bucket")
	bucket := entities.PerformanceBucket(bucketStr)
	switch bucket {
	case entities.BucketDaily, entities.BucketWeekly, entities.BucketMonthly:
		// valid
	default:
		http.Error(w, `bucket must be one of "daily", "weekly", "monthly"`, http.StatusBadRequest)
		return
	}

	limit := 30
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		parsed, err := strconv.Atoi(limitStr)
		if err != nil || parsed < 1 || parsed > 365 {
			http.Error(w, "limit must be an integer in [1, 365]", http.StatusBadRequest)
			return
		}
		limit = parsed
	}

	history, err := h.useCase.GetHistory(r.Context(), uint(id), symbol, bucket, limit)
	if err != nil {
		if errors.Is(err, usecase.ErrStrategyNotFound) {
			http.Error(w, "strategy not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if history == nil {
		history = []usecase.HistoryPoint{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(history)
}
