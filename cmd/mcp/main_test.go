package main

import (
	"testing"

	"go-trade-bot/app/entities"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestAppGraphIsValid(t *testing.T) {
	require.NoError(t, fx.ValidateApp(appOptions()))
}

// A-01 AC#1 for cmd/mcp.
func TestMigrate_FreshDBTwice(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, Migrate(db))
	require.NoError(t, Migrate(db))
	assert.True(t, db.Migrator().HasTable(&entities.AgentReport{}))
	assert.True(t, db.Migrator().HasTable(&entities.StrategyChangeProposal{}))
	assert.True(t, db.Migrator().HasTable(&entities.DeployGateConfig{}))
	var n int64
	require.NoError(t, db.Model(&entities.Agent{}).Where("is_default = ?", true).Count(&n).Error)
	assert.Equal(t, int64(1), n)
}
