package handler

import (
	"encoding/json"
	"go-trade-bot/app/entities"
	"go-trade-bot/internal/handler"
	"net/http"
)

type UseCase interface {
	CreateAccount(account entities.Account) error
	GetAccount() (entities.Account, error)
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
			Pattern: "/account",
			Action:  h.Post,
			Method:  http.MethodPost,
		},
		{
			Pattern: "/account",
			Action:  h.Get,
			Method:  http.MethodGet,
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
	account, err := h.UseCase.GetAccount()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(account); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}
