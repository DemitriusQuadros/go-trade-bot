package settings

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go-trade-bot/app/entities"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// i18n-02 §1: default_locale is returned by GET /settings (never empty) and
// written by PUT /settings.
func TestGetSettings_DefaultLocale(t *testing.T) {
	for stored, want := range map[string]string{"": "en", "es": "es", "pt-BR": "pt-BR"} {
		uc := new(mockUseCase)
		uc.On("Get", mock.Anything).Return(entities.Settings{DefaultLocale: stored}, nil)
		rec := httptest.NewRecorder()
		NewSettingsHandler(uc).GetSettings(rec, httptest.NewRequest(http.MethodGet, "/settings", nil))
		var resp map[string]any
		assert.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		assert.Equal(t, want, resp["default_locale"], stored)
	}
}

func TestPutSettings_DefaultLocale(t *testing.T) {
	cases := []struct {
		body, existing, want string
	}{
		{`{"mode":"dryrun","default_locale":"pt-BR"}`, "en", "pt-BR"},
		{`{"mode":"dryrun","default_locale":"es"}`, "", "es"},
		{`{"mode":"dryrun"}`, "es", "es"}, // omitted keeps the stored value
	}
	for _, c := range cases {
		uc := new(mockUseCase)
		uc.On("Get", mock.Anything).Return(entities.Settings{ID: 1, Mode: "dryrun", DefaultLocale: c.existing}, nil)
		uc.On("Apply", mock.Anything, mock.MatchedBy(func(s entities.Settings) bool { return s.DefaultLocale == c.want }), false).
			Return(entities.Settings{ID: 1, Mode: "dryrun", DefaultLocale: c.want}, nil)
		rec := httptest.NewRecorder()
		NewSettingsHandler(uc).PutSettings(rec, httptest.NewRequest(http.MethodPut, "/settings", strings.NewReader(c.body)))
		assert.Equal(t, http.StatusOK, rec.Code, c.body)
		assert.Contains(t, rec.Body.String(), `"default_locale":"`+c.want+`"`)
		uc.AssertExpectations(t)
	}
}

func TestPutSettings_InvalidDefaultLocale_400(t *testing.T) {
	uc := new(mockUseCase)
	rec := httptest.NewRecorder()
	NewSettingsHandler(uc).PutSettings(rec, httptest.NewRequest(http.MethodPut, "/settings", bytes.NewReader([]byte(`{"mode":"dryrun","default_locale":"fr"}`))))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.JSONEq(t, `{"error":"invalid_locale","message":"default_locale must be one of en, es, pt-BR"}`, rec.Body.String())
	uc.AssertNotCalled(t, "Apply", mock.Anything, mock.Anything, mock.Anything)
}
