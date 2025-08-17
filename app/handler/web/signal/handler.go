package handler

import (
	"context"
	"encoding/json"
	"go-trade-bot/app/entities"
	"go-trade-bot/internal/handler"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
)

type UseCase interface {
	Close(ctx context.Context, id uint) error
	GetAll(ctx context.Context) ([]entities.Signal, error)
	GetByID(ctx context.Context, id uint) (entities.Signal, error)
}
type SignalHandler struct {
	UseCase UseCase
}

func NewSignalHandler(u UseCase) *SignalHandler {
	return &SignalHandler{
		UseCase: u,
	}
}

func (h *SignalHandler) Handlers() []handler.Configuration {
	return []handler.Configuration{
		{
			Pattern: "/signal/close/{id}",
			Action:  h.Close,
			Method:  http.MethodPost,
		},
		{
			Pattern: "/signal",
			Action:  h.GetAll,
			Method:  http.MethodGet,
		},
		{
			Pattern: "/signal/{id}",
			Action:  h.GetById,
			Method:  http.MethodGet,
		},
	}
}

func (h *SignalHandler) Close(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])

	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}
	err = h.UseCase.Close(r.Context(), uint(id))

	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

func (h *SignalHandler) GetAll(w http.ResponseWriter, r *http.Request) {
	signals, err := h.UseCase.GetAll(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(signals)
}

func (h *SignalHandler) GetById(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])

	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	signal, err := h.UseCase.GetByID(r.Context(), uint(id))
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(signal)
}
