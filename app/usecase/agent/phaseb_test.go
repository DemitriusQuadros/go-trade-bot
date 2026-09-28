package agent_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"go-trade-bot/app/entities"
	"go-trade-bot/app/repository/agentplatform"
	proposalrepo "go-trade-bot/app/repository/proposal"
	signalrepo "go-trade-bot/app/repository/signal"
	strategyrepo "go-trade-bot/app/repository/strategy"
	"go-trade-bot/app/strategies"
	agentusecase "go-trade-bot/app/usecase/agent"
	"go-trade-bot/app/usecase/agent/deploygate"
	backtestusecase "go-trade-bot/app/usecase/backtest"
	strategyusecase "go-trade-bot/app/usecase/strategy"
	"go-trade-bot/internal/lock"
	"go-trade-bot/internal/modelprovider"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// Phase B-01 tests: real StrategyUseCase + GORM repositories on SQLite, a
// scripted model, and a fake (or real) deploy gate.

type noopStrategyWorker struct{}

func (noopStrategyWorker) EnqueueStrategyTask(entities.Strategy) error { return nil }

// fakeGate is a DeployGate returning a fixed verdict and recording calls.
type fakeGate struct {
	mu     sync.Mutex
	pass   bool
	calls  int
	config entities.DeployGateConfig
}

func (g *fakeGate) Run(_ context.Context, target entities.Strategy, candidate string) (agentusecase.GateOutcome, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls++
	res := deploygate.Evaluate(
		deploygate.Metrics{Sharpe: 2, MaxDrawdownPct: 5, ProfitFactor: 2, Trades: 40},
		deploygate.Metrics{Sharpe: 1, MaxDrawdownPct: 5, ProfitFactor: 1.5, Trades: 40},
		deploygate.Thresholds{MaxDrawdownRatio: 1.1, MinProfitFactor: 1, MinTrades: 20})
	if !g.pass {
		res = deploygate.Evaluate(
			deploygate.Metrics{Sharpe: 0.5, MaxDrawdownPct: 9, ProfitFactor: 0.8, Trades: 40},
			deploygate.Metrics{Sharpe: 1, MaxDrawdownPct: 5, ProfitFactor: 1.5, Trades: 40},
			deploygate.Thresholds{MaxDrawdownRatio: 1.1, MinProfitFactor: 1, MinTrades: 20})
	}
	res.BaselineRunID, res.CandidateRunID = 11, 12
	return agentusecase.GateOutcome{Result: res, Config: entities.DefaultDeployGateConfig(), Symbol: "BTCUSDT", Timeframe: "15m"}, nil
}

func (g *fakeGate) Config(context.Context) (entities.DeployGateConfig, error) {
	return entities.DefaultDeployGateConfig(), nil
}

func (g *fakeGate) count() int { g.mu.Lock(); defer g.mu.Unlock(); return g.calls }

// scriptedModel calls the given tools once, then ends the turn.
type scriptedModel struct {
	mu    sync.Mutex
	calls []modelprovider.ToolCall
	done  bool
}

func (m *scriptedModel) Complete(context.Context, modelprovider.CompletionRequest) (modelprovider.CompletionResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.done {
		m.done = true
		return modelprovider.CompletionResult{StopReason: "tool_use", ToolCalls: m.calls}, nil
	}
	return modelprovider.CompletionResult{StopReason: "end_turn", Text: "done"}, nil
}

type phaseBEnv struct {
	db        *gorm.DB
	uc        *agentusecase.AgentUseCase
	platform  *agentplatform.GormRepository
	proposals *proposalrepo.GormRepository
	gate      *fakeGate
	notifier  *recordingNotifier
}

func registerScriptStrategyOnce() {
	if !strategies.Exists("script") {
		strategies.Register("script", func(entities.Strategy) strategies.Strategy { return nil })
	}
}

