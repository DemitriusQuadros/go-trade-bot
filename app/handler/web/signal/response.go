package handler

import (
	"time"

	"go-trade-bot/app/entities"
)

type OrderResponseDTO struct {
	ID              uint      `json:"id"`
	SignalID        uint      `json:"signal_id"`
	BrokerOrderID   string    `json:"broker_order_id"`
	StopLossOrderID string    `json:"stop_loss_order_id"`
	StopLossPrice   float32   `json:"stop_loss_price"`
	EntryPrice      float32   `json:"entry_price"`
	ExitPrice       float32   `json:"exit_price"`
	Quantity        float32   `json:"quantity"`
	InvestedAmount  float32   `json:"invested_amount"`
	MarginType      string    `json:"margin_type"`
	EntryFee        float32   `json:"entry_fee"`
	ExitFee         float32   `json:"exit_fee"`
	Leverage        float32   `json:"leverage"`
	ExecutedQty     float32   `json:"executed_qty"`
	IsClosing       bool      `json:"is_closing"`
	Profit          float32   `json:"profit"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type SignalResponseDTO struct {
	ID         uint               `json:"id"`
	Symbol     string             `json:"symbol"`
	StrategyID uint               `json:"strategy_id"`
	Status     string             `json:"status"`
	Orders     []OrderResponseDTO `json:"orders"`
	CreatedAt  time.Time          `json:"created_at"`
	UpdatedAt  time.Time          `json:"updated_at"`
}

func ToOrderResponse(o entities.Order) OrderResponseDTO {
	return OrderResponseDTO{
		ID:              o.ID,
		SignalID:        o.SignalID,
		BrokerOrderID:   o.BrokerOrderID,
		StopLossOrderID: o.StopLossOrderID,
		StopLossPrice:   o.StopLossPrice,
		EntryPrice:      o.EntryPrice,
		ExitPrice:       o.ExitPrice,
		Quantity:        o.Quantity,
		InvestedAmount:  o.InvestedAmount,
		MarginType:      string(o.MarginType),
		EntryFee:        o.EntryFee,
		ExitFee:         o.ExitFee,
		Leverage:        o.Leverage,
		ExecutedQty:     o.ExecutedQty,
		IsClosing:       o.IsClosing,
		Profit:          o.Profit,
		CreatedAt:       o.CreatedAt,
		UpdatedAt:       o.UpdatedAt,
	}
}

func ToSignalResponse(s entities.Signal) SignalResponseDTO {
	orders := make([]OrderResponseDTO, len(s.Orders))
	for i, o := range s.Orders {
		orders[i] = ToOrderResponse(o)
	}

	return SignalResponseDTO{
		ID:         s.ID,
		Symbol:     s.Symbol,
		StrategyID: s.StrategyID,
		Status:     string(s.Status),
		Orders:     orders,
		CreatedAt:  s.CreatedAt,
		UpdatedAt:  s.UpdatedAt,
	}
}

func ToSignalResponseList(signals []entities.Signal) []SignalResponseDTO {
	res := make([]SignalResponseDTO, len(signals))
	for i, s := range signals {
		res[i] = ToSignalResponse(s)
	}
	return res
}
