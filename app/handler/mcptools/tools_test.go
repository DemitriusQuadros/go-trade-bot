package mcptools

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"testing"
	"time"

	"go-trade-bot/app/engine"
	"go-trade-bot/app/entities"
	strategyscript "go-trade-bot/app/strategies/script"
	agentplatform "go-trade-bot/app/usecase/agentplatform"
	candledata "go-trade-bot/app/usecase/candledata"
	scriptuc "go-trade-bot/app/usecase/script"
	"go-trade-bot/internal/exchange"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- fakes ---------------------------------------------------------------------

type fakeSettings struct{ s entities.Settings }

func (f fakeSettings) Get(context.Context) (*entities.Settings, error) { s := f.s; return &s, nil }

type fakeAccounts struct{}

func (fakeAccounts) GetAllAccounts() ([]entities.Account, error) {
	return []entities.Account{
		{Mode: entities.AccountModeDryRun, Amount: 100}, {Mode: entities.AccountModeLive, Amount: 5},
	}, nil
}

type fakeSignals struct{ all []entities.Signal }

func (f fakeSignals) GetAll(context.Context) ([]entities.Signal, error) { return f.all, nil }
func (f fakeSignals) GetAllOpen(context.Context) ([]entities.Signal, error) {
	var o []entities.Signal
	for _, s := range f.all {
		if s.Status == entities.SignalStatus("open") {
			o = append(o, s)
		}
	}
	return o, nil
}
func (f fakeSignals) GetAllClosed(context.Context) ([]entities.Signal, error) {
	var o []entities.Signal
	for _, s := range f.all {
		if s.Status != entities.SignalStatus("open") {
			o = append(o, s)
		}
	}
	return o, nil
}
func (f fakeSignals) GetByID(_ context.Context, id uint) (entities.Signal, error) {
	for _, s := range f.all {
		if s.ID == id {
			return s, nil
		}
	}
	return entities.Signal{}, assert.AnError
}

type fakeMarket struct{ gotLimit int }

func (f *fakeMarket) ListTickerPrices(context.Context, string) ([]exchange.TickerPrice, error) {
	return []exchange.TickerPrice{{Symbol: "BTCUSDT", Price: 1}}, nil
}
func (f *fakeMarket) ListKline(_ context.Context, _, _ string, limit int) ([]exchange.Candle, error) {
	f.gotLimit = limit
	return []exchange.Candle{{OpenTime: time.Unix(0, 0), Open: 1, High: 2, Low: 0.5, Close: 1.5, Volume: 9}}, nil
}

type fakeDatasets struct {
	created struct {
		symbol, tf string
		start      *time.Time
		keepLive   bool
	}
	paused  []uint
	status  *candledata.DatasetStatus
	gotChnk int
}

func (f *fakeDatasets) List(context.Context) ([]*candledata.DatasetStatus, error) {
	return []*candledata.DatasetStatus{f.status}, nil
}
func (f *fakeDatasets) Status(context.Context, uint) (*candledata.DatasetStatus, error) {
	return f.status, nil
}
func (f *fakeDatasets) Create(_ context.Context, symbol, tf string, start *time.Time, keepLive bool) (*entities.CandleDataset, error) {
	f.created.symbol, f.created.tf, f.created.start, f.created.keepLive = symbol, tf, start, keepLive
	return &entities.CandleDataset{ID: 3}, nil
}
func (f *fakeDatasets) Pause(_ context.Context, id uint) error {
	f.paused = append(f.paused, id)
	return nil
}
func (f *fakeDatasets) Resume(context.Context, uint) error             { return nil }
func (f *fakeDatasets) RetryFailed(context.Context, uint) (int, error) { return 0, nil }
func (f *fakeDatasets) Reconcile(context.Context, uint) (int, error)   { return 0, nil }
func (f *fakeDatasets) Chunks(_ context.Context, _ uint, _ entities.ChunkStatus, limit int) ([]entities.CandleChunk, error) {
	f.gotChnk = limit
	return nil, nil
}

type fakeScripts struct{ n int }

