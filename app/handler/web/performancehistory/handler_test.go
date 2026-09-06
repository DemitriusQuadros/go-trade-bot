package performancehistory_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"go-trade-bot/app/entities"
	handler "go-trade-bot/app/handler/web/performancehistory"
	"go-trade-bot/app/handler/web/performancehistory/mocks"
	usecase "go-trade-bot/app/usecase/performancehistory"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func newRouter(h *handler.Handler) *mux.Router {
	r := mux.NewRouter()
	for _, c := range h.Handlers() {
		r.HandleFunc(c.Pattern, c.Action).Methods(c.Method)
	}
	return r
}

func TestHandler_GetHistory_Success(t *testing.T) {
	uc := mocks.NewUseCase(t)
	uc.On("GetHistory", mock.Anything, uint(5), "BTCUSDT", entities.BucketDaily, 30).Return([]usecase.HistoryPoint{
		{Profit: 10, Trades: 2},
	}, nil)

	h := handler.NewHandler(uc)
	r := newRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/strategy/5/performance/history?symbol=BTCUSDT&bucket=daily", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp []usecase.HistoryPoint
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Len(t, resp, 1)
}

func TestHandler_GetHistory_MissingSymbol_400(t *testing.T) {
	uc := mocks.NewUseCase(t)
	h := handler.NewHandler(uc)
	r := newRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/strategy/5/performance/history?bucket=daily", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandler_GetHistory_InvalidBucket_400(t *testing.T) {
	uc := mocks.NewUseCase(t)
	h := handler.NewHandler(uc)
	r := newRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/strategy/5/performance/history?symbol=BTCUSDT&bucket=yearly", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandler_GetHistory_LimitOutOfRange_400(t *testing.T) {
	uc := mocks.NewUseCase(t)
	h := handler.NewHandler(uc)
	r := newRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/strategy/5/performance/history?symbol=BTCUSDT&bucket=daily&limit=400", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandler_GetHistory_StrategyNotFound_404(t *testing.T) {
	uc := mocks.NewUseCase(t)
	uc.On("GetHistory", mock.Anything, uint(999), "BTCUSDT", entities.BucketDaily, 30).
		Return(nil, usecase.ErrStrategyNotFound)

	h := handler.NewHandler(uc)
	r := newRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/strategy/999/performance/history?symbol=BTCUSDT&bucket=daily", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandler_GetHistory_CustomLimit_PassedThrough(t *testing.T) {
	uc := mocks.NewUseCase(t)
	uc.On("GetHistory", mock.Anything, uint(5), "BTCUSDT", entities.BucketWeekly, 10).Return([]usecase.HistoryPoint{}, nil)

	h := handler.NewHandler(uc)
	r := newRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/strategy/5/performance/history?symbol=BTCUSDT&bucket=weekly&limit=10", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}
