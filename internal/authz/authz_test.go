package authz_test

import (
	"context"
	"testing"

	"go-trade-bot/internal/authz"

	"github.com/stretchr/testify/assert"
)

func TestRolePresets(t *testing.T) {
	assert.Equal(t, authz.AllCapabilities, authz.RolePreset(authz.RoleAdmin))
	assert.Equal(t, []string{"view", "backtest", "edit_drafts", "agent_chat", "approve_proposals"}, authz.RolePreset(authz.RoleFriend))
	assert.Equal(t, []string{"view"}, authz.RolePreset(authz.RoleViewer))
	assert.Empty(t, authz.RolePreset("nope"))
}

func TestAdminImpliesEverything(t *testing.T) {
	p := authz.Principal{Caps: []string{authz.CapAdmin}}
	for _, c := range authz.AllCapabilities {
		assert.True(t, p.Has(c), c)
	}
	v := authz.Principal{Caps: []string{authz.CapView}}
	assert.True(t, v.Has(authz.CapView))
	assert.False(t, v.Has(authz.CapBacktest))
	assert.False(t, v.IsAdmin())
}

func TestNormalizeCapabilities(t *testing.T) {
	assert.Equal(t, []string{"view", "admin"}, authz.NormalizeCapabilities([]string{"admin", "bogus", "view", "view"}))
}

func TestContext(t *testing.T) {
	_, ok := authz.FromContext(context.Background())
	assert.False(t, ok)
	ctx := authz.WithUser(context.Background(), authz.Principal{UserID: 7, Username: "ana"})
	p, ok := authz.FromContext(ctx)
	assert.True(t, ok)
	assert.Equal(t, uint(7), p.UserID)
	_, ok = authz.FromContext(authz.WithoutUser(ctx))
	assert.False(t, ok, "WithoutUser shadows the parent principal")
}

func TestMustRouteCapability(t *testing.T) {
	assert.Panics(t, func() { authz.MustRouteCapability("GET", "/x", "") })
	assert.Panics(t, func() { authz.MustRouteCapability("GET", "/x", "superuser") })
	assert.NotPanics(t, func() { authz.MustRouteCapability("GET", "/x", authz.CapView) })
	assert.NotPanics(t, func() { authz.MustRouteCapability("POST", "/auth/login", authz.CapPublic) })
}
