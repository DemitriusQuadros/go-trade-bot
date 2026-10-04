package customerror_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"go-trade-bot/internal/customerror"

	"github.com/stretchr/testify/assert"
)

func TestWriteHTTPError_CodedIsJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	customerror.WriteHTTPError(rec, customerror.Forbidden("only admins can change non-backtest strategies"))
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	assert.JSONEq(t, `{"error":"forbidden","message":"only admins can change non-backtest strategies"}`, rec.Body.String())
}

func TestWriteHTTPError_UncodedStaysPlainText(t *testing.T) {
	rec := httptest.NewRecorder()
	customerror.WriteHTTPError(rec, customerror.New(http.StatusBadRequest, "bad"))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "bad\n", rec.Body.String())
}

func TestWriteJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	customerror.WriteJSON(rec, customerror.New(http.StatusNotFound, "nope"))
	assert.JSONEq(t, `{"error":"not_found","message":"nope"}`, rec.Body.String())
	rec = httptest.NewRecorder()
	customerror.WriteJSON(rec, errors.New("boom"))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.JSONEq(t, `{"error":"internal_error","message":"boom"}`, rec.Body.String())
}
