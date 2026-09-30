package auth_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"go-trade-bot/app/entities"
	userrepo "go-trade-bot/app/repository/user"
	"go-trade-bot/app/usecase/auth"
	"go-trade-bot/internal/authz"
	"go-trade-bot/internal/customerror"
	"go-trade-bot/internal/ratelimit"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type fixture struct {
	uc   *auth.UseCase
	repo *userrepo.GormRepository
	now  *time.Time
}

func newFixture(t *testing.T) fixture {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&entities.User{}, &entities.Session{}, &entities.UserUsage{}))
	repo := userrepo.NewGormRepository(db)
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	uc := auth.NewUseCase(repo, ratelimit.NewMemoryCounter())
	uc.BcryptCost = bcrypt.MinCost
	uc.Now = func() time.Time { return now }
	return fixture{uc: uc, repo: repo, now: &now}
}

func code(t *testing.T, err error) (int, string) {
	t.Helper()
	var ce *customerror.CustomError
	require.True(t, errors.As(err, &ce), "want a CustomError, got %v", err)
	return ce.Code, ce.ErrorCode
}

func (f fixture) mkUser(t *testing.T, name, role string) entities.User {
	v, err := f.uc.CreateUser(context.Background(), auth.CreateUserRequest{Username: name, Role: role, Password: "correct-horse-1"})
	require.NoError(t, err)
	return v.User
}

func TestBootstrap(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	setup, err := f.uc.SetupRequired(ctx)
	require.NoError(t, err)
	assert.True(t, setup)

	require.NoError(t, f.uc.Bootstrap(ctx, "", ""), "unset values are a no-op")
	require.NoError(t, f.uc.Bootstrap(ctx, "Root", "bootstrap-pass-1"))
	u, err := f.repo.GetUserByUsername(ctx, "root")
	require.NoError(t, err)
	assert.Equal(t, authz.RoleAdmin, u.Role)
	assert.Equal(t, authz.AllCapabilities, []string(u.Capabilities))
	cost, _ := bcrypt.Cost([]byte(u.PasswordHash))
	assert.Equal(t, bcrypt.MinCost, cost)

	// A second bootstrap with users present does nothing.
	require.NoError(t, f.uc.Bootstrap(ctx, "other", "bootstrap-pass-1"))
	_, err = f.repo.GetUserByUsername(ctx, "other")
	assert.ErrorIs(t, err, userrepo.ErrNotFound)
}

func TestDefaultBcryptCostIsAtLeast12(t *testing.T) {
	assert.GreaterOrEqual(t, auth.DefaultBcryptCost, 12)
}

func TestLogin_SuccessResolveLogout(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	u := f.mkUser(t, "ana", authz.RoleFriend)

	res, err := f.uc.Login(ctx, "ANA", "correct-horse-1", "1.2.3.4", "ua")
	require.NoError(t, err)
	assert.NotEmpty(t, res.Token)
	assert.Equal(t, u.ID, res.User.ID)

	p, err := f.uc.ResolveSession(ctx, res.Token)
	require.NoError(t, err)
	assert.Equal(t, u.ID, p.UserID)
	assert.True(t, p.Has(authz.CapEditDrafts))
	assert.False(t, p.IsAdmin())

	require.NoError(t, f.uc.Logout(ctx, res.Token))
	_, err = f.uc.ResolveSession(ctx, res.Token)
	assert.ErrorIs(t, err, auth.ErrNoSession)
}

func TestLogin_BadPasswordUnknownUserDisabledAreAll401(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	u := f.mkUser(t, "ana", authz.RoleFriend)

	_, err := f.uc.Login(ctx, "ana", "wrong-password", "ip1", "")
	c, ec := code(t, err)
	assert.Equal(t, http.StatusUnauthorized, c)
	assert.Equal(t, "invalid_credentials", ec)

	_, err = f.uc.Login(ctx, "ghost", "whatever-pass", "ip2", "")
	c, ec = code(t, err)
	assert.Equal(t, http.StatusUnauthorized, c)
	assert.Equal(t, "invalid_credentials", ec)

	disabled := true
	_, err = f.uc.UpdateUser(ctx, u.ID, auth.UpdateUserRequest{Disabled: &disabled})
	require.NoError(t, err)
	_, err = f.uc.Login(ctx, "ana", "correct-horse-1", "ip3", "")
	c, ec = code(t, err)
	assert.Equal(t, http.StatusUnauthorized, c)
	assert.Equal(t, "invalid_credentials", ec)
}