func (f fakeScripts) Eval(context.Context, scriptuc.EvalRequest) (scriptuc.EvalResponse, error) {
	return scriptuc.EvalResponse{Trace: make([]strategyscript.TraceRecord, f.n)}, nil
}
func (f fakeScripts) FastRerun(context.Context, scriptuc.FastRerunRequest) (scriptuc.FastRerunResponse, error) {
	return scriptuc.FastRerunResponse{Trace: make([]strategyscript.TraceRecord, f.n)}, nil
}

type fakeAgents struct{ setPaused *bool }

func (f *fakeAgents) ListAgents(context.Context) ([]agentplatform.AgentView, error) {
	return []agentplatform.AgentView{{Agent: entities.Agent{ID: 1, Name: "Copilot", Goal: "secret goal prompt"}}}, nil
}
func (f *fakeAgents) GetAgent(context.Context, uint) (agentplatform.AgentView, error) {
	return agentplatform.AgentView{Agent: entities.Agent{ID: 1, Name: "Copilot", Goal: "secret goal prompt"}}, nil
}
func (f *fakeAgents) SetPaused(_ context.Context, _ uint, p bool) (agentplatform.AgentView, error) {
	f.setPaused = &p
	return agentplatform.AgentView{Agent: entities.Agent{ID: 1, Paused: p}}, nil
}
func (f *fakeAgents) ListRuns(context.Context, uint, int, *uint) ([]entities.AgentRun, entities.Agent, error) {
	return []entities.AgentRun{{ID: 9, Status: "ok", Trigger: "cron", ResponseText: "TRANSCRIPT", InputSummary: "PROMPT", ToolCallsJSON: []byte(`[{"x":1}]`)}}, entities.Agent{ID: 1}, nil
}
func (f *fakeAgents) Usage(context.Context, uint, int) (agentplatform.UsageDay, []agentplatform.UsageDay, error) {
	return agentplatform.UsageDay{Day: time.Now()}, nil, nil
}
func (f *fakeAgents) KillSwitch(context.Context) (bool, error) { return true, nil }

type fakeVersions struct{}

func (fakeVersions) GetScriptVersions(context.Context, uint) ([]entities.ScriptVersion, error) {
	return []entities.ScriptVersion{{ID: 4, StrategyID: 2, Source: "-- v4"}}, nil
}

type fakeMC struct{ gotIter int }

func (f *fakeMC) RunMonteCarlo(_ context.Context, _ uint, n int) (engine.MonteCarloResult, error) {
	f.gotIter = n
	return engine.MonteCarloResult{Iterations: n}, nil
}
func (f *fakeMC) GetMonteCarlo(context.Context, uint) (engine.MonteCarloResult, error) {
	return engine.MonteCarloResult{}, nil
}

func fullDeps() (Deps, *fakeMarket, *fakeDatasets, *fakeAgents, *fakeMC) {
	m, d, a, mc := &fakeMarket{}, &fakeDatasets{status: &candledata.DatasetStatus{Dataset: entities.CandleDataset{ID: 3, Symbol: "BTCUSDT", Timeframe: "1h"}}}, &fakeAgents{}, &fakeMC{}
	return Deps{
		Settings: fakeSettings{entities.Settings{
			Mode: "dryrun", BrokerApiKey: "KEY-123456", BrokerApiSecret: "SECRET-654321", BrokerTestnetApiKey: "TKEY", BrokerTestnetApiSecret: "TSECRET",
			WebhookURL: "https://discord.example/hook/abc", APIToken: "tok-xyz", APIBaseURL: "https://base.example", PrometheusURL: "http://prom",
		}},
		Accounts: fakeAccounts{}, Signals: fakeSignals{}, Versions: fakeVersions{}, MonteCarlo: mc, Market: m,
		Datasets: d, Scripts: fakeScripts{n: 500}, Agents: a, WalkForward: &fakeWF{}, Tasks: &fakeTasks{},
		Reports: fakeReports{}, Backtests: fakeBacktests{}, BaseURL: "https://bot.example",
	}, m, d, a, mc
}

func byName(t *testing.T, tools []Tool, name string) Tool {
	t.Helper()
	for _, tl := range tools {
		if tl.Name == name {
			return tl
		}
	}
	t.Fatalf("tool %q not registered", name)
	return Tool{}
}

