package repository_test

import (
	"go-trade-bot/app/entities"
	repository "go-trade-bot/app/repository/account"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestAccountRepository_Create(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to connect to database: %v", err)
	}
	err = db.AutoMigrate(&entities.Account{})
	if err != nil {
		t.Fatalf("failed to migrate database: %v", err)
	}
	repo := repository.NewAccountRepository(db)
	account := entities.Account{
		Amount:          1000.0,
		AvailableOrders: 5,
		Currency:        "USD",
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
	err = repo.Create(account)
	require.NoError(t, err)

	var result entities.Account
	err = db.First(&result, "currency = ?", "USD").Error
	require.NoError(t, err)
	require.Equal(t, "USD", result.Currency)
}

func TestAccountRepository_GetAccountByID(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to connect to database: %v", err)
	}
	err = db.AutoMigrate(&entities.Account{})
	if err != nil {
		t.Fatalf("failed to migrate database: %v", err)
	}
	repo := repository.NewAccountRepository(db)
	account := entities.Account{
		ID:              1,
		Amount:          1000.0,
		AvailableOrders: 5,
		Currency:        "USD",
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
	err = repo.Create(account)
	require.NoError(t, err)

	result, err := repo.GetAccountByID(1)
	require.NoError(t, err)
	require.Equal(t, int64(1), result.ID)
}

func TestAccountRepository_UpdateAccount(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to connect to database: %v", err)
	}
	err = db.AutoMigrate(&entities.Account{})
	if err != nil {
		t.Fatalf("failed to migrate database: %v", err)
	}
	repo := repository.NewAccountRepository(db)
	account := entities.Account{
		ID:              1,
		Amount:          1000.0,
		AvailableOrders: 5,
		Currency:        "USD",
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
	err = repo.Create(account)
	require.NoError(t, err)

	account.Amount = 2000.0
	err = repo.UpdateAccount(account)
	require.NoError(t, err)

	result, err := repo.GetAccountByID(1)
	require.NoError(t, err)
	require.Equal(t, float32(2000.0), result.Amount)
}
