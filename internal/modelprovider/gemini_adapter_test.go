package modelprovider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

func newTestGeminiAdapter(serverURL string) *GeminiAdapter {
	return NewGeminiAdapter("test-key", "", withGeminiHTTPOptions(genai.HTTPOptions{BaseURL: serverURL}))
}

// TestGeminiAdapter_Complete_ToolCallsTranslated is Acceptance Criterion #4
// for the Gemini side: every functionCall the vendor returns must show up
// in CompletionResult.ToolCalls with a non-empty Name and valid JSON Args.
func TestGeminiAdapter_Complete_ToolCallsTranslated(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"candidates": [{
				"content": {
					"role": "model",
					"parts": [
						{"functionCall": {"name": "get_strategy", "args": {"strategy_id": 7}}}
					]
				},
				"finishReason": "STOP"
			}]
		}`))
	}))
	defer server.Close()

	adapter := newTestGeminiAdapter(server.URL)
	result, err := adapter.Complete(context.Background(), CompletionRequest{
		System:   "you are a trading strategy assistant",
		Messages: []Message{{Role: "user", Content: "look up strategy 7"}},
		Tools: []ToolDefinition{
			{Name: "get_strategy", Description: "get", InputSchema: json.RawMessage(`{"type":"object","properties":{"strategy_id":{"type":"integer"}},"required":["strategy_id"]}`)},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "tool_use", result.StopReason)
	require.Len(t, result.ToolCalls, 1)
	assert.Equal(t, "get_strategy", result.ToolCalls[0].Name)
	var args struct {
		StrategyID int `json:"strategy_id"`
	}
	require.NoError(t, json.Unmarshal(result.ToolCalls[0].Args, &args))
	assert.Equal(t, 7, args.StrategyID)
}

// TestGeminiAdapter_Complete_EndTurn covers the plain-text response path.
func TestGeminiAdapter_Complete_EndTurn(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"candidates": [{
				"content": {"role": "model", "parts": [{"text": "All done."}]},
				"finishReason": "STOP"
			}]
		}`))
	}))
	defer server.Close()

	adapter := newTestGeminiAdapter(server.URL)
	result, err := adapter.Complete(context.Background(), CompletionRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "end_turn", result.StopReason)
	assert.Equal(t, "All done.", result.Text)
	assert.Empty(t, result.ToolCalls)
}

// TestGeminiAdapter_Complete_VendorError is Acceptance Criterion #5 for the
// Gemini side: a failing HTTP call must return a non-nil error, never a
// zero-value CompletionResult with a nil error.
func TestGeminiAdapter_Complete_VendorError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error": {"code": 429, "message": "rate limited", "status": "RESOURCE_EXHAUSTED"}}`))
	}))
	defer server.Close()

	adapter := newTestGeminiAdapter(server.URL)
	result, err := adapter.Complete(context.Background(), CompletionRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	require.Error(t, err)
	assert.Equal(t, CompletionResult{}, result)
}
