package middleware_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go-trade-bot/internal/authz"
	"go-trade-bot/internal/configuration"
	"go-trade-bot/internal/middleware"

	"github.com/stretchr/testify/assert"
)

type fakeSessions map[string]authz.Principal

func (f fakeSessions) ResolveSession(_ context.Context, raw string) (authz.Principal, error) {
	if p, ok := f[raw]; ok {
		return p, nil
	}
	return authz.Principal{}, errors.New("no session")
}

var friend = authz.Principal{UserID: 2, Username: "ana", Role: authz.RoleFriend, Caps: authz.RolePreset(authz.RoleFriend)}

// chain mirrors NewServeMux: Authenticate -> RequireCapability -> handler.
func chain(cfg *configuration.Configuration, capability string) (http.HandlerFunc, *authz.Principal) {
	var seen authz.Principal
	auth := middleware.NewAuth(cfg, fakeSessions{"good": friend})
	h := auth.Authenticate(middleware.RequireCapability(capability, func(w http.ResponseWriter, r *http.Request) {
		seen, _ = authz.FromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))
	return h, &seen
}

func do(h http.HandlerFunc, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

func TestAuth_OnByDefault(t *testing.T) {
	// No users, no token, no insecure flag -> 401.
	h, _ := chain(&configuration.Configuration{}, authz.CapView)
	rec := do(h, httptest.NewRequest(http.MethodGet, "/api/strategy", nil))
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), `"error":"unauthorized"`)
}

func TestAuth_ServiceTokenIsAdmin(t *testing.T) {
	h, seen := chain(&configuration.Configuration{APIToken: "tok-123"}, authz.CapAdmin)
	req := httptest.NewRequest(http.MethodPost, "/api/strategy/enqueue", nil)
	req.Header.Set("Authorization", "Bearer tok-123")
	rec := do(h, req)
	assert.Equal(t, http.StatusOK, rec.Code, "bearer requests are CSRF-exempt")
	assert.Equal(t, "service", seen.Username)

	req = httptest.NewRequest(http.MethodGet, "/api/strategy", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	assert.Equal(t, http.StatusUnauthorized, do(h, req).Code)
}

func TestAuth_QueryTokenFallbackRemoved(t *testing.T) {
	h, _ := chain(&configuration.Configuration{APIToken: "tok-123"}, authz.CapView)
	rec := do(h, httptest.NewRequest(http.MethodGet, "/api/stream/dashboard?token=tok-123", nil))
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestAuth_BearerWithoutConfiguredToken(t *testing.T) {
	h, _ := chain(&configuration.Configuration{}, authz.CapView)
	req := httptest.NewRequest(http.MethodGet, "/api/strategy", nil)
	req.Header.Set("Authorization", "Bearer anything")
	assert.Equal(t, http.StatusUnauthorized, do(h, req).Code)
}

func TestAuth_CookieSessionAndCapability(t *testing.T) {
	h, seen := chain(&configuration.Configuration{}, authz.CapView)
	req := httptest.NewRequest(http.MethodGet, "/api/strategy", nil)
	req.AddCookie(&http.Cookie{Name: middleware.SessionCookieName, Value: "good"})
	assert.Equal(t, http.StatusOK, do(h, req).Code)
	assert.Equal(t, uint(2), seen.UserID)

	admin, _ := chain(&configuration.Configuration{}, authz.CapAdmin)
	req = httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	req.AddCookie(&http.Cookie{Name: middleware.SessionCookieName, Value: "good"})
	rec := do(admin, req)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Contains(t, rec.Body.String(), `"error":"forbidden"`)

	req = httptest.NewRequest(http.MethodGet, "/api/strategy", nil)
	req.AddCookie(&http.Cookie{Name: middleware.SessionCookieName, Value: "stale"})
	assert.Equal(t, http.StatusUnauthorized, do(h, req).Code)
}

func TestAuth_CSRF(t *testing.T) {
	cfg := &configuration.Configuration{Auth: configuration.AuthConfig{AllowedOrigins: []string{"https://bot.example.com"}}}
	h, _ := chain(cfg, authz.CapBacktest)
	mk := func() *http.Request {
		req := httptest.NewRequest(http.MethodPost, "http://localhost:8080/api/backtest", nil)
		req.AddCookie(&http.Cookie{Name: middleware.SessionCookieName, Value: "good"})
		return req
	}

	rec := do(h, mk())
	assert.Equal(t, http.StatusForbidden, rec.Code, "cookie POST without X-Requested-With or Origin")

	req := mk()
	req.Header.Set("Origin", "https://evil.example")
	assert.Equal(t, http.StatusForbidden, do(h, req).Code)

	req = mk()
	req.Header.Set("X-Requested-With", "gtb")
	assert.Equal(t, http.StatusOK, do(h, req).Code)

	req = mk()
	req.Header.Set("Origin", "http://localhost:8080")
	assert.Equal(t, http.StatusOK, do(h, req).Code, "same host origin")

	req = mk()
	req.Header.Set("Origin", "https://bot.example.com")
	assert.Equal(t, http.StatusOK, do(h, req).Code, "AUTH.ALLOWED_ORIGINS")

	get := httptest.NewRequest(http.MethodGet, "/api/backtest", nil)
	get.AddCookie(&http.Cookie{Name: middleware.SessionCookieName, Value: "good"})
	assert.Equal(t, http.StatusOK, do(h, get).Code, "GETs are exempt")
}

func TestAuth_InsecureOnlyWhenExplicit(t *testing.T) {
	h, seen := chain(&configuration.Configuration{AllowInsecureNoAuth: true}, authz.CapAdmin)
	rec := do(h, httptest.NewRequest(http.MethodPut, "/api/settings", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "insecure", seen.Username)
}

func TestRequireCapability_Public(t *testing.T) {
	h, _ := chain(&configuration.Configuration{}, authz.CapPublic)
	assert.Equal(t, http.StatusOK, do(h, httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)).Code)
}

func TestRequireAuth_ServiceTokenOnly(t *testing.T) {
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }

	h := middleware.RequireAuth(&configuration.Configuration{}, ok)
	assert.Equal(t, http.StatusUnauthorized, do(h, httptest.NewRequest(http.MethodGet, "/", nil)).Code, "no token configured -> refuse")

	h = middleware.RequireAuth(&configuration.Configuration{APIToken: "t"}, ok)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer t")
	assert.Equal(t, http.StatusOK, do(h, req).Code)
	assert.Equal(t, http.StatusUnauthorized, do(h, httptest.NewRequest(http.MethodGet, "/?token=t", nil)).Code)

	h = middleware.RequireAuth(&configuration.Configuration{AllowInsecureNoAuth: true}, ok)
	assert.Equal(t, http.StatusOK, do(h, httptest.NewRequest(http.MethodGet, "/", nil)).Code)
}

func TestIsHTTPS(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	assert.False(t, middleware.IsHTTPS(req, false), "untrusted proxy header")
	assert.True(t, middleware.IsHTTPS(req, true))
}

func TestSetSessionCookie(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	middleware.SetSessionCookie(rec, req, "abc", timeNowPlusDay(), false)
	c := rec.Result().Cookies()[0]
	assert.Equal(t, "gtb_session", c.Name)
	assert.True(t, c.HttpOnly)
	assert.Equal(t, http.SameSiteLaxMode, c.SameSite)
	assert.Equal(t, "/", c.Path)
	assert.False(t, c.Secure)
}

func timeNowPlusDay() time.Time { return time.Now().Add(24 * time.Hour) }
