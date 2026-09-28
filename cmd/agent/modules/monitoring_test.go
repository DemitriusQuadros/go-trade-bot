package modules

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go-trade-bot/internal/configuration"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fix-02 B5: cmd/agent serves the Asynqmon UI next to /metrics. Neither
// request touches Redis (the UI shell is static; /metrics is in-process).
func TestNewMonitoringHandler_ServesMetricsAndAsynqmon(t *testing.T) {
	cfg := &configuration.Configuration{}
	cfg.Redis.Addr = "127.0.0.1:1" // never dialled by these requests
	h := NewMonitoringHandler(cfg)

	for _, path := range []string{"/tasks/monitoring", "/tasks/monitoring/", "/tasks/monitoring/queues/agents"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusOK, rec.Code, path)
		body, _ := io.ReadAll(rec.Body)
		assert.True(t, strings.Contains(strings.ToLower(string(body)), "<html"), "%s serves the Asynqmon UI shell", path)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	assert.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nope", nil))
	assert.Equal(t, http.StatusNotFound, rec.Code)
}