func TestLogin_SixthFailureIsRateLimited(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.mkUser(t, "ana", authz.RoleFriend)
	for i := 0; i < auth.MaxFailuresPerUser; i++ {
		_, err := f.uc.Login(ctx, "ana", "nope-nope-nope", "10.0.0.1", "")
		c, _ := code(t, err)
		require.Equal(t, http.StatusUnauthorized, c, "attempt %d", i+1)
	}
	_, err := f.uc.Login(ctx, "ana", "correct-horse-1", "10.0.0.2", "")
	c, ec := code(t, err)
	assert.Equal(t, http.StatusTooManyRequests, c)
	assert.Equal(t, "rate_limited", ec)
}

func TestLogin_IPLimit(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	for i := 0; i < auth.MaxFailuresPerIP; i++ {
		_, _ = f.uc.Login(ctx, "user"+string(rune('a'+i)), "nope-nope-nope", "10.9.9.9", "")
	}
	_, err := f.uc.Login(ctx, "fresh", "nope-nope-nope", "10.9.9.9", "")
	c, _ := code(t, err)
	assert.Equal(t, http.StatusTooManyRequests, c)
}

func TestSession_ExpiresAndSlides(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.mkUser(t, "ana", authz.RoleFriend)
	res, err := f.uc.Login(ctx, "ana", "correct-horse-1", "ip", "")
	require.NoError(t, err)

	// 29 days later the session is still valid, and is slid forward.
	*f.now = f.now.Add(29 * 24 * time.Hour)
	_, err = f.uc.ResolveSession(ctx, res.Token)
	require.NoError(t, err)

	// 29 more days: valid only because it slid.
	*f.now = f.now.Add(29 * 24 * time.Hour)
	_, err = f.uc.ResolveSession(ctx, res.Token)
	require.NoError(t, err)

	// 31 idle days: expired.
	*f.now = f.now.Add(31 * 24 * time.Hour)
	_, err = f.uc.ResolveSession(ctx, res.Token)
	assert.ErrorIs(t, err, auth.ErrNoSession)
}

func TestUpdateMe_PasswordChangeSignsOutOtherSessions(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.mkUser(t, "ana", authz.RoleFriend)
	a, _ := f.uc.Login(ctx, "ana", "correct-horse-1", "ip", "")
	b, _ := f.uc.Login(ctx, "ana", "correct-horse-1", "ip", "")
	pa, err := f.uc.ResolveSession(ctx, a.Token)
	require.NoError(t, err)

	wrong := "nope"
	np := "brand-new-pass-1"
	_, err = f.uc.UpdateMe(ctx, pa, auth.UpdateMeRequest{CurrentPassword: &wrong, NewPassword: &np})
	c, _ := code(t, err)
	assert.Equal(t, http.StatusBadRequest, c)

	cur := "correct-horse-1"
	name := "Ana Maria"
	loc := "pt-BR"
	me, err := f.uc.UpdateMe(ctx, pa, auth.UpdateMeRequest{DisplayName: &name, Locale: &loc, CurrentPassword: &cur, NewPassword: &np})
	require.NoError(t, err)
	assert.Equal(t, "Ana Maria", me.DisplayName)
	assert.Equal(t, "pt-BR", me.Locale)

	_, err = f.uc.ResolveSession(ctx, a.Token)
	assert.NoError(t, err, "the current session survives")
	_, err = f.uc.ResolveSession(ctx, b.Token)
	assert.ErrorIs(t, err, auth.ErrNoSession, "other sessions are signed out")
	_, err = f.uc.Login(ctx, "ana", np, "ip", "")
	assert.NoError(t, err)

	bad := "xx"
	_, err = f.uc.UpdateMe(ctx, pa, auth.UpdateMeRequest{Locale: &bad})
	assert.Error(t, err)
}

