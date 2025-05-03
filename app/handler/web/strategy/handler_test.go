package handler_test

import (
	"bytes"
	"encoding/json"
	"go-trade-bot/app/entities"
	handler "go-trade-bot/app/handler/web/strategy"
	"go-trade-bot/app/handler/web/strategy/mocks"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"gorm.io/datatypes"
)

func TestStrategyHandler_Post(t *testing.T) {
	mockUseCase := new(mocks.UseCase)
	h := handler.NewStrategyHandler(mockUseCase)

	dto := handler.StrategyDto{
		Name:             "Test Strategy",
		Description:      "Test Description",
		MonitoredSymbols: []string{"BTCUSDT", "ETHUSDT"},
		Algorithm:        "grid",
		Cycle:            5,
		Configuration:    json.RawMessage(`{"param1":"value1","param2":"value2"}`),
	}

	body, err := json.Marshal(dto)
	assert.NoError(t, err)

	req, err := http.NewRequest(http.MethodPost, "/strategy", bytes.NewBuffer(body))
	assert.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	mockUseCase.On("Save", mock.Anything, dto.ToModel()).Return(nil)

	h.Post(rec, req)
	assert.Equal(t, http.StatusCreated, rec.Code)
	mockUseCase.AssertExpectations(t)
}

func TestStrategyHandler_Post_InvalidJSON(t *testing.T) {
	mockUseCase := new(mocks.UseCase)
	handler := handler.NewStrategyHandler(mockUseCase)

	req, err := http.NewRequest(http.MethodPost, "/strategy", bytes.NewBuffer([]byte("{invalid json}")))
	assert.NoError(t, err)

	rec := httptest.NewRecorder()

	handler.Post(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestStrategyHandler_Enqueue(t *testing.T) {
	mockUseCase := new(mocks.UseCase)
	h := handler.NewStrategyHandler(mockUseCase)

	req, err := http.NewRequest(http.MethodPost, "/strategy/enqueue", nil)
	assert.NoError(t, err)

	rec := httptest.NewRecorder()
	mockUseCase.On("Enqueue", mock.Anything).Return(nil)

	h.Enqueue(rec, req)
	assert.Equal(t, http.StatusAccepted, rec.Code)
	mockUseCase.AssertExpectations(t)
}
func TestStrategyHandler_Enqueue_Error(t *testing.T) {
	mockUseCase := new(mocks.UseCase)
	h := handler.NewStrategyHandler(mockUseCase)

	req, err := http.NewRequest(http.MethodPost, "/strategy/enqueue", nil)
	assert.NoError(t, err)

	rec := httptest.NewRecorder()
	mockUseCase.On("Enqueue", mock.Anything).Return(nil)

	h.Enqueue(rec, req)
	assert.Equal(t, http.StatusAccepted, rec.Code)
	mockUseCase.AssertExpectations(t)
}
func TestStrategyHandler_GetAll(t *testing.T) {
	mockUseCase := new(mocks.UseCase)
	h := handler.NewStrategyHandler(mockUseCase)

	strategies := []entities.Strategy{
		{
			Name:             "Test Strategy 1",
			Description:      "Test Description 1",
			MonitoredSymbols: []string{"BTCUSDT", "ETHUSDT"},
			Algorithm:        "grid",
			StrategyConfiguration: entities.StrategyConfiguration{
				Cycle:         entities.Cycle(5),
				Configuration: datatypes.JSON([]byte(`{"param1":"value1","param2":"value2"}`)),
			},
		},
	}

	req, err := http.NewRequest(http.MethodGet, "/strategy", nil)
	assert.NoError(t, err)

	rec := httptest.NewRecorder()
	mockUseCase.On("GetAll", mock.Anything).Return(strategies, nil)

	h.GetAll(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	var response []entities.Strategy
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, strategies, response)
	mockUseCase.AssertExpectations(t)
}
func TestStrategyHandler_GetAll_Error(t *testing.T) {
	mockUseCase := new(mocks.UseCase)
	h := handler.NewStrategyHandler(mockUseCase)

	req, err := http.NewRequest(http.MethodGet, "/strategy", nil)
	assert.NoError(t, err)

	rec := httptest.NewRecorder()
	mockUseCase.On("GetAll", mock.Anything).Return(nil, assert.AnError)

	h.GetAll(rec, req)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	mockUseCase.AssertExpectations(t)
}
func TestStrategyHandler_GetAll_EmptyResponse(t *testing.T) {
	mockUseCase := new(mocks.UseCase)
	h := handler.NewStrategyHandler(mockUseCase)

	req, err := http.NewRequest(http.MethodGet, "/strategy", nil)
	assert.NoError(t, err)

	rec := httptest.NewRecorder()
	mockUseCase.On("GetAll", mock.Anything).Return([]entities.Strategy{}, nil)

	h.GetAll(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	var response []entities.Strategy
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Empty(t, response)
	mockUseCase.AssertExpectations(t)
}
