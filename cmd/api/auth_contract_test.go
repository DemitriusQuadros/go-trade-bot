package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	authhandler "go-trade-bot/app/handler/web/auth"
	usershandler "go-trade-bot/app/handler/web/users"
	userrepo "go-trade-bot/app/repository/user"
	authusecase "go-trade-bot/app/usecase/auth"
	"go-trade-bot/internal/authz"
	"go-trade-bot/internal/cfaccess"
	"go-trade-bot/internal/configuration"
	"go-trade-bot/internal/handler"
	"go-trade-bot/internal/middleware"
	"go-trade-bot/internal/ratelimit"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// Auth-01 tests at the cmd/api level: the route walk, the role x route
// matrix, auth on by default and the end-to-end login flow.

// buildRoute calls a route constructor with zero-value arguments - enough
// for Handlers(), which only lists patterns and method values.
func buildRoute(t *testing.T, ctor any) Route {
	t.Helper()
	fn := reflect.ValueOf(ctor)
	args := make([]reflect.Value, fn.Type().NumIn())
	for i := range args {
		args[i] = reflect.Zero(fn.Type().In(i))
	}
	out := fn.Call(args)
	r, ok := out[0].Interface().(Route)
	require.True(t, ok, "%T is not a Route", out[0].Interface())
	return r
}

func allRoutes(t *testing.T) []handler.Configuration {
	var cfgs []handler.Configuration
	for _, c := range routeConstructors {
		cfgs = append(cfgs, buildRoute(t, c).Handlers()...)
	}
	return cfgs
}

// Test 1: every registered route has a (valid) capability.
func TestEveryRouteHasACapability(t *testing.T) {
	cfgs := allRoutes(t)
	require.NotEmpty(t, cfgs)
	for _, c := range cfgs {
		assert.True(t, authz.IsValidRouteCapability(c.Capability), "%s %s has capability %q", c.Method, c.Pattern, c.Capability)
		if c.Capability == authz.CapPublic {
			assert.True(t, strings.HasPrefix(c.Pattern, "/auth/"), "only /auth routes may be public: %s %s", c.Method, c.Pattern)
		}
		if c.Method != http.MethodGet && c.Capability == authz.CapView {
			t.Errorf("write route %s %s must not be view-only", c.Method, c.Pattern)
		}
	}
	assert.NotPanics(t, func() {
		routes := make([]Route, 0, len(routeConstructors))
		for _, c := range routeConstructors {
			routes = append(routes, buildRoute(t, c))
		}
		NewServeMux(routes, &configuration.Configuration{}, nil)
	})
}

type staticRoute []handler.Configuration

func (s staticRoute) Handlers() []handler.Configuration { return s }

// Test 1b: startup panics on an empty capability.
func TestNewServeMux_PanicsOnEmptyCapability(t *testing.T) {
	ok := func(w http.ResponseWriter, r *http.Request) {}
	assert.PanicsWithValue(t, "authz: route GET /oops has no capability", func() {
		NewServeMux([]Route{staticRoute{{Pattern: "/oops", Method: http.MethodGet, Action: ok}}}, &configuration.Configuration{}, nil)
	})
	assert.Panics(t, func() {
		NewServeMux([]Route{staticRoute{{Pattern: "/oops", Method: http.MethodGet, Action: ok, Capability: "root"}}}, &configuration.Configuration{}, nil)
	})
}

// principalSessions maps a cookie value to a fake principal.
type principalSessions map[string]authz.Principal

func (p principalSessions) ResolveSession(_ context.Context, raw string) (authz.Principal, error) {
	if pr, ok := p[raw]; ok {
		return pr, nil
	}
	return authz.Principal{}, errors.New("no session")
}

// stubbedRoutes are the real route declarations with every action replaced
// by a 200 stub, so the matrix tests the declared capabilities only.
func stubbedRoutes(t *testing.T) []Route {
	var out []Route
	for _, c := range routeConstructors {
		cfgs := buildRoute(t, c).Handlers()
		stub := make(staticRoute, len(cfgs))
		for i, cfg := range cfgs {
			cfg.Action = func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
			stub[i] = cfg
		}
		out = append(out, stub)
	}
	return out
}

