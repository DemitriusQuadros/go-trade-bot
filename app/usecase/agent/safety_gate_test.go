package agent_test

import (
	"context"
	"testing"

	"go-trade-bot/app/entities"
	agentusecase "go-trade-bot/app/usecase/agent"
	"go-trade-bot/internal/modelprovider"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// This file is the single most important test in this feature (per Backend
// Spec 03): app/usecase/agent.AgentUseCase must be STRUCTURALLY incapable
// of producing a live-tradeable Strategy row, no matter what a model
// (untrusted input) asks for via save_strategy_script's tool-call
// arguments. Every test below drives the FULL public RunToolLoop path - the
// way a real model interaction actually reaches the gate - rather than
// calling any unexported helper directly, so a regression that bypasses the
// gate anywhere in the call chain is caught here.

// newAgentUseCaseForGateTest wires an AgentUseCase whose ModelProvider mock
// returns exactly one save_strategy_script tool call with the given raw
// args, then ends the turn - the minimal loop shape needed to exercise the
// gate once per test.
func newAgentUseCaseForGateTest(t *testing.T, toolArgs map[string]any) (*agentusecase.AgentUseCase, *mockStrategyUseCase) {
	t.Helper()

	model := &mockModelProvider{}
	repo := &mockAgentRepository{}
	strategyUC := &mockStrategyUseCase{}
	backtestUC := &mockBacktestUseCase{}
	signalUC := &mockSignalUseCase{}
	snapshotUC := &mockSnapshotUseCase{}

	repo.On("GetInstruction", mock.Anything).Return(entities.AgentInstruction{}, nil)
	repo.On("CreateRun", mock.Anything, mock.Anything).Return(entities.AgentRun{ID: 1}, nil)
	repo.On("UpdateRun", mock.Anything, mock.Anything).Return(nil)

	model.On("Complete", mock.Anything, mock.Anything).Return(modelprovider.CompletionResult{
		StopReason: "tool_use",
		ToolCalls: []modelprovider.ToolCall{
			{ID: "call_1", Name: "save_strategy_script", Args: rawArgs(toolArgs)},
		},
	}, nil).Once()
	model.On("Complete", mock.Anything, mock.Anything).Return(modelprovider.CompletionResult{
		StopReason: "end_turn",
		Text:       "done",
	}, nil).Once()

	uc := agentusecase.NewAgentUseCase(model, repo, strategyUC, backtestUC, signalUC, snapshotUC)
	return uc, strategyUC
}

// TestSaveStrategyScript_ClampsLiveModeToDryRun is Backend Spec 03
// Acceptance Criterion #1: a model requesting "mode": "live" must never
// result in a persisted Strategy.Mode of "live".
func TestSaveStrategyScript_ClampsLiveModeToDryRun(t *testing.T) {
	uc, strategyUC := newAgentUseCaseForGateTest(t, map[string]any{
		"name":          "Malicious Live Strategy",
		"description":   "attempts to go live",
		"script_source": "function GoLong() end",
		"symbols":       []string{"BTCUSDT"},
		"cycle_minutes": 15,
		"mode":          "live",
	})

	strategyUC.On("Save", mock.Anything, mock.MatchedBy(func(s entities.Strategy) bool {
		return s.Mode == "dryrun" && s.Status == entities.Testing
	})).Return(entities.Strategy{ID: 1, Mode: "dryrun", Status: entities.Testing}, nil)

	_, err := uc.RunToolLoop(context.Background(), "mcp_tool", "make me a live strategy", nil, nil)
	require.NoError(t, err)
	strategyUC.AssertExpectations(t)
	strategyUC.AssertNotCalled(t, "Save", mock.Anything, mock.MatchedBy(func(s entities.Strategy) bool {
		return s.Mode == "live"
	}))
}

// TestSaveStrategyScript_ClampsPaperModeToDryRun is Acceptance Criterion #2:
// "paper" is not on the allowlist either - only the literal string
// "backtest" passes through unmodified, everything else (including a
// second, still-risky tier) clamps to "dryrun".
func TestSaveStrategyScript_ClampsPaperModeToDryRun(t *testing.T) {
	uc, strategyUC := newAgentUseCaseForGateTest(t, map[string]any{
		"name":          "Paper Strategy",
		"description":   "attempts paper mode",
		"script_source": "function GoLong() end",
		"symbols":       []string{"ETHUSDT"},
		"cycle_minutes": 5,
		"mode":          "paper",
	})

	strategyUC.On("Save", mock.Anything, mock.MatchedBy(func(s entities.Strategy) bool {
		return s.Mode == "dryrun"
	})).Return(entities.Strategy{ID: 2, Mode: "dryrun", Status: entities.Testing}, nil)

	_, err := uc.RunToolLoop(context.Background(), "mcp_tool", "make me a paper strategy", nil, nil)
	require.NoError(t, err)
	strategyUC.AssertExpectations(t)
}

// TestSaveStrategyScript_ClampsGarbageModeToDryRun covers typos/garbage/
// empty mode values - the gate is an allowlist-of-one-plus-default, not a
// denylist of "live", so anything not exactly "backtest" clamps down.
func TestSaveStrategyScript_ClampsGarbageModeToDryRun(t *testing.T) {
	for _, badMode := range []string{"Live", "LIVE ", "l1ve", "", "production", "backtestt"} {
		badMode := badMode
		t.Run(badMode, func(t *testing.T) {
			uc, strategyUC := newAgentUseCaseForGateTest(t, map[string]any{
				"name":          "Strategy",
				"description":   "desc",
				"script_source": "function GoLong() end",
				"symbols":       []string{"BTCUSDT"},
				"cycle_minutes": 15,
				"mode":          badMode,
			})
			strategyUC.On("Save", mock.Anything, mock.MatchedBy(func(s entities.Strategy) bool {
				return s.Mode == "dryrun"
			})).Return(entities.Strategy{ID: 3, Mode: "dryrun", Status: entities.Testing}, nil)

			_, err := uc.RunToolLoop(context.Background(), "mcp_tool", "input", nil, nil)
			require.NoError(t, err)
			strategyUC.AssertExpectations(t)
		})
	}
}

// TestSaveStrategyScript_AllowsExactlyBacktestModeThrough is the positive
// control for the gate: the ONE value that is allowed to pass through
// unmodified is the literal string "backtest" - itself a fully simulated,
// no-live-risk mode.
func TestSaveStrategyScript_AllowsExactlyBacktestModeThrough(t *testing.T) {
	uc, strategyUC := newAgentUseCaseForGateTest(t, map[string]any{
		"name":          "Backtest Strategy",
		"description":   "desc",
		"script_source": "function GoLong() end",
		"symbols":       []string{"BTCUSDT"},
		"cycle_minutes": 15,
		"mode":          "backtest",
	})
	strategyUC.On("Save", mock.Anything, mock.MatchedBy(func(s entities.Strategy) bool {
		return s.Mode == "backtest" && s.Status == entities.Testing
	})).Return(entities.Strategy{ID: 4, Mode: "backtest", Status: entities.Testing}, nil)

	_, err := uc.RunToolLoop(context.Background(), "mcp_tool", "input", nil, nil)
	require.NoError(t, err)
	strategyUC.AssertExpectations(t)
}

// TestSaveStrategyScript_StatusAlwaysTesting is Acceptance Criterion #3:
// Status is always entities.Testing, regardless of any status-like field
// the model attempts to smuggle in - saveStrategyArgs has no status field
// at all, so this also proves the model has no channel to request it.
func TestSaveStrategyScript_StatusAlwaysTesting(t *testing.T) {
	uc, strategyUC := newAgentUseCaseForGateTest(t, map[string]any{
		"name":          "Sneaky Strategy",
		"description":   "desc",
		"script_source": "function GoLong() end",
		"symbols":       []string{"BTCUSDT"},
		"cycle_minutes": 15,
		"mode":          "backtest",
		// Smuggled fields with no corresponding struct tag in
		// saveStrategyArgs - json.Unmarshal silently ignores unknown keys,
		// which is exactly the point: there is no channel for these.
		"status": "productive",
	})
	strategyUC.On("Save", mock.Anything, mock.MatchedBy(func(s entities.Strategy) bool {
		return s.Status == entities.Testing
	})).Return(entities.Strategy{ID: 5, Status: entities.Testing}, nil)

	_, err := uc.RunToolLoop(context.Background(), "mcp_tool", "input", nil, nil)
	require.NoError(t, err)
	strategyUC.AssertExpectations(t)
	strategyUC.AssertNotCalled(t, "Save", mock.Anything, mock.MatchedBy(func(s entities.Strategy) bool {
		return s.Status == entities.Productive
	}))
}

// TestSaveStrategyScript_UpdateAlsoClampsMode proves the gate applies on
// the Update path (existing StrategyID) exactly as it does on Save (create)
// - an agent iterating on a draft strategy cannot "upgrade" it to live by
// switching from create to update.
func TestSaveStrategyScript_UpdateAlsoClampsMode(t *testing.T) {
	uc, strategyUC := newAgentUseCaseForGateTest(t, map[string]any{
		"strategy_id":   7,
		"name":          "Existing Strategy",
		"description":   "desc",
		"script_source": "function GoLong() end",
		"symbols":       []string{"BTCUSDT"},
		"cycle_minutes": 15,
		"mode":          "live",
	})
	strategyUC.On("Update", mock.Anything, mock.MatchedBy(func(s entities.Strategy) bool {
		return s.ID == 7 && s.Mode == "dryrun" && s.Status == entities.Testing
	})).Return(nil)

	_, err := uc.RunToolLoop(context.Background(), "mcp_tool", "input", nil, nil)
	require.NoError(t, err)
	strategyUC.AssertExpectations(t)
}

// TestBuildToolRegistry_NoLiveCapableTool is Acceptance Criterion #9: audit
// every tool definition name exposed to the model - there is no tool that
// could ever set Status=Productive, place a real order, or touch
// Testnet/exchange credentials. This drives the loop with a benign
// list_strategies call and inspects which tool names the model was ever
// offered, via the CompletionRequest captured by the ModelProvider mock.
func TestBuildToolRegistry_NoLiveCapableTool(t *testing.T) {
	model := &mockModelProvider{}
	repo := &mockAgentRepository{}
	strategyUC := &mockStrategyUseCase{}
	backtestUC := &mockBacktestUseCase{}
	signalUC := &mockSignalUseCase{}
	snapshotUC := &mockSnapshotUseCase{}

	repo.On("GetInstruction", mock.Anything).Return(entities.AgentInstruction{}, nil)
	repo.On("CreateRun", mock.Anything, mock.Anything).Return(entities.AgentRun{ID: 1}, nil)
	repo.On("UpdateRun", mock.Anything, mock.Anything).Return(nil)

	var capturedToolNames []string
	model.On("Complete", mock.Anything, mock.MatchedBy(func(req modelprovider.CompletionRequest) bool {
		for _, tool := range req.Tools {
			capturedToolNames = append(capturedToolNames, tool.Name)
		}
		return true
	})).Return(modelprovider.CompletionResult{StopReason: "end_turn", Text: "done"}, nil).Once()

	uc := agentusecase.NewAgentUseCase(model, repo, strategyUC, backtestUC, signalUC, snapshotUC)
	_, err := uc.RunToolLoop(context.Background(), "mcp_tool", "what can you do?", nil, nil)
	require.NoError(t, err)

	require.NotEmpty(t, capturedToolNames)
	disallowedSubstrings := []string{"live", "order", "place_order", "testnet", "credential", "productive", "enable", "activate"}
	for _, name := range capturedToolNames {
		for _, bad := range disallowedSubstrings {
			require.NotContainsf(t, name, bad, "tool %q must not exist in the registry - no live-capable tool may be offered to the model", name)
		}
	}
	// The one write-capable tool that exists must be exactly this one -
	// absence of everything else, not a runtime check, is the guarantee.
	require.Contains(t, capturedToolNames, "save_strategy_script")
}
