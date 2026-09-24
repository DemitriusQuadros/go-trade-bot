package agent_test

import (
	"context"
	"testing"

	"go-trade-bot/app/entities"
	agentusecase "go-trade-bot/app/usecase/agent"
	"go-trade-bot/internal/modelprovider"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func newBareAgentUseCase() (*agentusecase.AgentUseCase, *mockModelProvider, *mockAgentRepository, *mockStrategyUseCase, *mockBacktestUseCase) {
	model := &mockModelProvider{}
	repo := &mockAgentRepository{}
	strategyUC := &mockStrategyUseCase{}
	backtestUC := &mockBacktestUseCase{}
	signalUC := &mockSignalUseCase{}
	snapshotUC := &mockSnapshotUseCase{}
	uc := agentusecase.NewAgentUseCase(model, repo, strategyUC, backtestUC, signalUC, snapshotUC)
	return uc, model, repo, strategyUC, backtestUC
}

// TestRunToolLoop_EndTurnWithNoToolCalls covers the simplest possible turn:
// the model answers directly with no tool use.
func TestRunToolLoop_EndTurnWithNoToolCalls(t *testing.T) {
	uc, model, repo, _, _ := newBareAgentUseCase()

	repo.On("GetInstruction", mock.Anything).Return(entities.AgentInstruction{Content: "house rules"}, nil)
	repo.On("CreateRun", mock.Anything, mock.Anything).Return(entities.AgentRun{ID: 1}, nil)
	repo.On("UpdateRun", mock.Anything, mock.MatchedBy(func(r entities.AgentRun) bool {
		return r.Status == entities.AgentRunOK && !r.FinishedAt.IsZero()
	})).Return(nil)

	model.On("Complete", mock.Anything, mock.MatchedBy(func(req modelprovider.CompletionRequest) bool {
		// The system prompt concatenates AgentInstruction + the generated
		// authoring doc (Backend Spec 03's "Out of Scope" note - Spec 05
		// defines the doc content, this usecase only concatenates it).
		return req.System != "" && req.Messages[0].Content == "hello"
	})).Return(modelprovider.CompletionResult{StopReason: "end_turn", Text: "hi there"}, nil).Once()

	run, err := uc.RunToolLoop(context.Background(), "mcp_tool", "hello", nil, nil)
	require.NoError(t, err)
	require.Equal(t, entities.AgentRunOK, run.Status)
	// Regression: the model's final answer text was computed but never
	// persisted onto AgentRun, so the copilot UI had no way to ever show
	// it - only its tool-call cards, even on a run with no tool calls at
	// all like this one.
	assert.Equal(t, "hi there", run.ResponseText)
}

// TestRunToolLoop_ResponseTextCapturedAfterToolCalls covers the more common
// real-world shape: the model calls a tool, then wraps up with a final
// text answer. ResponseText must reflect that final answer, not the empty
// Text of the earlier tool-calling turn.
func TestRunToolLoop_ResponseTextCapturedAfterToolCalls(t *testing.T) {
	uc, model, repo, strategyUC, _ := newBareAgentUseCase()

	repo.On("GetInstruction", mock.Anything).Return(entities.AgentInstruction{}, nil)
	repo.On("CreateRun", mock.Anything, mock.Anything).Return(entities.AgentRun{ID: 1}, nil)
	repo.On("UpdateRun", mock.Anything, mock.Anything).Return(nil)
	strategyUC.On("GetAll", mock.Anything).Return([]entities.Strategy{}, nil)

	model.On("Complete", mock.Anything, mock.Anything).Return(modelprovider.CompletionResult{
		StopReason: "tool_use",
		Text:       "",
		ToolCalls:  []modelprovider.ToolCall{{ID: "1", Name: "list_strategies", Args: rawArgs(map[string]any{})}},
	}, nil).Once()
	model.On("Complete", mock.Anything, mock.Anything).Return(modelprovider.CompletionResult{
		StopReason: "end_turn", Text: "You have no strategies without recent backtests.",
	}, nil).Once()

	run, err := uc.RunToolLoop(context.Background(), "mcp_tool", "list my strategies", nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "You have no strategies without recent backtests.", run.ResponseText)
}

// TestRunToolLoop_PersistsIncrementallyAfterEveryToolCall is Acceptance
// Criterion #5: Repository.UpdateRun is called before the loop proceeds to
// the next iteration, not only after the loop terminates.
func TestRunToolLoop_PersistsIncrementallyAfterEveryToolCall(t *testing.T) {
	uc, model, repo, strategyUC, _ := newBareAgentUseCase()

	repo.On("GetInstruction", mock.Anything).Return(entities.AgentInstruction{}, nil)
	repo.On("CreateRun", mock.Anything, mock.Anything).Return(entities.AgentRun{ID: 1}, nil)

	updateCalls := 0
	repo.On("UpdateRun", mock.Anything, mock.Anything).Run(func(_ mock.Arguments) {
		updateCalls++
	}).Return(nil)

	strategyUC.On("GetAll", mock.Anything).Return([]entities.Strategy{}, nil)

	model.On("Complete", mock.Anything, mock.Anything).Return(modelprovider.CompletionResult{
		StopReason: "tool_use",
		ToolCalls:  []modelprovider.ToolCall{{ID: "1", Name: "list_strategies", Args: rawArgs(map[string]any{})}},
	}, nil).Once()
	model.On("Complete", mock.Anything, mock.Anything).Return(modelprovider.CompletionResult{
		StopReason: "end_turn", Text: "done",
	}, nil).Once()

	_, err := uc.RunToolLoop(context.Background(), "mcp_tool", "list my strategies", nil, nil)
	require.NoError(t, err)
	// One UpdateRun after the tool call, one more at loop-end persistence.
	require.GreaterOrEqual(t, updateCalls, 2)
}

// TestRunToolLoop_RecordsToolCallAsAssistantTurnEvenWithNoText is a
// regression test for a real bug found manually against a live Gemini
// model: when the model's tool-call turn produced no text (the common
// case - a model that just wants to call a function typically returns
// empty text alongside the function call), RunToolLoop previously only
// appended an assistant message `if result.Text != ""`, so that turn was
// dropped from history entirely. The next Complete() call then saw the
// tool's result appear as a "user" turn with NO preceding assistant turn
// ever having asked for it - two consecutive "user" turns with nothing
// between them. Gemini did not cope with this: with no record of its own
// prior function call, it treated the situation as if it had never called
// the tool at all and re-issued the IDENTICAL call, repeatedly, instead of
// answering from the result already in front of it (observed: the same
// list_strategies call, same empty args, four times in a row, correct
// result ignored every time). The fix records an assistant turn
// covering the tool call(s) even when Text is empty - this test asserts
// the second Complete() call's message history actually contains that
// turn, so a regression here fails loudly instead of only showing up as a
// live-model behavioral oddity that's expensive to notice and reproduce.
func TestRunToolLoop_RecordsToolCallAsAssistantTurnEvenWithNoText(t *testing.T) {
	uc, model, repo, strategyUC, _ := newBareAgentUseCase()

	repo.On("GetInstruction", mock.Anything).Return(entities.AgentInstruction{}, nil)
	repo.On("CreateRun", mock.Anything, mock.Anything).Return(entities.AgentRun{ID: 1}, nil)
	repo.On("UpdateRun", mock.Anything, mock.Anything).Return(nil)
	strategyUC.On("GetAll", mock.Anything).Return([]entities.Strategy{}, nil)

	model.On("Complete", mock.Anything, mock.MatchedBy(func(req modelprovider.CompletionRequest) bool {
		// The FIRST call: no prior assistant turn should exist yet.
		return len(req.Messages) == 1
	})).Return(modelprovider.CompletionResult{
		StopReason: "tool_use",
		Text:       "", // the realistic case that triggered the bug: no text alongside the tool call
		ToolCalls:  []modelprovider.ToolCall{{ID: "1", Name: "list_strategies", Args: rawArgs(map[string]any{})}},
	}, nil).Once()

	var secondCallMessages []modelprovider.Message
	model.On("Complete", mock.Anything, mock.MatchedBy(func(req modelprovider.CompletionRequest) bool {
		secondCallMessages = req.Messages
		return len(req.Messages) > 1
	})).Return(modelprovider.CompletionResult{StopReason: "end_turn", Text: "done"}, nil).Once()

	_, err := uc.RunToolLoop(context.Background(), "mcp_tool", "list my strategies", nil, nil)
	require.NoError(t, err)

	require.Len(t, secondCallMessages, 3, "expected [user: input, assistant: tool-call record, user: tool result]")
	assert.Equal(t, "assistant", secondCallMessages[1].Role)
	assert.Contains(t, secondCallMessages[1].Content, "list_strategies", "the assistant turn must record which tool was called, even though Text was empty")
}

// TestRunToolLoop_ExceedsMaxIterations is Acceptance Criterion #6: hitting
// the iteration cap without StopReason == "end_turn" returns
// AgentRun{Status: Error} with a message naming the cap, not a silent
// partial success.
// TestRunToolLoop_ReplaysPriorTurnsIntoFirstMessage is a regression test
// for the "no conversational memory across messages" bug found during
// manual verification: every chat message previously started RunToolLoop
// with ONLY the latest input as the sole message, so a follow-up like "now
// tighten the stop loss" had no idea what script an earlier message in the
// same session had drafted. This asserts a passed-in PriorTurn (with a
// tool call and a final answer, exactly what a real prior turn looks like)
// is actually replayed into the very first Model.Complete call, before the
// new user input.
// TestRunToolLoop_KnownStrategyIDPersistsEvenWithoutToolCall is a
// regression test: a turn that answers purely from replayed memory (no
// tool call at all) must still have its AgentRun tagged with
// knownStrategyID, not just a turn whose own tool call happens to resolve
// one. Without this, a pure-recall turn in an otherwise strategy-scoped
// conversation would persist with StrategyID == nil and become invisible
// to every later history lookup for that strategy (which filters strictly
// on strategy_id) - silently truncating memory after exactly one
// no-tool-call turn.
func TestRunToolLoop_KnownStrategyIDPersistsEvenWithoutToolCall(t *testing.T) {
	uc, model, repo, _, _ := newBareAgentUseCase()

	strategyID := uint(5)

	repo.On("GetInstruction", mock.Anything).Return(entities.AgentInstruction{}, nil)
	var createdRun entities.AgentRun
	// Mirrors the real repository's CreateRun (app/repository/agent/repository.go):
	// it echoes the input struct back with only ID populated by GORM, not a
	// disconnected fresh struct - a mock that discarded the input here would
	// mask exactly the kind of "field silently dropped after CreateRun" bug
	// this test exists to catch.
	repo.On("CreateRun", mock.Anything, mock.MatchedBy(func(r entities.AgentRun) bool {
		createdRun = r
		return true
	})).Return(entities.AgentRun{ID: 1, StrategyID: &strategyID}, nil)
	repo.On("UpdateRun", mock.Anything, mock.Anything).Return(nil)

	model.On("Complete", mock.Anything, mock.Anything).Return(modelprovider.CompletionResult{
		StopReason: "end_turn", Text: "We used SOLUSDT.",
	}, nil).Once()

	run, err := uc.RunToolLoop(context.Background(), "chat_ui", "what symbol did we use?", nil, &strategyID)
	require.NoError(t, err)

	require.NotNil(t, createdRun.StrategyID, "the AgentRun row must be tagged with knownStrategyID at creation, before any tool call could opportunistically set it")
	assert.Equal(t, strategyID, *createdRun.StrategyID)
	require.NotNil(t, run.StrategyID)
	assert.Equal(t, strategyID, *run.StrategyID)
}

func TestRunToolLoop_ReplaysPriorTurnsIntoFirstMessage(t *testing.T) {
	uc, model, repo, _, _ := newBareAgentUseCase()

	repo.On("GetInstruction", mock.Anything).Return(entities.AgentInstruction{}, nil)
	repo.On("CreateRun", mock.Anything, mock.Anything).Return(entities.AgentRun{ID: 1}, nil)
	repo.On("UpdateRun", mock.Anything, mock.Anything).Return(nil)

	history := []agentusecase.PriorTurn{
		{
			Input: "draft an EMA crossover strategy for BTCUSDT",
			ToolCalls: []agentusecase.PriorToolCall{
				{Tool: "save_strategy_script", Args: rawArgs(map[string]any{"name": "EMA Cross"}), Result: "strategy_id=9 name=\"EMA Cross\""},
			},
			ResponseText: "I've drafted an EMA crossover strategy as strategy #9.",
		},
	}

	var firstCallMessages []modelprovider.Message
	model.On("Complete", mock.Anything, mock.MatchedBy(func(req modelprovider.CompletionRequest) bool {
		firstCallMessages = req.Messages
		return true
	})).Return(modelprovider.CompletionResult{StopReason: "end_turn", Text: "Tightened the stop loss to 1%."}, nil).Once()

	_, err := uc.RunToolLoop(context.Background(), "chat_ui", "now tighten the stop loss to 1%", history, nil)
	require.NoError(t, err)

	require.Len(t, firstCallMessages, 5, "expected [user: drafted input, assistant: tool-call record, user: tool result, assistant: prior answer, user: new input]")
	assert.Equal(t, "user", firstCallMessages[0].Role)
	assert.Equal(t, "draft an EMA crossover strategy for BTCUSDT", firstCallMessages[0].Content)
	assert.Equal(t, "assistant", firstCallMessages[1].Role)
	assert.Contains(t, firstCallMessages[1].Content, "save_strategy_script")
	assert.Equal(t, "user", firstCallMessages[2].Role)
	assert.Contains(t, firstCallMessages[2].Content, "strategy_id=9")
	assert.Equal(t, "assistant", firstCallMessages[3].Role)
	assert.Equal(t, "I've drafted an EMA crossover strategy as strategy #9.", firstCallMessages[3].Content)
	assert.Equal(t, "user", firstCallMessages[4].Role)
	assert.Equal(t, "now tighten the stop loss to 1%", firstCallMessages[4].Content)
}

func TestRunToolLoop_ExceedsMaxIterations(t *testing.T) {
	uc, model, repo, strategyUC, _ := newBareAgentUseCase()
	uc.MaxToolLoopIterations = 2

	repo.On("GetInstruction", mock.Anything).Return(entities.AgentInstruction{}, nil)
	repo.On("CreateRun", mock.Anything, mock.Anything).Return(entities.AgentRun{ID: 1}, nil)
	repo.On("UpdateRun", mock.Anything, mock.Anything).Return(nil)
	strategyUC.On("GetAll", mock.Anything).Return([]entities.Strategy{}, nil)

	// Always returns tool_use - the model never stops, so the loop must hit
	// the cap rather than spin forever.
	model.On("Complete", mock.Anything, mock.Anything).Return(modelprovider.CompletionResult{
		StopReason: "tool_use",
		ToolCalls:  []modelprovider.ToolCall{{ID: "1", Name: "list_strategies", Args: rawArgs(map[string]any{})}},
	}, nil)

	run, err := uc.RunToolLoop(context.Background(), "mcp_tool", "keep going forever", nil, nil)
	require.Error(t, err)
	require.Equal(t, entities.AgentRunError, run.Status)
	require.Contains(t, run.ErrorMessage, "2 iterations")
}

// TestRunToolLoop_MalformedToolCallArgsDoesNotAbortLoop is Acceptance
// Criterion #7 (edge case): unparseable tool-call args produce an error fed
// back to the model as the tool result, not an aborted loop.
func TestRunToolLoop_MalformedToolCallArgsDoesNotAbortLoop(t *testing.T) {
	uc, model, repo, strategyUC, _ := newBareAgentUseCase()

	repo.On("GetInstruction", mock.Anything).Return(entities.AgentInstruction{}, nil)
	repo.On("CreateRun", mock.Anything, mock.Anything).Return(entities.AgentRun{ID: 1}, nil)
	repo.On("UpdateRun", mock.Anything, mock.Anything).Return(nil)
	strategyUC.On("GetByID", mock.Anything, mock.Anything).Return(entities.Strategy{}, nil)

	model.On("Complete", mock.Anything, mock.Anything).Return(modelprovider.CompletionResult{
		StopReason: "tool_use",
		ToolCalls:  []modelprovider.ToolCall{{ID: "1", Name: "get_strategy", Args: []byte(`{not valid json`)}},
	}, nil).Once()
	model.On("Complete", mock.Anything, mock.MatchedBy(func(req modelprovider.CompletionRequest) bool {
		last := req.Messages[len(req.Messages)-1]
		return last.Role == "user" && len(last.Content) > 0
	})).Return(modelprovider.CompletionResult{StopReason: "end_turn", Text: "retrying"}, nil).Once()

	run, err := uc.RunToolLoop(context.Background(), "mcp_tool", "get strategy", nil, nil)
	require.NoError(t, err)
	require.Equal(t, entities.AgentRunOK, run.Status)
	strategyUC.AssertNotCalled(t, "GetByID", mock.Anything, mock.Anything)
}

// TestRunToolLoop_ModelCompletionErrorRecordsErrorRun covers the "vendor
// API error" path from the caller's side: Model.Complete returning an
// error must produce AgentRun{Status: Error}, not a panic or a silently
// swallowed failure.
func TestRunToolLoop_ModelCompletionErrorRecordsErrorRun(t *testing.T) {
	uc, model, repo, _, _ := newBareAgentUseCase()

	repo.On("GetInstruction", mock.Anything).Return(entities.AgentInstruction{}, nil)
	repo.On("CreateRun", mock.Anything, mock.Anything).Return(entities.AgentRun{ID: 1}, nil)
	repo.On("UpdateRun", mock.Anything, mock.Anything).Return(nil)

	model.On("Complete", mock.Anything, mock.Anything).Return(modelprovider.CompletionResult{}, assertAnError())

	run, err := uc.RunToolLoop(context.Background(), "mcp_tool", "hello", nil, nil)
	require.Error(t, err)
	require.Equal(t, entities.AgentRunError, run.Status)
}

func assertAnError() error {
	return &testError{"vendor api unreachable"}
}

type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }

