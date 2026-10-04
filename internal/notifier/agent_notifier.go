package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log"
	"net/http"
	"strings"
	"time"

	"go-trade-bot/app/entities"
	"go-trade-bot/internal/i18n"
)

// AgentMessage is one agent notification (agents-platform A-01 §6). It is
// link-only by design: formatters never emit action buttons, and approvals
// are never taken over a webhook.
type AgentMessage struct {
	AgentName   string    `json:"agent_name"`
	Severity    string    `json:"severity"` // info|warning|critical
	Title       string    `json:"title"`
	Message     string    `json:"message"`
	Link        string    `json:"link,omitempty"` // absolute deep link, optional
	StrategyIDs []uint    `json:"strategy_ids,omitempty"`
	Timestamp   time.Time `json:"timestamp"`

	// i18n-02 §4 (never serialized): Locale is the formatters' language,
	// set by the notifier from Settings.DefaultLocale at send time.
	// TitleText/MessageText, when set (see WithText), re-render Title/Message
	// in that locale; model-written text leaves them nil and is sent as is.
	Locale      i18n.Locale `json:"-"`
	TitleText   *Text       `json:"-"`
	MessageText *Text       `json:"-"`
}

// Formatter turns an AgentMessage into the HTTP request for one target.
type Formatter interface {
	Format(target entities.WebhookTarget, m AgentMessage) (url string, body []byte, contentType string, err error)
}

// AgentNotifier delivers an AgentMessage to a set of targets.
type AgentNotifier interface {
	SendToTargets(ctx context.Context, targets []entities.WebhookTarget, m AgentMessage) []error
}

// SyncSender delivers to ONE target synchronously and reports the outcome -
// used by POST /webhook-targets/{id}/test.
type SyncSender interface {
	SendSync(ctx context.Context, target entities.WebhookTarget, m AgentMessage) error
}

const (
	agentWebhookTimeout = 10 * time.Second
	// maxInFlightDeliveries bounds concurrently running delivery
	// goroutines; beyond it deliveries are dropped (and logged) rather than
	// queued, so a flood of notifications can never pile up goroutines.
	maxInFlightDeliveries = 32
)

// MultiTargetNotifier is the default AgentNotifier.
//
// Delivery semantics: SendToTargets validates + formats synchronously
// (returning formatting errors, e.g. a target missing its URL) and then
// delivers ASYNCHRONOUSLY on bounded goroutines with one retry after
// retryDelay - reusing WebhookNotifier's fire-and-forget approach - so the
// agent tool loop is never blocked on a slow webhook. Delivery failures are
// therefore logged, not returned.
//
// Secrets: target URLs (discord/slack webhook URLs embed their token) and
// the telegram bot token are never logged - log lines identify a target by
// id, name and kind only.
type MultiTargetNotifier struct {
	client     *http.Client
	formatters map[entities.WebhookTargetKind]Formatter
	sem        chan struct{}
	retryDelay time.Duration
	// telegramBaseURL is overridable for tests.
	telegramBaseURL string
	// locales is Settings.DefaultLocale (i18n-02 §4); nil = en.
	locales i18n.Source
}

// SetLocaleSource sets where the notification language comes from
// (Settings.DefaultLocale, cached). Call before the notifier is used.
func (n *MultiTargetNotifier) SetLocaleSource(src i18n.Source) { n.locales = src }

// NewMultiTargetNotifier builds a MultiTargetNotifier.
func NewMultiTargetNotifier() *MultiTargetNotifier {
	n := &MultiTargetNotifier{
		client:          &http.Client{Timeout: agentWebhookTimeout},
		sem:             make(chan struct{}, maxInFlightDeliveries),
		retryDelay:      retryDelay,
		telegramBaseURL: "https://api.telegram.org",
	}
	n.formatters = map[entities.WebhookTargetKind]Formatter{
		entities.WebhookGeneric:  GenericFormatter{},
		entities.WebhookDiscord:  DiscordFormatter{},
		entities.WebhookSlack:    SlackFormatter{},
		entities.WebhookTelegram: TelegramFormatter{BaseURL: n.telegramBaseURL},
	}
	return n
}

func targetLabel(t entities.WebhookTarget) string {
	return fmt.Sprintf("target #%d %q (%s)", t.ID, t.Name, t.Kind)
}

