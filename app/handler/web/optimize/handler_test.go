package optimize_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"go-trade-bot/app/entities"
	handler "go-trade-bot/app/handler/web/optimize"
	"go-trade-bot/app/handler/web/optimize/mocks"
	usecase "go-trade-bot/app/usecase/optimize"
	"go-trade-bot/internal/metrics_provider"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
)

func newRouter(h *handler.OptimizeHandler) *mux.Router {
	r := mux.NewRouter()
	for _, c := range h.Handlers() {
		r.HandleFunc(c.Pattern, c.Action).Methods(c.Method)
	}
	return r
}

// TestOptimizeHandler_Create_Accepted covers backend-01 AC#1: 202, with
// total_combinations and status in the body, and the job enqueued.
func TestOptimizeHandler_Create_Accepted(t *testing.T) {
	uc := mocks.NewUseCase(t)
	w := mocks.NewWorker(t)

	uc.On("Create", mock.Anything, mock.Anything).Return(entities.OptimizationRun{
		ID:                1,
		Status:            entities.OptimizationPending,
		TotalCombinations: 55,
	}, nil)
	w.On("EnqueueOptimizeTask", uint(1)).Return(nil)

	h := handler.NewOptimizeHandler(uc, w)
	body := `{"strategy_id":1,"symbol":"BTCUSDT","param_grid":{"rsi_period":{"min":10,"max":20,"step":1},"stop_loss_pct":{"min":1,"max":3,"step":0.5}}}`
	req := httptest.NewRequest(http.MethodPost, "/optimize", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	assert.Equal(t, http.StatusAccepted, rec.Code)
	var resp handler.CreateOptimizationResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, uint(1), resp.ID)
	assert.Equal(t, entities.OptimizationPending, resp.Status)
	assert.Equal(t, 55, resp.TotalCombinations)
}