func call(t *testing.T, tool Tool, args string) string {
	t.Helper()
	v, err := tool.Run(context.Background(), json.RawMessage(args))
	require.NoError(t, err)
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return string(b)
}

// --- the boundary ----------------------------------------------------------------

// The write set is pinned: adding a state-changing MCP tool must be a
// conscious edit of this list.
func TestWriteSetIsPinned(t *testing.T) {
	d, _, _, _, _ := fullDeps()
	var writes []string
	for _, tl := range New(d) {
		if tl.Write {
			writes = append(writes, tl.Name)
		}
	}
	sort.Strings(writes)
	assert.Equal(t, []string{
		"create_agent", "create_candle_dataset", "delete_agent", "pause_agent", "pause_candle_dataset", "reconcile_candle_dataset",
		"resume_candle_dataset", "retry_failed_candle_chunks", "run_agent", "run_monte_carlo", "run_walk_forward", "script_fast_rerun", "script_repl",
		"set_kill_switch", "update_agent",
	}, writes)
}

// The ports are the allowlist: none of them may carry a method that can
// change live money, settings, accounts, users or run an agent.
func TestPortsCarryNoForbiddenMethods(t *testing.T) {
	forbidden := []string{
		"Close", "Delete", "Apply", "Save",
		"Update", "UpdateDryRunCapital", "ResetDryRunCapital", "UpdateLiveAllocation", "SyncExchangeBalance", "CreateAccount",
		"RevertScriptVersion", "PlaceOrder", "CancelOrder", "Approve", "Reject", "GenerateBuySignal", "GenerateSellSignal",
	}
	ports := []reflect.Type{
		reflect.TypeOf((*SettingsReader)(nil)).Elem(), reflect.TypeOf((*AccountReader)(nil)).Elem(),
		reflect.TypeOf((*SignalReader)(nil)).Elem(), reflect.TypeOf((*VersionReader)(nil)).Elem(),
		reflect.TypeOf((*MonteCarlo)(nil)).Elem(), reflect.TypeOf((*MarketData)(nil)).Elem(),
		reflect.TypeOf((*DatasetManager)(nil)).Elem(), reflect.TypeOf((*ScriptRunner)(nil)).Elem(),
		reflect.TypeOf((*AgentManager)(nil)).Elem(),
	}
	for _, p := range ports {
		for i := 0; i < p.NumMethod(); i++ {
			assert.NotContains(t, forbidden, p.Method(i).Name, "%s must not expose %s", p.Name(), p.Method(i).Name)
		}
	}
}

func TestToolsHaveUniqueNamesAndValidSchemas(t *testing.T) {
	d, _, _, _, _ := fullDeps()
	seen := map[string]bool{}
	for _, tl := range New(d) {
		assert.False(t, seen[tl.Name], "duplicate %s", tl.Name)
		seen[tl.Name] = true
		assert.NotEmpty(t, tl.Description, tl.Name)
		var m map[string]any
		require.NoError(t, json.Unmarshal(tl.InputSchema, &m), tl.Name)
		assert.Equal(t, "object", m["type"], tl.Name)
		require.NotNil(t, tl.Run, tl.Name)
	}
	assert.Len(t, seen, 33)
}

func TestNilPortOmitsItsTools(t *testing.T) {
	d, _, _, _, _ := fullDeps()
	d.Agents = nil
	d.Datasets = nil
	for _, tl := range New(d) {
		assert.NotContains(t, []string{"list_agents", "pause_agent", "list_candle_datasets", "create_candle_dataset"}, tl.Name)
	}
}

// --- behavior --------------------------------------------------------------------

func TestGetSettings_NeverLeaksCredentialsOrURLs(t *testing.T) {
	d, _, _, _, _ := fullDeps()
	out := call(t, byName(t, New(d), "get_settings"), `{}`)
	for _, secret := range []string{"KEY-123456", "SECRET-654321", "TKEY", "TSECRET", "discord.example", "tok-xyz", "base.example", "prom"} {
		assert.NotContains(t, out, secret)
	}
	assert.NotContains(t, out, "••••", "masked secrets are not shown either")
	assert.Contains(t, out, `"mode":"dryrun"`)
	assert.Contains(t, out, `"default_locale":"en"`)
	assert.Contains(t, out, `"backtest_timeout_minutes":120`)
}

