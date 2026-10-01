package handler

import (
	"go-trade-bot/app/entities"
)

type AccountDto struct {
	Mode            string  `json:"mode,omitempty"`
	Amount          float32 `json:"amount"`
	AvailableOrders int64   `json:"available_orders"`
	Currency        string  `json:"currency"`
}

func (s AccountDto) ToModel() entities.Account {
	mode := entities.AccountMode(s.Mode)
	if mode == "" {
		mode = entities.AccountModeDryRun
	}
	return entities.Account{
		Mode:            mode,
		Amount:          s.Amount,
		InitialAmount:   s.Amount,
		AvailableOrders: s.AvailableOrders,
		Currency:        s.Currency,
	}
}

type UpdateDryRunDto struct {
	Amount          float32 `json:"amount"`
	AvailableOrders int64   `json:"available_orders"`
	Currency        string  `json:"currency"`
}

type UpdateLiveDto struct {
	MaxAllocation   float32 `json:"max_allocation"`
	AvailableOrders int64   `json:"available_orders"`
}

type SyncAccountDto struct {
	Mode string `json:"mode,omitempty"`
}

type AccountResponse struct {
	entities.Account
	Accounts map[string]entities.Account `json:"accounts,omitempty"`
}
