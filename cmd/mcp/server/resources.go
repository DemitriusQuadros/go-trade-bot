package server

import (
	"context"

	agentusecase "go-trade-bot/app/usecase/agent"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerResources exposes docs://strategy-authoring (Backend Spec 05) to
// any external MCP client. It serves the exact same
// agentusecase.StrategyAuthoringDoc bytes AgentUseCase.RunToolLoop injects
// into its own system prompt - one source of truth, not two
// independently-drifting copies (Backend Spec 05 AC#4).
func registerResources(s *mcp.Server) {
	s.AddResource(&mcp.Resource{
		URI:         "docs://strategy-authoring",
		Name:        "strategy-authoring",
		Description: "Strategy/Context/Signal contract, the full ind.* indicator catalogue, and config conventions for authoring a Lua strategy script.",
		MIMEType:    "text/markdown",
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{
				{
					URI:      "docs://strategy-authoring",
					MIMEType: "text/markdown",
					Text:     agentusecase.StrategyAuthoringDoc,
				},
			},
		}, nil
	})
}