func newPhaseBEnv(t *testing.T) *phaseBEnv {
	t.Helper()
	registerScriptStrategyOnce()
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&entities.Strategy{}, &entities.ScriptVersion{}, &entities.ScriptState{}, &entities.Signal{}, &entities.Order{},
		&entities.Agent{}, &entities.AgentStrategyBinding{}, &entities.StrategyMemoryEntry{}, &entities.AgentReport{},
		&entities.WebhookTarget{}, &entities.AgentUsage{}, &entities.StrategyChangeProposal{}, &entities.DeployGateConfig{},
	))
	platform := agentplatform.NewGormRepository(db)
	_, err = platform.EnsureDefaultAgent(context.Background())
	require.NoError(t, err)
	proposals := proposalrepo.NewGormRepository(db)
	require.NoError(t, proposals.EnsureGateConfig(context.Background()))

	strategyUC := strategyusecase.NewStrategyUseCase(strategyrepo.NewStrategyRepository(db), noopStrategyWorker{})
	uc := agentusecase.NewAgentUseCase(&scriptedModel{}, newFakeRunRepo(), strategyUC, &mockBacktestUseCase{}, &mockSignalUseCase{}, &mockSnapshotUseCase{})
	rec := &recordingNotifier{}
	gate := &fakeGate{pass: true}
	uc.Platform = platform
	uc.Proposals = proposals
	uc.Gate = gate
	uc.ForwardTest = signalrepo.NewSignalRepository(db)
	uc.Notifier = rec
	uc.Lock = lock.NewMemoryStrategyLock()
	uc.APIBaseURL = "http://bot.local:8080"
	return &phaseBEnv{db: db, uc: uc, platform: platform, proposals: proposals, gate: gate, notifier: rec}
}

const oldSource = "function should_long(ctx) return false end"
const newSource = "function should_long(ctx) return true end"

func (e *phaseBEnv) strategy(t *testing.T, name, mode string, status entities.StrategyStatus) entities.Strategy {
	t.Helper()
	s := entities.Strategy{
		Name: name, Description: "d", StrategyName: "script", ScriptSource: oldSource, Mode: mode, Status: status,
		MonitoredSymbols:      datatypes.JSONSlice[string]{"BTCUSDT"},
		StrategyConfiguration: entities.StrategyConfiguration{Cycle: entities.FifteenMinutes, Configuration: datatypes.JSON(`{"stop_loss_pct": 2}`)},
	}
	require.NoError(t, e.db.Create(&s).Error)
	return s
}

func (e *phaseBEnv) agent(t *testing.T, name string, maxDeploys int, perms []string, bound ...uint) entities.Agent {
	t.Helper()
	a, err := e.platform.CreateAgent(context.Background(), entities.Agent{Name: name, Permissions: perms, MaxAutoDeploysPerDay: maxDeploys})
	require.NoError(t, err)
	require.NoError(t, e.platform.SetBindings(context.Background(), a.ID, bound))
	return a
}

var allPerms = []string{"read", "backtest", "edit_testing", "create_strategy", "propose_live", "notify"}

// call runs one agent turn in which the model calls tool with args, and
// returns that tool call's result text and error text.
func (e *phaseBEnv) call(t *testing.T, a entities.Agent, tool string, args map[string]any) (string, string) {
	t.Helper()
	uc := *e.uc
	uc.Model = &scriptedModel{calls: []modelprovider.ToolCall{{ID: "1", Name: tool, Args: rawArgs(args)}}}
	run, err := uc.Run(context.Background(), agentusecase.RunRequest{Agent: a, Trigger: "manual", UserInput: "go"})
	require.NoError(t, err)
	var records []struct {
		Tool   string `json:"tool"`
		Result string `json:"result"`
		Error  string `json:"error"`
	}
	require.NoError(t, json.Unmarshal(run.ToolCallsJSON, &records))
	require.Len(t, records, 1)
	return records[0].Result, records[0].Error
}

func (e *phaseBEnv) reload(t *testing.T, id uint) entities.Strategy {
	t.Helper()
	var s entities.Strategy
	require.NoError(t, e.db.First(&s, id).Error)
	return s
}

func (e *phaseBEnv) versions(t *testing.T, id uint) []entities.ScriptVersion {
	var vs []entities.ScriptVersion
	require.NoError(t, e.db.Where("strategy_id = ?", id).Find(&vs).Error)
	return vs
}

func (e *phaseBEnv) usage(t *testing.T, agentID uint) entities.AgentUsage {
	u, err := e.platform.GetUsage(context.Background(), agentID, time.Now())
	require.NoError(t, err)
	return u
}

func deployArgs(id uint) map[string]any {
	return map[string]any{"strategy_id": id, "script_source": newSource, "rationale": "tighter entries"}
}

