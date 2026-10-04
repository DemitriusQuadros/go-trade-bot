package notifier

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go-trade-bot/app/entities"
	"go-trade-bot/internal/configuration"
	"go-trade-bot/internal/i18n"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// i18n-02 test 1: every EN notifier word exists in ES and PT-BR.
func TestMessages_CatalogComplete(t *testing.T) {
	assert.Empty(t, Messages.MissingKeys())
	for _, loc := range i18n.Supported {
		for k := range Messages[loc] {
			_, ok := Messages[i18n.EN][k]
			assert.True(t, ok, "%s has key %q that EN lacks", loc, k)
		}
	}
}

// i18n-02 test 5: PT-BR words in every formatter.
func TestFormatters_PTBR(t *testing.T) {
	m := sampleMessage().localize(i18n.PTBR)

	_, body, _, err := DiscordFormatter{}.Format(entities.WebhookTarget{URL: "https://d/x"}, m)
	require.NoError(t, err)
	assert.Contains(t, string(body), "[CRÍTICO] Risk Monitor")
	assert.Contains(t, string(body), `"text":"Agente Risk Monitor"`)

	_, body, _, err = SlackFormatter{}.Format(entities.WebhookTarget{URL: "https://s/x"}, m)
	require.NoError(t, err)
	assert.Contains(t, string(body), "|Abrir relatório\\u003e")
	assert.Contains(t, string(body), "Agente Risk Monitor")
	assert.NotContains(t, string(body), "Open report")

	_, body, _, err = TelegramFormatter{}.Format(entities.WebhookTarget{Secret: "tok", ChatID: "1"}, m)
	require.NoError(t, err)
	var tg struct{ Text string }
	require.NoError(t, json.Unmarshal(body, &tg))
	assert.Contains(t, tg.Text, "<b>[CRÍTICO] ")
	assert.Contains(t, tg.Text, ">Abrir relatório</a>")
	assert.Contains(t, tg.Text, "<i>Agente Risk Monitor</i>")

	p := sampleMessage()
	p.Link = "https://bot/agents/proposals/4"
	_, body, _, _ = SlackFormatter{}.Format(entities.WebhookTarget{URL: "https://s/x"}, p.localize(i18n.ES))
	assert.Contains(t, string(body), "|Abrir propuesta\\u003e")
}


func captureTelegram(t *testing.T, src i18n.Source, m AgentMessage) string {
	t.Helper()
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var tg struct{ Text string }
		_ = json.Unmarshal(b, &tg)
		got = tg.Text
	}))
	defer srv.Close()
	n := NewMultiTargetNotifier()
	n.formatters[entities.WebhookTelegram] = TelegramFormatter{BaseURL: srv.URL}
	n.SetLocaleSource(src)
	require.NoError(t, n.SendSync(context.Background(), entities.WebhookTarget{Kind: entities.WebhookTelegram, Secret: "t", ChatID: "1", Enabled: true}, m))
	return got
}

// Catalog-backed Go text is rendered in DefaultLocale; model text as is;
// a settings error falls back to EN.
func TestMultiTargetNotifier_LocaleSource(t *testing.T) {
	m := AgentMessage{AgentName: "Risk", Severity: "critical", Timestamp: time.Unix(0, 0)}.
		WithText(T("budget.title"), T("budget.message", "Risk", 1.5, 1.0))
	assert.Equal(t, "Daily budget exhausted", m.Title, "EN rendering kept on the struct")

	pt := captureTelegram(t, i18n.Static(i18n.PTBR), m)
	assert.Contains(t, pt, "Orçamento diário esgotado")
	assert.Contains(t, pt, "O agente &#34;Risk&#34; gastou $1.50")

	broken := i18n.NewCachedSource(func(context.Context) (string, error) { return "", errors.New("db down") }, 0)
	en := captureTelegram(t, broken, m)
	assert.Contains(t, en, "[CRITICAL] Daily budget exhausted")

	model := AgentMessage{AgentName: "Risk", Severity: "info", Title: "Resumen", Message: "Texto del modelo"}
	es := captureTelegram(t, i18n.Static(i18n.ES), model)
	assert.Contains(t, es, "Texto del modelo")
	assert.Contains(t, es, "<i>Agente Risk</i>")
}

func TestText_NestedAndBool(t *testing.T) {
	txt := T("proposal.promotion.message", "A", 2, "Champ", T("proposal.promotion.early", Bool(true)))
	assert.Contains(t, txt.Render(i18n.EN), "(gate passed: yes, EARLY)")
	assert.Contains(t, txt.Render(i18n.PTBR), "(gate aprovado: sim, ANTECIPADA)")
}

func TestLocalizeEvent(t *testing.T) {
	e := Event{Type: EventPositionClosed, Symbol: "BTCUSDT", Message: "Position closed for BTCUSDT via simulated stop-loss (dryrun)"}
	assert.Equal(t, "Posición cerrada en BTCUSDT por el stop-loss simulado (dryrun)", LocalizeEvent(e, i18n.ES).Message)
	o := Event{Type: EventPositionOpened, Symbol: "ETHUSDT", Message: "Position opened for ETHUSDT"}
	assert.Equal(t, "Posição aberta em ETHUSDT", LocalizeEvent(o, i18n.PTBR).Message)
	errEv := Event{Type: EventStrategyError, Message: "boom"}
	assert.Equal(t, "boom", LocalizeEvent(errEv, i18n.PTBR).Message, "dynamic text unchanged")
	assert.Equal(t, o, LocalizeEvent(o, i18n.EN))
}

// The trade-event webhook is localized on the delivery goroutine and the
// locale source survives Swap.
func TestSwappableNotifier_LocalizesTradeEvents(t *testing.T) {
	got := make(chan Event, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var e Event
		_ = json.NewDecoder(r.Body).Decode(&e)
		got <- e
	}))
	defer srv.Close()
	s, err := NewSwappableNotifier(&configuration.Configuration{})
	require.NoError(t, err)
	s.SetLocaleSource(i18n.Static(i18n.PTBR))
	require.NoError(t, s.Swap(&configuration.Configuration{WebhookURL: srv.URL}))
	require.NoError(t, s.Send(context.Background(), Event{Type: EventPositionOpened, Symbol: "SOLUSDT", Message: "Position opened for SOLUSDT"}))
	select {
	case e := <-got:
		assert.Equal(t, "Posição aberta em SOLUSDT", e.Message)
		assert.Equal(t, EventPositionOpened, e.Type)
	case <-time.After(3 * time.Second):
		t.Fatal("webhook not delivered")
	}
}