func TestGetAccount_FiltersByMode(t *testing.T) {
	d, _, _, _, _ := fullDeps()
	tool := byName(t, New(d), "get_account")
	assert.Contains(t, call(t, tool, `{}`), `"live"`)
	out := call(t, tool, `{"mode":"dryrun"}`)
	assert.Contains(t, out, `"dryrun"`)
	assert.NotContains(t, out, `"live"`)
}

func TestListSignals_NewestFirstFilterAndLimit(t *testing.T) {
	d, _, _, _, _ := fullDeps()
	var sigs []entities.Signal
	for i := uint(1); i <= 300; i++ {
		st := "closed"
		if i%2 == 0 {
			st = "open"
		}
		sigs = append(sigs, entities.Signal{ID: i, StrategyID: i % 3, Status: entities.SignalStatus(st), Orders: []entities.Order{{BrokerOrderID: "SIM-1", Profit: 1.5, ExitReason: "take_profit"}}})
	}
	d.Signals = fakeSignals{all: sigs}
	tool := byName(t, New(d), "list_signals")

	var res struct {
		Signals []signalView `json:"signals"`
		Limit   int          `json:"limit"`
	}
	require.NoError(t, json.Unmarshal([]byte(call(t, tool, `{}`)), &res))
	require.Len(t, res.Signals, 50, "default limit")
	assert.Equal(t, uint(300), res.Signals[0].ID, "newest first")
	assert.True(t, res.Signals[0].Orders[0].Simulated)
	assert.Equal(t, "take_profit", res.Signals[0].Orders[0].ExitReason)

	require.NoError(t, json.Unmarshal([]byte(call(t, tool, `{"limit":9999}`)), &res))
	assert.Len(t, res.Signals, 200, "limit is capped")

	require.NoError(t, json.Unmarshal([]byte(call(t, tool, `{"status":"closed","strategy_id":1}`)), &res))
	for _, s := range res.Signals {
		assert.Equal(t, uint(1), s.StrategyID)
		assert.Equal(t, "closed", s.Status)
	}

	_, err := tool.Run(context.Background(), json.RawMessage(`{"status":"bogus"}`))
	assert.Error(t, err)
}

func TestGetKlines_CapsLimitAndReturnsRows(t *testing.T) {
	d, m, _, _, _ := fullDeps()
	tool := byName(t, New(d), "get_klines")
	out := call(t, tool, `{"symbol":"btcusdt","interval":"1h","limit":100000}`)
	assert.Equal(t, 500, m.gotLimit)
	assert.Contains(t, out, `"columns":["open_time","open","high","low","close","volume"]`)
	call(t, tool, `{"symbol":"btcusdt","interval":"1h"}`)
	assert.Equal(t, 100, m.gotLimit)

	_, err := tool.Run(context.Background(), json.RawMessage(`{"symbol":"BTCUSDT"}`))
	assert.Error(t, err, "interval is required")
}

func TestCreateCandleDataset_NormalizesAndDefaultsKeepLive(t *testing.T) {
	d, _, ds, _, _ := fullDeps()
	tool := byName(t, New(d), "create_candle_dataset")
	call(t, tool, `{"symbol":" btcusdt ","timeframe":"1h","start":"2024-01-02"}`)
	assert.Equal(t, "BTCUSDT", ds.created.symbol)
	assert.True(t, ds.created.keepLive, "keep_live defaults to true")
	require.NotNil(t, ds.created.start)
	assert.Equal(t, time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), *ds.created.start)

	call(t, tool, `{"symbol":"ETHUSDT","timeframe":"1h","keep_live":false}`)
	assert.False(t, ds.created.keepLive)
	assert.Nil(t, ds.created.start)

	_, err := tool.Run(context.Background(), json.RawMessage(`{"symbol":"ETHUSDT","timeframe":"1h","start":"yesterday"}`))
	assert.Error(t, err)
}