func TestOptimizeHandler_Create_GridTooLarge_413(t *testing.T) {
	uc := mocks.NewUseCase(t)
	w := mocks.NewWorker(t)
	uc.On("Create", mock.Anything, mock.Anything).Return(entities.OptimizationRun{}, usecase.ErrGridTooLarge)

	h := handler.NewOptimizeHandler(uc, w)
	body := `{"strategy_id":1,"symbol":"BTCUSDT","param_grid":{"x":{"min":1,"max":1000,"step":1}}}`
	req := httptest.NewRequest(http.MethodPost, "/optimize", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	w.AssertNotCalled(t, "EnqueueOptimizeTask", mock.Anything)
}

func TestOptimizeHandler_Create_ZeroStep_400(t *testing.T) {
	uc := mocks.NewUseCase(t)
	w := mocks.NewWorker(t)
	uc.On("Create", mock.Anything, mock.Anything).Return(entities.OptimizationRun{}, usecase.ErrInvalidStep)

	h := handler.NewOptimizeHandler(uc, w)
	body := `{"strategy_id":1,"symbol":"BTCUSDT","param_grid":{"x":{"min":1,"max":10,"step":0}}}`
	req := httptest.NewRequest(http.MethodPost, "/optimize", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestOptimizeHandler_Create_InvalidJSON_400(t *testing.T) {
	uc := mocks.NewUseCase(t)
	w := mocks.NewWorker(t)
	h := handler.NewOptimizeHandler(uc, w)

	req := httptest.NewRequest(http.MethodPost, "/optimize", bytes.NewBufferString("not json"))
	rec := httptest.NewRecorder()

	h.Create(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestOptimizeHandler_GetByID_Running covers backend-01 AC#4: best_config /
// best_metrics remain null while status == "running".
func TestOptimizeHandler_GetByID_Running(t *testing.T) {
	uc := mocks.NewUseCase(t)
	w := mocks.NewWorker(t)
	uc.On("GetByID", mock.Anything, uint(5)).Return(entities.OptimizationRun{
		ID:                5,
		Status:            entities.OptimizationRunning,
		Progress:          30,
		TotalCombinations: 55,
	}, nil)

	h := handler.NewOptimizeHandler(uc, w)
	r := newRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/optimize/5", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp handler.StatusResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, entities.OptimizationRunning, resp.Status)
	assert.Equal(t, 30, resp.Progress)
	assert.Equal(t, 55, resp.TotalCombinations)
	assert.Nil(t, resp.BestConfig)
	assert.Nil(t, resp.BestMetrics)
}

func TestOptimizeHandler_GetByID_NotFound(t *testing.T) {
	uc := mocks.NewUseCase(t)
	w := mocks.NewWorker(t)
	uc.On("GetByID", mock.Anything, uint(999)).Return(entities.OptimizationRun{}, errors.New("not found"))

	h := handler.NewOptimizeHandler(uc, w)
	r := newRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/optimize/999", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// TestOptimizeHandler_GetResults_Completed covers backend-01 AC#5.
func TestOptimizeHandler_GetResults_Completed(t *testing.T) {
	uc := mocks.NewUseCase(t)
	w := mocks.NewWorker(t)

	gridJSON, _ := json.Marshal([]usecase.GridPoint{
		{Params: map[string]float64{"x": 1}, Metrics: &metrics_provider.BacktestMetrics{SharpeRatio: 1.5}},
	})

	uc.On("GetByID", mock.Anything, uint(3)).Return(entities.OptimizationRun{
		ID:              3,
		Status:          entities.OptimizationCompleted,
		BestConfigJSON:  datatypes.JSON(`{"x":1}`),
		BestMetricsJSON: datatypes.JSON(`{"sharpe_ratio":1.5}`),
		ResultsGridJSON: datatypes.JSON(gridJSON),
	}, nil)

	h := handler.NewOptimizeHandler(uc, w)
	r := newRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/optimize/3/results", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp handler.ResultsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Len(t, resp.Grid, 1)
}

// TestOptimizeHandler_GetResults_StillRunning_409 covers backend-01 AC#6:
// distinguishing "not ready yet" (409) from "doesn't exist" (404).
func TestOptimizeHandler_GetResults_StillRunning_409(t *testing.T) {
	uc := mocks.NewUseCase(t)
	w := mocks.NewWorker(t)
	uc.On("GetByID", mock.Anything, uint(4)).Return(entities.OptimizationRun{
		ID:     4,
		Status: entities.OptimizationRunning,
	}, nil)

	h := handler.NewOptimizeHandler(uc, w)
	r := newRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/optimize/4/results", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestOptimizeHandler_GetResults_NotFound_404(t *testing.T) {
	uc := mocks.NewUseCase(t)
	w := mocks.NewWorker(t)
	uc.On("GetByID", mock.Anything, uint(404)).Return(entities.OptimizationRun{}, errors.New("not found"))

	h := handler.NewOptimizeHandler(uc, w)
	r := newRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/optimize/404/results", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestOptimizeHandler_List_RequiresStrategyID(t *testing.T) {
	uc := mocks.NewUseCase(t)
	w := mocks.NewWorker(t)
	h := handler.NewOptimizeHandler(uc, w)

	req := httptest.NewRequest(http.MethodGet, "/optimize", nil)
	rec := httptest.NewRecorder()

	h.List(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestOptimizeHandler_List_Success(t *testing.T) {
	uc := mocks.NewUseCase(t)
	w := mocks.NewWorker(t)
	uc.On("ListByStrategy", mock.Anything, uint(1)).Return([]entities.OptimizationRun{
		{ID: 1, Status: entities.OptimizationCompleted},
		{ID: 2, Status: entities.OptimizationRunning},
	}, nil)

	h := handler.NewOptimizeHandler(uc, w)
	req := httptest.NewRequest(http.MethodGet, "/optimize?strategy_id=1", nil)
	rec := httptest.NewRecorder()

	h.List(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp []handler.ListItemResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Len(t, resp, 2)
}