// TestRunBacktestTool_SucceedsForArbitraryExistingStrategy is Acceptance
// Criterion #8: run_backtest against a StrategyID the agent didn't create
// succeeds exactly as POST /api/backtest would for a human - no additional
// restriction.
func TestRunBacktestTool_SucceedsForArbitraryExistingStrategy(t *testing.T) {
	uc, model, repo, _, backtestUC := newBareAgentUseCase()

	repo.On("GetInstruction", mock.Anything).Return(entities.AgentInstruction{}, nil)
	repo.On("CreateRun", mock.Anything, mock.Anything).Return(entities.AgentRun{ID: 1}, nil)
	repo.On("UpdateRun", mock.Anything, mock.Anything).Return(nil)

	backtestUC.On("Run", mock.Anything, mock.MatchedBy(func(req interface{}) bool { return true })).
		Return(entities.BacktestRun{ID: 99, StrategyID: 123, Symbol: "BTCUSDT", TotalTrades: 5}, nil)

	model.On("Complete", mock.Anything, mock.Anything).Return(modelprovider.CompletionResult{
		StopReason: "tool_use",
		ToolCalls: []modelprovider.ToolCall{{ID: "1", Name: "run_backtest", Args: rawArgs(map[string]any{
			"strategy_id": 123,
			"symbol":      "BTCUSDT",
			"timeframe":   "1h",
			"start_date":  "2024-01-01T00:00:00Z",
			"end_date":    "2024-02-01T00:00:00Z",
		})}},
	}, nil).Once()
	model.On("Complete", mock.Anything, mock.Anything).Return(modelprovider.CompletionResult{StopReason: "end_turn", Text: "done"}, nil).Once()

	run, err := uc.RunToolLoop(context.Background(), "mcp_tool", "backtest strategy 123", nil, nil)
	require.NoError(t, err)
	require.Equal(t, entities.AgentRunOK, run.Status)
	backtestUC.AssertExpectations(t)
}