func TestDatasetActions_RequireIDAndListOmitsRanges(t *testing.T) {
	d, _, ds, _, _ := fullDeps()
	tools := New(d)
	_, err := byName(t, tools, "pause_candle_dataset").Run(context.Background(), json.RawMessage(`{}`))
	assert.Error(t, err)
	call(t, byName(t, tools, "pause_candle_dataset"), `{"dataset_id":3}`)
	assert.Equal(t, []uint{3}, ds.paused)

	ds.status.Loaded = []candledata.Range{{From: time.Unix(0, 0), To: time.Unix(60, 0)}}
	assert.NotContains(t, call(t, byName(t, tools, "list_candle_datasets"), `{}`), `"loaded"`)
	assert.Contains(t, call(t, byName(t, tools, "get_candle_dataset"), `{"dataset_id":3}`), `"loaded"`)

	call(t, byName(t, tools, "list_candle_chunks"), `{"dataset_id":3,"limit":5000}`)
	assert.Equal(t, 200, ds.gotChnk)
}

func TestScripts_TraceIsTailedAndCapped(t *testing.T) {
	d, _, _, _, _ := fullDeps()
	for _, name := range []string{"script_repl", "script_fast_rerun"} {
		tool := byName(t, New(d), name)
		args := `{"source":"x","symbol":"BTCUSDT","timeframe":"1h","strategy_id":1}`
		var res struct{ Trace []json.RawMessage }
		require.NoError(t, json.Unmarshal([]byte(call(t, tool, args)), &res))
		assert.Len(t, res.Trace, 20, name)
		args = `{"source":"x","symbol":"BTCUSDT","timeframe":"1h","strategy_id":1,"trace_tail":9999}`
		require.NoError(t, json.Unmarshal([]byte(call(t, tool, args)), &res))
		assert.Len(t, res.Trace, 100, name)
	}
}

func TestAgents_RunHistoryHasNoTranscripts_GoalOnlyInGetAgent(t *testing.T) {
	d, _, _, ag, _ := fullDeps()
	tools := New(d)
	hist := call(t, byName(t, tools, "list_agent_run_history"), `{"agent_id":1}`)
	for _, leaked := range []string{"TRANSCRIPT", "PROMPT", `"x":1`} {
		assert.NotContains(t, hist, leaked)
	}
	assert.NotContains(t, call(t, byName(t, tools, "list_agents"), `{}`), "secret goal prompt")
	assert.Contains(t, call(t, byName(t, tools, "get_agent"), `{"agent_id":1}`), "secret goal prompt")

	pause := byName(t, tools, "pause_agent")
	_, err := pause.Run(context.Background(), json.RawMessage(`{"agent_id":1}`))
	assert.Error(t, err, "paused is required, no implicit default")
	assert.Nil(t, ag.setPaused)
	call(t, pause, `{"agent_id":1,"paused":true}`)
	require.NotNil(t, ag.setPaused)
	assert.True(t, *ag.setPaused)
	assert.Contains(t, call(t, byName(t, tools, "get_kill_switch"), `{}`), `"agents_paused":true`)
}

func TestVersionsAndMonteCarlo(t *testing.T) {
	d, _, _, _, mc := fullDeps()
	tools := New(d)
	out := call(t, byName(t, tools, "list_strategy_versions"), `{"strategy_id":2}`)
	assert.NotContains(t, out, "-- v4", "the list carries sizes, not sources")
	assert.Contains(t, call(t, byName(t, tools, "list_strategy_versions"), `{"strategy_id":2,"version_id":4}`), "-- v4")
	_, err := byName(t, tools, "list_strategy_versions").Run(context.Background(), json.RawMessage(`{"strategy_id":2,"version_id":99}`))
	assert.Error(t, err)

	call(t, byName(t, tools, "run_monte_carlo"), `{"run_id":5,"iterations":250}`)
	assert.Equal(t, 250, mc.gotIter)
	_, err = byName(t, tools, "run_monte_carlo").Run(context.Background(), json.RawMessage(`{}`))
	assert.Error(t, err)
}

func TestBadArgumentsAreErrors(t *testing.T) {
	d, _, _, _, _ := fullDeps()
	_, err := byName(t, New(d), "get_signal").Run(context.Background(), json.RawMessage(`{"signal_id":"abc"}`))
	assert.Error(t, err)
	_, err = byName(t, New(d), "get_signal").Run(context.Background(), json.RawMessage(`{}`))
	assert.Error(t, err)
}
