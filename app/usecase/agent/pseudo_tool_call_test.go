package agent_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"go-trade-bot/app/entities"
	agentusecase "go-trade-bot/app/usecase/agent"
	"go-trade-bot/internal/modelprovider"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestParsePseudoToolCalls(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expectedCalls int
		expectedTool  string
		expectedArgs  string
		expectedRem   string
	}{
		{
			name:          "plain text without pseudo calls",
			input:         "Hello! How can I help you today?",
			expectedCalls: 0,
			expectedRem:   "Hello! How can I help you today?",
		},
		{
			name:          "empty args pseudo call",
			input:         "[called tool get_open_positions with args {}]",
			expectedCalls: 1,
			expectedTool:  "get_open_positions",
			expectedArgs:  "{}",
			expectedRem:   "",
		},
		{
			name:          "pseudo call with complex json containing brackets inside strings",
			input:         `[called tool deploy_to_testing with args {"rationale": "testing [brackets] and symbols", "script_source": "-- lua [1] = true\nfunction init(ctx)\nend", "strategy_id": 13}]`,
			expectedCalls: 1,
			expectedTool:  "deploy_to_testing",
			expectedRem:   "",
		},
		{
			name:          "pseudo call with text before and after",
			input:         "I am now going to fetch positions.\n[called tool get_open_positions with args {}]\nPlease wait.",
			expectedCalls: 1,
			expectedTool:  "get_open_positions",
			expectedArgs:  "{}",
			expectedRem:   "I am now going to fetch positions.\n\nPlease wait.",
		},
		{
			name: "multiple pseudo calls in one response",
			input: "[called tool notify with args {\"message\": \"alert\"}]\n" +
				"[called tool write_journal with args {\"content\": \"note\", \"kind\": \"finding\", \"strategy_id\": 13}]",
			expectedCalls: 2,
			expectedRem:   "",
		},
		{
			name:          "pseudo call wrapped in markdown code fence",
			input:         "```\n[called tool get_open_positions with args {}]\n```",
			expectedCalls: 1,
			expectedTool:  "get_open_positions",
			expectedArgs:  "{}",
			expectedRem:   "",
		},
		{
			name:          "malformed pseudo call - unclosed json",
			input:         "[called tool get_open_positions with args {unclosed",
			expectedCalls: 0,
			expectedRem:   "[called tool get_open_positions with args {unclosed",
		},
		{
			name:          "malformed pseudo call - missing closing bracket",
			input:         "[called tool get_open_positions with args {}",
			expectedCalls: 0,
			expectedRem:   "[called tool get_open_positions with args {}",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			calls, rem := agentusecase.ParsePseudoToolCalls(tc.input)
			if len(calls) != tc.expectedCalls {
				t.Fatalf("expected %d calls, got %d", tc.expectedCalls, len(calls))
			}
			if tc.expectedTool != "" && len(calls) > 0 {
				if calls[0].Name != tc.expectedTool {
					t.Errorf("expected tool %q, got %q", tc.expectedTool, calls[0].Name)
				}
			}
			if tc.expectedArgs != "" && len(calls) > 0 {
				if string(calls[0].Args) != tc.expectedArgs {
					t.Errorf("expected args %q, got %q", tc.expectedArgs, string(calls[0].Args))
				}
			}
			if tc.expectedRem != "" && rem != tc.expectedRem {
				t.Errorf("expected remaining %q, got %q", tc.expectedRem, rem)
			}
			if tc.expectedRem == "" && rem != "" {
				t.Errorf("expected empty remaining, got %q", rem)
			}
		})
	}
}

func TestBuildHistoryMessages_SanitizesCorruptedPseudoCalls(t *testing.T) {
	history := []agentusecase.PriorTurn{
		{
			Input:        "Como está a estratégia?",
			ResponseText: "Aqui está o status.",
		},
		{
			Input:        "Pode modificar?",
			ResponseText: `[called tool deploy_to_testing with args {"rationale": "update", "strategy_id": 13}]`,
		},
	}

	msgs := agentusecase.BuildHistoryMessages(history)
	// Turn 1: user + assistant (2 msgs)
	// Turn 2: user input ("Pode modificar?"). Since ResponseText was entirely a pseudo call,
	// it should be stripped so it does not inject pseudo-call text to the model!
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages after sanitizing pseudo tool call from history, got %d: %+v", len(msgs), msgs)
	}

	for _, m := range msgs {
		if strings.Contains(m.Content, "[called tool deploy_to_testing") {
			t.Errorf("found unsanitized pseudo tool call in history message: %q", m.Content)
		}
	}
}

func TestRunToolLoop_ExecutesPseudoToolCallEmittedAsText(t *testing.T) {
	uc, model, repo, strategyUC, _ := newBareAgentUseCase()

	repo.On("GetInstruction", mock.Anything).Return(entities.AgentInstruction{}, nil)
	repo.On("CreateRun", mock.Anything, mock.Anything).Return(entities.AgentRun{ID: 1}, nil)
	repo.On("UpdateRun", mock.Anything, mock.Anything).Return(nil)
	strategyUC.On("GetAll", mock.Anything).Return([]entities.Strategy{}, nil)

	// Turn 1: model emits pseudo tool call in Text with no ToolCalls
	model.On("Complete", mock.Anything, mock.Anything).Return(modelprovider.CompletionResult{
		StopReason: "end_turn",
		Text:       `[called tool list_strategies with args {}]`,
	}, nil).Once()

	// Turn 2: model sees tool result and returns final answer
	model.On("Complete", mock.Anything, mock.MatchedBy(func(req modelprovider.CompletionRequest) bool {
		for _, m := range req.Messages {
			if strings.Contains(m.Content, "[tool_result for list_strategies]") {
				return true
			}
		}
		return false
	})).Return(modelprovider.CompletionResult{
		StopReason: "end_turn",
		Text:       "Você não tem estratégias cadastradas.",
	}, nil).Once()

	run, err := uc.RunToolLoop(context.Background(), "chat_ui", "verificar estratégias", nil, nil)
	require.NoError(t, err)
	assert.Equal(t, entities.AgentRunOK, run.Status)
	assert.Equal(t, "Você não tem estratégias cadastradas.", run.ResponseText)

	// Tool call must be persisted in ToolCallsJSON
	var records []map[string]any
	err = json.Unmarshal(run.ToolCallsJSON, &records)
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, "list_strategies", records[0]["tool"])
}
