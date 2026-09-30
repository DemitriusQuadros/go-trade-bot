package agent_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"go-trade-bot/app/entities"
	agentusecase "go-trade-bot/app/usecase/agent"
	"go-trade-bot/internal/lock"
	"go-trade-bot/internal/modelprovider"
	"go-trade-bot/internal/report/agentreport"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type platformHarness struct {
	uc       *agentusecase.AgentUseCase
	model    *mockModelProvider
	repo     *fakeRunRepo
	strategy *fakeStrategies
	platform *fakePlatform
	settings *fakeSettings
	notifier *recordingNotifier
	def      entities.Agent
}

func newPlatformHarness(t *testing.T) *platformHarness {
	t.Helper()
	model := &mockModelProvider{}
	repo := newFakeRunRepo()
	strategyUC := &fakeStrategies{overrides: map[uint]entities.Strategy{}}
	uc := agentusecase.NewAgentUseCase(model, repo, strategyUC, &mockBacktestUseCase{}, &mockSignalUseCase{}, &mockSnapshotUseCase{})

	platform := newFakePlatform()
	def, _ := platform.EnsureDefaultAgent(context.Background())
	settings := &fakeSettings{}
	rec := &recordingNotifier{}

	uc.Platform = platform
	uc.Notifier = rec
	uc.Reports = agentreport.NewHTMLRenderer(staticData{})
	uc.Guard = agentusecase.NewDefaultGuard(settings, platform, rec)
	uc.APIBaseURL = "http://bot.local:8080/"

	return &platformHarness{uc: uc, model: model, repo: repo, strategy: strategyUC, platform: platform, settings: settings, notifier: rec, def: def}
}

func toolNames(req modelprovider.CompletionRequest) []string {
	var out []string
	for _, t := range req.Tools {
		out = append(out, t.Name)
	}
	return out
}

// A-01 AC#2: a read-only persona is offered only read tools + the always-
// granted memory/report tools; a save_strategy_script call is NOT executed
// and an error result is fed back.
func TestRun_PermissionFilteredRegistry(t *testing.T) {
	h := newPlatformHarness(t)
	agent := h.platform.addAgent(entities.Agent{Name: "Reader", Permissions: []string{"read"}})

	var offered []string
	var secondCallMessages []modelprovider.Message
	h.model.On("Complete", mock.Anything, mock.MatchedBy(func(req modelprovider.CompletionRequest) bool {
		if offered == nil {
			offered = toolNames(req)
		}
		return true
	})).Return(modelprovider.CompletionResult{
		StopReason: "tool_use",
		ToolCalls: []modelprovider.ToolCall{{ID: "1", Name: "save_strategy_script", Args: rawArgs(map[string]any{
			"strategy_id": 3, "name": "x", "description": "x", "script_source": "x", "symbols": []string{"BTCUSDT"}, "cycle_minutes": 5,
		})}},
	}, nil).Once()
	h.model.On("Complete", mock.Anything, mock.MatchedBy(func(req modelprovider.CompletionRequest) bool {
		secondCallMessages = req.Messages
		return true
	})).Return(modelprovider.CompletionResult{StopReason: "end_turn", Text: "ok"}, nil).Once()

	run, err := h.uc.Run(context.Background(), agentusecase.RunRequest{Agent: agent, Trigger: "manual", UserInput: "go"})
	require.NoError(t, err)
	assert.Equal(t, entities.AgentRunOK, run.Status)

	assert.ElementsMatch(t, []string{
		"list_strategies", "get_strategy", "list_backtests", "get_backtest", "get_open_positions", "get_performance_snapshots",
		"get_candle_coverage", // fix-02 B2, read
		"get_scheduler_health",
		"list_optimizations", "get_optimization_results",
		"read_memory", "write_journal", "list_reports", "write_report",
		"list_proposals", "get_deploy_gate_config", // Phase B, always granted (read-only)
	}, offered)

	assert.Zero(t, h.strategy.writes())
	last := secondCallMessages[len(secondCallMessages)-1].Content
	assert.Contains(t, last, "not available to agent")
	assert.Contains(t, last, "edit_testing")
}

