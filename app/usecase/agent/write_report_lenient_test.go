package agent_test

import (
	"context"
	"encoding/json"
	"testing"

	agentusecase "go-trade-bot/app/usecase/agent"
	"go-trade-bot/internal/modelprovider"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// runWriteReport drives one write_report call through Run and returns the
// tool result text fed back to the model.
func runWriteReport(t *testing.T, h *platformHarness, blocks any) string {
	t.Helper()
	var toolResult string
	h.model.On("Complete", mock.Anything, mock.MatchedBy(func(req modelprovider.CompletionRequest) bool {
		return len(req.Messages) == 1
	})).Return(modelprovider.CompletionResult{
		StopReason: "tool_use", ToolCalls: []modelprovider.ToolCall{writeReportCall(t, blocks, []uint{3})},
	}, nil).Once()
	h.model.On("Complete", mock.Anything, mock.MatchedBy(func(req modelprovider.CompletionRequest) bool {
		toolResult = req.Messages[len(req.Messages)-1].Content
		return len(req.Messages) > 1
	})).Return(modelprovider.CompletionResult{StopReason: "end_turn", Text: "done"}, nil).Once()
	_, err := h.uc.Run(context.Background(), agentusecase.RunRequest{Agent: h.def, Trigger: "manual", UserInput: "report"})
	require.NoError(t, err)
	return toolResult
}

// fix-02 B3: run #49's first write_report put the summary text next to
// "type" instead of under "data" - accepted leniently now.
func TestWriteReport_LenientTopLevelFieldsAccepted(t *testing.T) {
	h := newPlatformHarness(t)
	res := runWriteReport(t, h, []map[string]any{
		{"type": "summary", "text": "Flattened summary."},
		{"type": "kpi_grid", "source": map[string]any{"kind": "backtest_run", "id": 7}, "metrics": []string{"sharpe"}},
		{"type": "callout", "data": map[string]any{"severity": "info", "title": "Canonical"}},
	})
	assert.Contains(t, res, `"report_id"`, res)
	require.Len(t, h.platform.reports, 1)
	rep := h.platform.reports[0]
	assert.Equal(t, "Flattened summary.", rep.Summary)
	assert.Contains(t, rep.RenderedHTML, ">1.50<")

	// Stored blocks are the canonical {type, data} form.
	var stored []map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(rep.BlocksJSON, &stored))
	assert.JSONEq(t, `{"text":"Flattened summary."}`, string(stored[0]["data"]))
}

func TestWriteReport_MalformedBlockStillErrorsWithAllowedTypes(t *testing.T) {
	h := newPlatformHarness(t)
	res := runWriteReport(t, h, []map[string]any{{"type": "pie_chart", "slices": []int{1, 2}}})
	assert.Contains(t, res, `unknown block type "pie_chart"; allowed types: callout, code_diff, equity_chart, kpi_grid, recommendation, summary, text_table, trade_table`)
	assert.Empty(t, h.platform.reports)

	h = newPlatformHarness(t)
	res = runWriteReport(t, h, []map[string]any{{"type": "summary"}})
	assert.Contains(t, res, `block 0 ("summary"): missing data`)

	h = newPlatformHarness(t)
	res = runWriteReport(t, h, []any{"just a string"})
	assert.Contains(t, res, "block 0: each block must be a {type, data} object")
	assert.Empty(t, h.platform.reports)
}

func TestWriteReport_SchemaDocumentsEveryBlockType(t *testing.T) {
	h := newPlatformHarness(t)
	var def modelprovider.ToolDefinition
	for _, tool := range h.uc.Tools() {
		if tool.Def.Name == "write_report" {
			def = tool.Def
		}
	}
	require.NotEmpty(t, def.Name)
	assert.Contains(t, def.Description, `{"type":"summary","data":{"text":`)

	var schema struct {
		Properties struct {
			Blocks struct {
				Items struct {
					OneOf []struct {
						Properties struct {
							Type struct {
								Enum []string `json:"enum"`
							} `json:"type"`
							Data struct {
								Required []string `json:"required"`
							} `json:"data"`
						} `json:"properties"`
						Required []string `json:"required"`
					} `json:"oneOf"`
				} `json:"items"`
			} `json:"blocks"`
		} `json:"properties"`
	}
	require.NoError(t, json.Unmarshal(def.InputSchema, &schema))
	got := map[string][]string{}
	for _, b := range schema.Properties.Blocks.Items.OneOf {
		require.Len(t, b.Properties.Type.Enum, 1)
		assert.Equal(t, []string{"type", "data"}, b.Required)
		got[b.Properties.Type.Enum[0]] = b.Properties.Data.Required
	}
	assert.Equal(t, map[string][]string{
		"summary":        {"text"},
		"callout":        {"severity"},
		"kpi_grid":       {"source", "metrics"},
		"equity_chart":   {"source"},
		"trade_table":    {"source"},
		"code_diff":      {"strategy_id"},
		"recommendation": {"items"},
		"text_table":     {"columns", "rows"},
	}, got)
}
