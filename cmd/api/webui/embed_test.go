package webui_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"go-trade-bot/cmd/api/webui"

	"github.com/stretchr/testify/assert"
)

func TestWebUIHandler_ServesSPA(t *testing.T) {
	h := webui.Handler()

	// GET / returns index.html
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/html")
	assert.Equal(t, "no-cache", rec.Header().Get("Cache-Control"))
	assert.Contains(t, rec.Body.String(), "<title>Go Trade Bot — Dashboard</title>")
}

func TestWebUIHandler_DeepPathFallback(t *testing.T) {
	h := webui.Handler()

	// Deep path like /strategies/5 falls back to index.html with 200 OK
	req := httptest.NewRequest(http.MethodGet, "/strategies/5", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/html")
	assert.Equal(t, "no-cache", rec.Header().Get("Cache-Control"))
	assert.Contains(t, rec.Body.String(), "<div id=\"root\"></div>")
}

func TestWebUIHandler_NotBuiltFallback(t *testing.T) {
	// Test fallback when index.html is missing
	emptyFS := fstest.MapFS{}
	rec := httptest.NewRecorder()
	webui.ServeIndexOrFallback(rec, emptyFS)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Contains(t, rec.Body.String(), "Frontend not built")
	assert.Contains(t, rec.Body.String(), "make web-build")
}