// A-01 AC#3: the kill switch flipped between two iterations halts the run
// before the next model call.
func TestRun_KillSwitchHaltsInFlightRunBeforeNextModelCall(t *testing.T) {
	h := newPlatformHarness(t)

	h.model.On("Complete", mock.Anything, mock.Anything).Run(func(mock.Arguments) {
		h.settings.set(true) // operator hits the kill switch mid-run
	}).Return(modelprovider.CompletionResult{
		StopReason: "tool_use",
		ToolCalls:  []modelprovider.ToolCall{{ID: "1", Name: "list_strategies", Args: rawArgs(map[string]any{})}},
	}, nil).Once()

	run, err := h.uc.Run(context.Background(), agentusecase.RunRequest{Agent: h.def, Trigger: "chat_ui", UserInput: "hi"})
	require.Error(t, err)
	assert.Equal(t, entities.AgentRunError, run.Status)
	assert.True(t, strings.HasPrefix(run.ErrorMessage, "halted:"), run.ErrorMessage)
	h.model.AssertNumberOfCalls(t, "Complete", 1)
}

// A-01 AC#4: an exhausted budget stops the run before any model call and
// sends exactly one critical notification per day.
func TestRun_BudgetExhaustedNoModelCallAndOneCriticalNotification(t *testing.T) {
	h := newPlatformHarness(t)
	target, _ := h.platform.CreateWebhookTarget(context.Background(), entities.WebhookTarget{Kind: "generic", URL: "http://x", Enabled: true})
	agent := h.platform.addAgent(entities.Agent{Name: "Spender", Permissions: []string{"read"}, DailyBudgetUSD: 1, WebhookTargetIDs: []uint{target.ID}})
	require.NoError(t, h.platform.AddUsage(context.Background(), agent.ID, time.Now(), 0, 0, 1.01))

	for i := 0; i < 3; i++ {
		run, err := h.uc.Run(context.Background(), agentusecase.RunRequest{Agent: agent, Trigger: "cron", UserInput: "evaluate"})
		require.Error(t, err)
		assert.Contains(t, run.ErrorMessage, "halted: daily budget")
	}
	h.model.AssertNotCalled(t, "Complete", mock.Anything, mock.Anything)
	require.Equal(t, 1, h.notifier.count())
	assert.Equal(t, "critical", h.notifier.calls[0].Severity)

	// A guard shared by another process (fresh map) still dedupes via the
	// AgentUsage flag.
	other := agentusecase.NewDefaultGuard(h.settings, h.platform, h.notifier)
	assert.Error(t, other.Check(context.Background(), agent))
	assert.Equal(t, 1, h.notifier.count())
}

func TestRun_PausedAgentHalts(t *testing.T) {
	h := newPlatformHarness(t)
	agent := h.platform.addAgent(entities.Agent{Name: "Sleepy", Paused: true})
	run, err := h.uc.Run(context.Background(), agentusecase.RunRequest{Agent: agent, Trigger: "manual", UserInput: "x"})
	require.Error(t, err)
	assert.Equal(t, `halted: agent "Sleepy" is paused`, run.ErrorMessage)
	assert.True(t, agentusecase.IsHalt(h.uc.CheckGuard(context.Background(), agent)))
}

// A-01 AC#5: usage is accounted on the run and today's AgentUsage.
func TestRun_UsageAccounting(t *testing.T) {
	h := newPlatformHarness(t)
	h.uc.Provider, h.uc.ModelName = "anthropic", "claude-sonnet-5"
	h.model.On("Complete", mock.Anything, mock.Anything).Return(modelprovider.CompletionResult{
		StopReason: "end_turn", Text: "done", Usage: modelprovider.Usage{InputTokens: 1000, OutputTokens: 500},
	}, nil).Once()

	run, err := h.uc.Run(context.Background(), agentusecase.RunRequest{Agent: h.def, Trigger: "chat_ui", UserInput: "hi"})
	require.NoError(t, err)
	assert.Equal(t, int64(1000), run.InputTokens)
	assert.Equal(t, int64(500), run.OutputTokens)
	assert.Greater(t, run.CostUSD, 0.0)
	require.NotNil(t, run.AgentID)
	assert.Equal(t, h.def.ID, *run.AgentID)

	u, _ := h.platform.GetUsage(context.Background(), h.def.ID, time.Now())
	assert.Equal(t, int64(1000), u.InputTokens)
	assert.Equal(t, int64(500), u.OutputTokens)
	assert.InDelta(t, run.CostUSD, u.CostUSD, 1e-12)
	assert.Equal(t, 1, u.Runs)
}

