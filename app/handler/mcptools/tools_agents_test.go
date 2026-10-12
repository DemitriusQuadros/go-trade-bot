package mcptools

import (
	"context"
	"encoding/json"
	"testing"

	"go-trade-bot/app/entities"
	agentplatform "go-trade-bot/app/usecase/agentplatform"
	backtestuc "go-trade-bot/app/usecase/backtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
)

// agentStore is an in-memory AgentManager recording what the tools send.
type agentStore struct {
	fakeAgents
	current   agentplatform.AgentView
	created   *agentplatform.AgentInput
	updated   *agentplatform.AgentInput
	updatedID uint
	deleted   uint
	runID     uint
	runPrompt string
	kill      *bool
}

func (a *agentStore) GetAgent(context.Context, uint) (agentplatform.AgentView, error) {
	return a.current, nil
}
func (a *agentStore) CreateAgent(_ context.Context, in agentplatform.AgentInput) (agentplatform.AgentView, error) {
	a.created = &in
	return agentplatform.AgentView{Agent: entities.Agent{ID: 7, Name: in.Name, Goal: in.Goal}}, nil
}
func (a *agentStore) UpdateAgent(_ context.Context, id uint, in agentplatform.AgentInput) (agentplatform.AgentView, error) {
	a.updated, a.updatedID = &in, id
	return agentplatform.AgentView{Agent: entities.Agent{ID: id, Name: in.Name}}, nil
}
func (a *agentStore) DeleteAgent(_ context.Context, id uint) error { a.deleted = id; return nil }
func (a *agentStore) EnqueueManualRun(_ context.Context, id uint, prompt string) (string, error) {
	a.runID, a.runPrompt = id, prompt
	return "task-1", nil
}
func (a *agentStore) SetKillSwitch(_ context.Context, p bool) (bool, error) {
	a.kill = &p
	return p, nil
}

type fakeWebhooks struct{}

func (fakeWebhooks) ListWebhookTargets(context.Context) ([]entities.WebhookTarget, error) {
	return []entities.WebhookTarget{{ID: 2, Name: "ops", Kind: "discord", URL: "https://discord.example/SECRET", Secret: "TOKEN", Enabled: true}}, nil
}

func agentToolSet(a *agentStore) []Tool {
	return New(Deps{Agents: a, Webhooks: fakeWebhooks{}})
}

func TestCreateAgent_MapsAllFields(t *testing.T) {
	a := &agentStore{}
	out := call(t, byName(t, agentToolSet(a), "create_agent"), `{
		"name":"Nightly","goal":"review","provider":"anthropic","model":"m","permissions":["read","backtest"],
		"triggers":{"cron":["0 3 * * *"],"events":["position.closed"],"chain_from":[4]},
		"strategy_ids":[1,2],"webhook_target_ids":[2],"daily_budget_usd":1.5,"max_auto_deploys_per_day":2,"max_iterations":10}`)
	require.NotNil(t, a.created)
	in := *a.created
	assert.Equal(t, "Nightly", in.Name)
	assert.Equal(t, []string{"read", "backtest"}, in.Permissions)
	assert.Equal(t, []uint{1, 2}, in.StrategyIDs)
	assert.Equal(t, []uint{2}, in.WebhookTargetIDs)
	assert.Equal(t, 1.5, in.DailyBudgetUSD)
	assert.Equal(t, 2, in.MaxAutoDeploysPerDay)
	assert.Equal(t, 10, in.MaxIterations)
	assert.Equal(t, []string{"0 3 * * *"}, in.Triggers.Cron)
	require.Len(t, in.Triggers.Events, 1, "legacy string event form is accepted")
	assert.Equal(t, "position.closed", in.Triggers.Events[0].Type)
	require.Len(t, in.Triggers.ChainFrom, 1)
	assert.Equal(t, uint(4), in.Triggers.ChainFrom[0].AgentID)
	assert.Contains(t, out, `"id":7`)
}