func (n *MultiTargetNotifier) format(t entities.WebhookTarget, m AgentMessage) (string, []byte, string, error) {
	f, ok := n.formatters[t.Kind]
	if !ok {
		return "", nil, "", fmt.Errorf("notifier: %s has unknown kind", targetLabel(t))
	}
	return f.Format(t, m)
}

// SendToTargets implements AgentNotifier (see the type doc for semantics).
func (n *MultiTargetNotifier) SendToTargets(ctx context.Context, targets []entities.WebhookTarget, m AgentMessage) []error {
	if m.Timestamp.IsZero() {
		m.Timestamp = time.Now().UTC()
	}
	m = m.localize(sourceLocale(ctx, n.locales))
	var errs []error
	for _, t := range targets {
		if !t.Enabled {
			continue
		}
		url, body, contentType, err := n.format(t, m)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		select {
		case n.sem <- struct{}{}:
			go func(t entities.WebhookTarget) {
				defer func() { <-n.sem }()
				defer func() {
					if r := recover(); r != nil {
						log.Printf("notifier: recovered panic delivering agent notification to %s", targetLabel(t))
					}
				}()
				if err := n.post(context.Background(), url, body, contentType); err == nil {
					return
				}
				time.Sleep(n.retryDelay)
				if err := n.post(context.Background(), url, body, contentType); err != nil {
					log.Printf("notifier: agent notification delivery failed after retry for %s: %v", targetLabel(t), err)
				}
			}(t)
		default:
			log.Printf("notifier: dropping agent notification to %s: too many deliveries in flight", targetLabel(t))
		}
	}
	return errs
}

// SendSync implements SyncSender: one attempt, synchronous, error returned.
func (n *MultiTargetNotifier) SendSync(ctx context.Context, t entities.WebhookTarget, m AgentMessage) error {
	if m.Timestamp.IsZero() {
		m.Timestamp = time.Now().UTC()
	}
	m = m.localize(sourceLocale(ctx, n.locales))
	url, body, contentType, err := n.format(t, m)
	if err != nil {
		return err
	}
	return n.post(ctx, url, body, contentType)
}

// post never includes the URL in its error text (it may carry a token).
func (n *MultiTargetNotifier) post(ctx context.Context, url string, body []byte, contentType string) error {
	ctx, cancel := context.WithTimeout(ctx, agentWebhookTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("could not build request")
	}
	req.Header.Set("Content-Type", contentType)
	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %s", redactURLError(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("endpoint responded %d", resp.StatusCode)
	}
	return nil
}

// redactURLError strips the request URL out of net/http's *url.Error text.
func redactURLError(err error) string {
	type unwrapper interface{ Unwrap() error }
	if u, ok := err.(unwrapper); ok && u.Unwrap() != nil {
		return u.Unwrap().Error()
	}
	return "network error"
}

// --- Formatters --------------------------------------------------------------

// severityTag is "[INFO]" / "[WARNING]" / "[CRITICAL]" in loc (unknown
// severities are upper-cased as is).
func severityTag(loc i18n.Locale, sev string) string {
	key := "severity." + strings.ToLower(sev)
	if _, ok := Messages[i18n.EN][key]; ok {
		return "[" + Messages.T(loc, key) + "]"
	}
	return "[" + strings.ToUpper(sev) + "]"
}

func agentFooter(loc i18n.Locale, name string) string {
	return Messages.F(loc, "footer.agent", name)
}

func requireURL(t entities.WebhookTarget) error {
	if strings.TrimSpace(t.URL) == "" {
		return fmt.Errorf("notifier: %s has no URL configured", targetLabel(t))
	}
	return nil
}

// GenericFormatter POSTs the AgentMessage itself as snake_case JSON.
type GenericFormatter struct{}

func (GenericFormatter) Format(t entities.WebhookTarget, m AgentMessage) (string, []byte, string, error) {
	if err := requireURL(t); err != nil {
		return "", nil, "", err
	}
	body, err := json.Marshal(m)
	return t.URL, body, "application/json", err
}

// Discord embed colours by severity.
const (
	discordColorInfo     = 0xC57A1C // Console Pro amber
	discordColorWarning  = 0xD9A31F
	discordColorCritical = 0xD64541
)

// DiscordFormatter produces {"content", "embeds":[{title, description, color, url}]}.
type DiscordFormatter struct{}

