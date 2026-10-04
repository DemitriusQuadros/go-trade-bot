package internalapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"go-trade-bot/app/entities"
	"go-trade-bot/internal/settingsbridge"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type mockApplier struct{ mock.Mock }

func (m *mockApplier) Apply(ctx context.Context, next entities.Settings, confirmLive bool) (entities.Settings, error) {
	args := m.Called(ctx, next, confirmLive)
	return args.Get(0).(entities.Settings), args.Error(1)
}

func newApplyRequest(t *testing.T, secretHeader string) *http.Request {
	body, err := json.Marshal(settingsbridge.ApplyRequest{Settings: entities.Settings{Mode: "paper"}})
	assert.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/internal/settings/apply", bytes.NewReader(body))
	if secretHeader != "" {
		req.Header.Set(settingsbridge.InternalSecretHeader, secretHeader)
	}
	return req
}

// TestApply_SecretConfigured_RejectsMissingOrWrongSecret proves the
// defense-in-depth shared-secret check actually blocks requests, since this
// endpoint (Spec backend-05 post-review security fix) can change broker
// credentials/Mode and must never process an unauthenticated write.
func TestApply_SecretConfigured_RejectsMissingOrWrongSecret(t *testing.T) {
	applier := new(mockApplier)
	h := NewSettingsHandler(applier, "correct-secret")

	rec := httptest.NewRecorder()
	h.Apply(rec, newApplyRequest(t, ""))
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	rec2 := httptest.NewRecorder()
	h.Apply(rec2, newApplyRequest(t, "wrong-secret"))
	assert.Equal(t, http.StatusUnauthorized, rec2.Code)

	applier.AssertNotCalled(t, "Apply", mock.Anything, mock.Anything, mock.Anything)
}

func TestApply_SecretConfigured_AcceptsCorrectSecret(t *testing.T) {
	applier := new(mockApplier)
	applier.On("Apply", mock.Anything, mock.Anything, false).Return(entities.Settings{}, nil)
	h := NewSettingsHandler(applier, "correct-secret")

	rec := httptest.NewRecorder()
	h.Apply(rec, newApplyRequest(t, "correct-secret"))

	assert.Equal(t, http.StatusOK, rec.Code)
	applier.AssertExpectations(t)
}

// TestApply_NoSecretConfigured_AllowsAnyRequest documents the backward-
// compatible fallback (loopback bind is the only protection in this case) -
// existing config.yml files with no INTERNAL_BRIDGE_SECRET set must not
// break.
func TestApply_NoSecretConfigured_AllowsAnyRequest(t *testing.T) {
	applier := new(mockApplier)
	applier.On("Apply", mock.Anything, mock.Anything, false).Return(entities.Settings{}, nil)
	h := NewSettingsHandler(applier, "")

	rec := httptest.NewRecorder()
	h.Apply(rec, newApplyRequest(t, ""))

	assert.Equal(t, http.StatusOK, rec.Code)
	applier.AssertExpectations(t)
}
