package modelprovider

import (
	"fmt"
	"sync"

	"go-trade-bot/internal/configuration"
)

// ProviderFactory builds (or returns a cached) ModelProvider for a given
// provider/model pair, so one process can serve agent personas configured
// on different providers/models (agents-platform A-01 §3). An empty
// provider means the process default (configuration.Agent.Provider); an
// empty model means that provider's configured/built-in default.
type ProviderFactory interface {
	For(provider, model string) (ModelProvider, error)
}

// ConfigProviderFactory is the default ProviderFactory, building adapters
// from configuration.Agent's keys and caching one adapter per resolved
// (provider, model). A provider whose API key is missing yields an
// UnconfiguredProvider (not an error) so the run records a clear error
// instead of the caller failing construction - matching how cmd/api wires
// its default provider.
type ConfigProviderFactory struct {
	cfg   configuration.Agent
	mu    sync.Mutex
	cache map[string]ModelProvider
}

// NewConfigProviderFactory builds a ConfigProviderFactory over cfg.
func NewConfigProviderFactory(cfg configuration.Agent) *ConfigProviderFactory {
	return &ConfigProviderFactory{cfg: cfg, cache: map[string]ModelProvider{}}
}

// Resolve returns the effective provider/model names For would use.
func (f *ConfigProviderFactory) Resolve(provider, model string) (string, string) {
	if provider == "" {
		provider = f.cfg.Provider
	}
	if model == "" {
		switch provider {
		case "anthropic":
			model = f.cfg.AnthropicModel
		case "gemini":
			model = f.cfg.GeminiModel
		}
	}
	return provider, model
}

func (f *ConfigProviderFactory) For(provider, model string) (ModelProvider, error) {
	provider, model = f.Resolve(provider, model)
	key := provider + "|" + model

	f.mu.Lock()
	defer f.mu.Unlock()
	if p, ok := f.cache[key]; ok {
		return p, nil
	}

	var p ModelProvider
	switch provider {
	case "anthropic":
		if f.cfg.AnthropicKey == "" {
			p = UnconfiguredProvider{Reason: "AI agent provider \"anthropic\" is not configured (AGENT.ANTHROPIC_KEY is empty)"}
		} else {
			p = NewAnthropicAdapter(f.cfg.AnthropicKey, model)
		}
	case "gemini":
		if f.cfg.GeminiKey == "" {
			p = UnconfiguredProvider{Reason: "AI agent provider \"gemini\" is not configured (AGENT.GEMINI_KEY is empty)"}
		} else {
			p = NewGeminiAdapter(f.cfg.GeminiKey, model)
		}
	default:
		return nil, fmt.Errorf("modelprovider: unknown provider %q", provider)
	}
	f.cache[key] = p
	return p, nil
}