// Phase D follow-up: chat turns are no longer written to shared strategy
// memory (the chat has its own transcript). Deliberate notes still reach the
// next run's system prompt; legacy chat rows no longer do.
func TestRun_ChatDoesNotWriteMemoryAndPromptShowsOnlyNotes(t *testing.T) {
	h := newPlatformHarness(t)
	sid := uint(3)
	h.model.On("Complete", mock.Anything, mock.Anything).Return(modelprovider.CompletionResult{
		StopReason: "end_turn", Text: "RSI period 14 looks too short.",
	}, nil).Once()

	_, err := h.uc.Run(context.Background(), agentusecase.RunRequest{
		Agent: h.def, Trigger: "chat_ui", UserInput: "[Context: marker]\n\nwhat about the RSI period?",
		DisplayInput: "what about the RSI period?", StrategyID: &sid,
	})
	require.NoError(t, err)
	assert.Empty(t, h.platform.memoryFor(3, entities.MemoryChatUser))
	assert.Empty(t, h.platform.memoryFor(3, entities.MemoryChatAgent))

	// A legacy chat row plus an operator note ("Save to notes" writes a journal entry).
	_, _ = h.platform.AppendMemory(context.Background(), entities.StrategyMemoryEntry{StrategyID: sid, Kind: entities.MemoryChatUser, Content: "legacy chat line"})
	_, _ = h.platform.AppendMemory(context.Background(), entities.StrategyMemoryEntry{StrategyID: sid, Kind: entities.MemoryJournal, Content: "operator: keep RSI at 14"})

	other := h.platform.addAgent(entities.Agent{Name: "Reviewer", Permissions: []string{"read"}})
	var system string
	h.model.On("Complete", mock.Anything, mock.MatchedBy(func(req modelprovider.CompletionRequest) bool {
		system = req.System
		return true
	})).Return(modelprovider.CompletionResult{StopReason: "end_turn", Text: "noted"}, nil).Once()
	_, err = h.uc.Run(context.Background(), agentusecase.RunRequest{Agent: other, Trigger: "chat_ui", UserInput: "summary?", StrategyID: &sid})
	require.NoError(t, err)

	assert.Contains(t, system, "operator: keep RSI at 14")
	assert.NotContains(t, system, "legacy chat line")
	assert.NotContains(t, system, "RSI period 14 looks too short.")
	assert.Contains(t, system, `You are the agent "Reviewer".`)
	assert.Less(t, strings.Index(system, "house rules"), strings.Index(system, `You are the agent`))
}

// Cron/manual runs get every bound strategy's memory and binding list.
func TestRun_ScheduledRunSeesAllBoundStrategies(t *testing.T) {
	h := newPlatformHarness(t)
	agent := h.platform.addAgent(entities.Agent{Name: "Watcher", Goal: "Watch drawdowns.", Permissions: []string{"read"}})
	require.NoError(t, h.platform.SetBindings(context.Background(), agent.ID, []uint{3, 4}))
	_, _ = h.platform.AppendMemory(context.Background(), entities.StrategyMemoryEntry{StrategyID: 4, Kind: entities.MemoryFinding, Content: "strategy four finding"})

	var system string
	h.model.On("Complete", mock.Anything, mock.MatchedBy(func(req modelprovider.CompletionRequest) bool {
		system = req.System
		return true
	})).Return(modelprovider.CompletionResult{StopReason: "end_turn", Text: "ok"}, nil).Once()

	_, err := h.uc.Run(context.Background(), agentusecase.RunRequest{Agent: agent, Trigger: "cron", UserInput: agentusecase.BuildScheduledInput("")})
	require.NoError(t, err)
	assert.Contains(t, system, "Your goal:\nWatch drawdowns.")
	assert.Contains(t, system, "strategy_id=3")
	assert.Contains(t, system, "strategy_id=4")
	assert.Contains(t, system, "strategy four finding")
	assert.Contains(t, system, "live/productive strategies can never be modified")
}

func writeReportCall(t *testing.T, blocks any, strategyIDs []uint) modelprovider.ToolCall {
	t.Helper()
	return modelprovider.ToolCall{ID: "r", Name: "write_report", Args: rawArgs(map[string]any{
		"title": "Weekly", "severity": "warning", "strategy_ids": strategyIDs, "blocks": blocks,
	})}
}

