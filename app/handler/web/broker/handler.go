package handler

import (
	"encoding/json"
	"fmt"
	"go-trade-bot/internal/broker"
	"go-trade-bot/internal/handler"
	"net/http"
	"strconv"
)

type BrokerHandler struct {
	Broker broker.Broker
}

func NewBrokerHandler(b broker.Broker) *BrokerHandler {
	return &BrokerHandler{
		Broker: b,
	}
}

func (h *BrokerHandler) Handlers() []handler.Configuration {
	return []handler.Configuration{
		{
			Pattern: "/broker/prices",
			Action:  h.ListPrices,
			Method:  http.MethodGet,
		},
		{
			Pattern: "/broker/klines",
			Action:  h.ListKlines,
			Method:  http.MethodGet,
		},
	}
}

func (h *BrokerHandler) ListPrices(w http.ResponseWriter, r *http.Request) {

	symbol := r.URL.Query().Get("symbol")
	if symbol == "" {
		http.Error(w, "Symbol is required", http.StatusBadRequest)
		return
	}
	prices, err := h.Broker.ListTickerPrices(r.Context(), "BTCUSDT")
	if err != nil {
		http.Error(w, "Error fetching prices", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(prices)
}

func (h *BrokerHandler) ListKlines(w http.ResponseWriter, r *http.Request) {
	symbol := r.URL.Query().Get("symbol")
	if symbol == "" {
		http.Error(w, "Symbol is required", http.StatusBadRequest)
		return
	}
	interval := r.URL.Query().Get("interval")
	if interval == "" {
		http.Error(w, "Interval is required", http.StatusBadRequest)
		return
	}
	limit := r.URL.Query().Get("limit")
	if limit == "" {
		http.Error(w, "Limit is required", http.StatusBadRequest)
		return
	}

	limitInt, err := strconv.Atoi(limit)
	if err != nil {
		http.Error(w, "Limit must be a valid integer", http.StatusBadRequest)
		return
	}

	klines, err := h.Broker.ListKline(r.Context(), symbol, interval, limitInt)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error fetching klines: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(klines)
}
