package agentreports

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"go-trade-bot/app/entities"
	usecase "go-trade-bot/app/usecase/agentplatform"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeReports struct{ reports map[uint]entities.AgentReport }

func (f fakeReports) ListReports(context.Context, usecase.ReportFilter) ([]entities.AgentReport, error) {
	return nil, nil
}
func (f fakeReports) GetReport(_ context.Context, id uint) (entities.AgentReport, error) {
	return f.reports[id], nil
}
func (f fakeReports) AgentNames(context.Context) map[uint]string { return nil }

func page(lang, label string) string {
	return "<!DOCTYPE html>\n<html lang=\"" + lang + "\">\n<body>" + label + "</body></html>"
}

func getHTML(t *testing.T, h *Handler, url string) *httptest.ResponseRecorder {
	t.Helper()
	router := mux.NewRouter()
	for _, c := range h.Handlers() {
		router.HandleFunc(c.Pattern, c.Action).Methods(c.Method)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	return rec
}

// i18n-02 test 3 (read side): ?lang= serves that locale's snapshot; old
// reports without snapshots (and unknown langs) fall back to RenderedHTML.
func TestHTML_LangSnapshots(t *testing.T) {
	withSnapshots := entities.AgentReport{ID: 1, RenderedHTML: page("es", "Métricas clave")}
	require.NoError(t, withSnapshots.SetLocaleSnapshots(map[string]string{
		"en": page("en", "Key metrics"), "es": page("es", "Métricas clave"), "pt-BR": page("pt-BR", "Principais métricas"),
	}))
	old := entities.AgentReport{ID: 2, RenderedHTML: page("en", "Key metrics")}
	h := NewAgentReportsHandler(fakeReports{reports: map[uint]entities.AgentReport{1: withSnapshots, 2: old}})

	cases := map[string]string{
		"/agent-reports/1/html":                       "Métricas clave",
		"/agent-reports/1/html?lang=es":               "Métricas clave",
		"/agent-reports/1/html?lang=pt-BR":            "Principais métricas",
		"/agent-reports/1/html?lang=en":               "Key metrics",
		"/agent-reports/1/html?lang=fr":               "Métricas clave",
		"/agent-reports/2/html?lang=es":               "Key metrics",
		"/agent-reports/2/html?lang=pt-BR&theme=dark": "Key metrics",
	}
	for url, want := range cases {
		rec := getHTML(t, h, url)
		require.Equal(t, http.StatusOK, rec.Code, url)
		assert.Contains(t, rec.Body.String(), want, url)
		assert.Equal(t, ContentSecurityPolicy, rec.Header().Get("Content-Security-Policy"))
	}

	rec := getHTML(t, h, "/agent-reports/1/html?lang=pt-BR&theme=light")
	assert.Contains(t, rec.Body.String(), `<html lang="pt-BR" class="light">`)
	rec = getHTML(t, h, "/agent-reports/2/html?theme=dark")
	assert.Contains(t, rec.Body.String(), `<html lang="en" class="dark">`)
	rec = getHTML(t, h, "/agent-reports/2/html?theme=blue")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}
