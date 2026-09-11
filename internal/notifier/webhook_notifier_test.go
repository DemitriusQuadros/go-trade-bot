package notifier_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"go-trade-bot/internal/configuration"
	"go-trade-bot/internal/notifier"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewWebhookNotifier_EmptyURLIsAcceptedAsNoOp(t *testing.T) {
	n, err := notifier.NewWebhookNotifier(&configuration.Configuration{})
	require.NoError(t, err)

	start := time.Now()
	err = n.Send(context.Background(), notifier.Event{Type: notifier.EventPositionOpened})
	assert.NoError(t, err)
	assert.Less(t, time.Since(start), 100*time.Millisecond)
}

func TestNewWebhookNotifier_MalformedURLFailsFast(t *testing.T) {
	_, err := notifier.NewWebhookNotifier(&configuration.Configuration{WebhookURL: "not-a-url"})
	assert.Error(t, err)
}

func TestSend_DeliversOnFirstAttempt(t *testing.T) {
	var calls int32
	done := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
		done <- struct{}{}
	}))
	defer server.Close()

	n, err := notifier.NewWebhookNotifier(&configuration.Configuration{WebhookURL: server.URL})
	require.NoError(t, err)

	err = n.Send(context.Background(), notifier.Event{Type: notifier.EventPositionOpened})
	assert.NoError(t, err)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for webhook delivery")
	}
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls))
}

// TestSend_RetriesOnceThenSucceeds verifies Spec 09 AC#2: a 500 on the first
// attempt followed by a 200 on the retry is considered delivered, with no
// further retries.
func TestSend_RetriesOnceThenSucceeds(t *testing.T) {
	var calls int32
	done := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		done <- struct{}{}
	}))
	defer server.Close()

	n, err := notifier.NewWebhookNotifier(&configuration.Configuration{WebhookURL: server.URL})
	require.NoError(t, err)

	err = n.Send(context.Background(), notifier.Event{Type: notifier.EventStrategyError})
	assert.NoError(t, err)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for retried webhook delivery")
	}
	// give the retry loop a moment to settle - no third attempt should occur.
	time.Sleep(200 * time.Millisecond)
	assert.Equal(t, int32(2), atomic.LoadInt32(&calls))
}

// TestSend_DoesNotBlockCallerWhenEndpointUnreachable verifies Spec 09 AC#3:
// Send returns immediately even though the retry delay (2s) plus the
// original attempt would otherwise take real wall-clock time.
func TestSend_DoesNotBlockCallerWhenEndpointUnreachable(t *testing.T) {
	n, err := notifier.NewWebhookNotifier(&configuration.Configuration{WebhookURL: "http://127.0.0.1:1"})
	require.NoError(t, err)

	start := time.Now()
	err = n.Send(context.Background(), notifier.Event{Type: notifier.EventStrategyError})
	assert.NoError(t, err)
	assert.Less(t, time.Since(start), 100*time.Millisecond)
}
