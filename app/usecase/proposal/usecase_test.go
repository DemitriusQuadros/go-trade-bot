package proposal_test

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"go-trade-bot/app/entities"
	"go-trade-bot/app/repository/agentplatform"
	proposalrepo "go-trade-bot/app/repository/proposal"
	strategyrepo "go-trade-bot/app/repository/strategy"
	"go-trade-bot/app/strategies"
	"go-trade-bot/app/usecase/proposal"
	strategyusecase "go-trade-bot/app/usecase/strategy"
	"go-trade-bot/internal/customerror"
	"go-trade-bot/internal/lock"
	"go-trade-bot/internal/notifier"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type fakeEnqueuer struct {
	mu     sync.Mutex
	calls  []time.Duration
	failed bool
}

func (f *fakeEnqueuer) EnqueueApplyProposal(_ context.Context, _ uint, delay time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failed {
		return errors.New("redis down")
	}
	f.calls = append(f.calls, delay)
	return nil
}

type noopWorker struct{}

func (noopWorker) EnqueueStrategyTask(entities.Strategy) error { return nil }

type recNotifier struct {
	mu   sync.Mutex
	msgs []notifier.AgentMessage
}

func (r *recNotifier) SendToTargets(_ context.Context, _ []entities.WebhookTarget, m notifier.AgentMessage) []error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.msgs = append(r.msgs, m)
	return nil
}

type env struct {
	db        *gorm.DB
	uc        *proposal.UseCase
	applier   *proposal.Applier
	proposals *proposalrepo.GormRepository
	enqueuer  *fakeEnqueuer
	notifier  *recNotifier
	lock      *lock.MemoryStrategyLock
	champion  entities.Strategy
	challeng  entities.Strategy
	agent     entities.Agent
}

func newEnv(t *testing.T) *env {
	t.Helper()
	if !strategies.Exists("script") {
		strategies.Register("script", func(entities.Strategy) strategies.Strategy { return nil })
	}
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&entities.Strategy{}, &entities.Signal{}, &entities.Order{}, &entities.ScriptVersion{}, &entities.ScriptState{},
		&entities.Agent{}, &entities.AgentStrategyBinding{}, &entities.StrategyMemoryEntry{}, &entities.WebhookTarget{},
		&entities.StrategyChangeProposal{}, &entities.DeployGateConfig{},
	))
	platform := agentplatform.NewGormRepository(db)
	target, err := platform.CreateWebhookTarget(context.Background(), entities.WebhookTarget{Name: "t", Kind: entities.WebhookGeneric, URL: "http://x", Enabled: true})
	require.NoError(t, err)
	agent, err := platform.CreateAgent(context.Background(), entities.Agent{Name: "Improver", WebhookTargetIDs: datatypes.JSONSlice[uint]{target.ID}})
	require.NoError(t, err)

	champion := entities.Strategy{Name: "Champion", Description: "d", StrategyName: "script", ScriptSource: "old", Mode: "live", Status: entities.Productive,
		MonitoredSymbols: datatypes.JSONSlice[string]{"BTCUSDT"}, StrategyConfiguration: entities.StrategyConfiguration{Cycle: entities.FifteenMinutes}}
	require.NoError(t, db.Create(&champion).Error)
	cid := champion.ID
	challenger := entities.Strategy{Name: "Champion · challenger", Description: "d", StrategyName: "script", ScriptSource: "new", Mode: "dryrun", Status: entities.Testing,
		MonitoredSymbols: datatypes.JSONSlice[string]{"BTCUSDT"}, StrategyConfiguration: entities.StrategyConfiguration{Cycle: entities.FifteenMinutes}, ChallengerOfID: &cid}
	require.NoError(t, db.Create(&challenger).Error)
	require.NoError(t, db.Create(&entities.ScriptState{StrategyID: champion.ID, Symbol: "BTCUSDT", StateJSON: datatypes.JSON(`{"bars":3}`)}).Error)

	srepo := strategyrepo.NewStrategyRepository(db)
	prepo := proposalrepo.NewGormRepository(db)
	enq := &fakeEnqueuer{}
	rec := &recNotifier{}
	l := lock.NewMemoryStrategyLock()
	return &env{
		db: db, proposals: prepo, enqueuer: enq, notifier: rec, lock: l, champion: champion, challeng: challenger, agent: agent,
		uc: proposal.NewUseCase(prepo, srepo, platform, enq),
		applier: &proposal.Applier{
			Repo: prepo, Strategies: srepo, Status: strategyusecase.NewStrategyUseCase(srepo, noopWorker{}),
			Lock: l, Platform: platform, Notifier: rec, APIBaseURL: "http://bot.local",
		},
	}
}

