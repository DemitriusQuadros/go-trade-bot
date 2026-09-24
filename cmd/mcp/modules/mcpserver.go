package modules

import (
	agentusecase "go-trade-bot/app/usecase/agent"
	"go-trade-bot/cmd/mcp/server"
	"go-trade-bot/internal/configuration"

	"go.uber.org/fx"
)

// McpServerModule provides the MCP transport server (Backend Spec 04),
// wired against the SAME *agentusecase.AgentUseCase instance AgentModule
// builds - both the stdio and HTTP transports it can start wrap this one
// use case.
var McpServerModule = fx.Module("mcpserver",
	fx.Provide(
		func(uc *agentusecase.AgentUseCase, cfg *configuration.Configuration) *server.Server {
			return server.NewServer(uc, cfg)
		},
	),
)
