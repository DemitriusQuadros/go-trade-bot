package repository_test

import (
	"go-trade-bot/app/entities"
	repository "go-trade-bot/app/repository/signal"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestSignalRepository_Create(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)

	err = db.AutoMigrate(&entities.Signal{}, &entities.Order{})
	assert.NoError(t, err)

	repo := repository.NewSignalRepository(db)

	signal := entities.Signal{
		Symbol:     "BTCUSDT",
		StrategyID: 1,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
		Status:     entities.Open,
		Orders: []entities.Order{
			{
				SignalID:       1,
				BrokerOrderID:  "12345",
				EntryPrice:     50000.0,
				ExitPrice:      51000.0,
				Quantity:       0.1,
				InvestedAmount: 5000.0,
				MarginType:     entities.Isolated,
				EntryFee:       0.1,
				ExitFee:        0.1,
				Leverage:       10.0,
				ExecutedQty:    0.1,
				IsClosing:      true,
				CreatedAt:      time.Now(),
				UpdatedAt:      time.Now(),
			},
		},
	}

	err = repo.Create(signal)
	assert.NoError(t, err)

	var result entities.Signal
	err = db.First(&result, "symbol = ?", "BTCUSDT").Error
	assert.NoError(t, err)
	assert.Equal(t, "BTCUSDT", result.Symbol)
}