func (e *env) promotion(t *testing.T) entities.StrategyChangeProposal {
	t.Helper()
	ch := e.challeng.ID
	p, err := e.proposals.Create(context.Background(), entities.StrategyChangeProposal{
		Kind: entities.ProposalPromoteChallenger, TargetStrategyID: e.champion.ID, ChallengerStrategyID: &ch, AgentID: e.agent.ID,
		BaseSource: "old", ProposedSource: "new", Rationale: "better", EvidenceJSON: datatypes.JSON(`{"gate":{"passed":true}}`),
	})
	require.NoError(t, err)
	return p
}

func (e *env) openSignal(t *testing.T, strategyID uint) entities.Signal {
	t.Helper()
	s := entities.Signal{StrategyID: strategyID, Symbol: "BTCUSDT", Status: entities.Open}
	require.NoError(t, e.db.Create(&s).Error)
	return s
}

func (e *env) strategy(t *testing.T, id uint) entities.Strategy {
	var s entities.Strategy
	require.NoError(t, e.db.First(&s, id).Error)
	return s
}

// AC#7: approve while the target has an open signal -> stays approved and
// the apply re-schedules; after the signal closes -> applied with the
// proposed source, Mode/Status unchanged, ScriptVersion written, ScriptState
// cleared, challenger disabled.
func TestApproveThenApplyAtFlat(t *testing.T) {
	e := newEnv(t)
	p := e.promotion(t)
	sig := e.openSignal(t, e.champion.ID)
	ctx := context.Background()

	v, err := e.uc.Approve(ctx, p.ID, "ship it")
	require.NoError(t, err)
	assert.Equal(t, entities.ProposalApproved, v.Proposal.Status)
	assert.Equal(t, "ship it", v.Proposal.DecisionNote)
	assert.NotNil(t, v.Proposal.DecidedAt)
	assert.True(t, v.TargetHasOpenPosition)
	assert.True(t, v.TargetCurrentSourceMatchesBase)
	assert.Equal(t, []time.Duration{0}, e.enqueuer.calls)

	res, err := e.applier.Apply(ctx, p.ID)
	require.NoError(t, err)
	assert.Equal(t, proposal.OutcomeRescheduled, res.Outcome)
	assert.Equal(t, proposal.FlatRecheckInterval, res.RetryIn)
	got, _ := e.proposals.Get(ctx, p.ID)
	assert.Equal(t, entities.ProposalApproved, got.Status)
	assert.NotNil(t, got.LastFlatCheckAt)
	assert.Equal(t, "old", e.strategy(t, e.champion.ID).ScriptSource)

	require.NoError(t, e.db.Model(&entities.Signal{}).Where("id = ?", sig.ID).Update("status", entities.Closed).Error)
	res, err = e.applier.Apply(ctx, p.ID)
	require.NoError(t, err)
	assert.Equal(t, proposal.OutcomeApplied, res.Outcome)

	champ := e.strategy(t, e.champion.ID)
	assert.Equal(t, "new", champ.ScriptSource)
	assert.Equal(t, "live", champ.Mode, "apply never changes Mode")
	assert.Equal(t, entities.Productive, champ.Status, "apply never changes Status")
	var versions []entities.ScriptVersion
	require.NoError(t, e.db.Where("strategy_id = ?", e.champion.ID).Find(&versions).Error)
	require.Len(t, versions, 1)
	assert.Equal(t, "new", versions[0].Source)
	var states int64
	e.db.Model(&entities.ScriptState{}).Where("strategy_id = ?", e.champion.ID).Count(&states)
	assert.Zero(t, states)
	assert.Equal(t, entities.Disabled, e.strategy(t, e.challeng.ID).Status)

	got, _ = e.proposals.Get(ctx, p.ID)
	assert.Equal(t, entities.ProposalApplied, got.Status)
	assert.NotNil(t, got.AppliedAt)
	var mem []entities.StrategyMemoryEntry
	require.NoError(t, e.db.Where("strategy_id = ? AND kind = ?", e.champion.ID, entities.MemoryFinding).Find(&mem).Error)
	require.Len(t, mem, 1)
	assert.Nil(t, mem[0].AuthorAgentID)
	assert.Contains(t, mem[0].Content, "Promotion #")
	require.Len(t, e.notifier.msgs, 1)
	assert.Equal(t, "http://bot.local/agents/proposals/"+itoa(p.ID), e.notifier.msgs[0].Link)

	// A duplicate delivery is a no-op.
	res, err = e.applier.Apply(ctx, p.ID)
	require.NoError(t, err)
	assert.Equal(t, proposal.OutcomeSkipped, res.Outcome)
}

