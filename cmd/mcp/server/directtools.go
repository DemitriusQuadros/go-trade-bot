package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"go-trade-bot/app/handler/mcptools"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// maxResultBytes caps one tool result so a runaway listing can't flood the
// client's context.
const maxResultBytes = 256 * 1024

// registerDirectTools adds the in-process adapter's tools. A name already
// used by an agent tool is skipped (and logged) rather than replacing it.
func registerDirectTools(s *mcp.Server, tools []mcptools.Tool, taken map[string]bool) {
	for _, tool := range tools {
		tool := tool
		if taken[tool.Name] {
			log.Printf("mcp: tool %q skipped: name already used by an agent tool", tool.Name)
			continue
		}
		taken[tool.Name] = true
		s.AddTool(&mcp.Tool{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: tool.InputSchema,
		}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			out, err := runDirect(ctx, tool, req.Params.Arguments)
			if err != nil {
				return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}, nil
			}
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: out}}}, nil
		})
	}
}

func runDirect(ctx context.Context, tool mcptools.Tool, args json.RawMessage) (string, error) {
	v, err := tool.Run(ctx, args)
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("%s: encoding result: %w", tool.Name, err)
	}
	if len(b) > maxResultBytes {
		return "", fmt.Errorf("%s: result is %d bytes (limit %d); narrow the request (filters, limit)", tool.Name, len(b), maxResultBytes)
	}
	return string(b), nil
}
