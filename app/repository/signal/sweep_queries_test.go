package repository_test

import (
	"context"
	"testing"
	"time"

	"go-trade-bot/app/entities"
	repository "go-trade-bot/app/repository/signal"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestSignalRepository_SweepQueries(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&entities.Signal{}, &entities.Order{}))
	repo := repository.NewSignalRepository(db)
	ctx := context.Background()
	t0 := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	none, err := repo.LastOpenedAt(ctx, 7)
	require.NoError(t, err)
	assert.Nil(t, none)

	mk := func(strategy uint, status entities.SignalStatus, created, updated time.Time, profit float32) {
		s := entities.Signal{Symbol: "BTCUSDT", StrategyID: strategy, Status: status, CreatedAt: created, UpdatedAt: updated,
			Orders: []entities.Order{{Profit: profit, InvestedAmount: 100}}}
		require.NoError(t, db.Session(&gorm.Session{SkipHooks: true}).Create(&s).Error)
		require.NoError(t, db.Model(&entities.Signal{}).Where("id = ?", s.ID).UpdateColumn("updated_at", updated).Error)
	}
	mk(7, entities.Closed, t0.Add(-48*time.Hour), t0.Add(-30*time.Hour), 5) // closed before the window
	mk(7, entities.Closed, t0.Add(-10*time.Hour), t0.Add(-5*time.Hour), -3)
	mk(7, entities.Closed, t0.Add(-4*time.Hour), t0.Add(-2*time.Hour), 2)
	mk(7, entities.Open, t0.Add(-1*time.Hour), t0.Add(-1*time.Hour), 0)
	mk(8, entities.Closed, t0.Add(-3*time.Hour), t0.Add(-1*time.Hour), 9) // other strategy

	closed, err := repo.ListClosedBetween(ctx, 7, t0.Add(-24*time.Hour), t0)
	require.NoError(t, err)
	require.Len(t, closed, 2)
	assert.InDelta(t, -3, closed[0].Orders[0].Profit, 1e-6, "oldest close first, orders preloaded")
	assert.InDelta(t, 2, closed[1].Orders[0].Profit, 1e-6)

	last, err := repo.LastOpenedAt(ctx, 7)
	require.NoError(t, err)
	require.NotNil(t, last)
	assert.True(t, last.Equal(t0.Add(-1*time.Hour)), last)
}