// Test 2: role presets x a representative route per §3 row.
func TestAuthzMatrix(t *testing.T) {
	sessions := principalSessions{}
	for _, role := range []string{authz.RoleAdmin, authz.RoleFriend, authz.RoleViewer} {
		sessions[role] = authz.Principal{UserID: 1, Username: role, Role: role, Caps: authz.RolePreset(role)}
	}
	cfg := &configuration.Configuration{}
	router := NewServeMux(stubbedRoutes(t), cfg, middleware.NewAuth(cfg, sessions))

	type row struct {
		method, path       string
		admin, friend, vwr int
	}
	const ok, no = http.StatusOK, http.StatusForbidden
	rows := []row{
		// view
		{"GET", "/api/strategy", ok, ok, ok},
		{"GET", "/api/stream/dashboard", ok, ok, ok},
		{"GET", "/api/agent-reports/1/html", ok, ok, ok},
		{"GET", "/api/backtest/1/report", ok, ok, ok},
		{"GET", "/api/agents/kill-switch", ok, ok, ok},
		{"GET", "/api/agent/runs", ok, ok, ok},
		// admin GETs
		{"GET", "/api/settings", ok, no, no},
		{"GET", "/api/webhook-targets", ok, no, no},
		{"GET", "/api/deploy-gate", ok, no, no},
		{"GET", "/api/users", ok, no, no},
		// backtest
		{"POST", "/api/backtest", ok, ok, no},
		{"POST", "/api/backtest/walkforward", ok, ok, no},
		{"POST", "/api/backtest/1/montecarlo", ok, ok, no},
		{"POST", "/api/optimize", ok, ok, no},
		{"POST", "/api/script/repl", ok, ok, no},
		{"POST", "/api/script/fast-rerun", ok, ok, no},
		{"POST", "/api/candles/import", ok, ok, no},
		// edit_drafts
		{"POST", "/api/strategy", ok, ok, no},
		{"PUT", "/api/strategy/1", ok, ok, no},
		{"DELETE", "/api/strategy/1", ok, ok, no},
		{"POST", "/api/strategy/1/versions/2/revert", ok, ok, no},
		{"POST", "/api/strategies/1/memory", ok, ok, no},
		{"DELETE", "/api/backtest/1", ok, ok, no},
		// agent_chat
		{"POST", "/api/agent/runs", ok, ok, no},
		// approve_proposals
		{"POST", "/api/proposals/1/approve", ok, ok, no},
		{"POST", "/api/proposals/1/reject", ok, ok, no},
		// admin writes
		{"PUT", "/api/settings", ok, no, no},
		{"PUT", "/api/deploy-gate", ok, no, no},
		{"PUT", "/api/agents/kill-switch", ok, no, no},
		{"POST", "/api/agents", ok, no, no},
		{"PUT", "/api/agents/1", ok, no, no},
		{"DELETE", "/api/agents/1", ok, no, no},
		{"POST", "/api/agents/1/pause", ok, no, no},
		{"POST", "/api/agents/1/run", ok, no, no},
		{"PATCH", "/api/strategy/1/status", ok, no, no},
		{"PATCH", "/api/strategy/1/mode", ok, no, no},
		{"POST", "/api/strategy/enqueue", ok, no, no},
		{"POST", "/api/signal/close/1", ok, no, no},
		{"POST", "/api/candles/schedule", ok, no, no},
		{"PATCH", "/api/candles/schedule/1", ok, no, no},
		{"DELETE", "/api/candles/schedule/1", ok, no, no},
		{"POST", "/api/webhook-targets", ok, no, no},
		{"POST", "/api/webhook-targets/1/test", ok, no, no},
		{"POST", "/api/account", ok, no, no},
		{"PUT", "/api/account/dryrun", ok, no, no},
		{"POST", "/api/account/dryrun/reset", ok, no, no},
		{"PUT", "/api/account/live", ok, no, no},
		{"POST", "/api/account/sync", ok, no, no},
		{"POST", "/api/users", ok, no, no},
		{"PUT", "/api/users/1", ok, no, no},
		{"POST", "/api/users/1/reset-password", ok, no, no},
		{"DELETE", "/api/users/1", ok, no, no},
	}
	for _, r := range rows {
		for role, want := range map[string]int{authz.RoleAdmin: r.admin, authz.RoleFriend: r.friend, authz.RoleViewer: r.vwr} {
			req := httptest.NewRequest(r.method, r.path, nil)
			req.AddCookie(&http.Cookie{Name: middleware.SessionCookieName, Value: role})
			req.Header.Set("X-Requested-With", "gtb")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			assert.Equal(t, want, rec.Code, "%s %s as %s", r.method, r.path, role)
		}
		// No session at all -> 401 everywhere.
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(r.method, r.path, nil))
		assert.Equal(t, http.StatusUnauthorized, rec.Code, "%s %s unauthenticated", r.method, r.path)
	}
}