// A-01 AC#9 + AC#6 (through the tool): one report row, one report_ref
// memory entry per strategy, url returned, KPI values from the DataSource.
func TestWriteReport_PersistsReportAndReportRefs(t *testing.T) {
	h := newPlatformHarness(t)
	blocks := []map[string]any{
		{"type": "summary", "data": map[string]any{"text": "All quiet."}},
		{"type": "kpi_grid", "data": map[string]any{"source": map[string]any{"kind": "backtest_run", "id": 7}, "metrics": []string{"sharpe"}, "sharpe": 42.42}},
	}
	var toolResult string
	h.model.On("Complete", mock.Anything, mock.Anything).Return(modelprovider.CompletionResult{
		StopReason: "tool_use", ToolCalls: []modelprovider.ToolCall{writeReportCall(t, blocks, []uint{3, 4})},
	}, nil).Once()
	h.model.On("Complete", mock.Anything, mock.MatchedBy(func(req modelprovider.CompletionRequest) bool {
		toolResult = req.Messages[len(req.Messages)-1].Content
		return true
	})).Return(modelprovider.CompletionResult{StopReason: "end_turn", Text: "reported"}, nil).Once()

	run, err := h.uc.Run(context.Background(), agentusecase.RunRequest{Agent: h.def, Trigger: "manual", UserInput: "report"})
	require.NoError(t, err)

	require.Len(t, h.platform.reports, 1)
	rep := h.platform.reports[0]
	assert.Equal(t, "All quiet.", rep.Summary)
	assert.Equal(t, run.ID, rep.AgentRunID)
	assert.Equal(t, h.def.ID, rep.AgentID)
	assert.Contains(t, rep.RenderedHTML, ">1.50<")
	assert.NotContains(t, rep.RenderedHTML, "42.42")
	assert.Len(t, h.platform.memoryFor(3, entities.MemoryReportRef), 1)
	assert.Len(t, h.platform.memoryFor(4, entities.MemoryReportRef), 1)
	assert.Equal(t, rep.ID, *h.platform.memoryFor(3, entities.MemoryReportRef)[0].RefID)

	var out struct {
		ReportID uint   `json:"report_id"`
		URL      string `json:"url"`
	}
	require.NoError(t, json.Unmarshal([]byte(strings.SplitN(toolResult, "\n", 2)[1]), &out))
	assert.Equal(t, rep.ID, out.ReportID)
	assert.Equal(t, "http://bot.local:8080/agents/reports/"+uintStr(rep.ID), out.URL)
}

func uintStr(v uint) string { b, _ := json.Marshal(v); return string(b) }

// A-01 AC#8: an unknown block type returns an error listing allowed types
// and persists nothing.
func TestWriteReport_UnknownBlockTypePersistsNothing(t *testing.T) {
	h := newPlatformHarness(t)
	var toolResult string
	h.model.On("Complete", mock.Anything, mock.Anything).Return(modelprovider.CompletionResult{
		StopReason: "tool_use", ToolCalls: []modelprovider.ToolCall{writeReportCall(t, []map[string]any{{"type": "raw_html", "data": map[string]any{"html": "<b>"}}}, []uint{3})},
	}, nil).Once()
	h.model.On("Complete", mock.Anything, mock.MatchedBy(func(req modelprovider.CompletionRequest) bool {
		toolResult = req.Messages[len(req.Messages)-1].Content
		return true
	})).Return(modelprovider.CompletionResult{StopReason: "end_turn", Text: "failed"}, nil).Once()

	_, err := h.uc.Run(context.Background(), agentusecase.RunRequest{Agent: h.def, Trigger: "manual", UserInput: "report"})
	require.NoError(t, err)
	assert.Contains(t, toolResult, "unknown block type")
	for _, typ := range agentreport.AllowedTypes {
		assert.Contains(t, toolResult, typ)
	}
	assert.Empty(t, h.platform.reports)
	assert.Empty(t, h.platform.memoryFor(3, entities.MemoryReportRef))
}

