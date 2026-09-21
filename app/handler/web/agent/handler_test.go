package agent_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go-trade-bot/app/entities"
	handler "go-trade-bot/app/handler/web/agent"
	agentusecase "go-trade-bot/app/usecase/agent"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
)

type mockUseCase struct {
	run entities.AgentRun
	err error

	gotUserInput       string
	gotHistory         []agentusecase.PriorTurn
	gotKnownStrategyID *uint
}

func (m *mockUseCase) RunToolLoop(ctx context.Context, trigger, userInput string, history []agentusecase.PriorTurn, knownStrategyID *uint) (entities.AgentRun, error) {
	m.gotUserInput = userInput
	m.gotHistory = history
	m.gotKnownStrategyID = knownStrategyID
	return m.run, m.err
}

type mockRepository struct {
	runs       []entities.AgentRun
	run        entities.AgentRun
	err        error
	gotLimit   int
	gotStratID *uint
}

func (m *mockRepository) GetRun(ctx context.Context, id uint) (entities.AgentRun, error) {
	return m.run, m.err
}

func (m *mockRepository) ListRuns(ctx context.Context, limit int, strategyID *uint) ([]entities.AgentRun, error) {
	m.gotLimit = limit
	m.gotStratID = strategyID
	return m.runs, m.err
}

func newRouter(uc handler.UseCase, repo handler.Repository) *mux.Router {
	h := handler.NewAgentHandler(uc, repo)
	router := mux.NewRouter()
	for _, cfg := range h.Handlers() {
		router.HandleFunc(cfg.Pattern, cfg.Action).Methods(cfg.Method)
	}
	return router
}

