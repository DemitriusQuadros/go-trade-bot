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
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
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
		ID:              1,
		Mode:            entities.AccountModeDryRun,
		Amount:          1000.0,
		AvailableOrders: 5,
		Currency:        "USDT",
		CreatedAt:       time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:       time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	mockUseCase.On("GetAccount").Return(r, nil)
	mockUseCase.On("GetAllAccounts").Return([]entities.Account{r}, nil)

	req, err := http.NewRequest(http.MethodGet, "/account", nil)
	assert.NoError(t, err)

	rec := httptest.NewRecorder()

	h.Get(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var response handler.AccountResponse
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, r.Amount, response.Amount)
	assert.Equal(t, r.Currency, response.Currency)
	assert.Contains(t, response.Accounts, "dryrun")
	mockUseCase.AssertExpectations(t)
}

func TestAccountHandler_PutDryRun(t *testing.T) {
	mockUseCase := new(mocks.UseCase)
	h := handler.NewAccountHandler(mockUseCase)

	expected := entities.Account{
		ID:              1,
		Mode:            entities.AccountModeDryRun,
		Amount:          5000.0,
		AvailableOrders: 3,
		Currency:        "USDT",
	}

	mockUseCase.On("UpdateDryRunCapital", float32(5000.0), int64(3), "USDT").Return(expected, nil)

	payload := []byte(`{"amount": 5000.0, "available_orders": 3, "currency": "USDT"}`)
	req := httptest.NewRequest(http.MethodPut, "/account/dryrun", bytes.NewBuffer(payload))
	rec := httptest.NewRecorder()

	h.PutDryRun(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var res entities.Account
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &res))
	assert.Equal(t, float32(5000.0), res.Amount)
	assert.Equal(t, int64(3), res.AvailableOrders)
	mockUseCase.AssertExpectations(t)
}

func TestAccountHandler_PostResetDryRun(t *testing.T) {
	mockUseCase := new(mocks.UseCase)
	h := handler.NewAccountHandler(mockUseCase)

	expected := entities.Account{
		ID:              1,
		Mode:            entities.AccountModeDryRun,
		Amount:          10000.0,
		AvailableOrders: 5,
		Currency:        "USDT",
	}

	mockUseCase.On("ResetDryRunCapital").Return(expected, nil)

	req := httptest.NewRequest(http.MethodPost, "/account/dryrun/reset", nil)
	rec := httptest.NewRecorder()

	h.PostResetDryRun(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var res entities.Account
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &res))
	assert.Equal(t, float32(10000.0), res.Amount)
	mockUseCase.AssertExpectations(t)
}

func TestAccountHandler_PutLive(t *testing.T) {
	mockUseCase := new(mocks.UseCase)
	h := handler.NewAccountHandler(mockUseCase)

	expected := entities.Account{
		ID:              2,
		Mode:            entities.AccountModeLive,
		MaxAllocation:   2000.0,
		AvailableOrders: 2,
	}

	mockUseCase.On("UpdateLiveAllocation", float32(2000.0), int64(2)).Return(expected, nil)

	payload := []byte(`{"max_allocation": 2000.0, "available_orders": 2}`)
	req := httptest.NewRequest(http.MethodPut, "/account/live", bytes.NewBuffer(payload))
	rec := httptest.NewRecorder()

	h.PutLive(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var res entities.Account
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &res))
	assert.Equal(t, float32(2000.0), res.MaxAllocation)
	mockUseCase.AssertExpectations(t)
}

func TestAccountHandler_PostSync(t *testing.T) {
	mockUseCase := new(mocks.UseCase)
	h := handler.NewAccountHandler(mockUseCase)

	now := time.Now()
	expected := entities.Account{
		ID:           2,
		Mode:         entities.AccountModeLive,
		Amount:       8500.0,
		LockedAmount: 1500.0,
		Currency:     "USDT",
		LastSyncedAt: &now,
	}

	mockUseCase.On("SyncExchangeBalance", mock.Anything, entities.AccountModeLive).Return(expected, nil)

	payload := []byte(`{"mode": "live"}`)
	req := httptest.NewRequest(http.MethodPost, "/account/sync", bytes.NewBuffer(payload))
	rec := httptest.NewRecorder()

	h.PostSync(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var res entities.Account
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &res))
	assert.Equal(t, float32(8500.0), res.Amount)
	assert.Equal(t, float32(1500.0), res.LockedAmount)
	mockUseCase.AssertExpectations(t)
}