func TestWriteJournalAndReadMemory(t *testing.T) {
	h := newPlatformHarness(t)
	agent := h.platform.addAgent(entities.Agent{Name: "Journaler"})
	var results []string
	h.model.On("Complete", mock.Anything, mock.Anything).Return(modelprovider.CompletionResult{
		StopReason: "tool_use", ToolCalls: []modelprovider.ToolCall{
			{ID: "1", Name: "write_journal", Args: rawArgs(map[string]any{"strategy_id": 3, "content": "entry signal lags", "kind": "finding"})},
			{ID: "2", Name: "write_journal", Args: rawArgs(map[string]any{"strategy_id": 150, "content": "nope"})},
			{ID: "3", Name: "read_memory", Args: rawArgs(map[string]any{"strategy_id": 3})},
		},
	}, nil).Once()
	h.model.On("Complete", mock.Anything, mock.MatchedBy(func(req modelprovider.CompletionRequest) bool {
		for _, m := range req.Messages {
			if strings.HasPrefix(m.Content, "[tool_result") {
				results = append(results, m.Content)
			}
		}
		return true
	})).Return(modelprovider.CompletionResult{StopReason: "end_turn", Text: "ok"}, nil).Once()

	run, err := h.uc.Run(context.Background(), agentusecase.RunRequest{Agent: agent, Trigger: "manual", UserInput: "x"})
	require.NoError(t, err)
	require.Len(t, results, 3)
	assert.Contains(t, results[1], "strategy 150 does not exist")
	assert.Contains(t, results[2], `"author":"Journaler"`)
	assert.Contains(t, results[2], "entry signal lags")

	findings := h.platform.memoryFor(3, entities.MemoryFinding)
	require.Len(t, findings, 1)
	assert.Equal(t, run.ID, *findings[0].AgentRunID)
}

func TestNotify_RequiresPermissionAndIsRateLimited(t *testing.T) {
	h := newPlatformHarness(t)
	target, _ := h.platform.CreateWebhookTarget(context.Background(), entities.WebhookTarget{Kind: "generic", URL: "http://x", Enabled: true})
	agent := h.platform.addAgent(entities.Agent{Name: "Notifier", Permissions: []string{"notify"}, WebhookTargetIDs: []uint{target.ID}})

	calls := make([]modelprovider.ToolCall, 12)
	for i := range calls {
		calls[i] = modelprovider.ToolCall{ID: "n", Name: "notify", Args: rawArgs(map[string]any{"severity": "info", "message": "hello"})}
	}
	var last string
	h.model.On("Complete", mock.Anything, mock.Anything).Return(modelprovider.CompletionResult{StopReason: "tool_use", ToolCalls: calls}, nil).Once()
	h.model.On("Complete", mock.Anything, mock.MatchedBy(func(req modelprovider.CompletionRequest) bool {
		last = req.Messages[len(req.Messages)-1].Content
		return true
	})).Return(modelprovider.CompletionResult{StopReason: "end_turn", Text: "ok"}, nil).Once()

	_, err := h.uc.Run(context.Background(), agentusecase.RunRequest{Agent: agent, Trigger: "manual", UserInput: "x"})
	require.NoError(t, err)
	assert.Equal(t, 10, h.notifier.count())
	assert.Contains(t, last, "rate limit")
}

// Scheduled (unattended) runs may not edit productive/live strategies.
func TestSaveStrategyScript_NoTriggerCanEditLiveOrProductiveStrategy(t *testing.T) {
	cases := []struct {
		name     string
		strategy entities.Strategy
		trigger  string
	}{
		{"productive via chat", entities.Strategy{ID: 8, Status: entities.Productive, Mode: "dryrun"}, "chat_ui"},
		{"live mode via chat", entities.Strategy{ID: 8, Status: entities.Testing, Mode: "live"}, "chat_ui"},
		{"live+productive via cron", entities.Strategy{ID: 8, Status: entities.Productive, Mode: "live"}, "cron"},
		{"live via mcp", entities.Strategy{ID: 8, Status: entities.Testing, Mode: "live"}, "mcp_tool"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newPlatformHarness(t)
			h.strategy.overrides[8] = tc.strategy

			var last string
			h.model.On("Complete", mock.Anything, mock.Anything).Return(modelprovider.CompletionResult{
				StopReason: "tool_use", ToolCalls: []modelprovider.ToolCall{{ID: "1", Name: "save_strategy_script", Args: rawArgs(map[string]any{
					"strategy_id": 8, "name": "x", "description": "x", "script_source": "x", "symbols": []string{"BTCUSDT"}, "cycle_minutes": 5,
				})}},
			}, nil).Once()
			h.model.On("Complete", mock.Anything, mock.MatchedBy(func(req modelprovider.CompletionRequest) bool {
				last = req.Messages[len(req.Messages)-1].Content
				return true
			})).Return(modelprovider.CompletionResult{StopReason: "end_turn", Text: "ok"}, nil).Once()

			_, err := h.uc.Run(context.Background(), agentusecase.RunRequest{Agent: h.def, Trigger: tc.trigger, UserInput: "x"})
			require.NoError(t, err)
			assert.Contains(t, last, "agents may only edit non-productive, non-live strategies")
			assert.Zero(t, h.strategy.writes())
		})
	}
}

