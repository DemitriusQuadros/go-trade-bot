package handler

import (
	"context"
	"net/http"

	"github.com/gorilla/mux"
)

type UseCase interface {
	SaveSymbolPrice(ctx context.Context, symbol string) error
}
type SymbolHandler struct {
	UseCase UseCase
}

func NewSymbolHandler(u UseCase) *SymbolHandler {
	return &SymbolHandler{
		UseCase: u,
	}
}

func (*SymbolHandler) Pattern() string {
	return "/symbol/{symbol}"
}

func (h *SymbolHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	symbol := vars["symbol"]
	err := h.UseCase.SaveSymbolPrice(r.Context(), symbol)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}

	w.WriteHeader(http.StatusCreated)

	response := "Symbol " + symbol + " registered successfully"
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(response))
}
