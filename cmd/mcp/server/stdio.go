package server

import (
	"context"
	"log"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// startStdio runs the MCP server over stdin/stdout - the local-trust
// transport for a client (e.g. Claude Desktop) that launches this binary
// as a subprocess. No bearer-token check applies here (Backend Spec 04
// AC#5): the trust boundary is "who can spawn this process," identical to
// running cmd/backtest or cmd/candleimport directly. No HTTP port is
// opened.
//
// mcp.Server.Run blocks until the client disconnects, so it runs in its own
// goroutine - fx's OnStart hook must return promptly.
func startStdio(mcpServer *mcp.Server) error {
	go func() {
		if err := mcpServer.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
			log.Printf("mcp: stdio transport exited: %v", err)
		}
	}()
	return nil
}