func TestUpdateAgent_PatchesOnlyProvidedFields(t *testing.T) {
	cur := entities.Agent{
		ID: 5, Name: "Old", Goal: "old goal", Provider: "gemini", Model: "g", Permissions: []string{"read", "notify"},
		Triggers: datatypes.JSON(`{"cron":["*/5 * * * *"]}`), WebhookTargetIDs: []uint{2}, DailyBudgetUSD: 3, MaxAutoDeploysPerDay: 1, MaxIterations: 9,
	}
	a := &agentStore{current: agentplatform.AgentView{Agent: cur, StrategyIDs: []uint{1}}}
	call(t, byName(t, agentToolSet(a), "update_agent"), `{"agent_id":5,"goal":"new goal","strategy_ids":[]}`)

	require.NotNil(t, a.updated)
	assert.Equal(t, uint(5), a.updatedID)
	in := *a.updated
	assert.Equal(t, "new goal", in.Goal, "provided field changes")
	assert.Equal(t, []uint{}, in.StrategyIDs, "an explicitly empty array clears the field")
	assert.Equal(t, "Old", in.Name, "omitted fields keep their current value")
	assert.Equal(t, "gemini", in.Provider)
	assert.Equal(t, []string{"read", "notify"}, in.Permissions)
	assert.Equal(t, []string{"*/5 * * * *"}, in.Triggers.Cron)
	assert.Equal(t, []uint{2}, in.WebhookTargetIDs)
	assert.Equal(t, 3.0, in.DailyBudgetUSD)
	assert.Equal(t, 1, in.MaxAutoDeploysPerDay)
	assert.Equal(t, 9, in.MaxIterations)
}

