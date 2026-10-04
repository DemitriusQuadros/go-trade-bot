package user_test

import (
	"context"
	"testing"
	"time"

	"go-trade-bot/app/entities"
	userrepo "go-trade-bot/app/repository/user"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setup(t *testing.T) *userrepo.GormRepository {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&entities.User{}, &entities.Session{}, &entities.UserUsage{}))
	return userrepo.NewGormRepository(db)
}

func TestUsers_CRUDAndCaseInsensitiveUsername(t *testing.T) {
	r := setup(t)
	ctx := context.Background()

	n, err := r.CountUsers(ctx)
	require.NoError(t, err)
	assert.Zero(t, n)

	u, err := r.CreateUser(ctx, entities.User{Username: "Ana", DisplayName: "Ana", Role: "admin", Capabilities: []string{"admin"}, DailyAgentBudgetUSD: 1})
	require.NoError(t, err)
	assert.NotZero(t, u.ID)
	assert.Equal(t, "ana", u.Username)

	got, err := r.GetUserByUsername(ctx, " ANA ")
	require.NoError(t, err)
	assert.Equal(t, u.ID, got.ID)
	assert.Equal(t, []string{"admin"}, []string(got.Capabilities))

	_, err = r.CreateUser(ctx, entities.User{Username: "ana"})
	assert.Error(t, err, "username is unique")

	_, err = r.GetUser(ctx, 999)
	assert.ErrorIs(t, err, userrepo.ErrNotFound)

	// Two users without email are fine; duplicate emails are not.
	_, err = r.CreateUser(ctx, entities.User{Username: "bob", Role: "friend"})
	require.NoError(t, err)
	e := "x@y.z"
	_, err = r.CreateUser(ctx, entities.User{Username: "cid", Role: "friend", Email: &e})
	require.NoError(t, err)
	e2 := "x@y.z"
	_, err = r.CreateUser(ctx, entities.User{Username: "dan", Role: "friend", Email: &e2})
	assert.Error(t, err)

	admins, err := r.CountEnabledAdmins(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), admins)

	got.DisplayName = "Ana B"
	require.NoError(t, r.UpdateUser(ctx, got))
	names, err := r.UserNames(ctx)
	require.NoError(t, err)
	assert.Equal(t, "Ana B", names[u.ID])

	now := time.Now().UTC()
	require.NoError(t, r.TouchLogin(ctx, u.ID, now))
	got, _ = r.GetUser(ctx, u.ID)
	require.NotNil(t, got.LastLoginAt)

	require.NoError(t, r.DeleteUser(ctx, u.ID))
	_, err = r.GetUser(ctx, u.ID)
	assert.ErrorIs(t, err, userrepo.ErrNotFound)
}

func TestSessions(t *testing.T) {
	r := setup(t)
	ctx := context.Background()
	now := time.Now().UTC()
	s1, err := r.CreateSession(ctx, entities.Session{UserID: 1, TokenHash: "h1", CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(time.Hour)})
	require.NoError(t, err)
	s2, err := r.CreateSession(ctx, entities.Session{UserID: 1, TokenHash: "h2", CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(-time.Hour)})
	require.NoError(t, err)

	got, err := r.GetSessionByHash(ctx, "h1")
	require.NoError(t, err)
	assert.Equal(t, s1.ID, got.ID)

	later := now.Add(48 * time.Hour)
	require.NoError(t, r.TouchSession(ctx, s1.ID, now, later))
	got, _ = r.GetSessionByHash(ctx, "h1")
	assert.WithinDuration(t, later, got.ExpiresAt, time.Second)

	require.NoError(t, r.DeleteExpiredSessions(ctx, now))
	_, err = r.GetSessionByHash(ctx, "h2")
	assert.ErrorIs(t, err, userrepo.ErrNotFound)
	_ = s2

	_, err = r.CreateSession(ctx, entities.Session{UserID: 1, TokenHash: "h3", ExpiresAt: later})
	require.NoError(t, err)
	require.NoError(t, r.DeleteUserSessions(ctx, 1, s1.ID))
	_, err = r.GetSessionByHash(ctx, "h3")
	assert.ErrorIs(t, err, userrepo.ErrNotFound)
	_, err = r.GetSessionByHash(ctx, "h1")
	assert.NoError(t, err, "the excepted session survives")

	require.NoError(t, r.DeleteSession(ctx, s1.ID))
	_, err = r.GetSessionByHash(ctx, "h1")
	assert.ErrorIs(t, err, userrepo.ErrNotFound)
}

func TestUsage(t *testing.T) {
	r := setup(t)
	ctx := context.Background()
	day := time.Date(2026, 9, 29, 15, 0, 0, 0, time.UTC)

	u, err := r.GetUsage(ctx, 3, day)
	require.NoError(t, err)
	assert.Zero(t, u.CostUSD)

	require.NoError(t, r.AddUsage(ctx, 3, day, 0.25))
	require.NoError(t, r.AddUsage(ctx, 3, day.Add(time.Hour), 0.5))
	require.NoError(t, r.AddUsage(ctx, 3, day.Add(24*time.Hour), 9))

	u, err = r.GetUsage(ctx, 3, day)
	require.NoError(t, err)
	assert.Equal(t, 2, u.Runs)
	assert.InDelta(t, 0.75, u.CostUSD, 1e-9)

	all, err := r.ListUsageForDay(ctx, day)
	require.NoError(t, err)
	assert.InDelta(t, 0.75, all[3].CostUSD, 1e-9)
}
