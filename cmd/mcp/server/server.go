// Package server hosts cmd/mcp's MCP transport(s) (Backend Spec 04). It
// wraps app/usecase/agent.AgentUseCase's tool registry and the Backend Spec
// 05 authoring doc behind the github.com/modelcontextprotocol/go-sdk/mcp
// server - both the stdio and HTTP/SSE transports serve the identical
// AgentUseCase, never divergent logic.
package server

import (
	"context"
	"fmt"

	agentusecase "go-trade-bot/app/usecase/agent"
	"go-trade-bot/internal/configuration"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Server wraps the underlying mcp.Server plus whichever transport Start
// selects.
type Server struct {
	mcpServer *mcp.Server
	cfg       *configuration.Configuration
	http      *httpTransport
}

// NewServer builds the MCP server and registers every tool (Backend Spec
// 03/06) and resource (Backend Spec 05) up front - both transports below
// see the identical, fully-populated server.
func NewServer(uc *agentusecase.AgentUseCase, cfg *configuration.Configuration) *Server {
	impl := &mcp.Implementation{Name: "go-trade-bot-agent", Version: "0.1.0"}
	mcpServer := mcp.NewServer(impl, nil)
	registerTools(mcpServer, uc)
	registerResources(mcpServer)
	return &Server{mcpServer: mcpServer, cfg: cfg}
}

// Start launches the given transport ("stdio" | "http"). Only one runs per
// process invocation (Backend Spec 04's Out of Scope - two transports at
// once means two processes).
func (s *Server) Start(transport string) error {
	switch transport {
	case "stdio":
		return startStdio(s.mcpServer)
	case "http":
		t, err := startHTTP(s.mcpServer, s.cfg)
		if err != nil {
			return err
		}
		s.http = t
		return nil
	default:
		return fmt.Errorf("mcp: unknown transport %q (want \"stdio\" or \"http\")", transport)
	}
}

// Stop shuts down whichever transport Start launched. Stdio has nothing to
// drain beyond the process's own stdin/stdout, so this is a no-op unless
// the HTTP transport was started.
func (s *Server) Stop(ctx context.Context) error {
	if s.http != nil {
		return s.http.shutdown(ctx)
	}
	return nil
}
