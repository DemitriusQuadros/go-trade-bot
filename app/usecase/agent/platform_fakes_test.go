package agent_test

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"

	"go-trade-bot/app/entities"
	repoagent "go-trade-bot/app/repository/agent"
	"go-trade-bot/app/repository/agentplatform"
	"go-trade-bot/internal/customerror"
	"go-trade-bot/internal/notifier"
	"go-trade-bot/internal/report/agentreport"
)

// fakePlatform is an in-memory agentplatform.Repository for usecase tests
// (no DB). Only the behaviour the usecase relies on is modelled.
type fakePlatform struct {
	mu       sync.Mutex
	agents   map[uint]entities.Agent
	bindings map[uint][]uint
	memory   []entities.StrategyMemoryEntry
	reports  []entities.AgentReport
	targets  map[uint]entities.WebhookTarget
	usage    map[string]*entities.AgentUsage
	nextID   uint
}

func newFakePlatform() *fakePlatform {
	return &fakePlatform{
		agents: map[uint]entities.Agent{}, bindings: map[uint][]uint{},
		targets: map[uint]entities.WebhookTarget{}, usage: map[string]*entities.AgentUsage{},
	}
}

func (f *fakePlatform) id() uint { f.nextID++; return f.nextID }

func usageKey(agentID uint, day time.Time) string {
	return fmt.Sprintf("%d|%s", agentID, entities.UsageDay(day).Format("2006-01-02"))
}

func (f *fakePlatform) addAgent(a entities.Agent) entities.Agent {
	f.mu.Lock()
	defer f.mu.Unlock()
	if a.ID == 0 {
		a.ID = f.id()
	}
	f.agents[a.ID] = a
	return a
}

