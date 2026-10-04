package main

import (
	"testing"

	"go-trade-bot/app/entities"
	"go-trade-bot/cmd/agent/modules"
	"go-trade-bot/internal/configuration"
	"go-trade-bot/internal/exchange"
	"go-trade-bot/internal/metrics"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestAppGraphIsValid(t *testing.T) {
	require.NoError(t, fx.ValidateApp(appOptions()))
}

// Safety invariant (A-02 §1): cmd/agent's graph never provides an
// undecorated or swappable exchange client.
func TestAppGraphHasNoUndecoratedExchangeClient(t *testing.T) {
	assert.Error(t, fx.ValidateApp(appOptions(), fx.Invoke(func(*exchange.SwappableExchangeClient) {})))
	assert.Error(t, fx.ValidateApp(appOptions(), fx.Invoke(func(*exchange.BinanceAdapter) {})))
	assert.Error(t, fx.ValidateApp(appOptions(), fx.Invoke(func(*exchange.BinanceTestnetAdapter) {})))
}

func TestExchangeModuleProvidesReadOnlyClient(t *testing.T) {
	collector := metrics.NewMetricsCollector(modules.AgentRuntimeMetrics)
	client, err := modules.NewReadOnlyExchangeClient(&configuration.Configuration{Broker: configuration.Broker{ApiKey: "k", ApiSecret: "s"}}, collector)
	require.NoError(t, err)
	_, isReadOnly := client.(*exchange.ReadOnlyClient)
	assert.True(t, isReadOnly)
	_, err = client.PlaceOrder(t.Context(), exchange.PlaceOrderRequest{})
	assert.ErrorIs(t, err, exchange.ErrReadOnlyClient)
}

// A-01 AC#1: migrating a fresh DB twice creates every table and exactly
// one default agent.
func TestMigrate_FreshDBTwice(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, Migrate(db))
	require.NoError(t, Migrate(db))
	for _, m := range []any{&entities.Agent{}, &entities.AgentStrategyBinding{}, &entities.StrategyMemoryEntry{}, &entities.AgentReport{}, &entities.WebhookTarget{}, &entities.AgentUsage{},
		&entities.StrategyChangeProposal{}, &entities.DeployGateConfig{}} {
		assert.True(t, db.Migrator().HasTable(m))
	}
	var n int64
	require.NoError(t, db.Model(&entities.Agent{}).Where("is_default = ?", true).Count(&n).Error)
	assert.Equal(t, int64(1), n)
}