// AC#2 (pass): source updated, AutoDeploys incremented, ScriptVersion
// written, Mode/Status unchanged, no proposal.
func TestDeployToTesting_GatePassDeploys(t *testing.T) {
	e := newPhaseBEnv(t)
	s := e.strategy(t, "Dry", "dryrun", entities.Testing)
	a := e.agent(t, "Improver", 2, allPerms, s.ID)

	res, errText := e.call(t, a, "deploy_to_testing", deployArgs(s.ID))
	require.Empty(t, errText)
	assert.Contains(t, res, `"deployed":true`)

	got := e.reload(t, s.ID)
	assert.Equal(t, newSource, got.ScriptSource)
	assert.Equal(t, "dryrun", got.Mode)
	assert.Equal(t, entities.Testing, got.Status)
	vs := e.versions(t, s.ID)
	require.Len(t, vs, 1)
	assert.Equal(t, newSource, vs[0].Source)
	assert.Equal(t, 1, e.usage(t, a.ID).AutoDeploys)

	ps, err := e.proposals.List(context.Background(), proposalrepo.Filter{})
	require.NoError(t, err)
	assert.Empty(t, ps)
	mem, err := e.platform.ListMemory(context.Background(), s.ID, []entities.MemoryKind{entities.MemoryFinding}, 10, nil)
	require.NoError(t, err)
	require.Len(t, mem, 1)
	assert.Contains(t, mem[0].Content, "deploy gate passed")
}

// AC#2 (fail): source unchanged, pending gate_failed_change proposal.
func TestDeployToTesting_GateFailFilesProposal(t *testing.T) {
	e := newPhaseBEnv(t)
	e.gate.pass = false
	s := e.strategy(t, "Dry", "dryrun", entities.Testing)
	a := e.agent(t, "Improver", 2, allPerms, s.ID)

	res, errText := e.call(t, a, "deploy_to_testing", deployArgs(s.ID))
	require.Empty(t, errText)
	assert.Contains(t, res, `"deployed":false`)

	assert.Equal(t, oldSource, e.reload(t, s.ID).ScriptSource)
	assert.Empty(t, e.versions(t, s.ID))
	assert.Equal(t, 0, e.usage(t, a.ID).AutoDeploys)

	ps, err := e.proposals.List(context.Background(), proposalrepo.Filter{})
	require.NoError(t, err)
	require.Len(t, ps, 1)
	p := ps[0]
	assert.Equal(t, entities.ProposalGateFailedChange, p.Kind)
	assert.Equal(t, entities.ProposalPending, p.Status)
	assert.Equal(t, s.ID, p.TargetStrategyID)
	assert.Equal(t, a.ID, p.AgentID)
	assert.Equal(t, oldSource, p.BaseSource)
	assert.Equal(t, newSource, p.ProposedSource)
	assert.Equal(t, "tighter entries", p.Rationale)
	passed := agentusecase.GatePassedFromEvidence(p.EvidenceJSON)
	require.NotNil(t, passed)
	assert.False(t, *passed)
	var ev map[string]map[string]any
	require.NoError(t, json.Unmarshal(p.EvidenceJSON, &ev))
	assert.EqualValues(t, 11, ev["gate"]["baseline_run_id"])
	assert.EqualValues(t, 12, ev["gate"]["candidate_run_id"])
}

// AC#3: live or productive targets are refused, before any gate run.
func TestDeployToTesting_RefusesLiveAndProductive(t *testing.T) {
	e := newPhaseBEnv(t)
	live := e.strategy(t, "Live", "live", entities.Testing)
	prod := e.strategy(t, "Prod", "dryrun", entities.Productive)
	a := e.agent(t, "Improver", 5, allPerms, live.ID, prod.ID)

	for _, s := range []entities.Strategy{live, prod} {
		_, errText := e.call(t, a, "deploy_to_testing", deployArgs(s.ID))
		assert.Contains(t, errText, "agents never change live or productive strategies")
		got := e.reload(t, s.ID)
		assert.Equal(t, oldSource, got.ScriptSource)
		assert.Equal(t, s.Mode, got.Mode)
		assert.Equal(t, s.Status, got.Status)
	}
	assert.Zero(t, e.gate.count())
	ps, _ := e.proposals.List(context.Background(), proposalrepo.Filter{})
	assert.Empty(t, ps)
}

// AC#3: MaxAutoDeploysPerDay=0 -> refused with "file a proposal".
func TestDeployToTesting_AutoDeployDisabled(t *testing.T) {
	e := newPhaseBEnv(t)
	s := e.strategy(t, "Dry", "dryrun", entities.Testing)
	a := e.agent(t, "Improver", 0, allPerms, s.ID)

	_, errText := e.call(t, a, "deploy_to_testing", deployArgs(s.ID))
	assert.Contains(t, errText, "file a proposal")
	assert.Zero(t, e.gate.count())
	assert.Equal(t, oldSource, e.reload(t, s.ID).ScriptSource)
}

