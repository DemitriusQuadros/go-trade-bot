package usecase

import (
	"context"
	"fmt"
	"go-trade-bot/app/entities"
	"go-trade-bot/internal/exchange"
	"time"
)

type AccountRepository interface {
	Create(account entities.Account) error
	UpdateAccount(account entities.Account) error
	GetAccountByID(id int64) (entities.Account, error)
	GetByMode(mode entities.AccountMode) (entities.Account, error)
	GetAllAccounts() ([]entities.Account, error)
	EnsureDefaultAccounts() error
}

type AccountUseCase struct {
	Repository AccountRepository
	Exchange   exchange.ExchangeClient
}

func NewAccountUseCase(r AccountRepository, ex exchange.ExchangeClient) *AccountUseCase {
	return &AccountUseCase{
		Repository: r,
		Exchange:   ex,
	}
}

func (a *AccountUseCase) CreateAccount(account entities.Account) error {
	account.CreatedAt = time.Now()
	account.UpdatedAt = time.Now()
	if account.Mode == "" {
		account.Mode = entities.AccountModeDryRun
	}
	if account.InitialAmount <= 0 {
		account.InitialAmount = account.Amount
	}
	return a.Repository.Create(account)
}

func (a *AccountUseCase) GetAccount() (entities.Account, error) {
	account, err := a.Repository.GetByMode(entities.AccountModeDryRun)
	if err == nil {
		return account, nil
	}
	return a.Repository.GetAccountByID(1)
}

func (a *AccountUseCase) GetAccountByMode(mode entities.AccountMode) (entities.Account, error) {
	return a.Repository.GetByMode(mode)
}

func (a *AccountUseCase) GetAllAccounts() ([]entities.Account, error) {
	return a.Repository.GetAllAccounts()
}

func (a *AccountUseCase) UpdateDryRunCapital(amount float32, availableOrders int64, currency string) (entities.Account, error) {
	if amount <= 0 {
		return entities.Account{}, fmt.Errorf("amount must be greater than zero")
	}
	if availableOrders <= 0 {
		availableOrders = 1
	}
	if currency == "" {
		currency = "USDT"
	}

	account, err := a.Repository.GetByMode(entities.AccountModeDryRun)
	if err != nil {
		account, err = a.Repository.GetAccountByID(1)
		if err != nil {
			account = entities.Account{
				ID:              1,
				Mode:            entities.AccountModeDryRun,
				Amount:          amount,
				InitialAmount:   amount,
				AvailableOrders: availableOrders,
				Currency:        currency,
				CreatedAt:       time.Now(),
				UpdatedAt:       time.Now(),
			}
			if createErr := a.Repository.Create(account); createErr != nil {
				return entities.Account{}, createErr
			}
			return account, nil
		}
	}

	account.Amount = amount
	account.AvailableOrders = availableOrders
	account.Currency = currency
	if account.InitialAmount <= 0 {
		account.InitialAmount = amount
	}
	account.UpdatedAt = time.Now()

	if err := a.Repository.UpdateAccount(account); err != nil {
		return entities.Account{}, err
	}
	return account, nil
}

func (a *AccountUseCase) ResetDryRunCapital() (entities.Account, error) {
	account, err := a.Repository.GetByMode(entities.AccountModeDryRun)
	if err != nil {
		account, err = a.Repository.GetAccountByID(1)
		if err != nil {
			return entities.Account{}, err
		}
	}

	if account.InitialAmount <= 0 {
		account.InitialAmount = 10000
	}
	account.Amount = account.InitialAmount
	account.AvailableOrders = 5
	account.UpdatedAt = time.Now()

	if err := a.Repository.UpdateAccount(account); err != nil {
		return entities.Account{}, err
	}
	return account, nil
}

func (a *AccountUseCase) UpdateLiveAllocation(maxAllocation float32, availableOrders int64) (entities.Account, error) {
	if availableOrders <= 0 {
		availableOrders = 1
	}

	account, err := a.Repository.GetByMode(entities.AccountModeLive)
	if err != nil {
		account = entities.Account{
			ID:              2,
			Mode:            entities.AccountModeLive,
			Amount:          0,
			MaxAllocation:   maxAllocation,
			AvailableOrders: availableOrders,
			Currency:        "USDT",
			CreatedAt:       time.Now(),
			UpdatedAt:       time.Now(),
		}
		if createErr := a.Repository.Create(account); createErr != nil {
			return entities.Account{}, createErr
		}
		return account, nil
	}

	account.MaxAllocation = maxAllocation
	account.AvailableOrders = availableOrders
	account.UpdatedAt = time.Now()

	if err := a.Repository.UpdateAccount(account); err != nil {
		return entities.Account{}, err
	}
	return account, nil
}

func (a *AccountUseCase) SyncExchangeBalance(ctx context.Context, mode entities.AccountMode) (entities.Account, error) {
	if a.Exchange == nil {
		return entities.Account{}, fmt.Errorf("exchange client not configured")
	}

	balance, err := a.Exchange.GetAccountBalance(ctx)
	if err != nil {
		return entities.Account{}, fmt.Errorf("failed to fetch exchange balance: %w", err)
	}

	if mode == "" {
		mode = entities.AccountModeLive
	}

	account, err := a.Repository.GetByMode(mode)
	if err != nil {
		account = entities.Account{
			ID:              2,
			Mode:            mode,
			Amount:          float32(balance.Free),
			LockedAmount:    float32(balance.Locked),
			AvailableOrders: 5,
			Currency:        "USDT",
		}
		if balance.Asset != "" {
			account.Currency = balance.Asset
		}
		now := time.Now()
		account.LastSyncedAt = &now
		account.CreatedAt = now
		account.UpdatedAt = now
		if createErr := a.Repository.Create(account); createErr != nil {
			return entities.Account{}, createErr
		}
		return account, nil
	}

	account.Amount = float32(balance.Free)
	account.LockedAmount = float32(balance.Locked)
	if balance.Asset != "" {
		account.Currency = balance.Asset
	}
	now := time.Now()
	account.LastSyncedAt = &now
	account.UpdatedAt = now

	if err := a.Repository.UpdateAccount(account); err != nil {
		return entities.Account{}, err
	}
	return account, nil
}

func (a *AccountUseCase) DeductOrder(entryPrice float32) error {
	account, err := a.GetAccount()
	if err != nil {
		return err
	}

	account.AvailableOrders--
	account.Amount -= entryPrice
	account.UpdatedAt = time.Now()
	return a.Repository.UpdateAccount(account)
}

func (a *AccountUseCase) AddOrder(profit float32) error {
	account, err := a.GetAccount()
	if err != nil {
		return err
	}

	account.AvailableOrders++
	account.Amount += profit
	account.UpdatedAt = time.Now()
	return a.Repository.UpdateAccount(account)
}

func (a *AccountUseCase) GetDisponibleAmout() (float32, error) {
	account, err := a.GetAccount()
	if err != nil {
		return 0, err
	}
	if account.AvailableOrders <= 0 {
		return 0, nil
	}
	return account.Amount / float32(account.AvailableOrders), nil
}

func (a *AccountUseCase) CanOpenOrder() (bool, error) {
	account, err := a.GetAccount()
	if err != nil {
		return false, err
	}
	return account.AvailableOrders > 0 && account.Amount > 0, nil
}
