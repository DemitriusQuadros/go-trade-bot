package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"go-trade-bot/internal/configuration"
	"go-trade-bot/internal/middleware"

	"github.com/stretchr/testify/assert"
)

func TestRequireAuth_SuccessHeader(t *testing.T) {
	cfg := &configuration.Configuration{
		APIToken: "secret-token-123",
	}

	called := false
	handler := middleware.RequireAuth(cfg, func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/strategy", nil)
	req.Header.Set("Authorization", "Bearer secret-token-123")
	rec := httptest.NewRecorder()

	handler(rec, req)

	assert.True(t, called)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestRequireAuth_SuccessQueryParam(t *testing.T) {
	cfg := &configuration.Configuration{
		APIToken: "secret-token-123",
	}

	called := false
	handler := middleware.RequireAuth(cfg, func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/stream/dashboard?token=secret-token-123", nil)
	rec := httptest.NewRecorder()

	handler(rec, req)

	assert.True(t, called)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestRequireAuth_UnauthorizedMissing(t *testing.T) {
	cfg := &configuration.Configuration{
		APIToken: "secret-token-123",
	}

	called := false
	handler := middleware.RequireAuth(cfg, func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	req := httptest.NewRequest("GET", "/strategy", nil)
	rec := httptest.NewRecorder()

	handler(rec, req)

	assert.False(t, called)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), "unauthorized")
}

func TestRequireAuth_UnauthorizedWrongToken(t *testing.T) {
	cfg := &configuration.Configuration{
		APIToken: "secret-token-123",
	}

	called := false
	handler := middleware.RequireAuth(cfg, func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	req := httptest.NewRequest("GET", "/strategy", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	rec := httptest.NewRecorder()

	handler(rec, req)

	assert.False(t, called)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestRequireAuth_AllowInsecure(t *testing.T) {
	cfg := &configuration.Configuration{
		APIToken:            "secret-token-123",
		AllowInsecureNoAuth: true,
	}

	called := false
	handler := middleware.RequireAuth(cfg, func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/strategy", nil)
	rec := httptest.NewRecorder()

	handler(rec, req)

	assert.True(t, called)
	assert.Equal(t, http.StatusOK, rec.Code)
}
