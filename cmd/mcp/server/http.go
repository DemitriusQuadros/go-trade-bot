package server

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"

	"go-trade-bot/internal/configuration"
	"go-trade-bot/internal/middleware"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// defaultHTTPAddr is cmd/mcp's HTTP/SSE listen address - distinct from
// cmd/api's :8080 and cmd/worker's :9191, so all three can run at once on
// one host.
const defaultHTTPAddr = ":8090"

type httpTransport struct {
	srv *http.Server
}

// startHTTP serves the same tool/resource set as stdio.go's transport over
// HTTP/SSE (Backend Spec 04 AC#2), wrapped in the exact same
// Authorization: Bearer <API_TOKEN> check every cmd/api route already uses
// (internal/middleware.RequireAuth) - no separate, weaker auth scheme for
// this binary (AC#4).
func startHTTP(mcpServer *mcp.Server, cfg *configuration.Configuration) (*httpTransport, error) {
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return mcpServer }, nil)
	protected := middleware.RequireAuth(cfg, handler.ServeHTTP)

	srv := &http.Server{Addr: defaultHTTPAddr, Handler: protected}
	ln, err := net.Listen("tcp", defaultHTTPAddr)
	if err != nil {
		return nil, fmt.Errorf("mcp: failed to bind http transport on %s: %w", defaultHTTPAddr, err)
	}

	fmt.Println("Starting MCP HTTP/SSE transport at", defaultHTTPAddr)
	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("mcp: http transport exited: %v", err)
		}
	}()

	return &httpTransport{srv: srv}, nil
}

func (t *httpTransport) shutdown(ctx context.Context) error {
	return t.srv.Shutdown(ctx)
}
