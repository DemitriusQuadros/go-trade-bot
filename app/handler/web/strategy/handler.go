package handler

import (
	"context"
	"encoding/json"
	"go-trade-bot/app/entities"
	"go-trade-bot/internal/handler"
	"io"
	"net/http"
)

type UseCase interface {
	Save(ctx context.Context, strategy entities.Strategy) error
	Enqueue(ctx context.Context) error
	GetAll(ctx context.Context) ([]entities.Strategy, error)
}
type StrategyHandler struct {
	UseCase UseCase
}

func NewStrategyHandler(u UseCase) *StrategyHandler {
	return &StrategyHandler{
		UseCase: u,
	}
}

func (h *StrategyHandler) Handlers() []handler.Configuration {
	return []handler.Configuration{
		{
			Pattern: "/strategy",
			Action:  h.Post,
			Method:  http.MethodPost,
		},
		{
			Pattern: "/strategy/enqueue",
			Action:  h.Enqueue,
			Method:  http.MethodPost,
		},
		{
			Pattern: "/strategy",
			Action:  h.GetAll,
			Method:  http.MethodGet,
		},
	}
}

func (h *StrategyHandler) Post(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.Reader(r.Body))
	if err != nil {
		http.Error(w, "Invalid Body", http.StatusInternalServerError)
		return
	}
	defer r.Body.Close()

	var dto StrategyDto
	err = json.Unmarshal(body, &dto)

	if err != nil {
		http.Error(w, "Error converting body fields", http.StatusBadRequest)
		return
	}

	err = h.UseCase.Save(r.Context(), dto.ToModel())

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}

	w.WriteHeader(http.StatusCreated)
}

func (h *StrategyHandler) Enqueue(w http.ResponseWriter, r *http.Request) {
	/*err := UseCase.Enqueue(r.Context(),)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
	}*/
	w.WriteHeader(http.StatusAccepted)
}

func (h *StrategyHandler) GetAll(w http.ResponseWriter, r *http.Request) {
	strategies, err := h.UseCase.GetAll(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(strategies)
}
