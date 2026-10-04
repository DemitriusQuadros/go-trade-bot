package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"go-trade-bot/app/entities"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type MockSignalUseCase struct {
	mock.Mock
}

func (m *MockSignalUseCase) Close(ctx context.Context, id uint) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockSignalUseCase) GetAll(ctx context.Context) ([]entities.Signal, error) {
	args := m.Called(ctx)
	return args.Get(0).([]entities.Signal), args.Error(1)
}

func (m *MockSignalUseCase) GetAllOpen(ctx context.Context) ([]entities.Signal, error) {
	args := m.Called(ctx)
	return args.Get(0).([]entities.Signal), args.Error(1)
}

func (m *MockSignalUseCase) GetAllClosed(ctx context.Context) ([]entities.Signal, error) {
	args := m.Called(ctx)
	return args.Get(0).([]entities.Signal), args.Error(1)
}

func (m *MockSignalUseCase) GetByID(ctx context.Context, id uint) (entities.Signal, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(entities.Signal), args.Error(1)
}

func TestSignalHandler_GetAll_Unfiltered(t *testing.T) {
	mockUC := new(MockSignalUseCase)
	handler := NewSignalHandler(mockUC)

	expected := []entities.Signal{
		{ID: 1, Symbol: "BTCUSDT", Status: entities.Open},
		{ID: 2, Symbol: "ETHUSDT", Status: entities.Closed},
	}
	mockUC.On("GetAll", mock.Anything).Return(expected, nil)

	req := httptest.NewRequest(http.MethodGet, "/signal", nil)
	w := httptest.NewRecorder()

	handler.GetAll(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp []entities.Signal
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Len(t, resp, 2)
	mockUC.AssertExpectations(t)
}

func TestSignalHandler_GetAll_OpenFilter(t *testing.T) {
	mockUC := new(MockSignalUseCase)
	handler := NewSignalHandler(mockUC)

	expected := []entities.Signal{
		{ID: 1, Symbol: "BTCUSDT", Status: entities.Open},
	}
	mockUC.On("GetAllOpen", mock.Anything).Return(expected, nil)

	req := httptest.NewRequest(http.MethodGet, "/signal?status=open", nil)
	w := httptest.NewRecorder()

	handler.GetAll(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp []entities.Signal
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Len(t, resp, 1)
	assert.Equal(t, entities.Open, resp[0].Status)
	mockUC.AssertExpectations(t)
}

func TestSignalHandler_GetAll_ClosedFilter(t *testing.T) {
	mockUC := new(MockSignalUseCase)
	handler := NewSignalHandler(mockUC)

	expected := []entities.Signal{
		{ID: 2, Symbol: "ETHUSDT", Status: entities.Closed},
	}
	mockUC.On("GetAllClosed", mock.Anything).Return(expected, nil)

	req := httptest.NewRequest(http.MethodGet, "/signal?status=closed", nil)
	w := httptest.NewRecorder()

	handler.GetAll(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp []entities.Signal
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Len(t, resp, 1)
	assert.Equal(t, entities.Closed, resp[0].Status)
	mockUC.AssertExpectations(t)
}

func TestSignalHandler_GetAll_InvalidStatus(t *testing.T) {
	mockUC := new(MockSignalUseCase)
	handler := NewSignalHandler(mockUC)

	req := httptest.NewRequest(http.MethodGet, "/signal?status=bogus", nil)
	w := httptest.NewRecorder()

	handler.GetAll(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "invalid status filter")
}

func TestSignalHandler_GetAll_Error(t *testing.T) {
	mockUC := new(MockSignalUseCase)
	handler := NewSignalHandler(mockUC)

	mockUC.On("GetAll", mock.Anything).Return([]entities.Signal{}, errors.New("db error"))

	req := httptest.NewRequest(http.MethodGet, "/signal", nil)
	w := httptest.NewRecorder()

	handler.GetAll(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}
