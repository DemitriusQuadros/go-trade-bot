package agent_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"go-trade-bot/app/entities"
	agentusecase "go-trade-bot/app/usecase/agent"
	agentworker "go-trade-bot/app/workers/agent"
	"go-trade-bot/internal/modelprovider"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// fakeChain applies the real guards (BuildChainPayload) and records.
type fakeChain struct {
	mu       sync.Mutex
	launched []agentworker.RunPayload
}

func (f *fakeChain) LaunchChain(_ context.Context, req agentworker.ChainRequest) (string, error) {
	p, err := agentworker.BuildChainPayload(req)
	if err != nil {
		return "", err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.launched = append(f.launched, p)
	return "task", nil
}

func triggerCall(agentID uint, msg string) modelprovider.ToolCall {
	return modelprovider.ToolCall{ID: "c", Name: "trigger_agent", Args: rawArgs(map[string]any{"agent_id": agentID, "message": msg})}
}

// C-01 AC#7: trigger_agent without the chain permission isn't offered to
// the model (and isn't executed if called anyway).
func TestTriggerAgent_RequiresChainPermission(t *testing.T) {
	h := newPlatformHarness(t)
	h.uc.Chain = &fakeChain{}
	target := h.platform.addAgent(entities.Agent{Name: "Target"})
	caller := h.platform.addAgent(entities.Agent{Name: "NoChain", Permissions: []string{"read"}})

	var offered []string
	var last string
	h.model.On("Complete", mock.Anything, mock.MatchedBy(func(req modelprovider.CompletionRequest) bool {
		if offered == nil {
			offered = toolNames(req)
		}
		return true
	})).Return(modelprovider.CompletionResult{StopReason: "tool_use", ToolCalls: []modelprovider.ToolCall{triggerCall(target.ID, "look")}}, nil).Once()
	h.model.On("Complete", mock.Anything, mock.MatchedBy(func(req modelprovider.CompletionRequest) bool {
		last = req.Messages[len(req.Messages)-1].Content
		return true
	})).Return(modelprovider.CompletionResult{StopReason: "end_turn"}, nil).Once()

	_, err := h.uc.Run(context.Background(), agentusecase.RunRequest{Agent: caller, Trigger: "cron", UserInput: "go"})
	require.NoError(t, err)
	assert.NotContains(t, offered, "trigger_agent")
	assert.Contains(t, last, "not available to agent")
	assert.Empty(t, h.uc.Chain.(*fakeChain).launched)

	// With the permission it is offered.
	withChain := h.platform.addAgent(entities.Agent{Name: "Chainer", Permissions: []string{"chain"}})
	offered = nil
	h.model.On("Complete", mock.Anything, mock.MatchedBy(func(req modelprovider.CompletionRequest) bool {
		offered = toolNames(req)
		return true
	})).Return(modelprovider.CompletionResult{StopReason: "end_turn"}, nil).Once()
	_, err = h.uc.Run(context.Background(), agentusecase.RunRequest{Agent: withChain, Trigger: "cron", UserInput: "go"})
	require.NoError(t, err)
	assert.Contains(t, offered, "trigger_agent")
}

func TestTriggerAgent_GuardsAndLimit(t *testing.T) {
	h := newPlatformHarness(t)
	chain := &fakeChain{}
	h.uc.Chain = chain
	a := h.platform.addAgent(entities.Agent{Name: "A"})
	paused := h.platform.addAgent(entities.Agent{Name: "Paused", Paused: true})
	b := h.platform.addAgent(entities.Agent{Name: "B", Permissions: []string{"chain"}})
	c := h.platform.addAgent(entities.Agent{Name: "C"})

	calls := []modelprovider.ToolCall{
		triggerCall(b.ID, "self"),       // caller itself
		triggerCall(a.ID, "cycle"),      // already in chain path (A -> B)
		triggerCall(paused.ID, "sleep"), // paused
		triggerCall(999, "ghost"),       // missing
		triggerCall(c.ID, "one"),
		triggerCall(c.ID, "two"),
		triggerCall(c.ID, "three"),
		triggerCall(c.ID, "four"), // over the 3 per run limit
	}
	var results []string
	h.model.On("Complete", mock.Anything, mock.Anything).Return(modelprovider.CompletionResult{StopReason: "tool_use", ToolCalls: calls}, nil).Once()
	h.model.On("Complete", mock.Anything, mock.MatchedBy(func(req modelprovider.CompletionRequest) bool {
		for _, m := range req.Messages[len(req.Messages)-len(calls):] {
			results = append(results, m.Content)
		}
		return true
	})).Return(modelprovider.CompletionResult{StopReason: "end_turn"}, nil).Once()

	parent := uint(50)
	run, err := h.uc.Run(context.Background(), agentusecase.RunRequest{
		Agent: b, Trigger: "chain", UserInput: "go", ChainDepth: 1, ChainPath: []uint{a.ID}, ParentRunID: &parent,
		TriggerDetail: json.RawMessage(`{"source_agent_id":1,"source_run_id":50,"report_ids":[],"on":"report"}`),
	})
	require.NoError(t, err)
	require.Len(t, results, len(calls))
	assert.Contains(t, results[0], "cannot trigger itself")
	assert.Contains(t, results[1], "already in this chain")
	assert.Contains(t, results[2], "paused")
	assert.Contains(t, results[3], "does not exist")
	assert.Contains(t, results[7], "limit reached")

	// Launches go through the shared guards; with a fakeChain applying
	// BuildChainPayload the per-(target, run) dedupe is not modelled here.
	require.Len(t, chain.launched, 3)
	p := chain.launched[0]
	assert.Equal(t, c.ID, p.AgentID)
	assert.Equal(t, 2, p.ChainDepth)
	assert.Equal(t, []uint{a.ID, b.ID}, p.ChainPath)
	assert.Equal(t, run.ID, *p.ParentRunID)
	assert.Equal(t, "one", p.Prompt)
}

// A run at depth 3 cannot chain further, even via trigger_agent.
func TestTriggerAgent_DepthGuard(t *testing.T) {
	h := newPlatformHarness(t)
	chain := &fakeChain{}
	h.uc.Chain = chain
	d := h.platform.addAgent(entities.Agent{Name: "D", Permissions: []string{"chain"}})
	e := h.platform.addAgent(entities.Agent{Name: "E"})
	var last string
	h.model.On("Complete", mock.Anything, mock.Anything).Return(modelprovider.CompletionResult{StopReason: "tool_use", ToolCalls: []modelprovider.ToolCall{triggerCall(e.ID, "deeper")}}, nil).Once()
	h.model.On("Complete", mock.Anything, mock.MatchedBy(func(req modelprovider.CompletionRequest) bool {
		last = req.Messages[len(req.Messages)-1].Content
		return true
	})).Return(modelprovider.CompletionResult{StopReason: "end_turn"}, nil).Once()
	_, err := h.uc.Run(context.Background(), agentusecase.RunRequest{Agent: d, Trigger: "chain", UserInput: "go", ChainDepth: 3, ChainPath: []uint{100, 101, 102}})
	require.NoError(t, err)
	assert.Contains(t, last, "chain suppressed")
	assert.Empty(t, chain.launched)
}

// Run reports the reports it wrote and successful notifications (the
// processor's declarative ChainFrom input), and the operating context of a
// chain run states depth and path.
func TestRun_RecordsChainOutcomeAndChainContext(t *testing.T) {
	h := newPlatformHarness(t)
	target, _ := h.platform.CreateWebhookTarget(context.Background(), entities.WebhookTarget{Kind: "generic", URL: "http://x", Enabled: true})
	agent := h.platform.addAgent(entities.Agent{Name: "Reporter", Permissions: []string{"notify"}, WebhookTargetIDs: []uint{target.ID}})
	blocks := []map[string]any{{"type": "summary", "data": map[string]any{"text": "Found it."}}}
	var system string
	h.model.On("Complete", mock.Anything, mock.MatchedBy(func(req modelprovider.CompletionRequest) bool {
		if system == "" {
			system = req.System
		}
		return true
	})).Return(modelprovider.CompletionResult{StopReason: "tool_use", ToolCalls: []modelprovider.ToolCall{
		writeReportCall(t, blocks, nil),
		{ID: "n", Name: "notify", Args: rawArgs(map[string]any{"severity": "info", "message": "hello"})},
	}}, nil).Once()
	h.model.On("Complete", mock.Anything, mock.Anything).Return(modelprovider.CompletionResult{StopReason: "end_turn", Text: "done"}, nil).Once()

	run, err := h.uc.Run(context.Background(), agentusecase.RunRequest{
		Agent: agent, Trigger: "chain", UserInput: "go", ChainDepth: 2, ChainPath: []uint{7, 8},
		TriggerDetail: json.RawMessage(`{"source_agent_id":8,"source_run_id":77,"report_ids":[3],"on":"report"}`),
	})
	require.NoError(t, err)
	require.Len(t, run.ReportIDs, 1)
	assert.Equal(t, h.platform.reports[0].ID, run.ReportIDs[0])
	assert.Equal(t, 1, run.NotificationsSent)
	assert.Contains(t, system, "Trigger: chain -")
	assert.Contains(t, system, `"source_run_id":77`)
	assert.Contains(t, system, "Chain: depth 2 of max 3")
	assert.Contains(t, system, "#7 → #8")
}

func TestBuildTriggerInput(t *testing.T) {
	ev := agentusecase.BuildTriggerInput(agentusecase.TriggerInput{Trigger: "event",
		Detail: json.RawMessage(`{"event":"stoploss.hit","strategy_id":5,"symbol":"BTCUSDT","occurred_at":"2026-01-01T00:00:00Z","data":{"exit_reason":"stop_loss"}}`)})
	assert.Contains(t, ev, "Strategy #5 emitted stoploss.hit on BTCUSDT")
	assert.Contains(t, ev, "write_journal")
	assert.Contains(t, ev, "exit_reason")

	mk := agentusecase.BuildTriggerInput(agentusecase.TriggerInput{Trigger: "market",
		Detail: json.RawMessage(`{"rule_id":"r","symbol":"BTCUSDT","kind":"pct_move","window_minutes":60,"observed_pct":-3.4,"threshold":3,"price":61000,"at":"2026-01-01T00:00:00Z"}`)})
	assert.Contains(t, mk, "BTCUSDT moved -3.40% in 60m")
	assert.Contains(t, mk, "macro signal", "no bound strategy trades the symbol")

	vs := agentusecase.BuildTriggerInput(agentusecase.TriggerInput{Trigger: "market", BoundStrategy: true,
		Detail: json.RawMessage(`{"symbol":"ETHUSDT","kind":"volatility_spike","window_minutes":15,"observed_multiplier":3.2,"threshold":2.5,"price":3000,"at":"x"}`)})
	assert.Contains(t, vs, "ETHUSDT volatility spiked")
	assert.NotContains(t, vs, "macro signal")

	ch := agentusecase.BuildTriggerInput(agentusecase.TriggerInput{Trigger: "chain", SourceAgent: "Risk Monitor", Prompt: "check ETH",
		Detail: json.RawMessage(`{"source_agent_id":2,"source_run_id":123,"report_ids":[4,5],"on":"report"}`)})
	assert.Contains(t, ch, "Agent Risk Monitor finished run #123 and produced reports [4, 5]")
	assert.Contains(t, ch, "list_reports")
	assert.Contains(t, ch, "check ETH")

	assert.Equal(t, agentusecase.BuildScheduledInput("x"), agentusecase.BuildTriggerInput(agentusecase.TriggerInput{Trigger: "manual", Prompt: "x"}))
}

func TestStrategyInScope(t *testing.T) {
	bound := map[uint]bool{5: true}
	champ := uint(5)
	me := uint(1)
	assert.True(t, agentusecase.StrategyInScope(1, bound, entities.Strategy{ID: 5}))
	assert.True(t, agentusecase.StrategyInScope(1, bound, entities.Strategy{ID: 9, ChallengerOfID: &champ}))
	assert.True(t, agentusecase.StrategyInScope(1, bound, entities.Strategy{ID: 10, CreatedByAgentID: &me}))
	assert.False(t, agentusecase.StrategyInScope(1, bound, entities.Strategy{ID: 6}))
	assert.False(t, agentusecase.StrategyInScope(2, map[uint]bool{}, entities.Strategy{ID: 5}))
}
