package usecase

import (
	"go-trade-bot/app/entities"
	"time"
)

type AccountRepository interface {
	Create(account entities.Account) error
	UpdateAccount(account entities.Account) error
	GetAccountByID(id int64) (entities.Account, error)
}

type AccountUseCase struct {
	Repository AccountRepository
}

func NewAccountUseCase(r AccountRepository) *AccountUseCase {
	return &AccountUseCase{
		Repository: r,
	}
}

func (a *AccountUseCase) CreateAccount(account entities.Account) error {
	account.CreatedAt = time.Now()
	account.UpdatedAt = time.Now()
	return a.Repository.Create(account)
}

func (a *AccountUseCase) DeductOrder(entryPrice float32) error {
	account, err := a.Repository.GetAccountByID(1)
	if err != nil {
		return err
	}

	account.AvailableOrders--
	account.Amount -= entryPrice
	account.UpdatedAt = time.Now()
	return a.Repository.UpdateAccount(account)
}

func (a *AccountUseCase) AddOrder(profit float32) error {
	account, err := a.Repository.GetAccountByID(1)
	if err != nil {
		return err
	}

	account.AvailableOrders++
	account.Amount += profit
	account.UpdatedAt = time.Now()
	return a.Repository.UpdateAccount(account)
}

func (a *AccountUseCase) GetDisponibleAmout() (float32, error) {
	account, err := a.Repository.GetAccountByID(1)
	if err != nil {
		return 0, err
	}
	return account.Amount / float32(account.AvailableOrders), nil
}

func (a *AccountUseCase) CanOpenOrder() (bool, error) {
	account, err := a.Repository.GetAccountByID(1)
	if err != nil {
		return false, err
	}
	return account.AvailableOrders > 0, nil
}

func (a *AccountUseCase) GetAccount() (entities.Account, error) {
	account, err := a.Repository.GetAccountByID(1)
	if err != nil {
		return entities.Account{}, err
	}
	return account, nil
}
