package realtime_test

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	handler "go-trade-bot/app/handler/web/realtime"
	usecase "go-trade-bot/app/usecase/realtime"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockBroadcaster struct {
	subCh chan usecase.Event
}

func (m *mockBroadcaster) Subscribe() (<-chan usecase.Event, func()) {
	return m.subCh, func() {}
}

func TestRealtimeHandler_StreamDashboard(t *testing.T) {
	subCh := make(chan usecase.Event, 10)
	mb := &mockBroadcaster{subCh: subCh}
	h := handler.NewRealtimeHandler(mb)

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/stream/dashboard", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	// Push events
	subCh <- usecase.Event{
		Type: "price_update",
		Data: usecase.PriceUpdatePayload{
			Symbol:    "BTCUSDT",
			Price:     50000.0,
			Timestamp: time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC),
		},
	}
	subCh <- usecase.Event{
		Type: "heartbeat",
		Data: usecase.HeartbeatPayload{
			Timestamp: time.Date(2026, 9, 6, 12, 0, 15, 0, time.UTC),
		},
	}

	// Cancel after brief delay so handler terminates
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	h.StreamDashboard(rec, req)

	assert.Equal(t, "text/event-stream", rec.Header().Get("Content-Type"))
	assert.Equal(t, "no-cache", rec.Header().Get("Cache-Control"))
	assert.Equal(t, "keep-alive", rec.Header().Get("Connection"))

	body := rec.Body.String()
	assert.Contains(t, body, "retry: 5000")
	assert.Contains(t, body, "event: price_update")
	assert.Contains(t, body, `"symbol":"BTCUSDT"`)
	assert.Contains(t, body, "event: heartbeat")

	// Read lines to verify SSE format: event: ... \n data: ... \n\n
	scanner := bufio.NewScanner(strings.NewReader(body))
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	require.NotEmpty(t, lines)
	assert.Equal(t, "retry: 5000", lines[0])
}
