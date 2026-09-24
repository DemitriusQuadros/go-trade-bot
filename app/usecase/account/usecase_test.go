package usecase_test

import (
	"go-trade-bot/app/entities"
	usecase "go-trade-bot/app/usecase/account"
	"go-trade-bot/app/usecase/account/mocks"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestAccountUseCase_CreateAccount(t *testing.T) {
	account := entities.Account{
		ID:              1,
		Amount:          1000.0,
		AvailableOrders: 10,
		Currency:        "USD",
	}
	repo := new(mocks.AccountRepository)
	repo.On("Create", mock.MatchedBy(func(a entities.Account) bool {
		return a.ID == 1 && a.Amount == 1000.0 && a.AvailableOrders == 10 && a.Currency == "USD" && !a.CreatedAt.IsZero()
	})).Return(nil)

	usecase := usecase.NewAccountUseCase(repo)
	err := usecase.CreateAccount(account)

	assert.NoError(t, err)
	repo.AssertExpectations(t)
}
func TestAccountUseCase_DeductOrder(t *testing.T) {
	account := entities.Account{
		ID:              1,
		Amount:          1000.0,
		AvailableOrders: 10,
		Currency:        "USD",
	}

	repo := new(mocks.AccountRepository)
	repo.On("GetAccountByID", int64(1)).Return(account, nil)
	repo.On("UpdateAccount", mock.MatchedBy(func(a entities.Account) bool {
		return a.ID == 1 && a.Amount == 900.0 && a.AvailableOrders == 9 && a.Currency == "USD"
	})).Return(nil)

	usecase := usecase.NewAccountUseCase(repo)
	err := usecase.DeductOrder(100.0)

	assert.NoError(t, err)
	repo.AssertExpectations(t)
}
func TestAccountUseCase_AddOrder(t *testing.T) {
	account := entities.Account{
		ID:              1,
		Amount:          900.0,
		AvailableOrders: 9,
		Currency:        "USD",
	}

	repo := new(mocks.AccountRepository)
	repo.On("GetAccountByID", int64(1)).Return(account, nil)
	repo.On("UpdateAccount", mock.MatchedBy(func(a entities.Account) bool {
		return a.ID == 1 && a.Amount == 1000.0 && a.AvailableOrders == 10 && a.Currency == "USD"
	})).Return(nil)

	usecase := usecase.NewAccountUseCase(repo)
	err := usecase.AddOrder(100.0)

	assert.NoError(t, err)
	repo.AssertExpectations(t)
}
func TestAccountUseCase_GetDisponibleAmout(t *testing.T) {
	account := entities.Account{
		ID:              1,
		Amount:          1000.0,
		AvailableOrders: 10,
		Currency:        "USD",
	}
	repo := new(mocks.AccountRepository)
	repo.On("GetAccountByID", int64(1)).Return(account, nil)

	usecase := usecase.NewAccountUseCase(repo)
	amount, err := usecase.GetDisponibleAmout()

	assert.NoError(t, err)
	assert.Equal(t, float32(100.0), amount)
	repo.AssertExpectations(t)
}
func TestAccountUseCase_CanOpenOrder(t *testing.T) {
	account := entities.Account{
		ID:              1,
		Amount:          1000.0,
		AvailableOrders: 10,
		Currency:        "USD",
	}
	repo := new(mocks.AccountRepository)
	repo.On("GetAccountByID", int64(1)).Return(account, nil)

	usecase := usecase.NewAccountUseCase(repo)
	canOpen, err := usecase.CanOpenOrder()

	assert.NoError(t, err)
	assert.True(t, canOpen)
	repo.AssertExpectations(t)
}
func TestAccountUseCase_CanOpenOrder_NoAvailableOrders(t *testing.T) {
	account := entities.Account{
		ID:              1,
		Amount:          1000.0,
		AvailableOrders: 0,
		Currency:        "USD",
	}
	repo := new(mocks.AccountRepository)
	repo.On("GetAccountByID", int64(1)).Return(account, nil)

	usecase := usecase.NewAccountUseCase(repo)
	canOpen, err := usecase.CanOpenOrder()

	assert.NoError(t, err)
	assert.False(t, canOpen)
	repo.AssertExpectations(t)
}

func TestAccountUseCase_GetAccount(t *testing.T) {
	account := entities.Account{
		ID:              1,
		Amount:          1000.0,
		AvailableOrders: 10,
		Currency:        "USD",
	}
	repo := new(mocks.AccountRepository)
	repo.On("GetAccountByID", int64(1)).Return(account, nil)

	usecase := usecase.NewAccountUseCase(repo)
	acc, err := usecase.GetAccount()

	assert.NoError(t, err)
	assert.Equal(t, account, acc)
	repo.AssertExpectations(t)
}
