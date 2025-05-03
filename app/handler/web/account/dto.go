package handler

import (
	"go-trade-bot/app/entities"
)

type AccountDto struct {
	Amount          float32 `json:"amount"`
	AvailableOrders int64   `json:"available_orders"`
	Currency        string  `json:"currency"`
}

func (s AccountDto) ToModel() entities.Account {
	return entities.Account{
		Amount:          s.Amount,
		AvailableOrders: s.AvailableOrders,
		Currency:        s.Currency,
	}
}
