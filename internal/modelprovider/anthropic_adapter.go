package modelprovider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// defaultAnthropicModel is used when Configuration.Agent.AnthropicModel is
// left empty (Backend Spec 01's "empty uses the adapter's built-in default").
const defaultAnthropicModel = "claude-sonnet-5"

// defaultAnthropicMaxTokens bounds a single Complete call's response size.
// The tool-call loop (Backend Spec 03) makes many of these calls per run, so
// this is deliberately conservative rather than the SDK's max.
const defaultAnthropicMaxTokens = 4096

// AnthropicAdapter is the internal/modelprovider ACL implementation backed
// by the official github.com/anthropics/anthropic-sdk-go client. No type
// from that package is exported outside this file - every caller works
// against the provider-agnostic Message/ToolDefinition/ToolCall shapes in
// provider.go.
type AnthropicAdapter struct {
	client anthropic.Client
	model  string
}

// NewAnthropicAdapter builds an AnthropicAdapter. baseURLOpts is variadic
// purely so tests can inject option.WithBaseURL(...) against an
// httptest.Server without any test-only exported field on the struct.
func NewAnthropicAdapter(apiKey, model string, extraOpts ...option.RequestOption) *AnthropicAdapter {
	if model == "" {
		model = defaultAnthropicModel
	}
	opts := append([]option.RequestOption{option.WithAPIKey(apiKey)}, extraOpts...)
	return &AnthropicAdapter{
		client: anthropic.NewClient(opts...),
		model:  model,
	}
}

func (a *AnthropicAdapter) Complete(ctx context.Context, req CompletionRequest) (CompletionResult, error) {
	messages := make([]anthropic.MessageParam, 0, len(req.Messages))
	for _, m := range req.Messages {
		block := anthropic.NewTextBlock(m.Content)
		switch m.Role {
		case "assistant":
			messages = append(messages, anthropic.NewAssistantMessage(block))
		default:
			// "user" and anything else (this adapter never receives a
			// "system" role message - CompletionRequest.System is the
			// dedicated channel for that) fold into a user turn.
			messages = append(messages, anthropic.NewUserMessage(block))
		}
	}

	tools := make([]anthropic.ToolUnionParam, 0, len(req.Tools))
	for _, t := range req.Tools {
		schema, err := toAnthropicInputSchema(t.InputSchema)
		if err != nil {
			return CompletionResult{}, fmt.Errorf("modelprovider: invalid input schema for tool %q: %w", t.Name, err)
		}
		tool := anthropic.ToolParam{
			Name:        t.Name,
			Description: anthropic.String(t.Description),
			InputSchema: schema,
		}
		tools = append(tools, anthropic.ToolUnionParam{OfTool: &tool})
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(a.model),
		MaxTokens: defaultAnthropicMaxTokens,
		Messages:  messages,
		Tools:     tools,
	}
	if req.System != "" {
		params.System = []anthropic.TextBlockParam{{Text: req.System}}
	}

	resp, err := a.client.Messages.New(ctx, params)
	if err != nil {
		// Never a zero-value CompletionResult with a nil error - callers in
		// app/usecase/agent record AgentRun{Status: Error} off this.
		return CompletionResult{}, fmt.Errorf("modelprovider: anthropic completion failed: %w", err)
	}

	result := CompletionResult{StopReason: mapAnthropicStopReason(resp.StopReason)}
	for _, block := range resp.Content {
		switch variant := block.AsAny().(type) {
		case anthropic.TextBlock:
			result.Text += variant.Text
		case anthropic.ToolUseBlock:
			result.ToolCalls = append(result.ToolCalls, ToolCall{
				ID:   variant.ID,
				Name: variant.Name,
				Args: json.RawMessage(variant.JSON.Input.Raw()),
			})
		}
	}
	return result, nil
}

func mapAnthropicStopReason(r anthropic.StopReason) string {
	switch r {
	case anthropic.StopReasonEndTurn:
		return "end_turn"
	case anthropic.StopReasonToolUse:
		return "tool_use"
	case anthropic.StopReasonMaxTokens:
		return "max_tokens"
	default:
		if r == "" {
			return "end_turn"
		}
		return string(r)
	}
}

// toAnthropicInputSchema converts a ToolDefinition's raw JSON Schema
// document (type/properties/required) into the SDK's typed
// ToolInputSchemaParam shape.
func toAnthropicInputSchema(raw json.RawMessage) (anthropic.ToolInputSchemaParam, error) {
	if len(raw) == 0 {
		return anthropic.ToolInputSchemaParam{Properties: map[string]any{}}, nil
	}
	var parsed struct {
		Properties map[string]any `json:"properties"`
		Required   []string       `json:"required"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return anthropic.ToolInputSchemaParam{}, err
	}
	if parsed.Properties == nil {
		parsed.Properties = map[string]any{}
	}
	return anthropic.ToolInputSchemaParam{
		Properties: parsed.Properties,
		Required:   parsed.Required,
	}, nil
}