func (f *fakePlatform) CreateAgent(_ context.Context, a entities.Agent) (entities.Agent, error) {
	return f.addAgent(a), nil
}
func (f *fakePlatform) UpdateAgent(_ context.Context, a entities.Agent) (entities.Agent, error) {
	return f.addAgent(a), nil
}
func (f *fakePlatform) GetAgent(_ context.Context, id uint) (entities.Agent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.agents[id]
	if !ok {
		return a, &customerror.CustomError{Code: http.StatusNotFound, Message: "agent not found"}
	}
	return a, nil
}
func (f *fakePlatform) GetAgentByName(_ context.Context, name string) (entities.Agent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, a := range f.agents {
		if a.Name == name {
			return a, nil
		}
	}
	return entities.Agent{}, &customerror.CustomError{Code: http.StatusNotFound, Message: "agent not found"}
}
func (f *fakePlatform) GetDefaultAgent(_ context.Context) (entities.Agent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, a := range f.agents {
		if a.IsDefault {
			return a, nil
		}
	}
	return entities.Agent{}, fmt.Errorf("no default agent")
}
func (f *fakePlatform) ListAgents(_ context.Context) ([]entities.Agent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]entities.Agent, 0, len(f.agents))
	for _, a := range f.agents {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
func (f *fakePlatform) DeleteAgent(_ context.Context, id uint) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.agents, id)
	return nil
}
func (f *fakePlatform) SetAgentPaused(ctx context.Context, id uint, paused bool) (entities.Agent, error) {
	a, err := f.GetAgent(ctx, id)
	if err != nil {
		return a, err
	}
	a.Paused = paused
	return f.addAgent(a), nil
}
func (f *fakePlatform) EnsureDefaultAgent(ctx context.Context) (entities.Agent, error) {
	if a, err := f.GetDefaultAgent(ctx); err == nil {
		return a, nil
	}
	return f.addAgent(entities.Agent{Name: entities.DefaultAgentName, IsDefault: true, Permissions: agentplatform.DefaultAgentPermissions()}), nil
}
func (f *fakePlatform) SetBindings(_ context.Context, agentID uint, ids []uint) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bindings[agentID] = append([]uint(nil), ids...)
	return nil
}
func (f *fakePlatform) ListBindingsByAgent(_ context.Context, agentID uint) ([]entities.AgentStrategyBinding, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []entities.AgentStrategyBinding
	for _, sid := range f.bindings[agentID] {
		out = append(out, entities.AgentStrategyBinding{AgentID: agentID, StrategyID: sid})
	}
	return out, nil
}
func (f *fakePlatform) ListAgentsByStrategy(context.Context, uint) ([]entities.Agent, error) {
	return nil, nil
}
func (f *fakePlatform) AppendMemory(_ context.Context, e entities.StrategyMemoryEntry) (entities.StrategyMemoryEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	e.ID = f.id()
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now()
	}
	f.memory = append(f.memory, e)
	return e, nil
}
func (f *fakePlatform) ListMemory(_ context.Context, strategyID uint, kinds []entities.MemoryKind, limit int, beforeID *uint) ([]entities.StrategyMemoryEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []entities.StrategyMemoryEntry
	for i := len(f.memory) - 1; i >= 0 && (limit <= 0 || len(out) < limit); i-- {
		e := f.memory[i]
		if e.StrategyID != strategyID || (beforeID != nil && e.ID >= *beforeID) {
			continue
		}
		if len(kinds) > 0 {
			match := false
			for _, k := range kinds {
				match = match || k == e.Kind
			}
			if !match {
				continue
			}
		}
		out = append(out, e)
	}
	return out, nil
}
func (f *fakePlatform) memoryFor(strategyID uint, kind entities.MemoryKind) []entities.StrategyMemoryEntry {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []entities.StrategyMemoryEntry
	for _, e := range f.memory {
		if e.StrategyID == strategyID && e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}
func (f *fakePlatform) CreateReport(_ context.Context, r entities.AgentReport) (entities.AgentReport, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r.ID = f.id()
	f.reports = append(f.reports, r)
	return r, nil
}
func (f *fakePlatform) GetReport(_ context.Context, id uint) (entities.AgentReport, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.reports {
		if r.ID == id {
			return r, nil
		}
	}
	return entities.AgentReport{}, fmt.Errorf("report %d not found", id)
}
func (f *fakePlatform) ListReports(_ context.Context, filter agentplatform.ReportFilter) ([]entities.AgentReport, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []entities.AgentReport
	for i := len(f.reports) - 1; i >= 0; i-- {
		r := f.reports[i]
		if filter.StrategyID != nil {
			found := false
			for _, s := range r.StrategyIDs {
				found = found || s == *filter.StrategyID
			}
			if !found {
				continue
			}
		}
		out = append(out, r)
	}
	return out, nil
}
func (f *fakePlatform) CreateWebhookTarget(_ context.Context, t entities.WebhookTarget) (entities.WebhookTarget, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t.ID = f.id()
	f.targets[t.ID] = t
	return t, nil
}
func (f *fakePlatform) UpdateWebhookTarget(_ context.Context, t entities.WebhookTarget) (entities.WebhookTarget, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.targets[t.ID] = t
	return t, nil
}
func (f *fakePlatform) GetWebhookTarget(_ context.Context, id uint) (entities.WebhookTarget, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.targets[id], nil
}
func (f *fakePlatform) ListWebhookTargets(context.Context) ([]entities.WebhookTarget, error) {
	return nil, nil
}
func (f *fakePlatform) DeleteWebhookTarget(context.Context, uint) error { return nil }
func (f *fakePlatform) ListWebhookTargetsByIDs(_ context.Context, ids []uint) ([]entities.WebhookTarget, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []entities.WebhookTarget
	for _, id := range ids {
		if t, ok := f.targets[id]; ok {
			out = append(out, t)
		}
	}
	return out, nil
}
func (f *fakePlatform) usageRow(agentID uint, day time.Time) *entities.AgentUsage {
	k := usageKey(agentID, day)
	if f.usage[k] == nil {
		f.usage[k] = &entities.AgentUsage{AgentID: agentID, Day: entities.UsageDay(day)}
	}
	return f.usage[k]
}
func (f *fakePlatform) AddUsage(_ context.Context, agentID uint, day time.Time, in, out int64, cost float64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	u := f.usageRow(agentID, day)
	u.InputTokens += in
	u.OutputTokens += out
	u.CostUSD += cost
	return nil
}
func (f *fakePlatform) IncRuns(_ context.Context, agentID uint, day time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.usageRow(agentID, day).Runs++
	return nil
}
func (f *fakePlatform) GetUsage(_ context.Context, agentID uint, day time.Time) (entities.AgentUsage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return *f.usageRow(agentID, day), nil
}
func (f *fakePlatform) ListUsage(context.Context, uint, time.Time, time.Time) ([]entities.AgentUsage, error) {
	return nil, nil
}
func (f *fakePlatform) MarkBudgetAlertSent(_ context.Context, agentID uint, day time.Time) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u := f.usageRow(agentID, day)
	if u.BudgetAlertSent {
		return false, nil
	}
	u.BudgetAlertSent = true
	return true, nil
}

func (f *fakePlatform) AddBinding(_ context.Context, agentID, strategyID uint) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, id := range f.bindings[agentID] {
		if id == strategyID {
			return nil
		}
	}
	f.bindings[agentID] = append(f.bindings[agentID], strategyID)
	return nil
}
func (f *fakePlatform) TryIncAutoDeploys(_ context.Context, agentID uint, day time.Time, max int) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u := f.usageRow(agentID, day)
	if u.AutoDeploys >= max {
		return false, nil
	}
	u.AutoDeploys++
	return true, nil
}
func (f *fakePlatform) TryIncStrategiesCreated(_ context.Context, agentID uint, day time.Time, max int) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u := f.usageRow(agentID, day)
	if u.StrategiesCreated >= max {
		return false, nil
	}
	u.StrategiesCreated++
	return true, nil
}

