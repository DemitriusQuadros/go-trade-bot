package handler

import (
	"context"
	"encoding/json"
	"go-trade-bot/app/entities"
	"go-trade-bot/internal/authz"
	"go-trade-bot/internal/handler"
	"net/http"
)

type UseCase interface {
	CreateAccount(account entities.Account) error
	GetAccount() (entities.Account, error)
	GetAccountByMode(mode entities.AccountMode) (entities.Account, error)
	GetAllAccounts() ([]entities.Account, error)
	UpdateDryRunCapital(amount float32, availableOrders int64, currency string) (entities.Account, error)
	ResetDryRunCapital() (entities.Account, error)
	UpdateLiveAllocation(maxAllocation float32, availableOrders int64) (entities.Account, error)
	SyncExchangeBalance(ctx context.Context, mode entities.AccountMode) (entities.Account, error)
}

type AccountHandler struct {
	UseCase UseCase
}

func NewAccountHandler(u UseCase) *AccountHandler {
	return &AccountHandler{
		UseCase: u,
	}
}

func (h *AccountHandler) Handlers() []handler.Configuration {
	return []handler.Configuration{
		{
			Pattern:    "/account",
			Action:     h.Post,
			Method:     http.MethodPost,
			Capability: authz.CapAdmin,
		},
		{
			Pattern:    "/account",
			Action:     h.Get,
			Method:     http.MethodGet,
			Capability: authz.CapView,
		},
		{
			Pattern:    "/account/dryrun",
			Action:     h.PutDryRun,
			Method:     http.MethodPut,
			Capability: authz.CapAdmin,
		},
		{
			Pattern:    "/account/dryrun/reset",
			Action:     h.PostResetDryRun,
			Method:     http.MethodPost,
			Capability: authz.CapAdmin,
		},
		{
			Pattern:    "/account/live",
			Action:     h.PutLive,
			Method:     http.MethodPut,
			Capability: authz.CapAdmin,
		},
		{
			Pattern:    "/account/sync",
			Action:     h.PostSync,
			Method:     http.MethodPost,
			Capability: authz.CapAdmin,
		},
	}
}

func (h *AccountHandler) Post(w http.ResponseWriter, r *http.Request) {
	var account AccountDto
	if err := json.NewDecoder(r.Body).Decode(&account); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.UseCase.CreateAccount(account.ToModel()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func (h *AccountHandler) Get(w http.ResponseWriter, r *http.Request) {
	modeQuery := r.URL.Query().Get("mode")
	var primary entities.Account
	var err error
	if modeQuery != "" {
		primary, err = h.UseCase.GetAccountByMode(entities.AccountMode(modeQuery))
	} else {
		primary, err = h.UseCase.GetAccount()
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	accountsList, _ := h.UseCase.GetAllAccounts()
	accMap := make(map[string]entities.Account)
	for _, acc := range accountsList {
		accMap[string(acc.Mode)] = acc
	}

	resp := AccountResponse{
		Account:  primary,
		Accounts: accMap,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (h *AccountHandler) PutDryRun(w http.ResponseWriter, r *http.Request) {
	var dto UpdateDryRunDto
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	account, err := h.UseCase.UpdateDryRunCapital(dto.Amount, dto.AvailableOrders, dto.Currency)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(account)
}

func (h *AccountHandler) PostResetDryRun(w http.ResponseWriter, r *http.Request) {
	account, err := h.UseCase.ResetDryRunCapital()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(account)
}

func (h *AccountHandler) PutLive(w http.ResponseWriter, r *http.Request) {
	var dto UpdateLiveDto
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	account, err := h.UseCase.UpdateLiveAllocation(dto.MaxAllocation, dto.AvailableOrders)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(account)
}

func (h *AccountHandler) PostSync(w http.ResponseWriter, r *http.Request) {
	var dto SyncAccountDto
	_ = json.NewDecoder(r.Body).Decode(&dto)
	mode := entities.AccountMode(dto.Mode)
	if mode == "" {
		mode = entities.AccountModeLive
	}
	account, err := h.UseCase.SyncExchangeBalance(r.Context(), mode)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(account)
}