func TestGetAgent_ReturnsFullTriggers(t *testing.T) {
	a := &agentStore{current: agentplatform.AgentView{Agent: entities.Agent{ID: 5, Triggers: datatypes.JSON(`{"cron":["0 * * * *"]}`)}}}
	out := call(t, byName(t, agentToolSet(a), "get_agent"), `{"agent_id":5}`)
	var v struct {
		Triggers entities.AgentTriggers `json:"triggers"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &v))
	assert.Equal(t, []string{"0 * * * *"}, v.Triggers.Cron)
}

func TestDeleteRunAndKillSwitch(t *testing.T) {
	a := &agentStore{}
	tools := agentToolSet(a)

	_, err := byName(t, tools, "delete_agent").Run(context.Background(), json.RawMessage(`{}`))
	assert.Error(t, err)
	call(t, byName(t, tools, "delete_agent"), `{"agent_id":3}`)
	assert.Equal(t, uint(3), a.deleted)

	out := call(t, byName(t, tools, "run_agent"), `{"agent_id":3,"prompt":"look at BTC"}`)
	assert.Equal(t, uint(3), a.runID)
	assert.Equal(t, "look at BTC", a.runPrompt)
	assert.Contains(t, out, "task-1")

	ks := byName(t, tools, "set_kill_switch")
	_, err = ks.Run(context.Background(), json.RawMessage(`{}`))
	assert.Error(t, err, "paused is required, no implicit default")
	assert.Nil(t, a.kill)
	call(t, ks, `{"paused":true}`)
	require.NotNil(t, a.kill)
	assert.True(t, *a.kill)
}

func TestListWebhookTargets_NeverShowsURLOrSecret(t *testing.T) {
	out := call(t, byName(t, agentToolSet(&agentStore{}), "list_webhook_targets"), `{}`)
	assert.Contains(t, out, `"name":"ops"`)
	for _, leaked := range []string{"SECRET", "TOKEN", "discord.example"} {
		assert.NotContains(t, out, leaked)
	}
}

// fakeAgents (tools_test.go) only needs the read/pause half; these stubs
// satisfy the wider AgentManager so it can still be used in fullDeps.
func (f *fakeAgents) CreateAgent(context.Context, agentplatform.AgentInput) (agentplatform.AgentView, error) {
	return agentplatform.AgentView{}, nil
}
func (f *fakeAgents) UpdateAgent(context.Context, uint, agentplatform.AgentInput) (agentplatform.AgentView, error) {
	return agentplatform.AgentView{}, nil
}
func (f *fakeAgents) DeleteAgent(context.Context, uint) error { return nil }
func (f *fakeAgents) EnqueueManualRun(context.Context, uint, string) (string, error) {
	return "", nil
}
func (f *fakeAgents) SetKillSwitch(context.Context, bool) (bool, error) { return false, nil }

// --- run_walk_forward ----------------------------------------------------------

type fakeWF struct {
	got    *backtestuc.WalkForwardRequest
	failed []uint
	err    error
}

func (f *fakeWF) EnqueueWalkForward(_ context.Context, r backtestuc.WalkForwardRequest) (entities.BacktestRun, error) {
	f.got = &r
	return entities.BacktestRun{ID: 11, Status: entities.BacktestQueued}, f.err
}
func (f *fakeWF) MarkEnqueueFailed(_ context.Context, id uint, _ error) error {
	f.failed = append(f.failed, id)
	return nil
}

type fakeTasks struct {
	ids []uint
	err error
}

func (f *fakeTasks) EnqueueBacktestTask(id uint) error { f.ids = append(f.ids, id); return f.err }

func TestRunWalkForward_QueuesAndReturnsImmediately(t *testing.T) {
	wf, tk := &fakeWF{}, &fakeTasks{}
	tool := byName(t, New(Deps{WalkForward: wf, Tasks: tk}), "run_walk_forward")

	out := call(t, tool, `{"strategy_id":3,"symbol":"btcusdt","timeframe":"1h","start_date":"2024-01-01","end_date":"2024-06-01","train_months":3,"test_months":1}`)

	assert.Contains(t, out, `"backtest_id":11`)
	assert.Contains(t, out, `"status":"queued"`)
	assert.Equal(t, []uint{11}, tk.ids)
	require.NotNil(t, wf.got)
	assert.Equal(t, "BTCUSDT", wf.got.Symbol)
	assert.Equal(t, 3, wf.got.TrainMonths)
	assert.True(t, tool.Write)
}

func TestRunWalkForward_QueueFailureMarksRunFailed(t *testing.T) {
	wf, tk := &fakeWF{}, &fakeTasks{err: assert.AnError}
	tool := byName(t, New(Deps{WalkForward: wf, Tasks: tk}), "run_walk_forward")

	_, err := tool.Run(context.Background(), json.RawMessage(`{"strategy_id":3,"symbol":"BTCUSDT","start_date":"2024-01-01","end_date":"2024-06-01"}`))

	require.Error(t, err)
	assert.Equal(t, []uint{11}, wf.failed)
}

func TestRunWalkForward_RejectsBadDatesAndMissingStrategy(t *testing.T) {
	tool := byName(t, New(Deps{WalkForward: &fakeWF{}, Tasks: &fakeTasks{}}), "run_walk_forward")
	for _, args := range []string{
		`{"symbol":"BTCUSDT","start_date":"2024-01-01","end_date":"2024-06-01"}`,
		`{"strategy_id":1,"symbol":"BTCUSDT","start_date":"nope","end_date":"2024-06-01"}`,
		`{"strategy_id":1,"symbol":"BTCUSDT","start_date":"2024-01-01","end_date":"nope"}`,
	} {
		_, err := tool.Run(context.Background(), json.RawMessage(args))
		assert.Error(t, err, args)
	}
}

// --- report links --------------------------------------------------------------

type fakeReports struct{ err error }

func (f fakeReports) GetReport(_ context.Context, id uint) (entities.AgentReport, error) {
	return entities.AgentReport{ID: id, Title: "Weekly"}, f.err
}

type fakeBacktests struct{ run entities.BacktestRun }

func (f fakeBacktests) GetByID(context.Context, uint) (entities.BacktestRun, error) {
	return f.run, nil
}

func TestReportURLs(t *testing.T) {
	d := Deps{BaseURL: "https://bot.example/", Reports: fakeReports{}, Backtests: fakeBacktests{run: entities.BacktestRun{ID: 4, Status: entities.BacktestDone}}}
	tools := New(d)

	out := call(t, byName(t, tools, "get_agent_report_url"), `{"report_id":12}`)
	assert.Contains(t, out, `"url":"https://bot.example/agents/reports/12"`)
	assert.Contains(t, out, `"html_url":"https://bot.example/api/agent-reports/12/html"`)

	out = call(t, byName(t, tools, "get_backtest_report_url"), `{"backtest_id":4}`)
	assert.Contains(t, out, `"url":"https://bot.example/api/backtest/4/report"`)
	out = call(t, byName(t, tools, "get_backtest_report_url"), `{"backtest_id":4,"lang":"pt-BR"}`)
	assert.Contains(t, out, `/api/backtest/4/report?lang=pt-BR`)
	assert.NotContains(t, out, "token", "no credential ever goes into a link")
}

func TestBacktestReportURL_RefusesUnfinishedRuns(t *testing.T) {
	d := Deps{BaseURL: "https://bot.example", Backtests: fakeBacktests{run: entities.BacktestRun{ID: 4, Status: entities.BacktestQueued}}}
	_, err := byName(t, New(d), "get_backtest_report_url").Run(context.Background(), json.RawMessage(`{"backtest_id":4}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "queued")
}

func TestReportToolsOmittedWithoutPorts(t *testing.T) {
	for _, tl := range New(Deps{BaseURL: "x"}) {
		assert.NotContains(t, []string{"get_agent_report_url", "get_backtest_report_url"}, tl.Name)
	}
}
