package todo

import (
	"encoding/json"
	"go-trade-bot/internal/broker"
	"net/http"
)

type ToDoHandler struct {
	broker *broker.Broker
}

func NewTodoHandler(broker *broker.Broker) *ToDoHandler {
	return &ToDoHandler{
		broker: broker,
	}
}

func (*ToDoHandler) Pattern() string {
	return "/todo"
}

func (t *ToDoHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	prices, err := t.broker.ListTickerPrices(r.Context(), "BTCUSDT")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}

	if err := json.NewEncoder(w).Encode(prices); err != nil {
		http.Error(w, "Failed to encode JSON", http.StatusInternalServerError)
	}
}