// AC#3: at the daily limit -> refused.
func TestDeployToTesting_AtLimit(t *testing.T) {
	e := newPhaseBEnv(t)
	s := e.strategy(t, "Dry", "dryrun", entities.Testing)
	a := e.agent(t, "Improver", 1, allPerms, s.ID)

	_, errText := e.call(t, a, "deploy_to_testing", deployArgs(s.ID))
	require.Empty(t, errText)
	second := deployArgs(s.ID)
	second["script_source"] = "function should_long(ctx) return ctx ~= nil end"
	_, errText = e.call(t, a, "deploy_to_testing", second)
	assert.Contains(t, errText, "daily auto-deploy limit reached")
	assert.Contains(t, errText, "file a proposal")
	assert.Equal(t, 1, e.gate.count())
	assert.Equal(t, newSource, e.reload(t, s.ID).ScriptSource)
	assert.Equal(t, 1, e.usage(t, a.ID).AutoDeploys)
}

// Uncompilable Lua never reaches the gate.
func TestDeployToTesting_RejectsUncompilableLua(t *testing.T) {
	e := newPhaseBEnv(t)
	s := e.strategy(t, "Dry", "dryrun", entities.Testing)
	a := e.agent(t, "Improver", 2, allPerms, s.ID)
	args := deployArgs(s.ID)
	args["script_source"] = "function should_long(ctx) return"
	_, errText := e.call(t, a, "deploy_to_testing", args)
	assert.Contains(t, errText, "does not compile")
	assert.Zero(t, e.gate.count())
}

// AC#5: create_challenger on a live champion creates exactly one dryrun/
// testing challenger linked by ChallengerOfID; a second call returns it.
func TestCreateChallenger_OnePerChampion(t *testing.T) {
	e := newPhaseBEnv(t)
	champion := e.strategy(t, "Champion", "live", entities.Productive)
	a := e.agent(t, "Improver", 2, allPerms, champion.ID)

	res, errText := e.call(t, a, "create_challenger", map[string]any{"champion_strategy_id": champion.ID})
	require.Empty(t, errText)
	var first map[string]any
	require.NoError(t, json.Unmarshal([]byte(res), &first))
	assert.Equal(t, true, first["created"])
	challengerID := uint(first["challenger_strategy_id"].(float64))

	ch := e.reload(t, challengerID)
	require.NotNil(t, ch.ChallengerOfID)
	assert.Equal(t, champion.ID, *ch.ChallengerOfID)
	assert.Equal(t, "dryrun", ch.Mode)
	assert.Equal(t, entities.Testing, ch.Status)
	assert.Equal(t, champion.ScriptSource, ch.ScriptSource)
	assert.Equal(t, "Champion · challenger", ch.Name)
	require.NotNil(t, ch.CreatedByAgentID)
	assert.Equal(t, a.ID, *ch.CreatedByAgentID)

	// Auto-bound, finding on the champion, champion untouched.
	bindings, _ := e.platform.ListBindingsByAgent(context.Background(), a.ID)
	var boundIDs []uint
	for _, b := range bindings {
		boundIDs = append(boundIDs, b.StrategyID)
	}
	assert.ElementsMatch(t, []uint{champion.ID, challengerID}, boundIDs)
	mem, _ := e.platform.ListMemory(context.Background(), champion.ID, []entities.MemoryKind{entities.MemoryFinding}, 10, nil)
	require.Len(t, mem, 1)
	got := e.reload(t, champion.ID)
	assert.Equal(t, "live", got.Mode)
	assert.Equal(t, entities.Productive, got.Status)

	res, errText = e.call(t, a, "create_challenger", map[string]any{"champion_strategy_id": champion.ID})
	require.Empty(t, errText)
	var second map[string]any
	require.NoError(t, json.Unmarshal([]byte(res), &second))
	assert.Equal(t, false, second["created"])
	assert.EqualValues(t, challengerID, second["challenger_strategy_id"])

	var n int64
	e.db.Model(&entities.Strategy{}).Where("challenger_of_id = ?", champion.ID).Count(&n)
	assert.EqualValues(t, 1, n)

	// The operator editing the challenger (full-column Update) keeps the link.
	edit := e.reload(t, challengerID)
	edit.ChallengerOfID, edit.CreatedByAgentID = nil, nil
	edit.Description = "edited"
	require.NoError(t, strategyrepo.NewStrategyRepository(e.db).Update(context.Background(), edit))
	after := e.reload(t, challengerID)
	require.NotNil(t, after.ChallengerOfID)
	assert.Equal(t, champion.ID, *after.ChallengerOfID)
}