// A-02 AC#8: two concurrent runs editing strategy 5 - exactly one writes,
// the other gets the lock error result.
func TestSaveStrategyScript_WriterLockOneWinner(t *testing.T) {
	h := newPlatformHarness(t)
	h.uc.Lock = lock.NewMemoryStrategyLock()
	// Phase B: save_strategy_script only updates in-scope backtest drafts.
	h.strategy.overrides[5] = entities.Strategy{ID: 5, Name: "Draft", Status: entities.Testing, Mode: "backtest"}
	require.NoError(t, h.platform.SetBindings(context.Background(), h.def.ID, []uint{5}))

	release := make(chan struct{})
	updating := make(chan struct{})
	var once sync.Once
	h.strategy.onUpdate = func() {
		once.Do(func() {
			close(updating)
			<-release // hold run A inside its write until run B has finished
		})
	}

	saveArgs := rawArgs(map[string]any{"strategy_id": 5, "name": "x", "description": "x", "script_source": "x", "symbols": []string{"BTCUSDT"}, "cycle_minutes": 5})
	modelA, modelB := &mockModelProvider{}, &mockModelProvider{}
	for _, m := range []*mockModelProvider{modelA, modelB} {
		m.On("Complete", mock.Anything, mock.Anything).Return(modelprovider.CompletionResult{
			StopReason: "tool_use", ToolCalls: []modelprovider.ToolCall{{ID: "1", Name: "save_strategy_script", Args: saveArgs}},
		}, nil).Once()
	}
	var bResult string
	modelA.On("Complete", mock.Anything, mock.Anything).Return(modelprovider.CompletionResult{StopReason: "end_turn", Text: "saved"}, nil).Once()
	modelB.On("Complete", mock.Anything, mock.MatchedBy(func(req modelprovider.CompletionRequest) bool {
		bResult = req.Messages[len(req.Messages)-1].Content
		return true
	})).Return(modelprovider.CompletionResult{StopReason: "end_turn", Text: "blocked"}, nil).Once()

	ucA, ucB := *h.uc, *h.uc
	ucA.Model, ucB.Model = modelA, modelB

	doneA := make(chan error, 1)
	go func() {
		_, err := ucA.Run(context.Background(), agentusecase.RunRequest{Agent: h.def, Trigger: "chat_ui", UserInput: "edit"})
		doneA <- err
	}()
	<-updating
	_, err := ucB.Run(context.Background(), agentusecase.RunRequest{Agent: h.def, Trigger: "chat_ui", UserInput: "edit"})
	require.NoError(t, err)
	close(release)
	require.NoError(t, <-doneA)

	assert.Contains(t, bResult, "strategy 5 is being edited by another agent run; try later")
	assert.Equal(t, 1, h.strategy.writes())

	// The lock is released when run A returns.
	ok, err := h.uc.Lock.Acquire(context.Background(), 5, "someone-else", time.Minute)
	require.NoError(t, err)
	assert.True(t, ok)
}

// MCP exposure (A-02 §5): notify is never exposed; permission-gated tools
// follow the default agent's permissions.
func TestTools_MCPExposureFollowsDefaultAgent(t *testing.T) {
	h := newPlatformHarness(t)
	names := func() []string {
		var out []string
		for _, tool := range h.uc.Tools() {
			out = append(out, tool.Def.Name)
		}
		return out
	}
	all := names()
	assert.NotContains(t, all, "notify")
	assert.Contains(t, all, "save_strategy_script")
	assert.Contains(t, all, "write_report")

	restricted := h.def
	restricted.Permissions = []string{"read"}
	h.platform.addAgent(restricted)
	got := names()
	assert.NotContains(t, got, "save_strategy_script")
	assert.NotContains(t, got, "run_backtest")
	assert.Contains(t, got, "list_strategies")
	assert.Contains(t, got, "write_journal")
}

