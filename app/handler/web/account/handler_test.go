package handler_test

import (
	"bytes"
	"encoding/json"
	"go-trade-bot/app/entities"
	handler "go-trade-bot/app/handler/web/account"
	"go-trade-bot/app/handler/web/account/mocks"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestAccountHandler_Post(t *testing.T) {
	mockUseCase := new(mocks.UseCase)
	h := handler.NewAccountHandler(mockUseCase)

	dto := handler.AccountDto{
		Amount:          1000.0,
		AvailableOrders: 5,
		Currency:        "USDT",
	}

	body, err := json.Marshal(dto)
	assert.NoError(t, err)

	req, err := http.NewRequest(http.MethodPost, "/account", bytes.NewBuffer(body))
	assert.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	mockUseCase.On("CreateAccount", dto.ToModel()).Return(nil)

	h.Post(rec, req)
	assert.Equal(t, http.StatusCreated, rec.Code)
	mockUseCase.AssertExpectations(t)
}
func TestAccountHandler_Post_InvalidJSON(t *testing.T) {
	mockUseCase := new(mocks.UseCase)
	h := handler.NewAccountHandler(mockUseCase)

	req, err := http.NewRequest(http.MethodPost, "/account", bytes.NewBuffer([]byte("{invalid json}")))
	assert.NoError(t, err)

	rec := httptest.NewRecorder()

	h.Post(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestAccountHandler_Get(t *testing.T) {
	mockUseCase := new(mocks.UseCase)
	h := handler.NewAccountHandler(mockUseCase)

	r := entities.Account{
		Amount:          1000.0,
		AvailableOrders: 5,
		Currency:        "USDT",
		CreatedAt:       time.Date(1, time.January, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:       time.Date(1, time.January, 1, 0, 0, 0, 0, time.UTC),
	}

	mockUseCase.On("GetAccount").Return(r, nil)

	req, err := http.NewRequest(http.MethodGet, "/account", nil)
	assert.NoError(t, err)

	rec := httptest.NewRecorder()

	h.Get(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var response entities.Account
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, r, response)
	mockUseCase.AssertExpectations(t)
}