func TestCreateChallenger_RefusesNonLiveChampion(t *testing.T) {
	e := newPhaseBEnv(t)
	s := e.strategy(t, "Dry", "dryrun", entities.Testing)
	a := e.agent(t, "Improver", 2, allPerms, s.ID)
	_, errText := e.call(t, a, "create_challenger", map[string]any{"champion_strategy_id": s.ID})
	assert.Contains(t, errText, "deploy_to_testing")
}

// The challenger (dryrun, challenger of a bound champion) is in scope for
// deploy_to_testing even though it was never explicitly bound.
func TestDeployToTesting_ChallengerOfBoundChampionInScope(t *testing.T) {
	e := newPhaseBEnv(t)
	champion := e.strategy(t, "Champion", "live", entities.Testing)
	a := e.agent(t, "Improver", 2, allPerms, champion.ID)
	championID := champion.ID
	ch := e.strategy(t, "Challenger", "dryrun", entities.Testing)
	require.NoError(t, e.db.Model(&entities.Strategy{}).Where("id = ?", ch.ID).Update("challenger_of_id", championID).Error)

	_, errText := e.call(t, a, "deploy_to_testing", deployArgs(ch.ID))
	require.Empty(t, errText)
	assert.Equal(t, newSource, e.reload(t, ch.ID).ScriptSource)
	assert.Equal(t, oldSource, e.reload(t, champion.ID).ScriptSource)
}

func (e *phaseBEnv) challenger(t *testing.T, champion entities.Strategy, age time.Duration) entities.Strategy {
	t.Helper()
	id := champion.ID
	ch := entities.Strategy{
		Name: champion.Name + " · challenger", Description: "d", StrategyName: "script", ScriptSource: newSource, Mode: "dryrun",
		Status: entities.Testing, MonitoredSymbols: champion.MonitoredSymbols, StrategyConfiguration: champion.StrategyConfiguration,
		ChallengerOfID: &id, CreatedAt: time.Now().Add(-age),
	}
	require.NoError(t, e.db.Create(&ch).Error)
	return ch
}

func (e *phaseBEnv) closedSignal(t *testing.T, strategyID uint, profit float32, at time.Time) {
	t.Helper()
	s := entities.Signal{Symbol: "BTCUSDT", StrategyID: strategyID, Status: entities.Closed, CreatedAt: at, UpdatedAt: at,
		Orders: []entities.Order{{EntryPrice: 1, ExitPrice: 1, Quantity: 1, InvestedAmount: 1, MarginType: entities.Isolated, Profit: profit}}}
	require.NoError(t, e.db.Create(&s).Error)
}

