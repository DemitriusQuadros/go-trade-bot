package backtest_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"go-trade-bot/app/entities"
	handler "go-trade-bot/app/handler/web/backtest"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
)

// i18n-02 §3: ?lang= renders the backtest report with translated labels
// from the persisted metrics; EN and runs without metrics serve the file.
func TestGetReport_Lang(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "report-*.html")
	require.NoError(t, err)
	_, _ = f.WriteString("<html>stored EN report</html>")
	f.Close()

	run := entities.BacktestRun{
		ID: 5, Symbol: "ETHUSDT", Strategy: entities.Strategy{Name: "Breakout"},
		StartDate: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), EndDate: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
		HTMLReportPath: f.Name(),
		MetricsJSON:    datatypes.JSON(`{"sharpe_ratio":1.2,"total_trades":1,"profit_factor":2,"equity_curve":[]}`),
		TradeLogJSON:   datatypes.JSON(`[{"symbol":"ETHUSDT","profit":10}]`),
	}
	get := func(run entities.BacktestRun, url string) string {
		h := handler.NewBacktestHandler(&mockUseCase{runResult: run})
		r := mux.NewRouter()
		r.HandleFunc("/backtest/{id:[0-9]+}/report", h.GetReport).Methods(http.MethodGet)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
		require.Equal(t, http.StatusOK, rec.Code)
		return rec.Body.String()
	}

	pt := get(run, "/backtest/5/report?lang=pt-BR")
	assert.Contains(t, pt, `<html lang="pt-BR">`)
	assert.Contains(t, pt, "Relatório de backtest: Breakout")
	assert.Contains(t, pt, "Taxa de acerto")
	assert.Contains(t, pt, "Histórico de operações (1)")

	es := get(run, "/backtest/5/report?lang=es")
	assert.Contains(t, es, "Reporte de backtest: Breakout")

	assert.Equal(t, "<html>stored EN report</html>", get(run, "/backtest/5/report"))
	assert.Equal(t, "<html>stored EN report</html>", get(run, "/backtest/5/report?lang=en"))
	run.MetricsJSON = nil
	assert.Equal(t, "<html>stored EN report</html>", get(run, "/backtest/5/report?lang=es"), "old runs fall back to the file")
}

// A finished run whose stored report file is gone (reports are pruned per
// strategy) is rendered again from its persisted metrics instead of 404ing.
func TestGetReport_RegeneratesWhenStoredFileIsMissing(t *testing.T) {
	run := entities.BacktestRun{
		ID: 6, Symbol: "BTCUSDT", Strategy: entities.Strategy{Name: "Pruned"},
		StartDate: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), EndDate: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		HTMLReportPath: "/nonexistent/pruned.html",
		MetricsJSON:    datatypes.JSON(`{"sharpe_ratio":1.2,"total_trades":1,"profit_factor":2,"equity_curve":[]}`),
		TradeLogJSON:   datatypes.JSON(`[{"symbol":"BTCUSDT","profit":10}]`),
	}
	get := func(run entities.BacktestRun) *httptest.ResponseRecorder {
		h := handler.NewBacktestHandler(&mockUseCase{runResult: run})
		r := mux.NewRouter()
		r.HandleFunc("/backtest/{id:[0-9]+}/report", h.GetReport).Methods(http.MethodGet)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/backtest/6/report", nil))
		return rec
	}

	rec := get(run)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "Pruned")
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/html")

	run.MetricsJSON = nil
	assert.Equal(t, http.StatusNotFound, get(run).Code, "nothing to render from: still a 404")
}
