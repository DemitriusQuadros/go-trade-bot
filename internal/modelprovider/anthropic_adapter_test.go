package modelprovider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestAnthropicAdapter points the adapter at an httptest.Server instead
// of the real Anthropic API - AC#4/#5's translation and error-propagation
// behavior is testable without a live API key.
func newTestAnthropicAdapter(serverURL string) *AnthropicAdapter {
	return NewAnthropicAdapter("test-key", "", option.WithBaseURL(serverURL))
}

// TestAnthropicAdapter_Complete_ToolCallsTranslated is Acceptance
// Criterion #4: every tool_use block the vendor returns must show up in
// CompletionResult.ToolCalls with a non-empty Name and valid JSON Args -
// none silently dropped in translation.
func TestAnthropicAdapter_Complete_ToolCallsTranslated(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "msg_1",
			"type": "message",
			"role": "assistant",
			"model": "claude-sonnet-5",
			"content": [
				{"type": "text", "text": "Sure, let me check."},
				{"type": "tool_use", "id": "toolu_1", "name": "list_strategies", "input": {}},
				{"type": "tool_use", "id": "toolu_2", "name": "get_strategy", "input": {"strategy_id": 7}}
			],
			"stop_reason": "tool_use",
			"stop_sequence": null,
			"usage": {"input_tokens": 10, "output_tokens": 20}
		}`))
	}))
	defer server.Close()

	adapter := newTestAnthropicAdapter(server.URL)
	result, err := adapter.Complete(context.Background(), CompletionRequest{
		System:   "you are a trading strategy assistant",
		Messages: []Message{{Role: "user", Content: "list my strategies"}},
		Tools: []ToolDefinition{
			{Name: "list_strategies", Description: "list", InputSchema: json.RawMessage(`{"type":"object","properties":{}}`)},
			{Name: "get_strategy", Description: "get", InputSchema: json.RawMessage(`{"type":"object","properties":{"strategy_id":{"type":"integer"}},"required":["strategy_id"]}`)},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "tool_use", result.StopReason)
	assert.Equal(t, "Sure, let me check.", result.Text)
	require.Len(t, result.ToolCalls, 2)

	assert.Equal(t, "list_strategies", result.ToolCalls[0].Name)
	assert.NotEmpty(t, result.ToolCalls[0].ID)
	var empty map[string]any
	require.NoError(t, json.Unmarshal(result.ToolCalls[0].Args, &empty))

	assert.Equal(t, "get_strategy", result.ToolCalls[1].Name)
	var args struct {
		StrategyID int `json:"strategy_id"`
	}
	require.NoError(t, json.Unmarshal(result.ToolCalls[1].Args, &args))
	assert.Equal(t, 7, args.StrategyID)
}

// TestAnthropicAdapter_Complete_EndTurn covers the plain-text response path
// (no tool calls) mapping to StopReason "end_turn".
func TestAnthropicAdapter_Complete_EndTurn(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "msg_2",
			"type": "message",
			"role": "assistant",
			"model": "claude-sonnet-5",
			"content": [{"type": "text", "text": "All done."}],
			"stop_reason": "end_turn",
			"usage": {"input_tokens": 5, "output_tokens": 5}
		}`))
	}))
	defer server.Close()

	adapter := newTestAnthropicAdapter(server.URL)
	result, err := adapter.Complete(context.Background(), CompletionRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "end_turn", result.StopReason)
	assert.Equal(t, "All done.", result.Text)
	assert.Empty(t, result.ToolCalls)
}

// TestAnthropicAdapter_Complete_VendorError is Acceptance Criterion #5: a
// failing HTTP call (rate limit, auth failure, timeout) must return a
// non-nil error, never a zero-value CompletionResult with a nil error.
func TestAnthropicAdapter_Complete_VendorError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"rate_limit_error","message":"rate limited"}}`))
	}))
	defer server.Close()

	// Disable retries so the test doesn't wait through the SDK's default
	// backoff schedule against a deterministic 429.
	adapter := NewAnthropicAdapter("test-key", "", option.WithBaseURL(server.URL), option.WithMaxRetries(0))
	result, err := adapter.Complete(context.Background(), CompletionRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	require.Error(t, err)
	assert.Equal(t, CompletionResult{}, result)
}