// AC#6: propose_promotion creates a pending proposal with evidence,
// supersedes an older pending one for the same target, and notifies.
func TestProposePromotion_CreatesSupersedesNotifies(t *testing.T) {
	e := newPhaseBEnv(t)
	e.gate.pass = false // recorded, not blocking
	champion := e.strategy(t, "Champion", "live", entities.Productive)
	ch := e.challenger(t, champion, 8*24*time.Hour)
	target, err := e.platform.CreateWebhookTarget(context.Background(), entities.WebhookTarget{Name: "hook", Kind: entities.WebhookGeneric, URL: "http://x", Enabled: true})
	require.NoError(t, err)
	a := e.agent(t, "Improver", 2, allPerms, champion.ID)
	a.WebhookTargetIDs = datatypes.JSONSlice[uint]{target.ID}
	a, err = e.platform.UpdateAgent(context.Background(), a)
	require.NoError(t, err)

	since := ch.CreatedAt.Add(time.Hour)
	e.closedSignal(t, ch.ID, 10, since)
	e.closedSignal(t, ch.ID, -4, since)
	e.closedSignal(t, champion.ID, -2, since)
	e.closedSignal(t, champion.ID, 1, ch.CreatedAt.Add(-48*time.Hour)) // before the challenger existed: excluded

	older, err := e.proposals.Create(context.Background(), entities.StrategyChangeProposal{
		Kind: entities.ProposalPromoteChallenger, TargetStrategyID: champion.ID, AgentID: a.ID, BaseSource: oldSource, ProposedSource: "x",
	})
	require.NoError(t, err)

	res, errText := e.call(t, a, "propose_promotion", map[string]any{"challenger_strategy_id": ch.ID, "rationale": "better Sharpe forward"})
	require.Empty(t, errText)
	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(res), &out))
	id := uint(out["proposal_id"].(float64))
	assert.Equal(t, false, out["early"])
	assert.EqualValues(t, 1, out["superseded"])

	p, err := e.proposals.Get(context.Background(), id)
	require.NoError(t, err)
	assert.Equal(t, entities.ProposalPending, p.Status)
	assert.Equal(t, entities.ProposalPromoteChallenger, p.Kind)
	assert.Equal(t, champion.ID, p.TargetStrategyID)
	require.NotNil(t, p.ChallengerStrategyID)
	assert.Equal(t, ch.ID, *p.ChallengerStrategyID)
	assert.Equal(t, oldSource, p.BaseSource)
	assert.Equal(t, newSource, p.ProposedSource)

	var ev struct {
		Gate        map[string]any `json:"gate"`
		ForwardTest struct {
			Since      time.Time      `json:"since"`
			Challenger map[string]any `json:"challenger"`
			Champion   map[string]any `json:"champion"`
		} `json:"forward_test"`
	}
	require.NoError(t, json.Unmarshal(p.EvidenceJSON, &ev))
	assert.Equal(t, false, ev.Gate["passed"])
	assert.EqualValues(t, 2, ev.ForwardTest.Challenger["trades"])
	assert.EqualValues(t, 50, ev.ForwardTest.Challenger["win_rate_pct"])
	assert.EqualValues(t, 6, ev.ForwardTest.Challenger["net_pnl"])
	assert.EqualValues(t, -4, ev.ForwardTest.Challenger["max_adverse"])
	assert.EqualValues(t, 1, ev.ForwardTest.Champion["trades"])

	old, err := e.proposals.Get(context.Background(), older.ID)
	require.NoError(t, err)
	assert.Equal(t, entities.ProposalSuperseded, old.Status)

	require.Equal(t, 1, e.notifier.count())
	e.notifier.mu.Lock()
	msg := e.notifier.calls[0]
	e.notifier.mu.Unlock()
	assert.Equal(t, "warning", msg.Severity)
	assert.Equal(t, "http://bot.local:8080/agents/proposals/"+uintStr(id), msg.Link)

	// The champion is untouched.
	got := e.reload(t, champion.ID)
	assert.Equal(t, oldSource, got.ScriptSource)
	assert.Equal(t, "live", got.Mode)
	assert.Equal(t, entities.Productive, got.Status)
}

func TestProposePromotion_NeedsSevenDaysOrEarly(t *testing.T) {
	e := newPhaseBEnv(t)
	champion := e.strategy(t, "Champion", "live", entities.Testing)
	ch := e.challenger(t, champion, 2*24*time.Hour)
	a := e.agent(t, "Improver", 2, allPerms, champion.ID)

	_, errText := e.call(t, a, "propose_promotion", map[string]any{"challenger_strategy_id": ch.ID, "rationale": "looks good"})
	assert.Contains(t, errText, "forward-testing")

	res, errText := e.call(t, a, "propose_promotion", map[string]any{"challenger_strategy_id": ch.ID, "rationale": "EARLY: regime shift"})
	require.Empty(t, errText)
	assert.Contains(t, res, `"early":true`)
}

// AC#11: every strategy-writing tool refuses out-of-scope ids.
func TestWritingTools_RefuseOutOfScope(t *testing.T) {
	e := newPhaseBEnv(t)
	draft := e.strategy(t, "Draft", "backtest", entities.Testing)
	dry := e.strategy(t, "Dry", "dryrun", entities.Testing)
	live := e.strategy(t, "Live", "live", entities.Testing)
	ch := e.challenger(t, live, 10*24*time.Hour)
	other := e.strategy(t, "Other", "dryrun", entities.Testing)
	a := e.agent(t, "Improver", 5, allPerms, other.ID) // bound to something else

	_, errText := e.call(t, a, "deploy_to_testing", deployArgs(dry.ID))
	assert.Contains(t, errText, "outside agent")
	_, errText = e.call(t, a, "deploy_to_testing", deployArgs(ch.ID))
	assert.Contains(t, errText, "outside agent")
	_, errText = e.call(t, a, "save_strategy_script", map[string]any{
		"strategy_id": draft.ID, "name": "x", "description": "x", "script_source": newSource, "symbols": []string{"BTCUSDT"}, "cycle_minutes": 15, "mode": "backtest",
	})
	assert.Contains(t, errText, "outside agent")
	_, errText = e.call(t, a, "create_challenger", map[string]any{"champion_strategy_id": live.ID})
	assert.Contains(t, errText, "not bound")
	_, errText = e.call(t, a, "propose_promotion", map[string]any{"challenger_strategy_id": ch.ID, "rationale": "x"})
	assert.Contains(t, errText, "outside agent")

	for _, s := range []entities.Strategy{draft, dry, live, ch} {
		assert.Equal(t, s.ScriptSource, e.reload(t, s.ID).ScriptSource)
	}
	assert.Zero(t, e.gate.count())
	ps, _ := e.proposals.List(context.Background(), proposalrepo.Filter{})
	assert.Empty(t, ps)
}