// AC#8 (usecase): approving when the target's source changed -> superseded.
func TestApprove_TargetChanged_Superseded(t *testing.T) {
	e := newEnv(t)
	p := e.promotion(t)
	require.NoError(t, e.db.Model(&entities.Strategy{}).Where("id = ?", e.champion.ID).Update("script_source", "edited by operator").Error)

	_, err := e.uc.Approve(context.Background(), p.ID, "")
	var sup *proposal.SupersededError
	require.ErrorAs(t, err, &sup)
	assert.ErrorIs(t, err, proposal.ErrSuperseded)
	got, _ := e.proposals.Get(context.Background(), p.ID)
	assert.Equal(t, entities.ProposalSuperseded, got.Status)
	assert.Empty(t, e.enqueuer.calls)
}

// The target changing after approval supersedes at apply time.
func TestApply_TargetChangedAfterApproval_Superseded(t *testing.T) {
	e := newEnv(t)
	p := e.promotion(t)
	_, err := e.uc.Approve(context.Background(), p.ID, "")
	require.NoError(t, err)
	require.NoError(t, e.db.Model(&entities.Strategy{}).Where("id = ?", e.champion.ID).Update("script_source", "edited").Error)
	res, err := e.applier.Apply(context.Background(), p.ID)
	require.NoError(t, err)
	assert.Equal(t, proposal.OutcomeSuperseded, res.Outcome)
	assert.Equal(t, "edited", e.strategy(t, e.champion.ID).ScriptSource)
	require.Len(t, e.notifier.msgs, 1)
}

func TestApply_NeverFlatFailsAfterSevenDays(t *testing.T) {
	e := newEnv(t)
	p := e.promotion(t)
	e.openSignal(t, e.champion.ID)
	_, err := e.uc.Approve(context.Background(), p.ID, "")
	require.NoError(t, err)
	e.applier.Now = func() time.Time { return time.Now().Add(proposal.NeverFlatTimeout + time.Hour) }
	res, err := e.applier.Apply(context.Background(), p.ID)
	require.NoError(t, err)
	assert.Equal(t, proposal.OutcomeFailed, res.Outcome)
	got, _ := e.proposals.Get(context.Background(), p.ID)
	assert.Equal(t, entities.ProposalFailed, got.Status)
	assert.Contains(t, got.FailureReason, "never flat")
	assert.Equal(t, "old", e.strategy(t, e.champion.ID).ScriptSource)
}

// While the worker holds strategy-cycle:<id>, the apply does not write.
func TestApply_CycleLockBusy_Reschedules(t *testing.T) {
	e := newEnv(t)
	p := e.promotion(t)
	_, err := e.uc.Approve(context.Background(), p.ID, "")
	require.NoError(t, err)
	ok, _ := e.lock.Acquire(context.Background(), e.champion.ID, "worker-cycle", time.Minute)
	require.True(t, ok)
	res, err := e.applier.Apply(context.Background(), p.ID)
	require.NoError(t, err)
	assert.Equal(t, proposal.OutcomeRescheduled, res.Outcome)
	assert.Equal(t, proposal.CycleLockBusyRetry, res.RetryIn)
	assert.Equal(t, "old", e.strategy(t, e.champion.ID).ScriptSource)
}

// A gate_failed_change on a non-live target applies immediately even with
// an open (dryrun) position, and keeps that position's script state.
func TestApply_GateFailedChangeOnNonLiveAppliesImmediately(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	dry := e.challeng // dryrun/testing
	e.openSignal(t, dry.ID)
	p, err := e.proposals.Create(ctx, entities.StrategyChangeProposal{
		Kind: entities.ProposalGateFailedChange, TargetStrategyID: dry.ID, AgentID: e.agent.ID, BaseSource: "new", ProposedSource: "newer",
	})
	require.NoError(t, err)
	_, err = e.uc.Approve(ctx, p.ID, "")
	require.NoError(t, err)
	res, err := e.applier.Apply(ctx, p.ID)
	require.NoError(t, err)
	assert.Equal(t, proposal.OutcomeApplied, res.Outcome)
	got := e.strategy(t, dry.ID)
	assert.Equal(t, "newer", got.ScriptSource)
	assert.Equal(t, "dryrun", got.Mode)
	assert.Equal(t, entities.Testing, got.Status)
}

