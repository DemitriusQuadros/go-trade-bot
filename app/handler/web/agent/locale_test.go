package agent_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"go-trade-bot/app/entities"
	"go-trade-bot/internal/authz"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// i18n-02 §2: the chat run's locale is the principal's locale, else the
// first supported Accept-Language match, else "" (the usecase then uses
// Settings.DefaultLocale).
func TestSendMessage_Locale(t *testing.T) {
	cases := []struct {
		name, userLocale, accept, want string
		principal                      bool
	}{
		{"user locale wins", "pt-BR", "es-AR,es", "pt-BR", true},
		{"accept-language when user has none", "", "fr-FR, es;q=0.8, en;q=0.5", "es", true},
		{"no principal uses accept-language", "", "pt-PT", "pt-BR", false},
		{"nothing -> empty (default locale)", "", "fr,de", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := &fakePersonas{agents: map[uint]entities.Agent{1: {ID: 1, Name: "Copilot"}}}
			router := personaRouter(p)
			req := httptest.NewRequest(http.MethodPost, "/agent/runs", bytes.NewBufferString(`{"input":"hola"}`))
			req.Header.Set("Accept-Language", c.accept)
			if c.principal {
				req = req.WithContext(authz.WithUser(req.Context(), authz.Principal{UserID: 4, Username: "ana", Locale: c.userLocale}))
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			assert.Equal(t, c.want, p.gotReq.Locale)
		})
	}
}
