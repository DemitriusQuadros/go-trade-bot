package modelprovider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"go-trade-bot/internal/configuration"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEstimateCostUSD_KnownModel(t *testing.T) {
	cost := EstimateCostUSD("anthropic", "claude-sonnet-5", Usage{InputTokens: 1_000_000, OutputTokens: 1_000_000})
	assert.InDelta(t, 18.0, cost, 1e-9)
}

func TestEstimateCostUSD_LongestPrefixWins(t *testing.T) {
	flashLite := EstimateCostUSD("gemini", "gemini-2.5-flash-lite", Usage{InputTokens: 1_000_000})
	flash := EstimateCostUSD("gemini", "gemini-2.5-flash", Usage{InputTokens: 1_000_000})
	assert.InDelta(t, 0.10, flashLite, 1e-9)
	assert.InDelta(t, 0.30, flash, 1e-9)
}

func TestEstimateCostUSD_UnknownModelNeverZero(t *testing.T) {
	cost := EstimateCostUSD("mystery", "some-new-model", Usage{InputTokens: 1000, OutputTokens: 500})
	assert.Greater(t, cost, 0.0)
}

func TestEstimateCostUSD_EmptyModelUsesProviderDefault(t *testing.T) {
	cost := EstimateCostUSD("anthropic", "", Usage{InputTokens: 1000, OutputTokens: 500})
	assert.InDelta(t, EstimateCostUSD("anthropic", defaultAnthropicModel, Usage{InputTokens: 1000, OutputTokens: 500}), cost, 1e-12)
	assert.Greater(t, cost, 0.0)
}

func TestConfigProviderFactory_CachesAndResolves(t *testing.T) {
	f := NewConfigProviderFactory(configuration.Agent{Provider: "anthropic", AnthropicKey: "k", AnthropicModel: "claude-sonnet-5"})

	p1, err := f.For("", "")
	require.NoError(t, err)
	p2, err := f.For("anthropic", "claude-sonnet-5")
	require.NoError(t, err)
	assert.Same(t, p1, p2, "same resolved (provider, model) must reuse the cached adapter")

	p3, err := f.For("anthropic", "claude-haiku-5")
	require.NoError(t, err)
	assert.NotSame(t, p1, p3)

	prov, model := f.Resolve("", "")
	assert.Equal(t, "anthropic", prov)
	assert.Equal(t, "claude-sonnet-5", model)
}

func TestConfigProviderFactory_MissingKeyIsUnconfigured(t *testing.T) {
	f := NewConfigProviderFactory(configuration.Agent{Provider: "anthropic"})
	p, err := f.For("gemini", "")
	require.NoError(t, err)
	_, cErr := p.Complete(context.Background(), CompletionRequest{})
	assert.Error(t, cErr)

	_, err = f.For("openai", "")
	assert.Error(t, err)
}

func TestAnthropicAdapter_Complete_PopulatesUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"m","type":"message","role":"assistant","model":"claude-sonnet-5",
			"content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","stop_sequence":null,
			"usage":{"input_tokens":1000,"output_tokens":500,"cache_read_input_tokens":10,"cache_creation_input_tokens":5}}`))
	}))
	defer server.Close()

	result, err := newTestAnthropicAdapter(server.URL).Complete(context.Background(), CompletionRequest{Messages: []Message{{Role: "user", Content: "x"}}})
	require.NoError(t, err)
	assert.Equal(t, Usage{InputTokens: 1015, OutputTokens: 500}, result.Usage)
}

func TestGeminiAdapter_Complete_PopulatesUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"role":"model","parts":[{"text":"hi"}]},"finishReason":"STOP"}],
			"usageMetadata":{"promptTokenCount":1000,"candidatesTokenCount":400,"thoughtsTokenCount":100}}`))
	}))
	defer server.Close()

	result, err := newTestGeminiAdapter(server.URL).Complete(context.Background(), CompletionRequest{Messages: []Message{{Role: "user", Content: "x"}}})
	require.NoError(t, err)
	assert.Equal(t, Usage{InputTokens: 1000, OutputTokens: 500}, result.Usage)
}

func TestUnconfiguredProvider_ZeroUsage(t *testing.T) {
	result, err := UnconfiguredProvider{Reason: "x"}.Complete(context.Background(), CompletionRequest{})
	assert.Error(t, err)
	assert.Equal(t, Usage{}, result.Usage)
}

func TestConfigProviderFactory_ResolveFallsBackToAdapterDefault(t *testing.T) {
	f := NewConfigProviderFactory(configuration.Agent{Provider: "anthropic"})
	if prov, model := f.Resolve("", ""); prov != "anthropic" || model != defaultAnthropicModel {
		t.Fatalf("Resolve(\"\", \"\") = %q, %q; want anthropic, %q", prov, model, defaultAnthropicModel)
	}
	if _, model := f.Resolve("gemini", ""); model != defaultGeminiModel {
		t.Fatalf("Resolve(gemini, \"\") model = %q; want %q", model, defaultGeminiModel)
	}
	if _, model := f.Resolve("anthropic", "claude-opus-5-5"); model != "claude-opus-5-5" {
		t.Fatalf("explicit model overridden: %q", model)
	}
}