// A gate_failed_change whose target became live waits for flat like a
// promotion.
func TestApply_GateFailedChangeOnLiveTargetWaitsForFlat(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.openSignal(t, e.champion.ID)
	p, err := e.proposals.Create(ctx, entities.StrategyChangeProposal{
		Kind: entities.ProposalGateFailedChange, TargetStrategyID: e.champion.ID, AgentID: e.agent.ID, BaseSource: "old", ProposedSource: "newer",
	})
	require.NoError(t, err)
	_, err = e.uc.Approve(ctx, p.ID, "")
	require.NoError(t, err)
	res, err := e.applier.Apply(ctx, p.ID)
	require.NoError(t, err)
	assert.Equal(t, proposal.OutcomeRescheduled, res.Outcome)
}

func TestRejectAndDecisionConflicts(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	p := e.promotion(t)
	v, err := e.uc.Reject(ctx, p.ID, "no")
	require.NoError(t, err)
	assert.Equal(t, entities.ProposalRejected, v.Proposal.Status)
	for _, fn := range []func(context.Context, uint, string) (proposal.View, error){e.uc.Approve, e.uc.Reject} {
		_, err = fn(ctx, p.ID, "")
		var ce *customerror.CustomError
		require.ErrorAs(t, err, &ce)
		assert.Equal(t, http.StatusConflict, ce.Code)
	}
	// Apply of a non-approved proposal does nothing.
	res, err := e.applier.Apply(ctx, p.ID)
	require.NoError(t, err)
	assert.Equal(t, proposal.OutcomeSkipped, res.Outcome)
}

// If the apply task can't be enqueued the proposal goes back to pending.
func TestApprove_EnqueueFailureRevertsToPending(t *testing.T) {
	e := newEnv(t)
	e.enqueuer.failed = true
	p := e.promotion(t)
	_, err := e.uc.Approve(context.Background(), p.ID, "")
	require.Error(t, err)
	got, _ := e.proposals.Get(context.Background(), p.ID)
	assert.Equal(t, entities.ProposalPending, got.Status)
}

func TestListViewsAndPendingCount(t *testing.T) {
	e := newEnv(t)
	e.promotion(t)
	views, err := e.uc.List(context.Background(), proposal.ListFilter{Statuses: []entities.ProposalStatus{entities.ProposalPending}})
	require.NoError(t, err)
	require.Len(t, views, 1)
	assert.Equal(t, "Champion", views[0].TargetStrategyName)
	assert.Equal(t, "Improver", views[0].AgentName)
	n, err := e.uc.PendingCount(context.Background())
	require.NoError(t, err)
	assert.EqualValues(t, 1, n)
}

func TestValidateGateConfig(t *testing.T) {
	ok := entities.DefaultDeployGateConfig()
	require.NoError(t, proposal.ValidateGateConfig(&ok))
	for name, mutate := range map[string]func(c *entities.DeployGateConfig){
		"ratio 0":            func(c *entities.DeployGateConfig) { c.MaxDrawdownRatio = 0 },
		"pf 0":               func(c *entities.DeployGateConfig) { c.MinProfitFactor = 0 },
		"trades 0":           func(c *entities.DeployGateConfig) { c.MinTrades = 0 },
		"train 0":            func(c *entities.DeployGateConfig) { c.TrainMonths = 0 },
		"test 0":             func(c *entities.DeployGateConfig) { c.TestMonths = 0 },
		"lookback too short": func(c *entities.DeployGateConfig) { c.LookbackMonths = 3 },
		"bad timeframe":      func(c *entities.DeployGateConfig) { c.Timeframe = "7m" },
	} {
		c := entities.DefaultDeployGateConfig()
		mutate(&c)
		assert.Errorf(t, proposal.ValidateGateConfig(&c), name)
	}
}

func itoa(v uint) string { return strconv.FormatUint(uint64(v), 10) }