// Persona provider/model override uses the ProviderFactory.
type fakeFactory struct {
	provider modelprovider.ModelProvider
	gotP     string
	gotM     string
}

func (f *fakeFactory) For(p, m string) (modelprovider.ModelProvider, error) {
	f.gotP, f.gotM = p, m
	return f.provider, nil
}

func TestRun_PersonaModelOverrideUsesFactory(t *testing.T) {
	h := newPlatformHarness(t)
	override := &mockModelProvider{}
	override.On("Complete", mock.Anything, mock.Anything).Return(modelprovider.CompletionResult{StopReason: "end_turn", Text: "hi"}, nil).Once()
	factory := &fakeFactory{provider: override}
	h.uc.Providers = factory
	agent := h.platform.addAgent(entities.Agent{Name: "Gem", Provider: "gemini", Model: "gemini-2.5-flash"})

	run, err := h.uc.Run(context.Background(), agentusecase.RunRequest{Agent: agent, Trigger: "manual", UserInput: "x"})
	require.NoError(t, err)
	assert.Equal(t, "gemini", factory.gotP)
	assert.Equal(t, "gemini-2.5-flash", factory.gotM)
	assert.Equal(t, "gemini", run.Provider)
	assert.Equal(t, "gemini-2.5-flash", run.Model)
	h.model.AssertNotCalled(t, "Complete", mock.Anything, mock.Anything)
}

// RunToolLoop uses the platform's default agent.
func TestRunToolLoop_UsesDefaultAgentFromPlatform(t *testing.T) {
	h := newPlatformHarness(t)
	h.model.On("Complete", mock.Anything, mock.Anything).Return(modelprovider.CompletionResult{StopReason: "end_turn", Text: "hi"}, nil).Once()
	run, err := h.uc.RunToolLoop(context.Background(), "chat_ui", "hello", nil, nil)
	require.NoError(t, err)
	require.NotNil(t, run.AgentID)
	assert.Equal(t, h.def.ID, *run.AgentID)
}

// Regression (E2E run #58): an unattended run with only edit_testing created
// a new dryrun strategy through save_strategy_script's create path, bypassing
// create_strategy's permission and daily cap. Chat drafting is unaffected.
func TestSaveStrategyScript_UnattendedCreateNeedsCreateStrategy(t *testing.T) {
	createArgs := rawArgs(map[string]any{"name": "scratch", "description": "x", "script_source": "x", "symbols": []string{"ETHUSDT"}, "cycle_minutes": 15})
	runCreate := func(h *platformHarness, agent entities.Agent, trigger string) string {
		var last string
		h.model.On("Complete", mock.Anything, mock.Anything).Return(modelprovider.CompletionResult{
			StopReason: "tool_use", ToolCalls: []modelprovider.ToolCall{{ID: "1", Name: "save_strategy_script", Args: createArgs}},
		}, nil).Once()
		h.model.On("Complete", mock.Anything, mock.MatchedBy(func(req modelprovider.CompletionRequest) bool {
			last = req.Messages[len(req.Messages)-1].Content
			return true
		})).Return(modelprovider.CompletionResult{StopReason: "end_turn", Text: "done"}, nil).Once()
		_, err := h.uc.Run(context.Background(), agentusecase.RunRequest{Agent: agent, Trigger: trigger, UserInput: "x"})
		require.NoError(t, err)
		return last
	}

	t.Run("cron without create_strategy is refused", func(t *testing.T) {
		h := newPlatformHarness(t)
		a := h.platform.addAgent(entities.Agent{Name: "Editor", Permissions: []string{"read", "edit_testing"}})
		last := runCreate(h, a, "cron")
		assert.Contains(t, last, "needs the create_strategy permission")
		assert.Zero(t, h.strategy.writes())
	})
	t.Run("cron with create_strategy creates, counts and binds", func(t *testing.T) {
		h := newPlatformHarness(t)
		a := h.platform.addAgent(entities.Agent{Name: "Creator", Permissions: []string{"read", "edit_testing", "create_strategy"}})
		runCreate(h, a, "cron")
		assert.Equal(t, 1, h.strategy.writes())
		bindings, _ := h.platform.ListBindingsByAgent(context.Background(), a.ID)
		require.Len(t, bindings, 1)
		assert.Equal(t, uint(50), bindings[0].StrategyID)
	})
	t.Run("chat drafting is unchanged", func(t *testing.T) {
		h := newPlatformHarness(t)
		a := h.platform.addAgent(entities.Agent{Name: "ChatEditor", Permissions: []string{"read", "edit_testing"}})
		runCreate(h, a, "chat_ui")
		assert.Equal(t, 1, h.strategy.writes())
	})
}

