package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"time"

	"go-trade-bot/internal/configuration"
)

const retryDelay = 2 * time.Second

// WebhookNotifier implements NotificationSender as a single configurable
// HTTP POST target. Delivery is fire-and-forget, asynchronous, with at most
// one retry (Spec 09's "Delivery guarantees" judgment call) - webhook
// delivery is an observability aid, not a transactional requirement.
type WebhookNotifier struct {
	url    string
	client *http.Client
}

// NewWebhookNotifier builds a WebhookNotifier. An empty WebhookURL is
// accepted (Send becomes a safe no-op, Spec 09 AC#4); a non-empty but
// syntactically invalid URL is rejected here, at construction/startup time,
// matching Spec 01 AC#8's fail-fast pattern (Spec 09 AC#7).
func NewWebhookNotifier(cfg *configuration.Configuration) (*WebhookNotifier, error) {
	if cfg.WebhookURL == "" {
		return &WebhookNotifier{}, nil
	}
	u, err := url.Parse(cfg.WebhookURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("notifier: invalid WEBHOOK_URL %q", cfg.WebhookURL)
	}
	return &WebhookNotifier{
		url:    cfg.WebhookURL,
		client: &http.Client{Timeout: 5 * time.Second},
	}, nil
}

// Send never blocks the caller on the HTTP round-trip: it dispatches via a
// background goroutine (with panic recovery) so a slow/unreachable webhook
// endpoint never delays the trading loop (Spec 09 AC#3).
func (n *WebhookNotifier) Send(ctx context.Context, event Event) error {
	if n.url == "" {
		return nil // safe no-op, Spec 09 AC#4
	}

	body, err := json.Marshal(event)
	if err != nil {
		log.Printf("notifier: failed to marshal event %s: %v", event.Type, err)
		return nil
	}

	go n.deliver(event.Type, body)
	return nil
}

func (n *WebhookNotifier) deliver(eventType EventType, body []byte) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("notifier: recovered panic delivering webhook event %s: %v", eventType, r)
		}
	}()

	if n.post(body) {
		return
	}

	time.Sleep(retryDelay)
	if !n.post(body) {
		log.Printf("notifier: webhook delivery failed after retry for event %s", eventType)
	}
}

func (n *WebhookNotifier) post(body []byte) bool {
	req, err := http.NewRequest(http.MethodPost, n.url, bytes.NewReader(body))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode >= 200 && resp.StatusCode < 300
}
