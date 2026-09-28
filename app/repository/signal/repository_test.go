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

	openSignals, err := repo.GetAllOpenSignals()
	assert.NoError(t, err)
	assert.Len(t, openSignals, 1)
	assert.Equal(t, "BTCUSDT", openSignals[0].Symbol)

	closedSignals, err := repo.GetAllClosedSignals()
	assert.NoError(t, err)
	assert.Len(t, closedSignals, 1)
	assert.Equal(t, "ETHUSDT", closedSignals[0].Symbol)
}

// fix-01: the dryrun stop watermark is persisted on the order row only.
func TestSignalRepository_UpdateSimStopEvaluatedAt(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	assert.NoError(t, db.AutoMigrate(&entities.Signal{}, &entities.Order{}))
	repo := repository.NewSignalRepository(db)

	assert.NoError(t, repo.Create(entities.Signal{
		Symbol: "BTCUSDT", StrategyID: 3, Status: entities.Open, Mode: "dryrun",
		Orders: []entities.Order{{BrokerOrderID: "SIM-1-1", StopLossOrderID: "SIM-STOP-1-2", StopLossPrice: 98, EntryPrice: 100, Quantity: 1, MarginType: entities.Isolated}},
	}))
	open, err := repo.GetOpenSignals("BTCUSDT", 3)
	assert.NoError(t, err)
	assert.Equal(t, "dryrun", open.Mode)
	assert.Nil(t, open.Orders[0].SimStopEvaluatedAt)

	mark := time.Date(2026, 1, 1, 10, 5, 0, 0, time.UTC)
	assert.NoError(t, repo.UpdateSimStopEvaluatedAt(open.Orders[0].ID, mark))

	open, err = repo.GetOpenSignals("BTCUSDT", 3)
	assert.NoError(t, err)
	if assert.NotNil(t, open.Orders[0].SimStopEvaluatedAt) {
		assert.True(t, open.Orders[0].SimStopEvaluatedAt.Equal(mark))
	}
	assert.Equal(t, float32(98), open.Orders[0].StopLossPrice)
	assert.Equal(t, "SIM-STOP-1-2", open.Orders[0].StopLossOrderID)
}
