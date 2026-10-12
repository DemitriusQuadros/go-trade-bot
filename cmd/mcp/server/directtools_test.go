package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"go-trade-bot/app/handler/mcptools"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func connect(t *testing.T, tools []mcptools.Tool, taken map[string]bool) *mcp.ClientSession {
	t.Helper()
	s := mcp.NewServer(&mcp.Implementation{Name: "t", Version: "0"}, nil)
	registerDirectTools(s, tools, taken)
	ct, st := mcp.NewInMemoryTransports()
	ctx := context.Background()
	ss, err := s.Connect(ctx, st, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "0"}, nil).Connect(ctx, ct, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func echoTool(name string, out any, err error) mcptools.Tool {
	return mcptools.Tool{
		Name: name, Description: "d", InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
		Run: func(context.Context, json.RawMessage) (any, error) { return out, err },
	}
}

func TestRegisterDirectTools_ListCallAndErrors(t *testing.T) {
	tools := []mcptools.Tool{
		echoTool("ok_tool", map[string]any{"a": 1}, nil),
		echoTool("bad_tool", nil, assert.AnError),
		echoTool("taken_tool", "x", nil),
		echoTool("huge_tool", strings.Repeat("x", maxResultBytes+1), nil),
	}
	cs := connect(t, tools, map[string]bool{"taken_tool": true})
	ctx := context.Background()

	list, err := cs.ListTools(ctx, nil)
	require.NoError(t, err)
	names := map[string]bool{}
	for _, tl := range list.Tools {
		names[tl.Name] = true
	}
	assert.True(t, names["ok_tool"])
	assert.False(t, names["taken_tool"], "a name used by an agent tool is skipped")

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "ok_tool", Arguments: map[string]any{}})
	require.NoError(t, err)
	require.False(t, res.IsError)
	assert.JSONEq(t, `{"a":1}`, res.Content[0].(*mcp.TextContent).Text)

	res, err = cs.CallTool(ctx, &mcp.CallToolParams{Name: "bad_tool", Arguments: map[string]any{}})
	require.NoError(t, err)
	assert.True(t, res.IsError, "a tool error is flagged IsError, not a protocol failure")

	res, err = cs.CallTool(ctx, &mcp.CallToolParams{Name: "huge_tool", Arguments: map[string]any{}})
	require.NoError(t, err)
	assert.True(t, res.IsError)
	assert.Contains(t, res.Content[0].(*mcp.TextContent).Text, "narrow the request")
}
