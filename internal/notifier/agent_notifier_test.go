package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"go-trade-bot/app/entities"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sampleMessage() AgentMessage {
	return AgentMessage{
		AgentName:   "Risk Monitor",
		Severity:    "critical",
		Title:       "Drawdown <breach> & more",
		Message:     "Strategy 3 lost 12% <b>today</b>",
		Link:        "https://bot.example/agents/reports/12",
		StrategyIDs: []uint{3},
		Timestamp:   time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC),
	}
}

// AC#11: each kind produces the documented payload.
func TestFormatters_PayloadShapes(t *testing.T) {
	m := sampleMessage()

	t.Run("generic", func(t *testing.T) {
		url, body, ct, err := GenericFormatter{}.Format(entities.WebhookTarget{Kind: "generic", URL: "https://hook/x"}, m)
		require.NoError(t, err)
		assert.Equal(t, "https://hook/x", url)
		assert.Equal(t, "application/json", ct)
		var got map[string]any
		require.NoError(t, json.Unmarshal(body, &got))
		assert.Equal(t, "Risk Monitor", got["agent_name"])
		assert.Equal(t, "critical", got["severity"])
		assert.Equal(t, "Drawdown <breach> & more", got["title"])
		assert.Equal(t, "https://bot.example/agents/reports/12", got["link"])
		assert.Equal(t, []any{float64(3)}, got["strategy_ids"])
		assert.Equal(t, "2026-09-27T12:00:00Z", got["timestamp"])
	})

	t.Run("discord", func(t *testing.T) {
		url, body, _, err := DiscordFormatter{}.Format(entities.WebhookTarget{Kind: "discord", URL: "https://discord/api/webhooks/1/tok"}, m)
		require.NoError(t, err)
		assert.Equal(t, "https://discord/api/webhooks/1/tok", url)
		var got struct {
			Content string `json:"content"`
			Embeds  []struct {
				Title       string `json:"title"`
				Description string `json:"description"`
				Color       int    `json:"color"`
				URL         string `json:"url"`
			} `json:"embeds"`
			Components any `json:"components"`
		}
		require.NoError(t, json.Unmarshal(body, &got))
		assert.Contains(t, got.Content, "[CRITICAL]")
		require.Len(t, got.Embeds, 1)
		assert.Equal(t, m.Title, got.Embeds[0].Title)
		assert.Equal(t, m.Message, got.Embeds[0].Description)
		assert.Equal(t, discordColorCritical, got.Embeds[0].Color)
		assert.Equal(t, m.Link, got.Embeds[0].URL)
		assert.Nil(t, got.Components, "link-only: no action buttons")
	})

	t.Run("slack", func(t *testing.T) {
		_, body, _, err := SlackFormatter{}.Format(entities.WebhookTarget{Kind: "slack", URL: "https://hooks.slack/x"}, m)
		require.NoError(t, err)
		var got struct {
			Text   string `json:"text"`
			Blocks []struct {
				Type string `json:"type"`
				Text struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"text"`
			} `json:"blocks"`
		}
		require.NoError(t, json.Unmarshal(body, &got))
		assert.NotEmpty(t, got.Text)
		require.NotEmpty(t, got.Blocks)
		assert.Equal(t, "section", got.Blocks[0].Type)
		assert.Equal(t, "mrkdwn", got.Blocks[0].Text.Type)
		assert.Contains(t, got.Blocks[0].Text.Text, "&lt;breach&gt; &amp; more")
		assert.Contains(t, got.Blocks[0].Text.Text, "<https://bot.example/agents/reports/12|Open report>")
		assert.NotContains(t, string(body), `"button"`)
	})

	t.Run("telegram", func(t *testing.T) {
		url, body, _, err := TelegramFormatter{BaseURL: "https://api.telegram.org"}.Format(
			entities.WebhookTarget{Kind: "telegram", Secret: "123:SECRET", ChatID: "-100"}, m)
		require.NoError(t, err)
		assert.Equal(t, "https://api.telegram.org/bot123:SECRET/sendMessage", url)
		var got map[string]any
		require.NoError(t, json.Unmarshal(body, &got))
		assert.Equal(t, "-100", got["chat_id"])
		assert.Equal(t, "HTML", got["parse_mode"])
		assert.Equal(t, true, got["disable_web_page_preview"])
		text := got["text"].(string)
		assert.Contains(t, text, "Drawdown &lt;breach&gt; &amp; more")
		assert.Contains(t, text, "&lt;b&gt;today&lt;/b&gt;")
		assert.NotContains(t, text, "SECRET")
		assert.NotContains(t, string(body), "SECRET", "token only ever lives in the URL")
		assert.NotContains(t, string(body), "reply_markup", "link-only: no inline keyboard")
	})

	t.Run("missing config errors", func(t *testing.T) {
		_, _, _, err := GenericFormatter{}.Format(entities.WebhookTarget{Kind: "generic"}, m)
		assert.Error(t, err)
		_, _, _, err = TelegramFormatter{}.Format(entities.WebhookTarget{Kind: "telegram", ChatID: "1"}, m)
		assert.Error(t, err)
	})
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// AC#11: the telegram secret never appears in any log line, including
// delivery-failure logs.
func TestMultiTargetNotifier_NeverLogsSecrets(t *testing.T) {
	var logs syncBuffer
	prev := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(prev)

	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer failing.Close()

	n := NewMultiTargetNotifier()
	n.retryDelay = time.Millisecond
	n.formatters[entities.WebhookTelegram] = TelegramFormatter{BaseURL: failing.URL}

	targets := []entities.WebhookTarget{
		{ID: 1, Name: "tg", Kind: entities.WebhookTelegram, Secret: "999:TOPSECRETTOKEN", ChatID: "42", Enabled: true},
		{ID: 2, Name: "dc", Kind: entities.WebhookDiscord, URL: failing.URL + "/api/webhooks/1/DISCORDTOKEN", Enabled: true},
		{ID: 3, Name: "bad", Kind: entities.WebhookTelegram, Secret: "888:OTHERSECRET", Enabled: true}, // missing chat id
	}
	errs := n.SendToTargets(context.Background(), targets, sampleMessage())
	require.Len(t, errs, 1)
	assert.NotContains(t, errs[0].Error(), "OTHERSECRET")

	syncErr := n.SendSync(context.Background(), targets[0], sampleMessage())
	require.Error(t, syncErr)
	assert.NotContains(t, syncErr.Error(), "TOPSECRETTOKEN")

	require.Eventually(t, func() bool {
		return strings.Count(logs.String(), "delivery failed after retry") >= 2
	}, 3*time.Second, 10*time.Millisecond)
	out := logs.String()
	assert.NotContains(t, out, "TOPSECRETTOKEN")
	assert.NotContains(t, out, "DISCORDTOKEN")
	assert.NotContains(t, out, "OTHERSECRET")
}

func TestMultiTargetNotifier_DeliversAndSkipsDisabled(t *testing.T) {
	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(b))
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	n := NewMultiTargetNotifier()
	errs := n.SendToTargets(context.Background(), []entities.WebhookTarget{
		{ID: 1, Kind: entities.WebhookGeneric, URL: srv.URL, Enabled: true},
		{ID: 2, Kind: entities.WebhookGeneric, URL: srv.URL, Enabled: false},
	}, sampleMessage())
	assert.Empty(t, errs)
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(bodies) == 1
	}, 2*time.Second, 5*time.Millisecond)
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	assert.Len(t, bodies, 1, "disabled target must be skipped")
	mu.Unlock()

	require.NoError(t, n.SendSync(context.Background(), entities.WebhookTarget{Kind: entities.WebhookGeneric, URL: srv.URL}, sampleMessage()))
}