func TestChatBudget(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	u := f.mkUser(t, "ana", authz.RoleFriend)
	p := auth.PrincipalFor(u, 0)

	require.NoError(t, f.uc.CheckChatBudget(ctx, p))
	require.NoError(t, f.uc.RecordChatUsage(ctx, p, 0.6))
	require.NoError(t, f.uc.CheckChatBudget(ctx, p))
	require.NoError(t, f.uc.RecordChatUsage(ctx, p, 0.4))

	err := f.uc.CheckChatBudget(ctx, p)
	c, ec := code(t, err)
	assert.Equal(t, http.StatusConflict, c)
	assert.Equal(t, "user_budget_exceeded", ec)
	var ce *customerror.CustomError
	errors.As(err, &ce)
	assert.Equal(t, "your daily agent budget ($1.00) is used up; it resets at 00:00 UTC", ce.Message)

	usage, _ := f.repo.GetUsage(ctx, u.ID, *f.now)
	assert.Equal(t, 2, usage.Runs)

	me, err := f.uc.Me(ctx, p)
	require.NoError(t, err)
	assert.InDelta(t, 1.0, me.TodayAgentCostUSD, 1e-9)

	// Next UTC day resets.
	*f.now = f.now.Add(24 * time.Hour)
	assert.NoError(t, f.uc.CheckChatBudget(ctx, p))

	// Synthetic principals are not limited.
	assert.NoError(t, f.uc.CheckChatBudget(ctx, authz.ServicePrincipal()))
}

func TestUserManagement(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	admin := f.mkUser(t, "root", authz.RoleAdmin)
	friend := f.mkUser(t, "ana", authz.RoleFriend)
	actor := auth.PrincipalFor(admin, 0)

	_, err := f.uc.CreateUser(ctx, auth.CreateUserRequest{Username: "bob", Role: authz.RoleFriend, Password: "short"})
	c, _ := code(t, err)
	assert.Equal(t, http.StatusBadRequest, c, "password >= 10 chars")
	_, err = f.uc.CreateUser(ctx, auth.CreateUserRequest{Username: "x", Role: authz.RoleFriend, Password: "long-enough-1"})
	assert.Error(t, err, "username too short")
	_, err = f.uc.CreateUser(ctx, auth.CreateUserRequest{Username: "ANA", Role: authz.RoleFriend, Password: "long-enough-1"})
	c, _ = code(t, err)
	assert.Equal(t, http.StatusConflict, c)

	// Role change resets capabilities unless capabilities are also sent.
	viewer := authz.RoleViewer
	v, err := f.uc.UpdateUser(ctx, friend.ID, auth.UpdateUserRequest{Role: &viewer})
	require.NoError(t, err)
	assert.Equal(t, []string{"view"}, []string(v.User.Capabilities))
	friendRole := authz.RoleFriend
	caps := []string{"view", "backtest"}
	v, err = f.uc.UpdateUser(ctx, friend.ID, auth.UpdateUserRequest{Role: &friendRole, Capabilities: &caps})
	require.NoError(t, err)
	assert.Equal(t, caps, []string(v.User.Capabilities))

	// Disabling deletes sessions.
	res, err := f.uc.Login(ctx, "ana", "correct-horse-1", "ip", "")
	require.NoError(t, err)
	yes := true
	_, err = f.uc.UpdateUser(ctx, friend.ID, auth.UpdateUserRequest{Disabled: &yes})
	require.NoError(t, err)
	_, err = f.uc.ResolveSession(ctx, res.Token)
	assert.ErrorIs(t, err, auth.ErrNoSession)

	// Reset password signs out.
	no := false
	_, err = f.uc.UpdateUser(ctx, friend.ID, auth.UpdateUserRequest{Disabled: &no})
	require.NoError(t, err)
	res, _ = f.uc.Login(ctx, "ana", "correct-horse-1", "ip", "")
	require.NoError(t, f.uc.ResetPassword(ctx, friend.ID, "another-pass-12"))
	_, err = f.uc.ResolveSession(ctx, res.Token)
	assert.ErrorIs(t, err, auth.ErrNoSession)

	// Delete: not yourself, not the last enabled admin.
	c, _ = code(t, f.uc.DeleteUser(ctx, actor, admin.ID))
	assert.Equal(t, http.StatusBadRequest, c)
	c, _ = code(t, f.uc.DeleteUser(ctx, authz.ServicePrincipal(), admin.ID))
	assert.Equal(t, http.StatusBadRequest, c, "last enabled admin")
	_, err = f.uc.UpdateUser(ctx, admin.ID, auth.UpdateUserRequest{Role: &viewer})
	assert.Error(t, err, "cannot demote the last admin")
	require.NoError(t, f.uc.DeleteUser(ctx, actor, friend.ID))
	c, _ = code(t, f.uc.DeleteUser(ctx, actor, 999))
	assert.Equal(t, http.StatusNotFound, c)

	list, err := f.uc.ListUsers(ctx)
	require.NoError(t, err)
	assert.Len(t, list, 1)
}