// Strategies the agent created stay in scope without a binding.
func TestScope_CreatedByAgent(t *testing.T) {
	e := newPhaseBEnv(t)
	a := e.agent(t, "Improver", 2, allPerms)
	s := e.strategy(t, "Mine", "dryrun", entities.Testing)
	require.NoError(t, e.db.Model(&entities.Strategy{}).Where("id = ?", s.ID).Update("created_by_agent_id", a.ID).Error)
	_, errText := e.call(t, a, "deploy_to_testing", deployArgs(s.ID))
	require.Empty(t, errText)
	assert.Equal(t, newSource, e.reload(t, s.ID).ScriptSource)
}

// create_strategy: testing + backtest/dryrun, auto-bound, CreatedByAgentID,
// max 3 per UTC day.
func TestCreateStrategy_BoundClampedRateLimited(t *testing.T) {
	e := newPhaseBEnv(t)
	a := e.agent(t, "Creator", 0, []string{"read", "create_strategy"})
	args := func(name, mode string) map[string]any {
		return map[string]any{"name": name, "description": "d", "script_source": newSource, "symbols": []string{"BTCUSDT"}, "cycle_minutes": 15, "mode": mode}
	}
	for i, mode := range []string{"live", "backtest", "paper"} {
		res, errText := e.call(t, a, "create_strategy", args("S"+uintStr(uint(i)), mode))
		require.Empty(t, errText)
		assert.Contains(t, res, "status=testing")
	}
	_, errText := e.call(t, a, "create_strategy", args("S4", "dryrun"))
	assert.Contains(t, errText, "limit is 3 per day")

	var created []entities.Strategy
	require.NoError(t, e.db.Where("created_by_agent_id = ?", a.ID).Order("id").Find(&created).Error)
	require.Len(t, created, 3)
	assert.Equal(t, []string{"dryrun", "backtest", "dryrun"}, []string{created[0].Mode, created[1].Mode, created[2].Mode})
	for _, s := range created {
		assert.Equal(t, entities.Testing, s.Status)
	}
	bindings, _ := e.platform.ListBindingsByAgent(context.Background(), a.ID)
	assert.Len(t, bindings, 3)
	assert.Equal(t, 3, e.usage(t, a.ID).StrategiesCreated)
}

// create_strategy / propose_promotion are only offered with their
// permissions; list_proposals / get_deploy_gate_config always.
func TestPhaseBTools_Permissions(t *testing.T) {
	e := newPhaseBEnv(t)
	a := e.agent(t, "Reader", 0, []string{"read"})
	_, errText := e.call(t, a, "create_strategy", map[string]any{"name": "x"})
	assert.Contains(t, errText, "requires permission \"create_strategy\"")
	_, errText = e.call(t, a, "propose_promotion", map[string]any{"challenger_strategy_id": 1})
	assert.Contains(t, errText, "requires permission \"propose_live\"")
	_, errText = e.call(t, a, "deploy_to_testing", map[string]any{"strategy_id": 1})
	assert.Contains(t, errText, "requires permission \"edit_testing\"")

	res, errText := e.call(t, a, "get_deploy_gate_config", map[string]any{})
	require.Empty(t, errText)
	assert.Contains(t, res, `"min_trades":20`)
	assert.Contains(t, res, `"auto_deploys_remaining_today":0`)
	res, errText = e.call(t, a, "list_proposals", map[string]any{"status": "pending,approved"})
	require.Empty(t, errText)
	assert.Equal(t, "[]", res)
}

// --- AC#10: thresholds come only from DeployGateConfig -----------------------

type fakeWalkForward struct {
	mu   sync.Mutex
	reqs []backtestusecase.WalkForwardRequest
	next uint
}

func (f *fakeWalkForward) RunWalkForwardForStrategy(_ context.Context, strat entities.Strategy, req backtestusecase.WalkForwardRequest) (entities.BacktestRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reqs = append(f.reqs, req)
	f.next++
	// Candidate looks great on everything but trade count (5 < 20).
	if req.CandidateSourceHash != "" {
		return entities.BacktestRun{ID: 100 + f.next, StrategyID: strat.ID, Sharpe: 9, MaxDrawdownPct: 1, ProfitFactor: 9, TotalTrades: 5}, nil
	}
	return entities.BacktestRun{ID: 100 + f.next, StrategyID: strat.ID, Sharpe: 1, MaxDrawdownPct: 5, ProfitFactor: 1.2, TotalTrades: 30}, nil
}

