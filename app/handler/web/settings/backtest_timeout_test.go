package settings

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go-trade-bot/app/entities"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// B-02: the asynchronous-backtest limit is edited on the Settings page.
func TestGetSettings_BacktestTimeoutIsEffectiveValue(t *testing.T) {
	for stored, want := range map[int]float64{0: 120, 240: 240, 1: 120, 99999: 120} {
		uc := new(mockUseCase)
		uc.On("Get", mock.Anything).Return(entities.Settings{BacktestTimeoutMinutes: stored}, nil)
		rec := httptest.NewRecorder()
		NewSettingsHandler(uc).GetSettings(rec, httptest.NewRequest(http.MethodGet, "/settings", nil))
		var resp map[string]any
		assert.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		assert.Equal(t, want, resp["backtest_timeout_minutes"], "stored=%d", stored)
	}
}

func TestPutSettings_BacktestTimeout(t *testing.T) {
	cases := []struct {
		body     string
		existing int
		want     int
	}{
		{`{"mode":"dryrun","backtest_timeout_minutes":300}`, 120, 300},
		{`{"mode":"dryrun","backtest_timeout_minutes":5}`, 0, 5},
		{`{"mode":"dryrun","backtest_timeout_minutes":1440}`, 0, 1440},
		{`{"mode":"dryrun"}`, 180, 180}, // omitted keeps the stored value
	}
	for _, c := range cases {
		uc := new(mockUseCase)
		uc.On("Get", mock.Anything).Return(entities.Settings{ID: 1, Mode: "dryrun", BacktestTimeoutMinutes: c.existing}, nil)
		uc.On("Apply", mock.Anything, mock.MatchedBy(func(s entities.Settings) bool { return s.BacktestTimeoutMinutes == c.want }), false).
			Return(entities.Settings{ID: 1, Mode: "dryrun", BacktestTimeoutMinutes: c.want}, nil)
		rec := httptest.NewRecorder()
		NewSettingsHandler(uc).PutSettings(rec, httptest.NewRequest(http.MethodPut, "/settings", strings.NewReader(c.body)))
		assert.Equal(t, http.StatusOK, rec.Code, c.body)
		uc.AssertExpectations(t)
	}
}

func TestPutSettings_InvalidBacktestTimeout_400(t *testing.T) {
	for _, v := range []string{"4", "1441", "-10"} {
		uc := new(mockUseCase)
		rec := httptest.NewRecorder()
		NewSettingsHandler(uc).PutSettings(rec, httptest.NewRequest(http.MethodPut, "/settings", strings.NewReader(`{"mode":"dryrun","backtest_timeout_minutes":`+v+`}`)))
		assert.Equal(t, http.StatusBadRequest, rec.Code, v)
		assert.Contains(t, rec.Body.String(), `"error":"invalid_backtest_timeout"`)
		uc.AssertNotCalled(t, "Apply", mock.Anything, mock.Anything, mock.Anything)
	}
}
