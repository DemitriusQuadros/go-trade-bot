package handler_test

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"go-trade-bot/app/entities"
	handler "go-trade-bot/app/handler/web/strategy"
	"go-trade-bot/app/handler/web/strategy/mocks"
	"go-trade-bot/internal/customerror"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// i18n-02 §5: the draft-only guard keeps its stable {"error":"forbidden"}
// code on PATCH status/mode; other errors stay 400 plain text.
func TestPatchStatusMode_ErrorCodes(t *testing.T) {
	uc := new(mocks.UseCase)
	uc.On("UpdateStatus", mock.Anything, uint(1), entities.StrategyStatus("productive")).
		Return(entities.Strategy{}, customerror.Forbidden("only admins can change non-backtest strategies"))
	uc.On("UpdateMode", mock.Anything, uint(1), "live").Return(entities.Strategy{}, errors.New("bad mode"))
	h := handler.NewStrategyHandler(uc)
	r := mux.NewRouter()
	r.HandleFunc("/strategy/{id:[0-9]+}/status", h.PatchStatus).Methods(http.MethodPatch)
	r.HandleFunc("/strategy/{id:[0-9]+}/mode", h.PatchMode).Methods(http.MethodPatch)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPatch, "/strategy/1/status", bytes.NewBufferString(`{"status":"productive"}`)))
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.JSONEq(t, `{"error":"forbidden","message":"only admins can change non-backtest strategies"}`, rec.Body.String())

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPatch, "/strategy/1/mode", bytes.NewBufferString(`{"mode":"live"}`)))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "bad mode\n", rec.Body.String())
}
