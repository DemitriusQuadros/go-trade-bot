package usecase_test

import (
	"context"
	"go-trade-bot/app/entities"
	usecase "go-trade-bot/app/usecase/account"
	"go-trade-bot/app/usecase/account/mocks"
	signalmocks "go-trade-bot/app/usecase/signal/mocks"
	"go-trade-bot/internal/exchange"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestAccountUseCase_CreateAccount(t *testing.T) {
	account := entities.Account{
		ID:              1,
		Mode:            entities.AccountModeDryRun,
		Amount:          1000.0,
		AvailableOrders: 10,
		Currency:        "USD",
	}
	repo := new(mocks.AccountRepository)
	repo.On("Create", mock.MatchedBy(func(a entities.Account) bool {
		return a.ID == 1 && a.Amount == 1000.0 && a.AvailableOrders == 10 && a.Currency == "USD" && !a.CreatedAt.IsZero()
	})).Return(nil)

	uc := usecase.NewAccountUseCase(repo, nil)
	err := uc.CreateAccount(account)

	assert.NoError(t, err)
	repo.AssertExpectations(t)
}

func TestAccountUseCase_DeductOrder(t *testing.T) {
	account := entities.Account{
		ID:              1,
		Mode:            entities.AccountModeDryRun,
		Amount:          1000.0,
		AvailableOrders: 10,
		Currency:        "USD",
	}

	repo := new(mocks.AccountRepository)
	repo.On("GetByMode", entities.AccountModeDryRun).Return(account, nil)
	repo.On("UpdateAccount", mock.MatchedBy(func(a entities.Account) bool {
		return a.ID == 1 && a.Amount == 900.0 && a.AvailableOrders == 9 && a.Currency == "USD"
	})).Return(nil)

	uc := usecase.NewAccountUseCase(repo, nil)
	err := uc.DeductOrder(100.0)

	assert.NoError(t, err)
	repo.AssertExpectations(t)
}

func TestAccountUseCase_AddOrder(t *testing.T) {
	account := entities.Account{
		ID:              1,
		Mode:            entities.AccountModeDryRun,
		Amount:          900.0,
		AvailableOrders: 9,
		Currency:        "USD",
	}

	repo := new(mocks.AccountRepository)
	repo.On("GetByMode", entities.AccountModeDryRun).Return(account, nil)
	repo.On("UpdateAccount", mock.MatchedBy(func(a entities.Account) bool {
		return a.ID == 1 && a.Amount == 1000.0 && a.AvailableOrders == 10 && a.Currency == "USD"
	})).Return(nil)

	uc := usecase.NewAccountUseCase(repo, nil)
	err := uc.AddOrder(100.0)

	assert.NoError(t, err)
	repo.AssertExpectations(t)
}

func TestAccountUseCase_GetDisponibleAmout(t *testing.T) {
	account := entities.Account{
		ID:              1,
		Mode:            entities.AccountModeDryRun,
		Amount:          1000.0,
		AvailableOrders: 10,
		Currency:        "USD",
	}
	repo := new(mocks.AccountRepository)
	repo.On("GetByMode", entities.AccountModeDryRun).Return(account, nil)

	uc := usecase.NewAccountUseCase(repo, nil)
	amount, err := uc.GetDisponibleAmout()

	assert.NoError(t, err)
	assert.Equal(t, float32(100.0), amount)
	repo.AssertExpectations(t)
}

func TestAccountUseCase_CanOpenOrder(t *testing.T) {
	account := entities.Account{
		ID:              1,
		Mode:            entities.AccountModeDryRun,
		Amount:          1000.0,
		AvailableOrders: 10,
		Currency:        "USD",
	}
	repo := new(mocks.AccountRepository)
	repo.On("GetByMode", entities.AccountModeDryRun).Return(account, nil)

	uc := usecase.NewAccountUseCase(repo, nil)
	canOpen, err := uc.CanOpenOrder()

	assert.NoError(t, err)
	assert.True(t, canOpen)
	repo.AssertExpectations(t)
}

func TestAccountUseCase_CanOpenOrder_NoAvailableOrders(t *testing.T) {
	account := entities.Account{
		ID:              1,
		Mode:            entities.AccountModeDryRun,
		Amount:          1000.0,
		AvailableOrders: 0,
		Currency:        "USD",
	}
	repo := new(mocks.AccountRepository)
	repo.On("GetByMode", entities.AccountModeDryRun).Return(account, nil)

	uc := usecase.NewAccountUseCase(repo, nil)
	canOpen, err := uc.CanOpenOrder()

	assert.NoError(t, err)
	assert.False(t, canOpen)
	repo.AssertExpectations(t)
}

func TestAccountUseCase_GetAccount(t *testing.T) {
	account := entities.Account{
		ID:              1,
		Mode:            entities.AccountModeDryRun,
		Amount:          1000.0,
		AvailableOrders: 10,
		Currency:        "USD",
	}
	repo := new(mocks.AccountRepository)
	repo.On("GetByMode", entities.AccountModeDryRun).Return(account, nil)

	uc := usecase.NewAccountUseCase(repo, nil)
	acc, err := uc.GetAccount()

	assert.NoError(t, err)
	assert.Equal(t, account, acc)
	repo.AssertExpectations(t)
}

func TestAccountUseCase_UpdateDryRunCapital(t *testing.T) {
	existing := entities.Account{
		ID:              1,
		Mode:            entities.AccountModeDryRun,
		Amount:          1000.0,
		InitialAmount:   1000.0,
		AvailableOrders: 5,
		Currency:        "USDT",
	}
	repo := new(mocks.AccountRepository)
	repo.On("GetByMode", entities.AccountModeDryRun).Return(existing, nil)
	repo.On("UpdateAccount", mock.MatchedBy(func(a entities.Account) bool {
		return a.Amount == 5000.0 && a.AvailableOrders == 3 && a.Currency == "USDT"
	})).Return(nil)

	uc := usecase.NewAccountUseCase(repo, nil)
	updated, err := uc.UpdateDryRunCapital(5000.0, 3, "USDT")
	require.NoError(t, err)
	assert.Equal(t, float32(5000.0), updated.Amount)
	assert.Equal(t, int64(3), updated.AvailableOrders)
	repo.AssertExpectations(t)
}

func TestAccountUseCase_ResetDryRunCapital(t *testing.T) {
	existing := entities.Account{
		ID:              1,
		Mode:            entities.AccountModeDryRun,
		Amount:          2500.0,
		InitialAmount:   10000.0,
		AvailableOrders: 2,
		Currency:        "USDT",
	}
	repo := new(mocks.AccountRepository)
	repo.On("GetByMode", entities.AccountModeDryRun).Return(existing, nil)
	repo.On("UpdateAccount", mock.MatchedBy(func(a entities.Account) bool {
		return a.Amount == 10000.0 && a.AvailableOrders == 5
	})).Return(nil)

	uc := usecase.NewAccountUseCase(repo, nil)
	reset, err := uc.ResetDryRunCapital()
	require.NoError(t, err)
	assert.Equal(t, float32(10000.0), reset.Amount)
	assert.Equal(t, int64(5), reset.AvailableOrders)
	repo.AssertExpectations(t)
}

func TestAccountUseCase_UpdateLiveAllocation(t *testing.T) {
	existing := entities.Account{
		ID:              2,
		Mode:            entities.AccountModeLive,
		Amount:          50000.0,
		MaxAllocation:   0,
		AvailableOrders: 5,
		Currency:        "USDT",
	}
	repo := new(mocks.AccountRepository)
	repo.On("GetByMode", entities.AccountModeLive).Return(existing, nil)
	repo.On("UpdateAccount", mock.MatchedBy(func(a entities.Account) bool {
		return a.MaxAllocation == 5000.0 && a.AvailableOrders == 2
	})).Return(nil)

	uc := usecase.NewAccountUseCase(repo, nil)
	updated, err := uc.UpdateLiveAllocation(5000.0, 2)
	require.NoError(t, err)
	assert.Equal(t, float32(5000.0), updated.MaxAllocation)
	assert.Equal(t, int64(2), updated.AvailableOrders)
	repo.AssertExpectations(t)
}

func TestAccountUseCase_SyncExchangeBalance(t *testing.T) {
	existing := entities.Account{
		ID:              2,
		Mode:            entities.AccountModeLive,
		Amount:          0,
		LockedAmount:    0,
		AvailableOrders: 5,
		Currency:        "USDT",
	}
	repo := new(mocks.AccountRepository)
	repo.On("GetByMode", entities.AccountModeLive).Return(existing, nil)
	repo.On("UpdateAccount", mock.MatchedBy(func(a entities.Account) bool {
		return a.Amount == 8450.50 && a.LockedAmount == 1549.50 && a.LastSyncedAt != nil
	})).Return(nil)

	mockEx := new(signalmocks.ExchangeClient)
	mockEx.On("GetAccountBalance", mock.Anything).Return(exchange.AccountBalance{
		Asset:  "USDT",
		Free:   8450.50,
		Locked: 1549.50,
	}, nil)

	uc := usecase.NewAccountUseCase(repo, mockEx)
	synced, err := uc.SyncExchangeBalance(context.Background(), entities.AccountModeLive)
	require.NoError(t, err)
	assert.Equal(t, float32(8450.50), synced.Amount)
	assert.Equal(t, float32(1549.50), synced.LockedAmount)
	assert.NotNil(t, synced.LastSyncedAt)
	assert.WithinDuration(t, time.Now(), *synced.LastSyncedAt, 2*time.Second)

	repo.AssertExpectations(t)
	mockEx.AssertExpectations(t)
}