type fullCoverage struct{}

func (fullCoverage) CountInRange(_ context.Context, _, tf string, from, to time.Time) (int64, error) {
	d, _ := agentusecase.TimeframeDuration(tf)
	return int64(to.Sub(from) / d), nil
}

func TestDeployToTesting_ThresholdsCannotBeInfluencedByArgs(t *testing.T) {
	e := newPhaseBEnv(t)
	wf := &fakeWalkForward{}
	e.uc.Gate = agentusecase.NewGateRunner(wf, fullCoverage{}, e.proposals)
	s := e.strategy(t, "Dry", "dryrun", entities.Testing)
	a := e.agent(t, "Improver", 5, allPerms, s.ID)

	args := deployArgs(s.ID)
	for k, v := range map[string]any{
		"min_trades": 0, "MinTrades": 0, "min_sharpe_delta": -100, "max_drawdown_ratio": 1e9, "min_profit_factor": 0,
		"thresholds": map[string]any{"min_trades": 0, "min_sharpe_delta": -100},
		"gate":       map[string]any{"passed": true}, "passed": true, "sharpe": 99, "trades": 1000,
		"lookback_months": 1, "train_months": 1, "test_months": 1, "timeframe": "1m",
	} {
		args[k] = v
	}
	res, errText := e.call(t, a, "deploy_to_testing", args)
	require.Empty(t, errText)
	assert.Contains(t, res, `"deployed":false`, "5 trades < configured MinTrades 20 must fail regardless of args")
	assert.Equal(t, oldSource, e.reload(t, s.ID).ScriptSource)

	require.Len(t, wf.reqs, 2)
	for _, r := range wf.reqs {
		assert.Equal(t, 3, r.TrainMonths)
		assert.Equal(t, 1, r.TestMonths)
		assert.Equal(t, "15m", r.Timeframe, "the strategy's cycle-derived timeframe, not the args'")
		assert.Equal(t, s.ID, r.StrategyID, "runs persist under the target strategy")
		assert.InDelta(t, 6*30, r.EndDate.Sub(r.StartDate).Hours()/24, 5)
	}
	assert.Equal(t, agentusecase.SourceHash(newSource), wf.reqs[1].CandidateSourceHash)
	assert.Empty(t, wf.reqs[0].CandidateSourceHash)

	ps, _ := e.proposals.List(context.Background(), proposalrepo.Filter{})
	require.Len(t, ps, 1)
	var ev struct {
		Gate struct {
			Checks []struct {
				Name      string `json:"name"`
				Passed    bool   `json:"passed"`
				Threshold any    `json:"threshold"`
			} `json:"checks"`
		} `json:"gate"`
	}
	require.NoError(t, json.Unmarshal(ps[0].EvidenceJSON, &ev))
	var trades bool
	for _, c := range ev.Gate.Checks {
		if c.Name == deploygate.CheckTrades {
			trades = true
			assert.False(t, c.Passed)
			assert.EqualValues(t, 20, c.Threshold)
		}
	}
	assert.True(t, trades)

	// Operator-changed thresholds (REST) are what the gate uses next time.
	cfg := entities.DefaultDeployGateConfig()
	cfg.MinTrades = 5
	_, err := e.proposals.SaveGateConfig(context.Background(), cfg)
	require.NoError(t, err)
	res, errText = e.call(t, a, "deploy_to_testing", deployArgs(s.ID))
	require.Empty(t, errText)
	assert.Contains(t, res, `"deployed":true`)
}

// Insufficient history fails the gate (never passes by default).
func TestGateRunner_InsufficientHistoryFails(t *testing.T) {
	e := newPhaseBEnv(t)
	wf := &fakeWalkForward{}
	e.uc.Gate = agentusecase.NewGateRunner(wf, noCandles{}, e.proposals)
	s := e.strategy(t, "Dry", "dryrun", entities.Testing)
	a := e.agent(t, "Improver", 5, allPerms, s.ID)
	res, errText := e.call(t, a, "deploy_to_testing", deployArgs(s.ID))
	require.Empty(t, errText)
	assert.Contains(t, res, `"deployed":false`)
	assert.Contains(t, res, deploygate.CheckInsufficientHistory)
	assert.Empty(t, wf.reqs)
}

type noCandles struct{}

func (noCandles) CountInRange(context.Context, string, string, time.Time, time.Time) (int64, error) {
	return 1000, nil
}