// --- end-to-end flow on the real auth usecase (SQLite) ---------------------------

type authEnv struct {
	router http.Handler
	uc     *authusecase.UseCase
}

func newAuthEnv(t *testing.T, cfg *configuration.Configuration) authEnv {
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, Migrate(db))
	uc := authusecase.NewUseCase(userrepo.NewGormRepository(db), ratelimit.NewMemoryCounter())
	uc.BcryptCost = bcrypt.MinCost
	routes := stubbedRoutes(t)
	// Swap the stubbed /auth and /users routes for the real ones.
	var kept []Route
	for _, r := range routes {
		cfgs := r.Handlers()
		if len(cfgs) > 0 && (strings.HasPrefix(cfgs[0].Pattern, "/auth/") || strings.HasPrefix(cfgs[0].Pattern, "/users")) {
			continue
		}
		kept = append(kept, r)
	}
	kept = append(kept, authhandlerFor(uc, cfg), usershandlerFor(uc))
	return authEnv{router: NewServeMux(kept, cfg, middleware.NewAuth(cfg, uc)), uc: uc}
}

type client struct {
	t      *testing.T
	router http.Handler
	cookie *http.Cookie
}

func (c *client) do(method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("X-Requested-With", "gtb")
	if c.cookie != nil {
		req.AddCookie(c.cookie)
	}
	rec := httptest.NewRecorder()
	c.router.ServeHTTP(rec, req)
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == middleware.SessionCookieName {
			if ck.MaxAge < 0 {
				c.cookie = nil
			} else {
				c.cookie = ck
			}
		}
	}
	return rec
}

func authBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	var m map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &m), rec.Body.String())
	return m
}

// Test 8: auth is on by default; /auth/me reports setup_required.
func TestAuthOnByDefault_SetupRequired(t *testing.T) {
	e := newAuthEnv(t, &configuration.Configuration{})
	c := &client{t: t, router: e.router}
	assert.Equal(t, http.StatusUnauthorized, c.do("GET", "/api/strategy", "").Code)
	rec := c.do("GET", "/api/auth/me", "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Equal(t, true, authBody(t, rec)["setup_required"])
}

// Acceptance 1-2 + test 4/5: bootstrap admin, login, friend rights, CSRF,
// logout.
func TestLoginFlow(t *testing.T) {
	e := newAuthEnv(t, &configuration.Configuration{})
	require.NoError(t, e.uc.Bootstrap(context.Background(), "root", "bootstrap-pass-1"))

	admin := &client{t: t, router: e.router}
	rec := admin.do("POST", "/api/auth/login", `{"username":"root","password":"nope-nope-nope"}`)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Equal(t, "invalid_credentials", authBody(t, rec)["error"])

	rec = admin.do("POST", "/api/auth/login", `{"username":"root","password":"bootstrap-pass-1"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NotNil(t, admin.cookie)
	assert.True(t, admin.cookie.HttpOnly)
	user := authBody(t, rec)["user"].(map[string]any)
	assert.Equal(t, "admin", user["role"])

	rec = admin.do("GET", "/api/auth/me", "")
	require.Equal(t, http.StatusOK, rec.Code)
	me := authBody(t, rec)
	assert.Equal(t, "admin", me["role"])
	for _, k := range []string{"id", "username", "display_name", "email", "role", "capabilities", "locale", "daily_agent_budget_usd", "today_agent_cost_usd"} {
		assert.Contains(t, me, k)
	}

	rec = admin.do("POST", "/api/users", `{"username":"ana","display_name":"Ana","role":"friend","password":"friend-pass-12"}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	created := authBody(t, rec)
	assert.NotContains(t, created, "password_hash")
	assert.Equal(t, []any{"view", "backtest", "edit_drafts", "agent_chat", "approve_proposals"}, created["capabilities"])

	rec = admin.do("GET", "/api/users", "")
	require.Equal(t, http.StatusOK, rec.Code)
	var list []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list))
	assert.Len(t, list, 2)
	assert.Contains(t, list[0], "last_login_at")
	assert.Contains(t, list[0], "today_agent_cost_usd")

	friend := &client{t: t, router: e.router}
	require.Equal(t, http.StatusOK, friend.do("POST", "/api/auth/login", `{"username":"ANA","password":"friend-pass-12"}`).Code)
	assert.Equal(t, http.StatusOK, friend.do("GET", "/api/strategy", "").Code)
	assert.Equal(t, http.StatusOK, friend.do("POST", "/api/strategy", `{}`).Code)
	assert.Equal(t, http.StatusOK, friend.do("POST", "/api/backtest", `{}`).Code)
	assert.Equal(t, http.StatusOK, friend.do("POST", "/api/agent/runs", `{}`).Code)
	assert.Equal(t, http.StatusOK, friend.do("POST", "/api/proposals/1/approve", `{}`).Code)
	for _, r := range [][2]string{{"GET", "/api/settings"}, {"PUT", "/api/settings"}, {"GET", "/api/users"}, {"PUT", "/api/agents/kill-switch"}, {"PATCH", "/api/strategy/1/mode"}} {
		rec := friend.do(r[0], r[1], `{}`)
		assert.Equal(t, http.StatusForbidden, rec.Code, "%v", r)
		assert.Equal(t, "forbidden", authBody(t, rec)["error"])
	}

	// CSRF: a cookie-authenticated POST without X-Requested-With or Origin.
	req := httptest.NewRequest("POST", "/api/backtest", nil)
	req.AddCookie(friend.cookie)
	csrf := httptest.NewRecorder()
	e.router.ServeHTTP(csrf, req)
	assert.Equal(t, http.StatusForbidden, csrf.Code)

	// Logout invalidates the session.
	stale := friend.cookie
	require.Equal(t, http.StatusOK, friend.do("POST", "/api/auth/logout", "").Code)
	assert.Nil(t, friend.cookie, "cookie cleared")
	friend.cookie = stale
	assert.Equal(t, http.StatusUnauthorized, friend.do("GET", "/api/strategy", "").Code)
}

