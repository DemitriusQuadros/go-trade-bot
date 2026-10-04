package modules

import (
	"fmt"

	"go-trade-bot/internal/configuration"
	"go-trade-bot/internal/modelprovider"

	"go.uber.org/fx"
)

// ModelProviderModule is one of two places (alongside cmd/api/modules's
// AgentModule) that constructs a concrete modelprovider adapter (Backend
// Spec 01) - cmd/worker never imports this module or internal/modelprovider
// at all.
//
// cmd/mcp's own job - exposing app/usecase/agent's tools/resources to
// EXTERNAL MCP clients (Backend Spec 04) - never calls
// modelprovider.ModelProvider.Complete: cmd/mcp/server/tools.go's
// registerTools invokes each Tool.Execute directly, and Complete is only
// ever called from app/usecase/agent/usecase.go's RunToolLoop, which
// cmd/mcp does not invoke (that's cmd/api's chat-panel transport,
// Frontend Spec 01). An external MCP client brings its own model - that is
// the entire point of exposing these as MCP tools/resources rather than
// only as an in-platform chat agent. So construction here must NOT fail
// cmd/mcp's startup on missing/invalid AGENT.* config, mirroring
// cmd/api/modules/agent.go's identical reasoning for the identical
// underlying constraint: this binary must keep serving its actual job
// regardless of whether an AI provider is configured. A misconfigured
// provider only matters if something ever does drive RunToolLoop from
// inside cmd/mcp in the future - UnconfiguredProvider still fails loudly
// *then*, just not at process startup.
var ModelProviderModule = fx.Module("modelprovider",
	fx.Provide(func(cfg *configuration.Configuration) modelprovider.ModelProvider {
		switch cfg.Agent.Provider {
		case "anthropic":
			if cfg.Agent.AnthropicKey == "" {
				return modelprovider.UnconfiguredProvider{Reason: "AI agent is not configured on this MCP server (AGENT.PROVIDER=anthropic but AGENT.ANTHROPIC_KEY is empty) - this only affects driving RunToolLoop from cmd/mcp directly, not tool/resource calls from an external MCP client"}
			}
			return modelprovider.NewAnthropicAdapter(cfg.Agent.AnthropicKey, cfg.Agent.AnthropicModel)
		case "gemini":
			if cfg.Agent.GeminiKey == "" {
				return modelprovider.UnconfiguredProvider{Reason: "AI agent is not configured on this MCP server (AGENT.PROVIDER=gemini but AGENT.GEMINI_KEY is empty) - this only affects driving RunToolLoop from cmd/mcp directly, not tool/resource calls from an external MCP client"}
			}
			return modelprovider.NewGeminiAdapter(cfg.Agent.GeminiKey, cfg.Agent.GeminiModel)
		default:
			return modelprovider.UnconfiguredProvider{Reason: fmt.Sprintf("AI agent is not configured on this MCP server (unrecognized or unset AGENT.PROVIDER %q) - this only affects driving RunToolLoop from cmd/mcp directly, not tool/resource calls from an external MCP client", cfg.Agent.Provider)}
		}
	}),
)
