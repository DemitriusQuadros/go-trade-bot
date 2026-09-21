package modelprovider

import (
	"context"
	"encoding/json"
	"fmt"

	"google.golang.org/genai"
)

// defaultGeminiModel is used when Configuration.Agent.GeminiModel is left
// empty (Backend Spec 01's "empty uses the adapter's built-in default").
const defaultGeminiModel = "gemini-2.5-pro"

// GeminiAdapter is the internal/modelprovider ACL implementation backed by
// the official google.golang.org/genai client. No type from that package is
// exported outside this file - every caller works against the
// provider-agnostic Message/ToolDefinition/ToolCall shapes in provider.go.
type GeminiAdapter struct {
	client *genai.Client
	model  string
}

// geminiClientOption customizes the genai.ClientConfig built by
// NewGeminiAdapter - used by tests to point the SDK at an httptest.Server
// instead of the real Gemini API.
type geminiClientOption func(*genai.ClientConfig)

// withGeminiHTTPOptions is test-only plumbing (see gemini_adapter_test.go)
// to redirect the SDK's base URL.
func withGeminiHTTPOptions(opts genai.HTTPOptions) geminiClientOption {
	return func(cc *genai.ClientConfig) {
		cc.HTTPOptions = opts
	}
}

// NewGeminiAdapter builds a GeminiAdapter. It panics only if the underlying
// SDK's client construction fails for a reason unrelated to the API key
// (e.g. malformed HTTPOptions in tests) - a bad/empty key is a runtime
// (Complete-time) failure from the vendor API instead, matching how the
// Anthropic SDK also defers auth failures to the first request.
func NewGeminiAdapter(apiKey, model string, opts ...geminiClientOption) *GeminiAdapter {
	if model == "" {
		model = defaultGeminiModel
	}
	cc := &genai.ClientConfig{APIKey: apiKey, Backend: genai.BackendGeminiAPI}
	for _, opt := range opts {
		opt(cc)
	}
	client, err := genai.NewClient(context.Background(), cc)
	if err != nil {
		// Construction only fails on malformed local config (never a
		// network call), so this is safe to treat as a programmer error.
		panic(fmt.Sprintf("modelprovider: failed to construct gemini client: %v", err))
	}
	return &GeminiAdapter{client: client, model: model}
}

func (a *GeminiAdapter) Complete(ctx context.Context, req CompletionRequest) (CompletionResult, error) {
	contents := make([]*genai.Content, 0, len(req.Messages))
	for _, m := range req.Messages {
		role := genai.Role(genai.RoleUser)
		if m.Role == "assistant" {
			role = genai.Role(genai.RoleModel)
		}
		contents = append(contents, genai.NewContentFromText(m.Content, role))
	}

	cfg := &genai.GenerateContentConfig{}
	if req.System != "" {
		cfg.SystemInstruction = genai.NewContentFromText(req.System, genai.Role(genai.RoleUser))
	}
	if len(req.Tools) > 0 {
		decls := make([]*genai.FunctionDeclaration, 0, len(req.Tools))
		for _, t := range req.Tools {
			var schema map[string]any
			if len(t.InputSchema) > 0 {
				if err := json.Unmarshal(t.InputSchema, &schema); err != nil {
					return CompletionResult{}, fmt.Errorf("modelprovider: invalid input schema for tool %q: %w", t.Name, err)
				}
			}
			decls = append(decls, &genai.FunctionDeclaration{
				Name:                 t.Name,
				Description:          t.Description,
				ParametersJsonSchema: schema,
			})
		}
		cfg.Tools = []*genai.Tool{{FunctionDeclarations: decls}}
	}

	resp, err := a.client.Models.GenerateContent(ctx, a.model, contents, cfg)
	if err != nil {
		// Never a zero-value CompletionResult with a nil error - callers in
		// app/usecase/agent record AgentRun{Status: Error} off this.
		return CompletionResult{}, fmt.Errorf("modelprovider: gemini completion failed: %w", err)
	}

	result := CompletionResult{Text: resp.Text(), StopReason: "end_turn"}
	calls := resp.FunctionCalls()
	if len(calls) > 0 {
		result.StopReason = "tool_use"
		for _, c := range calls {
			argsBytes, mErr := json.Marshal(c.Args)
			if mErr != nil {
				return CompletionResult{}, fmt.Errorf("modelprovider: failed to marshal gemini function call args: %w", mErr)
			}
			result.ToolCalls = append(result.ToolCalls, ToolCall{
				ID:   c.ID,
				Name: c.Name,
				Args: argsBytes,
			})
		}
	} else if len(resp.Candidates) > 0 {
		result.StopReason = mapGeminiFinishReason(resp.Candidates[0].FinishReason)
	}
	return result, nil
}

func mapGeminiFinishReason(r genai.FinishReason) string {
	switch r {
	case genai.FinishReasonStop, "":
		return "end_turn"
	case genai.FinishReasonMaxTokens:
		return "max_tokens"
	default:
		return string(r)
	}
}