// Test 4: the 6th failure is a 429.
func TestLogin_RateLimited(t *testing.T) {
	e := newAuthEnv(t, &configuration.Configuration{})
	require.NoError(t, e.uc.Bootstrap(context.Background(), "root", "bootstrap-pass-1"))
	c := &client{t: t, router: e.router}
	for i := 0; i < 5; i++ {
		require.Equal(t, http.StatusUnauthorized, c.do("POST", "/api/auth/login", `{"username":"root","password":"wrong-wrong-1"}`).Code)
	}
	rec := c.do("POST", "/api/auth/login", `{"username":"root","password":"bootstrap-pass-1"}`)
	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
	assert.Equal(t, "rate_limited", authBody(t, rec)["error"])
}

// The API_TOKEN bearer is a synthetic admin; ?token= is gone.
func TestServiceToken(t *testing.T) {
	e := newAuthEnv(t, &configuration.Configuration{APIToken: "svc-token"})
	req := httptest.NewRequest("PUT", "/api/settings", nil)
	req.Header.Set("Authorization", "Bearer svc-token")
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	e.router.ServeHTTP(rec, httptest.NewRequest("GET", "/api/strategy?token=svc-token", nil))
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	req = httptest.NewRequest("GET", "/api/auth/me", nil)
	req.Header.Set("Authorization", "Bearer svc-token")
	rec = httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "service", authBody(t, rec)["username"])
}

func authhandlerFor(uc *authusecase.UseCase, cfg *configuration.Configuration) Route {
	return authhandler.NewAuthHandler(uc, cfg)
}

func usershandlerFor(uc *authusecase.UseCase) Route { return usershandler.NewUsersHandler(uc) }

// Acceptance 4: with CF_ACCESS.* set, a request without the Access JWT is a
// 403 even with a valid session cookie.
func TestCFAccessBlocksEvenWithSession(t *testing.T) {
	e := newAuthEnv(t, &configuration.Configuration{})
	require.NoError(t, e.uc.Bootstrap(context.Background(), "root", "bootstrap-pass-1"))
	c := &client{t: t, router: e.router}
	require.Equal(t, http.StatusOK, c.do("POST", "/api/auth/login", `{"username":"root","password":"bootstrap-pass-1"}`).Code)

	guarded := cfaccess.NewVerifierWithURLs("https://team.cloudflareaccess.com", "http://127.0.0.1:1/certs", "aud").Middleware(e.router)
	req := httptest.NewRequest("GET", "/api/strategy", nil)
	req.RemoteAddr = "203.0.113.5:1234"
	req.AddCookie(c.cookie)
	rec := httptest.NewRecorder()
	guarded.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, "access_required", authBody(t, rec)["error"])
}