var _ agentplatform.Repository = (*fakePlatform)(nil)

// fakeSettings is a mutable SettingsReader.
type fakeSettings struct {
	mu     sync.Mutex
	paused bool
}

func (s *fakeSettings) set(p bool) { s.mu.Lock(); s.paused = p; s.mu.Unlock() }
func (s *fakeSettings) Get(context.Context) (*entities.Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return &entities.Settings{AgentsPaused: s.paused}, nil
}

// recordingNotifier records every SendToTargets call.
type recordingNotifier struct {
	mu    sync.Mutex
	calls []notifier.AgentMessage
}

func (n *recordingNotifier) SendToTargets(_ context.Context, _ []entities.WebhookTarget, m notifier.AgentMessage) []error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.calls = append(n.calls, m)
	return nil
}

func (n *recordingNotifier) count() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.calls)
}

// staticData is an agentreport.DataSource with one backtest run (7).
type staticData struct{}

func (staticData) BacktestRun(_ context.Context, id uint) (agentreport.Series, error) {
	if id != 7 {
		return agentreport.Series{}, fmt.Errorf("backtest run %d not found", id)
	}
	return agentreport.Series{Label: "Backtest #7", Metrics: agentreport.Metrics{Sharpe: 1.5, TotalTrades: 3}}, nil
}
func (staticData) StrategyLive(context.Context, uint, int) (agentreport.Series, error) {
	return agentreport.Series{}, nil
}
func (staticData) ScriptVersion(context.Context, uint, uint) (string, error) { return "", nil }
func (staticData) CurrentScript(context.Context, uint) (string, error)       { return "", nil }
func (staticData) PreviousScript(context.Context, uint) (string, error)      { return "", nil }

// fakeRunRepo is an in-memory app/repository/agent.Repository.
type fakeRunRepo struct {
	mu   sync.Mutex
	runs map[uint]entities.AgentRun
	next uint
}

func newFakeRunRepo() *fakeRunRepo { return &fakeRunRepo{runs: map[uint]entities.AgentRun{}} }

func (r *fakeRunRepo) GetInstruction(context.Context) (entities.AgentInstruction, error) {
	return entities.AgentInstruction{Content: "house rules"}, nil
}
func (r *fakeRunRepo) SaveInstruction(_ context.Context, c string) (entities.AgentInstruction, error) {
	return entities.AgentInstruction{Content: c}, nil
}
func (r *fakeRunRepo) CreateRun(_ context.Context, run entities.AgentRun) (entities.AgentRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.next++
	run.ID = r.next
	r.runs[run.ID] = run
	return run, nil
}
func (r *fakeRunRepo) UpdateRun(_ context.Context, run entities.AgentRun) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.runs[run.ID] = run
	return nil
}
func (r *fakeRunRepo) GetRun(_ context.Context, id uint) (entities.AgentRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.runs[id], nil
}
func (r *fakeRunRepo) ListRuns(context.Context, int, *uint) ([]entities.AgentRun, error) {
	return nil, nil
}
func (r *fakeRunRepo) ListRunsFiltered(context.Context, repoagent.RunFilter) ([]entities.AgentRun, error) {
	return nil, nil
}

// fakeStrategies is an agent.StrategyUseCase whose strategies 1..99 exist
// as testing/dryrun unless overridden; it records writes.
type fakeStrategies struct {
	mu        sync.Mutex
	overrides map[uint]entities.Strategy
	updates   int
	saves     int
	onUpdate  func()
}

func (f *fakeStrategies) GetByID(_ context.Context, id uint) (entities.Strategy, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s, ok := f.overrides[id]; ok {
		return s, nil
	}
	if id == 0 || id >= 100 {
		return entities.Strategy{}, fmt.Errorf("strategy %d not found", id)
	}
	return entities.Strategy{ID: id, Name: fmt.Sprintf("Strategy %d", id), Status: entities.Testing, Mode: "dryrun"}, nil
}
func (f *fakeStrategies) GetAll(context.Context) ([]entities.Strategy, error) { return nil, nil }
func (f *fakeStrategies) Save(_ context.Context, s entities.Strategy) (entities.Strategy, error) {
	f.mu.Lock()
	f.saves++
	f.mu.Unlock()
	s.ID = 50
	return s, nil
}
func (f *fakeStrategies) Update(_ context.Context, s entities.Strategy) error {
	f.mu.Lock()
	f.updates++
	hook := f.onUpdate
	f.mu.Unlock()
	if hook != nil {
		hook()
	}
	return nil
}
func (f *fakeStrategies) writes() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.updates + f.saves
}
