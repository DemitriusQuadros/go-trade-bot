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

// A-01 AC#1 for cmd/api.
func TestMigrate_FreshDBTwice(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, Migrate(db))
	require.NoError(t, Migrate(db))
	for _, m := range []any{&entities.Agent{}, &entities.AgentStrategyBinding{}, &entities.StrategyMemoryEntry{}, &entities.AgentReport{}, &entities.WebhookTarget{}, &entities.AgentUsage{},
		&entities.StrategyChangeProposal{}, &entities.DeployGateConfig{}} {
		assert.True(t, db.Migrator().HasTable(m))
	}
	// Phase B-01: the deploy gate singleton is seeded with the defaults.
	var gate []entities.DeployGateConfig
	require.NoError(t, db.Find(&gate).Error)
	require.Len(t, gate, 1)
	assert.Equal(t, entities.DefaultDeployGateConfig().MinTrades, gate[0].MinTrades)
	assert.True(t, db.Migrator().HasColumn(&entities.Strategy{}, "ChallengerOfID"))
	assert.True(t, db.Migrator().HasColumn(&entities.BacktestRun{}, "CandidateSourceHash"))
	var agents []entities.Agent
	require.NoError(t, db.Where("is_default = ?", true).Find(&agents).Error)
	require.Len(t, agents, 1)
	assert.Equal(t, entities.DefaultAgentName, agents[0].Name)
}
