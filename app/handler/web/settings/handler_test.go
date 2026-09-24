package settings

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"go-trade-bot/app/entities"
	usecase "go-trade-bot/app/usecase/settings"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type mockUseCase struct{ mock.Mock }

func (m *mockUseCase) Get(ctx context.Context) (entities.Settings, error) {
	args := m.Called(ctx)
	return args.Get(0).(entities.Settings), args.Error(1)
}

func (m *mockUseCase) Apply(ctx context.Context, next entities.Settings, confirmLive bool) (entities.Settings, error) {
	args := m.Called(ctx, next, confirmLive)
	return args.Get(0).(entities.Settings), args.Error(1)
}

func TestGetSettings_NeverLeaksRawSecrets(t *testing.T) {
	uc := new(mockUseCase)
	uc.On("Get", mock.Anything).Return(entities.Settings{BrokerApiKey: "supersecretkey1234"}, nil)

	h := NewSettingsHandler(uc)
	req := httptest.NewRequest(http.MethodGet, "/settings", nil)
	rec := httptest.NewRecorder()

	h.GetSettings(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.NotContains(t, rec.Body.String(), "supersecretkey1234")

	var resp SettingsResponseDTO
	assert.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "••••1234", resp.BrokerApiKey)
}

func TestPutSettings_Success_ReturnsMaskedSettingsAndApplied(t *testing.T) {
	uc := new(mockUseCase)
	existing := entities.Settings{ID: 1, Mode: "dryrun", BrokerApiKey: "oldkey"}
	uc.On("Get", mock.Anything).Return(existing, nil)

	applied := entities.Settings{ID: 1, Mode: "paper", BrokerApiKey: "oldkey", Testnet: true}
	uc.On("Apply", mock.Anything, mock.MatchedBy(func(s entities.Settings) bool {
		return s.Mode == "paper" && s.BrokerApiKey == "oldkey"
	}), false).Return(applied, nil)

	h := NewSettingsHandler(uc)
	body, _ := json.Marshal(PlatformSettingsUpdateRequestDTO{Mode: "paper", Testnet: true})
	req := httptest.NewRequest(http.MethodPut, "/settings", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.PutSettings(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp PlatformSettingsUpdateResponseDTO
	assert.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.True(t, resp.Applied)
	assert.Equal(t, "paper", resp.Settings.Mode)
	assert.NotContains(t, rec.Body.String(), "oldkey")
}

func TestPutSettings_ConfirmLiveRequired_Returns400(t *testing.T) {
	uc := new(mockUseCase)
	uc.On("Get", mock.Anything).Return(entities.Settings{ID: 1}, nil)
	uc.On("Apply", mock.Anything, mock.Anything, false).Return(entities.Settings{}, usecase.ErrConfirmLiveRequired)

	h := NewSettingsHandler(uc)
	body, _ := json.Marshal(PlatformSettingsUpdateRequestDTO{Mode: "live"})
	req := httptest.NewRequest(http.MethodPut, "/settings", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.PutSettings(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestPutSettings_DrainTimeout_Returns503(t *testing.T) {
	uc := new(mockUseCase)
	uc.On("Get", mock.Anything).Return(entities.Settings{ID: 1}, nil)
	uc.On("Apply", mock.Anything, mock.Anything, false).Return(entities.Settings{}, usecase.ErrDrainTimeout)

	h := NewSettingsHandler(uc)
	body, _ := json.Marshal(PlatformSettingsUpdateRequestDTO{BrokerApiKey: "new-key-value"})
	req := httptest.NewRequest(http.MethodPut, "/settings", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.PutSettings(rec, req)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	var body2 DrainTimeoutErrorBody
	assert.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body2))
	assert.Equal(t, "drain_timeout", body2.Error)
}

func TestPutSettings_SwapFailed_Returns502(t *testing.T) {
	uc := new(mockUseCase)
	uc.On("Get", mock.Anything).Return(entities.Settings{ID: 1}, nil)
	uc.On("Apply", mock.Anything, mock.Anything, false).Return(entities.Settings{}, usecase.ErrSwapFailed)

	h := NewSettingsHandler(uc)
	body, _ := json.Marshal(PlatformSettingsUpdateRequestDTO{BrokerApiKey: "new-key-value"})
	req := httptest.NewRequest(http.MethodPut, "/settings", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.PutSettings(rec, req)

	assert.Equal(t, http.StatusBadGateway, rec.Code)
}

func TestPutSettings_InvalidJSON_Returns400(t *testing.T) {
	uc := new(mockUseCase)
	h := NewSettingsHandler(uc)
	req := httptest.NewRequest(http.MethodPut, "/settings", bytes.NewReader([]byte("{not json")))
	rec := httptest.NewRecorder()

	h.PutSettings(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	uc.AssertNotCalled(t, "Apply", mock.Anything, mock.Anything, mock.Anything)
}
