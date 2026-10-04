package server

import (
	"context"
	"encoding/json"

	agentusecase "go-trade-bot/app/usecase/agent"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerTools maps every app/usecase/agent.Tool definition (Backend Spec
// 03/06's closed registry) onto the MCP SDK's low-level Server.AddTool API.
// This is the ONLY adapter layer between the two - no tool logic lives
// here, so the safety gate in app/usecase/agent/tools.go is unaffected by
// which MCP SDK (or transport) is in use.
func registerTools(s *mcp.Server, uc *agentusecase.AgentUseCase) {
	for _, tool := range uc.Tools() {
		tool := tool // capture for the closure below
		s.AddTool(&mcp.Tool{
			Name:        tool.Def.Name,
			Description: tool.Def.Description,
			InputSchema: json.RawMessage(tool.Def.InputSchema),
		}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			result, err := tool.Execute(ctx, req.Params.Arguments)
			if err != nil {
				return &mcp.CallToolResult{
					IsError: true,
					Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
				}, nil
			}
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: result}},
			}, nil
		})
	}
}
