package settings_test

import (
	"context"
	"testing"

	"go-trade-bot/app/entities"
	"go-trade-bot/app/repository/settings"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setup(t *testing.T) settings.Repository {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&entities.Settings{}))
	return settings.NewRepository(db)
}

// The kill switch is written only by SetAgentsPaused; a normal Save (with
// a stale copy) must never flip it.
func TestSetAgentsPaused_IndependentOfSave(t *testing.T) {
	repo := setup(t)
	ctx := context.Background()

	require.NoError(t, repo.SetAgentsPaused(ctx, true)) // creates the row
	s, err := repo.Get(ctx)
	require.NoError(t, err)
	assert.True(t, s.AgentsPaused)

	stale := entities.Settings{Mode: "dryrun", WebhookURL: "https://x", AgentsPaused: false}
	require.NoError(t, repo.Save(ctx, &stale))
	s, err = repo.Get(ctx)
	require.NoError(t, err)
	assert.True(t, s.AgentsPaused, "Save must not overwrite the kill switch")
	assert.Equal(t, "https://x", s.WebhookURL)

	require.NoError(t, repo.SetAgentsPaused(ctx, false))
	s, err = repo.Get(ctx)
	require.NoError(t, err)
	assert.False(t, s.AgentsPaused)
	assert.Equal(t, "dryrun", s.Mode, "SetAgentsPaused touches only its column")
}
