package modelprovider

import (
	"context"
	"fmt"
)

// UnconfiguredProvider implements ModelProvider by always failing with a
// clear, operator-facing message. Used wherever a binary must keep serving
// its actual job with no configured AGENT.* key, rather than refusing to
// start: cmd/api's chat panel (RunToolLoop is the one call site that ever
// invokes Complete) and cmd/mcp's MCP tool/resource server (whose tool
// execution path never calls Complete at all in Phase 1 - external MCP
// clients call each tool's Execute directly; only a future call path that
// drove RunToolLoop from inside cmd/mcp would ever see this error).
type UnconfiguredProvider struct {
	Reason string
}

func (p UnconfiguredProvider) Complete(ctx context.Context, req CompletionRequest) (CompletionResult, error) {
	return CompletionResult{}, fmt.Errorf("%s", p.Reason)
}
