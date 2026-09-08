package backtest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"go-trade-bot/app/engine"
	"go-trade-bot/app/entities"
	handler "go-trade-bot/app/handler/web/backtest"
	usecase "go-trade-bot/app/usecase/backtest"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
)

type mockUseCase struct {
	runResult        entities.BacktestRun
	runErr           error
	listRuns         []entities.BacktestRun
	monteCarloResult engine.MonteCarloResult
	monteCarloErr    error
}

func (m *mockUseCase) RunMonteCarlo(ctx context.Context, runID uint, iterations int) (engine.MonteCarloResult, error) {
	return m.monteCarloResult, m.monteCarloErr
}

func (m *mockUseCase) GetMonteCarlo(ctx context.Context, runID uint) (engine.MonteCarloResult, error) {
	return m.monteCarloResult, m.monteCarloErr
}

func (m *mockUseCase) Run(ctx context.Context, req usecase.RunRequest) (entities.BacktestRun, error) {
	return m.runResult, m.runErr
}

func (m *mockUseCase) RunWalkForward(ctx context.Context, req usecase.WalkForwardRequest) (entities.BacktestRun, error) {
	return m.runResult, m.runErr
}

func (m *mockUseCase) GetByID(ctx context.Context, id uint) (entities.BacktestRun, error) {
	return m.runResult, m.runErr
}

func (m *mockUseCase) ListByStrategy(ctx context.Context, strategyID uint) ([]entities.BacktestRun, error) {
	return m.listRuns, m.runErr
}

func TestBacktestHandler_RunBacktest(t *testing.T) {
	mockUC := &mockUseCase{
		runResult: entities.BacktestRun{
			ID:             1,
			StrategyID:     1,
			Symbol:         "BTCUSDT",
			Sharpe:         1.2,
			MaxDrawdownPct: 15.0,
			ProfitFactor:   2.5,
			Passed:         true,
			HTMLReportPath: "/tmp/report.html",
			TradeLogJSON:   datatypes.JSON(`[{"symbol":"BTCUSDT","profit":10}]`),
		},
	}
	h := handler.NewBacktestHandler(mockUC)

	reqBody := `{"strategy_id":1,"symbol":"BTCUSDT","timeframe":"1m","initial_capital":1000}`
	req := httptest.NewRequest(http.MethodPost, "/backtest", bytes.NewBufferString(reqBody))
	rec := httptest.NewRecorder()

	h.RunBacktest(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp handler.BacktestRunResponse
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, uint(1), resp.ID)
	assert.True(t, resp.Passed)
	assert.Equal(t, 2.5, resp.ProfitFactor)
	assert.Equal(t, "/tmp/report.html", resp.HTMLReportPath)
	assert.NotNil(t, resp.TradeLog)
}

