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
func TestSignalRepository_GetByID(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)

	err = db.AutoMigrate(&entities.Signal{}, &entities.Order{})
	assert.NoError(t, err)

	repo := repository.NewSignalRepository(db)

	signal := entities.Signal{
		Symbol:     "ETHUSDT",
		StrategyID: 2,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
		Status:     entities.Open,
		Orders: []entities.Order{
			{
				BrokerOrderID:  "54321",
				EntryPrice:     2000.0,
				ExitPrice:      2100.0,
				Quantity:       0.2,
				InvestedAmount: 400.0,
				MarginType:     entities.Cross,
				EntryFee:       0.05,
				ExitFee:        0.05,
				Leverage:       5.0,
				ExecutedQty:    0.2,
				IsClosing:      false,
				CreatedAt:      time.Now(),
				UpdatedAt:      time.Now(),
			},
		},
	}

	err = repo.Create(signal)
	assert.NoError(t, err)

	var createdSignal entities.Signal
	err = db.First(&createdSignal, "symbol = ?", "ETHUSDT").Error
	assert.NoError(t, err)

	gotSignal, err := repo.GetByID(createdSignal.ID)
	assert.NoError(t, err)
	assert.Equal(t, createdSignal.ID, gotSignal.ID)
	assert.Equal(t, "ETHUSDT", gotSignal.Symbol)
	assert.Len(t, gotSignal.Orders, 1)
	assert.Equal(t, "54321", gotSignal.Orders[0].BrokerOrderID)
}

func TestSignalRepository_GetByID_NotFound(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)

	err = db.AutoMigrate(&entities.Signal{}, &entities.Order{})
	assert.NoError(t, err)

	repo := repository.NewSignalRepository(db)

	gotSignal, err := repo.GetByID(9999)
	assert.Error(t, err)
	assert.Equal(t, uint(0), gotSignal.ID)
}
func TestSignalRepository_GetAll(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)

	err = db.AutoMigrate(&entities.Signal{}, &entities.Order{})
	assert.NoError(t, err)

	repo := repository.NewSignalRepository(db)

	// Insert multiple signals
	signals := []entities.Signal{
		{
			Symbol:     "BTCUSDT",
			StrategyID: 1,
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
			Status:     entities.Open,
			Orders: []entities.Order{
				{
					BrokerOrderID:  "order1",
					EntryPrice:     10000.0,
					ExitPrice:      11000.0,
					Quantity:       0.5,
					InvestedAmount: 5000.0,
					MarginType:     entities.Isolated,
					EntryFee:       0.1,
					ExitFee:        0.1,
					Leverage:       10.0,
					ExecutedQty:    0.5,
					IsClosing:      false,
					CreatedAt:      time.Now(),
					UpdatedAt:      time.Now(),
				},
			},
		},
		{
			Symbol:     "ETHUSDT",
			StrategyID: 2,
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
			Status:     entities.Closed,
			Orders: []entities.Order{
				{
					BrokerOrderID:  "order2",
					EntryPrice:     2000.0,
					ExitPrice:      2100.0,
					Quantity:       1.0,
					InvestedAmount: 2000.0,
					MarginType:     entities.Cross,
					EntryFee:       0.05,
					ExitFee:        0.05,
					Leverage:       5.0,
					ExecutedQty:    1.0,
					IsClosing:      true,
					CreatedAt:      time.Now(),
					UpdatedAt:      time.Now(),
				},
			},
		},
	}

	for _, s := range signals {
		assert.NoError(t, repo.Create(s))
	}

	gotSignals, err := repo.GetAll()
	assert.NoError(t, err)
	assert.Len(t, gotSignals, 2)

	// Check that orders are preloaded
	for _, s := range gotSignals {
		assert.NotNil(t, s.Orders)
		assert.True(t, len(s.Orders) > 0)
	}
}