func TestSendMessage_RendersToolCalls(t *testing.T) {
	strategyID := uint(7)
	toolCalls := []byte(`[{"tool":"save_strategy_script","args":{"name":"x"},"result":"strategy_id=7 name=\"x\"","timestamp":"2026-01-01T00:00:00Z"}]`)
	uc := &mockUseCase{run: entities.AgentRun{
		ID:            1,
		Provider:      "anthropic",
		Model:         "claude-sonnet-5",
		Trigger:       "chat_ui",
		Status:        entities.AgentRunOK,
		InputSummary:  "build me a strategy",
		ToolCallsJSON: datatypes.JSON(toolCalls),
		StrategyID:    &strategyID,
		StartedAt:     time.Now(),
		FinishedAt:    time.Now(),
	}}
	repo := &mockRepository{}
	router := newRouter(uc, repo)

	body, _ := json.Marshal(map[string]string{"input": "build me a strategy"})
	req := httptest.NewRequest(http.MethodPost, "/agent/runs", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	var resp handler.AgentRunResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.Equal(t, "ok", resp.Status)
	require.Len(t, resp.ToolCalls, 1)
	assert.Equal(t, "save_strategy_script", resp.ToolCalls[0].Tool)
	require.NotNil(t, resp.StrategyID)
	assert.Equal(t, uint(7), *resp.StrategyID)
}

// TestSendMessage_ThreadsHistoryIntoUseCase is a regression test: every
// chat message previously started a brand-new, context-free RunToolLoop
// call - the agent had no memory of anything discussed or drafted in an
// earlier message of the same session, which made any real back-and-forth
// ("draft this script" then "now tighten the stop loss") impossible. The
// widget now sends the prior turns of the current session as `history` on
// every request; this asserts the handler actually decodes and forwards
// that history to RunToolLoop rather than silently dropping it.
func TestSendMessage_ThreadsHistoryIntoUseCase(t *testing.T) {
	uc := &mockUseCase{run: entities.AgentRun{ID: 1, Status: entities.AgentRunOK}}
	router := newRouter(uc, &mockRepository{})

	body, _ := json.Marshal(map[string]any{
		"input": "now tighten the stop loss to 1%",
		"history": []map[string]any{
			{
				"input": "draft an EMA crossover strategy for BTCUSDT",
				"tool_calls": []map[string]any{
					{"tool": "save_strategy_script", "args": map[string]any{"name": "EMA Cross"}, "result": "strategy_id=9 name=\"EMA Cross\""},
				},
				"response_text": "I've drafted an EMA crossover strategy as strategy #9.",
			},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/agent/runs", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.Len(t, uc.gotHistory, 1, "the prior turn must reach RunToolLoop, not be dropped")
	assert.Equal(t, "draft an EMA crossover strategy for BTCUSDT", uc.gotHistory[0].Input)
	require.Len(t, uc.gotHistory[0].ToolCalls, 1)
	assert.Equal(t, "save_strategy_script", uc.gotHistory[0].ToolCalls[0].Tool)
	assert.Equal(t, "I've drafted an EMA crossover strategy as strategy #9.", uc.gotHistory[0].ResponseText)
	assert.Contains(t, uc.gotUserInput, "now tighten the stop loss to 1%")
}

// TestSendMessage_StrategyScopedTurnLoadsHistoryFromDatabase verifies the
// per-strategy session behavior: once a strategy exists, memory belongs to
// IT, durably, in the database - not to whatever the browser tab's
// in-memory transcript happens to still hold. This asserts the client's
// own (here: empty, simulating a fresh reload/new tab) history is ignored
// in favor of the strategy's real prior turns loaded via
// repository.ListRuns, and that the strategy-context marker prefix is
// stripped back off before replay (so it doesn't compound turn after turn).
func TestSendMessage_StrategyScopedTurnLoadsHistoryFromDatabase(t *testing.T) {
	strategyID := uint(5)
	priorToolCalls := []byte(`[{"tool":"save_strategy_script","args":{"name":"EMA Cross"},"result":"strategy_id=5 name=\"EMA Cross\""}]`)
	repo := &mockRepository{
		runs: []entities.AgentRun{
			{
				ID:            9,
				Status:        entities.AgentRunOK,
				InputSummary:  "[Context: the operator currently has strategy #5 open in the workbench editor. Assume questions refer to it unless stated otherwise.]\n\ndraft an EMA crossover strategy",
				ToolCallsJSON: datatypes.JSON(priorToolCalls),
				ResponseText:  "I've drafted an EMA crossover strategy as strategy #5.",
				StrategyID:    &strategyID,
			},
		},
	}
	uc := &mockUseCase{run: entities.AgentRun{ID: 10, Status: entities.AgentRunOK}}
	router := newRouter(uc, repo)

	// Simulate a fresh tab/reload: the client has NO memory of the prior
	// turn at all (empty history) - this must not matter, since the
	// strategy_id hint makes the database the authoritative source.
	body, _ := json.Marshal(map[string]any{
		"input":       "now tighten the stop loss to 1%",
		"strategy_id": strategyID,
	})
	req := httptest.NewRequest(http.MethodPost, "/agent/runs", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.Equal(t, uint(20), uint(repo.gotLimit))
	require.NotNil(t, repo.gotStratID)
	assert.Equal(t, strategyID, *repo.gotStratID)

	require.Len(t, uc.gotHistory, 1, "the strategy's real prior turn must be loaded from the DB even though the client sent no history")
	assert.Equal(t, "draft an EMA crossover strategy", uc.gotHistory[0].Input, "the strategy-context marker prefix must be stripped before replay")
	require.Len(t, uc.gotHistory[0].ToolCalls, 1)
	assert.Equal(t, "save_strategy_script", uc.gotHistory[0].ToolCalls[0].Tool)
	assert.Equal(t, "I've drafted an EMA crossover strategy as strategy #5.", uc.gotHistory[0].ResponseText)

	require.NotNil(t, uc.gotKnownStrategyID, "the strategy_id hint must reach RunToolLoop so the new turn's own AgentRun row gets tagged with it too")
	assert.Equal(t, strategyID, *uc.gotKnownStrategyID)
}

// TestSendMessage_KnownStrategyIDPropagatesEvenWithoutToolCall is a
// regression test for a real bug found manually: a strategy-scoped turn
// that answers purely from replayed memory (no tool call at all - e.g.
// "what symbol did we use again?") previously persisted with
// StrategyID == nil, because StrategyID was ONLY ever set opportunistically
// from a tool call's own result. Since the DB-backed history lookup
// filters strictly on strategy_id, that turn would then be silently
// invisible to every future history replay for the same strategy - a
// compounding memory gap that only surfaces three or more turns into a
// conversation. This asserts the strategy_id hint reaches RunToolLoop
// regardless of whether the turn ends up calling any tool.
func TestSendMessage_KnownStrategyIDPropagatesEvenWithoutToolCall(t *testing.T) {
	strategyID := uint(5)
	uc := &mockUseCase{run: entities.AgentRun{ID: 11, Status: entities.AgentRunOK, StrategyID: &strategyID}}
	router := newRouter(uc, &mockRepository{})

	body, _ := json.Marshal(map[string]any{
		"input":       "what symbol did we use again?",
		"strategy_id": strategyID,
	})
	req := httptest.NewRequest(http.MethodPost, "/agent/runs", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.NotNil(t, uc.gotKnownStrategyID)
	assert.Equal(t, strategyID, *uc.gotKnownStrategyID)
}

func TestSendMessage_EmptyInputIsBadRequest(t *testing.T) {
	router := newRouter(&mockUseCase{}, &mockRepository{})

	body, _ := json.Marshal(map[string]string{"input": ""})
	req := httptest.NewRequest(http.MethodPost, "/agent/runs", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

// TestSendMessage_ErrorRunStillReturnsOK covers the AC#4 requirement (the
// chat panel must surface a failed run's error_message visibly, not
// silently) - a run that finished with status "error" is not itself an
// HTTP failure, since the AgentRun row was persisted and has content the
// operator needs to see.
func TestSendMessage_ErrorRunStillReturnsOK(t *testing.T) {
	uc := &mockUseCase{
		run: entities.AgentRun{ID: 2, Status: entities.AgentRunError, ErrorMessage: "model completion failed: no provider configured"},
		err: assert.AnError,
	}
	router := newRouter(uc, &mockRepository{})

	body, _ := json.Marshal(map[string]string{"input": "hello"})
	req := httptest.NewRequest(http.MethodPost, "/agent/runs", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	var resp handler.AgentRunResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.Equal(t, "error", resp.Status)
	assert.NotEmpty(t, resp.ErrorMessage)
}

func TestListRuns_FiltersByStrategyID(t *testing.T) {
	repo := &mockRepository{runs: []entities.AgentRun{{ID: 1}}}
	router := newRouter(&mockUseCase{}, repo)

	req := httptest.NewRequest(http.MethodGet, "/agent/runs?strategy_id=9&limit=5", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.NotNil(t, repo.gotStratID)
	assert.Equal(t, uint(9), *repo.gotStratID)
	assert.Equal(t, 5, repo.gotLimit)
}

func TestListRuns_DefaultsWithNoFilter(t *testing.T) {
	repo := &mockRepository{runs: []entities.AgentRun{}}
	router := newRouter(&mockUseCase{}, repo)

	req := httptest.NewRequest(http.MethodGet, "/agent/runs", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	assert.Nil(t, repo.gotStratID)
	assert.Equal(t, 20, repo.gotLimit)
}

func TestGetRun_NotFound(t *testing.T) {
	repo := &mockRepository{err: assert.AnError}
	router := newRouter(&mockUseCase{}, repo)

	req := httptest.NewRequest(http.MethodGet, "/agent/runs/42", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
}
