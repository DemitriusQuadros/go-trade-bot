// Package modelprovider is the ACL boundary for every LLM call this codebase
// makes (Backend Spec 01). It follows the exact discipline already
// established by internal/exchange (no code outside it imports go-binance)
// and internal/indicators (no code outside it imports go-talib): no handler,
// usecase, or cmd/mcp code may import the Anthropic or Gemini SDKs directly
// - everything goes through the ModelProvider interface defined here.
package modelprovider

import (
	"context"
	"encoding/json"
)

// Message is the provider-agnostic chat turn shape every caller works
// against.
type Message struct {
	Role    string // "user" | "assistant" | "system"
	Content string
}

// ToolDefinition/ToolCall are the provider-agnostic shape every caller works
// against. Anthropic's tool_use blocks and Gemini's functionCall responses
// are each translated into this shape inside their own adapter file - no
// caller branches on provider.
type ToolDefinition struct {
	Name        string
	Description string
	InputSchema json.RawMessage // JSON Schema
}

type ToolCall struct {
	ID   string
	Name string
	Args json.RawMessage
}

type CompletionRequest struct {
	System   string
	Messages []Message
	Tools    []ToolDefinition
}

type CompletionResult struct {
	Text       string
	ToolCalls  []ToolCall
	StopReason string // "end_turn" | "tool_use" | "max_tokens" | "error"
}

// ModelProvider is the one abstraction app/usecase/agent (Backend Spec 03)
// and cmd/mcp are allowed to depend on for LLM access. No streaming in
// Phase 1 - the tool-call loop is request/response per turn.
type ModelProvider interface {
	Complete(ctx context.Context, req CompletionRequest) (CompletionResult, error)
}