func TestBacktestHandler_InfinityProfitFactor_AC5(t *testing.T) {
	mockUC := &mockUseCase{
		runResult: entities.BacktestRun{
			ID:           2,
			StrategyID:   1,
			Symbol:       "BTCUSDT",
			ProfitFactor: math.MaxFloat64,
			Passed:       true,
		},
	}
	h := handler.NewBacktestHandler(mockUC)

	r := mux.NewRouter()
	r.HandleFunc("/backtest/{id:[0-9]+}", h.GetByID).Methods(http.MethodGet)

	req := httptest.NewRequest(http.MethodGet, "/backtest/2", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var rawMap map[string]any
	err := json.Unmarshal(rec.Body.Bytes(), &rawMap)
	require.NoError(t, err)
	assert.Equal(t, "Infinity", rawMap["profit_factor"], "ProfitFactor must serialize as 'Infinity' string in JSON response (AC#5)")
}

func TestBacktestHandler_List(t *testing.T) {
	mockUC := &mockUseCase{
		listRuns: []entities.BacktestRun{
			{ID: 1, StrategyID: 1, Symbol: "BTCUSDT", CreatedAt: time.Now()},
			{ID: 2, StrategyID: 1, Symbol: "BTCUSDT", CreatedAt: time.Now()},
		},
	}
	h := handler.NewBacktestHandler(mockUC)

	req := httptest.NewRequest(http.MethodGet, "/backtest?strategy_id=1", nil)
	rec := httptest.NewRecorder()

	h.List(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp []handler.BacktestRunResponse
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Len(t, resp, 2)
}

func TestBacktestHandler_RunMonteCarlo_Success(t *testing.T) {
	mockUC := &mockUseCase{
		monteCarloResult: engine.MonteCarloResult{Iterations: 1000},
	}
	h := handler.NewBacktestHandler(mockUC)
	r := mux.NewRouter()
	r.HandleFunc("/backtest/{id:[0-9]+}/montecarlo", h.RunMonteCarlo).Methods(http.MethodPost)

	req := httptest.NewRequest(http.MethodPost, "/backtest/1/montecarlo", bytes.NewBufferString(`{"iterations":1000}`))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp engine.MonteCarloResult
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 1000, resp.Iterations)
}

func TestBacktestHandler_RunMonteCarlo_NoBody_UsesDefault(t *testing.T) {
	mockUC := &mockUseCase{
		monteCarloResult: engine.MonteCarloResult{Iterations: 1000},
	}
	h := handler.NewBacktestHandler(mockUC)
	r := mux.NewRouter()
	r.HandleFunc("/backtest/{id:[0-9]+}/montecarlo", h.RunMonteCarlo).Methods(http.MethodPost)

	req := httptest.NewRequest(http.MethodPost, "/backtest/1/montecarlo", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestBacktestHandler_RunMonteCarlo_InvalidIterations_400(t *testing.T) {
	mockUC := &mockUseCase{
		monteCarloErr: usecase.ErrInvalidMonteCarloIterations,
	}
	h := handler.NewBacktestHandler(mockUC)
	r := mux.NewRouter()
	r.HandleFunc("/backtest/{id:[0-9]+}/montecarlo", h.RunMonteCarlo).Methods(http.MethodPost)

	req := httptest.NewRequest(http.MethodPost, "/backtest/1/montecarlo", bytes.NewBufferString(`{"iterations":50000}`))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestBacktestHandler_RunMonteCarlo_InsufficientTrades_422(t *testing.T) {
	mockUC := &mockUseCase{
		monteCarloErr: usecase.ErrInsufficientTradesForMonteCarlo,
	}
	h := handler.NewBacktestHandler(mockUC)
	r := mux.NewRouter()
	r.HandleFunc("/backtest/{id:[0-9]+}/montecarlo", h.RunMonteCarlo).Methods(http.MethodPost)

	req := httptest.NewRequest(http.MethodPost, "/backtest/1/montecarlo", bytes.NewBufferString(`{}`))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestBacktestHandler_RunMonteCarlo_RunNotFound_404(t *testing.T) {
	mockUC := &mockUseCase{
		monteCarloErr: usecase.ErrBacktestRunNotFound,
	}
	h := handler.NewBacktestHandler(mockUC)
	r := mux.NewRouter()
	r.HandleFunc("/backtest/{id:[0-9]+}/montecarlo", h.RunMonteCarlo).Methods(http.MethodPost)

	req := httptest.NewRequest(http.MethodPost, "/backtest/999/montecarlo", bytes.NewBufferString(`{}`))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestBacktestHandler_GetMonteCarlo_Success(t *testing.T) {
	mockUC := &mockUseCase{
		monteCarloResult: engine.MonteCarloResult{Iterations: 500},
	}
	h := handler.NewBacktestHandler(mockUC)
	r := mux.NewRouter()
	r.HandleFunc("/backtest/{id:[0-9]+}/montecarlo", h.GetMonteCarlo).Methods(http.MethodGet)

	req := httptest.NewRequest(http.MethodGet, "/backtest/1/montecarlo", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp engine.MonteCarloResult
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 500, resp.Iterations)
}

// TestBacktestHandler_GetMonteCarlo_NotComputed_404 covers backend-03's
// contract: GET before any POST returns 404, distinct from a successful
// cached read.
func TestBacktestHandler_GetMonteCarlo_NotComputed_404(t *testing.T) {
	mockUC := &mockUseCase{
		monteCarloErr: usecase.ErrMonteCarloNotYetComputed,
	}
	h := handler.NewBacktestHandler(mockUC)
	r := mux.NewRouter()
	r.HandleFunc("/backtest/{id:[0-9]+}/montecarlo", h.GetMonteCarlo).Methods(http.MethodGet)

	req := httptest.NewRequest(http.MethodGet, "/backtest/1/montecarlo", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestBacktestHandler_GetReport_Success(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "report-*.html")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	expectedContent := "<html><body><h1>Report</h1></body></html>"
	_, err = tmpFile.WriteString(expectedContent)
	require.NoError(t, err)
	tmpFile.Close()

	mockUC := &mockUseCase{
		runResult: entities.BacktestRun{
			ID:             1,
			HTMLReportPath: tmpFile.Name(),
		},
	}
	h := handler.NewBacktestHandler(mockUC)
	r := mux.NewRouter()
	r.HandleFunc("/backtest/{id:[0-9]+}/report", h.GetReport).Methods(http.MethodGet)

	req := httptest.NewRequest(http.MethodGet, "/backtest/1/report", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "text/html; charset=utf-8", rec.Header().Get("Content-Type"))
	assert.Equal(t, expectedContent, rec.Body.String())
}

func TestBacktestHandler_GetReport_EmptyPath_404(t *testing.T) {
	mockUC := &mockUseCase{
		runResult: entities.BacktestRun{
			ID:             1,
			HTMLReportPath: "",
		},
	}
	h := handler.NewBacktestHandler(mockUC)
	r := mux.NewRouter()
	r.HandleFunc("/backtest/{id:[0-9]+}/report", h.GetReport).Methods(http.MethodGet)

	req := httptest.NewRequest(http.MethodGet, "/backtest/1/report", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Body.String(), "report not found")
}

func TestBacktestHandler_GetReport_MissingFileOnDisk_404(t *testing.T) {
	mockUC := &mockUseCase{
		runResult: entities.BacktestRun{
			ID:             1,
			HTMLReportPath: "/nonexistent/path/report.html",
		},
	}
	h := handler.NewBacktestHandler(mockUC)
	r := mux.NewRouter()
	r.HandleFunc("/backtest/{id:[0-9]+}/report", h.GetReport).Methods(http.MethodGet)

	req := httptest.NewRequest(http.MethodGet, "/backtest/1/report", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
}

func TestBacktestHandler_GetByID_WithEquityCurve(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	metricsJSON := `{"sharpe_ratio":1.5,"max_drawdown_pct":10.0,"equity_curve":[{"time":"` + now.Format(time.RFC3339) + `","value":1000.0},{"time":"` + now.Add(time.Hour).Format(time.RFC3339) + `","value":1050.0}]}`

	mockUC := &mockUseCase{
		runResult: entities.BacktestRun{
			ID:          1,
			MetricsJSON: datatypes.JSON(metricsJSON),
		},
	}
	h := handler.NewBacktestHandler(mockUC)
	r := mux.NewRouter()
	r.HandleFunc("/backtest/{id:[0-9]+}", h.GetByID).Methods(http.MethodGet)

	req := httptest.NewRequest(http.MethodGet, "/backtest/1", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp handler.BacktestRunResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Len(t, resp.EquityCurve, 2)
	assert.Equal(t, 1000.0, resp.EquityCurve[0].Value)
	assert.Equal(t, 1050.0, resp.EquityCurve[1].Value)
}

