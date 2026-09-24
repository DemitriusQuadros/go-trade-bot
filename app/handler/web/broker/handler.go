package handler

import (
	"encoding/json"
	"fmt"
	"go-trade-bot/internal/exchange"
	"go-trade-bot/internal/handler"
	"net/http"
	"strconv"
)

// BrokerHandler is kept as a thin, read-only proxy over the ExchangeClient
// ACL (Spec 01) - routes and response shape are otherwise unchanged, per the
// blueprint's "rename package/route to exchange or keep as read-only proxy"
// option. This is the last app/handler/web call site that used to depend on
// the concrete internal/broker.Broker type; prices are now float64 (Spec 01
// AC#4) rather than unparsed strings.
type BrokerHandler struct {
	Exchange exchange.ExchangeClient
}

func NewBrokerHandler(e exchange.ExchangeClient) *BrokerHandler {
	return &BrokerHandler{
		Exchange: e,
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
	prices, err := h.Exchange.ListTickerPrices(r.Context(), symbol)
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

	klines, err := h.Exchange.ListKline(r.Context(), symbol, interval, limitInt)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error fetching klines: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(klines)
}
