package backtest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go-trade-bot/app/entities"
	handler "go-trade-bot/app/handler/web/backtest"
	usecase "go-trade-bot/app/usecase/backtest"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
)

type mockUseCase struct {
	runResult entities.BacktestRun
	runErr    error
	listRuns  []entities.BacktestRun
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
