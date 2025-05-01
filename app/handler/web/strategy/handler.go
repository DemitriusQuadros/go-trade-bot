package handler

import (
	"context"
	"encoding/json"
	"go-trade-bot/app/entities"
	"go-trade-bot/internal/handler"
	"io"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
)

type UseCase interface {
	Save(ctx context.Context, strategy entities.Strategy) error
	Update(ctx context.Context, strategy entities.Strategy) error
	Enqueue(ctx context.Context) error
	GetAll(ctx context.Context) ([]entities.Strategy, error)
	GetByID(ctx context.Context, id uint) (entities.Strategy, error)
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
		{
			Pattern: "/strategy/{id}",
			Action:  h.Put,
			Method:  http.MethodPut,
		},
		{
			Pattern: "/strategy/{id}",
			Action:  h.GetById,
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
	err := h.UseCase.Enqueue(r.Context())
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
	}
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

func (h *StrategyHandler) Put(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	sId := vars["id"]
	id, err := strconv.Atoi(sId)

	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
	}

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

	strategy := dto.ToModel()
	strategy.ID = uint(id)

	err = h.UseCase.Update(r.Context(), strategy)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (h *StrategyHandler) GetById(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])

	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	strategy, err := h.UseCase.GetByID(r.Context(), uint(id))
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(strategy)
}