// Regression (E2E run #58): end_turn with no text and no tool call was
// recorded as a successful run. The model is nudged once to continue.
func TestRun_EmptyAnswerIsNudgedThenErrors(t *testing.T) {
	t.Run("nudge then answer", func(t *testing.T) {
		h := newPlatformHarness(t)
		h.model.On("Complete", mock.Anything, mock.Anything).Return(modelprovider.CompletionResult{StopReason: "end_turn"}, nil).Once()
		h.model.On("Complete", mock.Anything, mock.MatchedBy(func(req modelprovider.CompletionRequest) bool {
			return strings.Contains(req.Messages[len(req.Messages)-1].Content, "ended your turn without an answer")
		})).Return(modelprovider.CompletionResult{StopReason: "end_turn", Text: "summary"}, nil).Once()
		run, err := h.uc.Run(context.Background(), agentusecase.RunRequest{Agent: h.def, Trigger: "cron", UserInput: "x"})
		require.NoError(t, err)
		assert.Equal(t, entities.AgentRunOK, run.Status)
		assert.Equal(t, "summary", run.ResponseText)
	})
	t.Run("two empty turns is an error", func(t *testing.T) {
		h := newPlatformHarness(t)
		h.model.On("Complete", mock.Anything, mock.Anything).Return(modelprovider.CompletionResult{StopReason: "end_turn"}, nil).Twice()
		run, err := h.uc.Run(context.Background(), agentusecase.RunRequest{Agent: h.def, Trigger: "cron", UserInput: "x"})
		require.Error(t, err)
		assert.Equal(t, entities.AgentRunError, run.Status)
	})
}

// ctxCheckingRunRepo fails writes made with a dead context, like a real DB
// driver does.
type ctxCheckingRunRepo struct{ *fakeRunRepo }

func (r ctxCheckingRunRepo) UpdateRun(ctx context.Context, run entities.AgentRun) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return r.fakeRunRepo.UpdateRun(ctx, run)
}

// Regression (E2E run #61): a run killed by its task deadline couldn't
// persist its final state (the save used the expired context), so its row
// stayed "in progress" forever. Runs now start as "running" and the final
// state is saved on a detached context.
func TestRun_DeadlineExceededStillPersistsErrorState(t *testing.T) {
	h := newPlatformHarness(t)
	repo := ctxCheckingRunRepo{h.repo}
	h.uc.Repository = repo

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // the task deadline has already passed

	run, err := h.uc.Run(ctx, agentusecase.RunRequest{Agent: h.def, Trigger: "cron", UserInput: "x"})
	require.Error(t, err)
	stored, _ := h.repo.GetRun(context.Background(), run.ID)
	assert.Equal(t, entities.AgentRunError, stored.Status)
	assert.Contains(t, stored.ErrorMessage, "timed out or was cancelled")
	assert.False(t, stored.FinishedAt.IsZero())
	h.model.AssertNotCalled(t, "Complete", mock.Anything, mock.Anything)
}

func TestRun_StartsAsRunning(t *testing.T) {
	h := newPlatformHarness(t)
	var statusDuringRun entities.AgentRunStatus
	h.model.On("Complete", mock.Anything, mock.Anything).Run(func(mock.Arguments) {
		r, _ := h.repo.GetRun(context.Background(), 1)
		statusDuringRun = r.Status
	}).Return(modelprovider.CompletionResult{StopReason: "end_turn", Text: "done"}, nil).Once()

	run, err := h.uc.Run(context.Background(), agentusecase.RunRequest{Agent: h.def, Trigger: "cron", UserInput: "x"})
	require.NoError(t, err)
	assert.Equal(t, entities.AgentRunRunning, statusDuringRun)
	assert.Equal(t, entities.AgentRunOK, run.Status)
}
