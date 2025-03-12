package handler_test

import (
	"bytes"
	"encoding/json"
	handler "go-trade-bot/app/handler/web/strategy"
	"go-trade-bot/app/handler/web/strategy/mocks"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestStrategyHandler_Post(t *testing.T) {
	mockUseCase := new(mocks.UseCase)
	h := handler.NewStrategyHandler(mockUseCase)

	dto := handler.StrategyDto{
		Name: "Test Strategy",
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