func (DiscordFormatter) Format(t entities.WebhookTarget, m AgentMessage) (string, []byte, string, error) {
	if err := requireURL(t); err != nil {
		return "", nil, "", err
	}
	color := discordColorInfo
	switch m.Severity {
	case "warning":
		color = discordColorWarning
	case "critical":
		color = discordColorCritical
	}
	embed := map[string]any{
		"title":       truncateRunes(m.Title, 256),
		"description": truncateRunes(m.Message, 4000),
		"color":       color,
		"footer":      map[string]string{"text": agentFooter(m.Locale, m.AgentName)},
		"timestamp":   m.Timestamp.UTC().Format(time.RFC3339),
	}
	if m.Link != "" {
		embed["url"] = m.Link
	}
	body, err := json.Marshal(map[string]any{
		"content": truncateRunes(fmt.Sprintf("%s %s: %s", severityTag(m.Locale, m.Severity), m.AgentName, m.Title), 2000),
		"embeds":  []any{embed},
		// Never ping @everyone/@here/roles from model-written text.
		"allowed_mentions": map[string]any{"parse": []string{}},
	})
	return t.URL, body, "application/json", err
}

// slackEscape escapes the three characters Slack mrkdwn treats specially.
func slackEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	return strings.ReplaceAll(s, ">", "&gt;")
}

// SlackFormatter produces {"text", "blocks":[section mrkdwn]} with a plain
// link (no interactive buttons).
type SlackFormatter struct{}

func (SlackFormatter) Format(t entities.WebhookTarget, m AgentMessage) (string, []byte, string, error) {
	if err := requireURL(t); err != nil {
		return "", nil, "", err
	}
	text := fmt.Sprintf("*%s %s*\n%s", severityTag(m.Locale, m.Severity), slackEscape(m.Title), slackEscape(m.Message))
	if m.Link != "" {
		text += fmt.Sprintf("\n<%s|%s>", slackEscape(m.Link), slackEscape(linkLabel(m.Locale, m.Link)))
	}
	blocks := []any{
		map[string]any{"type": "section", "text": map[string]string{"type": "mrkdwn", "text": truncateRunes(text, 3000)}},
		map[string]any{"type": "context", "elements": []any{map[string]string{"type": "mrkdwn", "text": slackEscape(agentFooter(m.Locale, m.AgentName))}}},
	}
	body, err := json.Marshal(map[string]any{
		"text":   fmt.Sprintf("%s %s: %s", severityTag(m.Locale, m.Severity), m.AgentName, m.Title),
		"blocks": blocks,
	})
	return t.URL, body, "application/json", err
}

// TelegramFormatter POSTs to <BaseURL>/bot<Secret>/sendMessage with an
// HTML-escaped message. The bot token lives only in the returned URL,
// which callers must never log.
type TelegramFormatter struct {
	BaseURL string
}

func (f TelegramFormatter) Format(t entities.WebhookTarget, m AgentMessage) (string, []byte, string, error) {
	if strings.TrimSpace(t.Secret) == "" || strings.TrimSpace(t.ChatID) == "" {
		return "", nil, "", fmt.Errorf("notifier: %s needs a bot token and chat id", targetLabel(t))
	}
	base := f.BaseURL
	if base == "" {
		base = "https://api.telegram.org"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "<b>%s %s</b>\n", html.EscapeString(severityTag(m.Locale, m.Severity)), html.EscapeString(truncateRunes(m.Title, 200)))
	// Truncate BEFORE escaping so the cut can never split an HTML entity or
	// tag (Telegram rejects malformed parse_mode=HTML text).
	b.WriteString(html.EscapeString(truncateRunes(m.Message, 3000)))
	if m.Link != "" {
		fmt.Fprintf(&b, "\n<a href=\"%s\">%s</a>", html.EscapeString(m.Link), html.EscapeString(linkLabel(m.Locale, m.Link)))
	}
	fmt.Fprintf(&b, "\n<i>%s</i>", html.EscapeString(agentFooter(m.Locale, m.AgentName)))
	body, err := json.Marshal(map[string]any{
		"chat_id":                  t.ChatID,
		"text":                     b.String(),
		"parse_mode":               "HTML",
		"disable_web_page_preview": true,
	})
	return base + "/bot" + t.Secret + "/sendMessage", body, "application/json", err
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}
