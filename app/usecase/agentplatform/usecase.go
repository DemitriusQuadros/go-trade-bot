// Package agentplatform is the management usecase behind the agents
// platform REST API (agents-platform A-02 §5): agent persona CRUD +
// validation, manual runs, usage, reports, webhook targets, strategy
// memory and the global kill switch. It never runs an agent itself (runs
// happen in cmd/agent, or in app/usecase/agent for chat).
package agentplatform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"go-trade-bot/app/entities"
	repo "go-trade-bot/app/repository/agentplatform"
	agentworker "go-trade-bot/app/workers/agent"
	"go-trade-bot/internal/cronspec"
	"go-trade-bot/internal/customerror"
	"go-trade-bot/internal/notifier"

	"gorm.io/datatypes"
)

// Repository is the persistence port (app/repository/agentplatform).
type Repository = repo.Repository

// ReportFilter re-exports the repository filter.
type ReportFilter = repo.ReportFilter

// StrategyReader resolves strategy existence/details.
type StrategyReader interface {
	GetByID(ctx context.Context, id uint) (entities.Strategy, error)
}

// RunReader lists AgentRun rows per agent (app/repository/agent.GormRepository).
type RunReader interface {
	ListRunsByAgent(ctx context.Context, agentID uint, limit int, beforeID *uint) ([]entities.AgentRun, error)
	LastRunByAgent(ctx context.Context, agentID uint) (*entities.AgentRun, error)
}

// RunEnqueuer enqueues agent:run tasks (app/workers/agent.AgentWorker).
type RunEnqueuer interface {
	EnqueueRun(ctx context.Context, p agentworker.RunPayload) (string, error)
}

// SettingsStore reads/writes the kill switch (app/repository/settings).
type SettingsStore interface {
	Get(ctx context.Context) (*entities.Settings, error)
	SetAgentsPaused(ctx context.Context, paused bool) error
}

// TestSender sends one synchronous test notification.
type TestSender interface {
	SendSync(ctx context.Context, target entities.WebhookTarget, m notifier.AgentMessage) error
}

// UseCase implements the agents platform management API.
type UseCase struct {
	repo       Repository
	strategies StrategyReader
	runs       RunReader
	enqueuer   RunEnqueuer
	settings   SettingsStore
	sender     TestSender
	now        func() time.Time
}

// NewUseCase builds a UseCase.
func NewUseCase(r Repository, s StrategyReader, runs RunReader, e RunEnqueuer, settings SettingsStore, sender TestSender) *UseCase {
	return &UseCase{repo: r, strategies: s, runs: runs, enqueuer: e, settings: settings, sender: sender, now: time.Now}
}

func badRequest(format string, args ...any) error {
	return &customerror.CustomError{Code: http.StatusBadRequest, Message: fmt.Sprintf(format, args...)}
}

func conflict(format string, args ...any) error {
	return &customerror.CustomError{Code: http.StatusConflict, Message: fmt.Sprintf(format, args...)}
}

func notFound(format string, args ...any) error {
	return &customerror.CustomError{Code: http.StatusNotFound, Message: fmt.Sprintf(format, args...)}
}

// StatusOf returns the HTTP status carried by err (500 if none).
func StatusOf(err error) int {
	var ce *customerror.CustomError
	if errors.As(err, &ce) {
		return ce.Code
	}
	return http.StatusInternalServerError
}

// --- Agents ------------------------------------------------------------------

// AgentInput is the validated write model for POST/PUT /agents.
type AgentInput struct {
	Name                 string
	Goal                 string
	Provider             string
	Model                string
	Permissions          []string
	Triggers             entities.AgentTriggers
	StrategyIDs          []uint
	WebhookTargetIDs     []uint
	DailyBudgetUSD       float64
	MaxAutoDeploysPerDay int
}

// AgentView is an agent plus the derived fields the API returns.
type AgentView struct {
	Agent        entities.Agent
	StrategyIDs  []uint
	TodayCostUSD float64
	LastRun      *entities.AgentRun
	NextRunAt    *time.Time
}

const (
	maxNameLen  = 100
	maxGoalLen  = 20000
	maxModelLen = 100
)

func (u *UseCase) validateInput(ctx context.Context, in *AgentInput, existing *entities.Agent) error {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return badRequest("name is required")
	}
	if len([]rune(in.Name)) > maxNameLen {
		return badRequest("name must be at most %d characters", maxNameLen)
	}
	if existing != nil && existing.IsDefault && in.Name != entities.DefaultAgentName {
		return badRequest("the default agent must keep the name %q", entities.DefaultAgentName)
	}
	if len([]rune(in.Goal)) > maxGoalLen {
		return badRequest("goal must be at most %d characters", maxGoalLen)
	}
	switch in.Provider {
	case "", "anthropic", "gemini":
	default:
		return badRequest("provider must be one of \"\", \"anthropic\", \"gemini\"")
	}
	in.Model = strings.TrimSpace(in.Model)
	if len(in.Model) > maxModelLen {
		return badRequest("model must be at most %d characters", maxModelLen)
	}
	seenPerm := map[string]bool{}
	perms := make([]string, 0, len(in.Permissions))
	for _, p := range in.Permissions {
		if !entities.IsValidAgentPermission(p) {
			known := make([]string, 0, len(entities.AllAgentPermissions))
			for _, k := range entities.AllAgentPermissions {
				known = append(known, string(k))
			}
			return badRequest("unknown permission %q; allowed: %s", p, strings.Join(known, ", "))
		}
		if !seenPerm[p] {
			seenPerm[p] = true
			perms = append(perms, p)
		}
	}
	in.Permissions = perms
	for i, spec := range in.Triggers.Cron {
		spec = strings.TrimSpace(spec)
		if err := cronspec.Validate(spec); err != nil {
			return badRequest("%v", err)
		}
		in.Triggers.Cron[i] = spec
	}
	if in.DailyBudgetUSD < 0 {
		return badRequest("daily_budget_usd must be >= 0")
	}
	if in.MaxAutoDeploysPerDay < 0 {
		return badRequest("max_auto_deploys_per_day must be >= 0")
	}
	in.StrategyIDs = dedupe(in.StrategyIDs)
	for _, sid := range in.StrategyIDs {
		if s, err := u.strategies.GetByID(ctx, sid); err != nil || s.ID == 0 {
			return badRequest("strategy %d does not exist", sid)
		}
	}
	in.WebhookTargetIDs = dedupe(in.WebhookTargetIDs)
	if len(in.WebhookTargetIDs) > 0 {
		found, err := u.repo.ListWebhookTargetsByIDs(ctx, in.WebhookTargetIDs)
		if err != nil {
			return err
		}
		have := map[uint]bool{}
		for _, t := range found {
			have[t.ID] = true
		}
		for _, id := range in.WebhookTargetIDs {
			if !have[id] {
				return badRequest("webhook target %d does not exist", id)
			}
		}
	}
	// Name uniqueness -> 409.
	if other, err := u.repo.GetAgentByName(ctx, in.Name); err == nil && (existing == nil || other.ID != existing.ID) {
		return conflict("an agent named %q already exists", in.Name)
	}
	return nil
}

func dedupe(in []uint) []uint {
	seen := map[uint]bool{}
	out := make([]uint, 0, len(in))
	for _, v := range in {
		if v != 0 && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

func applyInput(a *entities.Agent, in AgentInput) {
	a.Name = in.Name
	a.Goal = in.Goal
	a.Provider = in.Provider
	a.Model = in.Model
	a.Permissions = datatypes.JSONSlice[string](in.Permissions)
	a.Triggers = repo.MarshalTriggers(in.Triggers)
	a.WebhookTargetIDs = datatypes.JSONSlice[uint](in.WebhookTargetIDs)
	a.DailyBudgetUSD = in.DailyBudgetUSD
	a.MaxAutoDeploysPerDay = in.MaxAutoDeploysPerDay
}

func (u *UseCase) view(ctx context.Context, a entities.Agent, killSwitch bool) (AgentView, error) {
	v := AgentView{Agent: a, StrategyIDs: []uint{}}
	bindings, err := u.repo.ListBindingsByAgent(ctx, a.ID)
	if err != nil {
		return v, err
	}
	for _, b := range bindings {
		v.StrategyIDs = append(v.StrategyIDs, b.StrategyID)
	}
	now := u.now()
	if usage, err := u.repo.GetUsage(ctx, a.ID, now); err == nil {
		v.TodayCostUSD = usage.CostUSD
	}
	if u.runs != nil {
		if last, err := u.runs.LastRunByAgent(ctx, a.ID); err == nil {
			v.LastRun = last
		}
	}
	// next_run_at mirrors what cmd/agent's cron provider would schedule:
	// nothing while paused, kill-switched, or unbound.
	if !a.Paused && !killSwitch && len(v.StrategyIDs) > 0 {
		if next, ok := cronspec.Next(a.ParsedTriggers().Cron, now); ok {
			v.NextRunAt = &next
		}
	}
	return v, nil
}

func (u *UseCase) killSwitch(ctx context.Context) bool {
	if u.settings == nil {
		return false
	}
	s, err := u.settings.Get(ctx)
	return err == nil && s != nil && s.AgentsPaused
}

// ListAgents returns every agent (default first).
func (u *UseCase) ListAgents(ctx context.Context) ([]AgentView, error) {
	agents, err := u.repo.ListAgents(ctx)
	if err != nil {
		return nil, err
	}
	ks := u.killSwitch(ctx)
	out := make([]AgentView, 0, len(agents))
	for _, a := range agents {
		v, err := u.view(ctx, a, ks)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// GetAgent returns one agent.
func (u *UseCase) GetAgent(ctx context.Context, id uint) (AgentView, error) {
	a, err := u.repo.GetAgent(ctx, id)
	if err != nil {
		return AgentView{}, err
	}
	return u.view(ctx, a, u.killSwitch(ctx))
}

// CreateAgent validates and creates a (non-default) agent + its bindings.
func (u *UseCase) CreateAgent(ctx context.Context, in AgentInput) (AgentView, error) {
	if err := u.validateInput(ctx, &in, nil); err != nil {
		return AgentView{}, err
	}
	var a entities.Agent
	applyInput(&a, in)
	created, err := u.repo.CreateAgent(ctx, a)
	if err != nil {
		return AgentView{}, err
	}
	if err := u.repo.SetBindings(ctx, created.ID, in.StrategyIDs); err != nil {
		return AgentView{}, err
	}
	return u.GetAgent(ctx, created.ID)
}

// UpdateAgent validates and replaces an agent's editable fields + bindings.
// Paused and IsDefault are not editable here.
func (u *UseCase) UpdateAgent(ctx context.Context, id uint, in AgentInput) (AgentView, error) {
	existing, err := u.repo.GetAgent(ctx, id)
	if err != nil {
		return AgentView{}, err
	}
	if err := u.validateInput(ctx, &in, &existing); err != nil {
		return AgentView{}, err
	}
	applyInput(&existing, in)
	if _, err := u.repo.UpdateAgent(ctx, existing); err != nil {
		return AgentView{}, err
	}
	if err := u.repo.SetBindings(ctx, id, in.StrategyIDs); err != nil {
		return AgentView{}, err
	}
	return u.GetAgent(ctx, id)
}

// DeleteAgent deletes a non-default agent (409 for the default).
func (u *UseCase) DeleteAgent(ctx context.Context, id uint) error {
	return u.repo.DeleteAgent(ctx, id)
}

// SetPaused pauses/resumes one agent.
func (u *UseCase) SetPaused(ctx context.Context, id uint, paused bool) (AgentView, error) {
	if _, err := u.repo.SetAgentPaused(ctx, id, paused); err != nil {
		return AgentView{}, err
	}
	return u.GetAgent(ctx, id)
}

const maxManualPromptLen = 4000

// EnqueueManualRun queues an agent:run (trigger manual) for cmd/agent.
// 409 when the global kill switch is on or the agent is paused.
func (u *UseCase) EnqueueManualRun(ctx context.Context, id uint, prompt string) (string, error) {
	a, err := u.repo.GetAgent(ctx, id)
	if err != nil {
		return "", err
	}
	if u.killSwitch(ctx) {
		return "", conflict("agents are paused by the global kill switch")
	}
	if a.Paused {
		return "", conflict("agent %q is paused", a.Name)
	}
	prompt = strings.TrimSpace(prompt)
	if len([]rune(prompt)) > maxManualPromptLen {
		return "", badRequest("prompt must be at most %d characters", maxManualPromptLen)
	}
	detail, _ := json.Marshal(map[string]string{"requested_by": "operator", "prompt": prompt})
	return u.enqueuer.EnqueueRun(ctx, agentworker.RunPayload{AgentID: a.ID, Trigger: "manual", Detail: detail, Prompt: prompt})
}

// ListRuns returns an agent's runs newest-first.
func (u *UseCase) ListRuns(ctx context.Context, agentID uint, limit int, beforeID *uint) ([]entities.AgentRun, entities.Agent, error) {
	a, err := u.repo.GetAgent(ctx, agentID)
	if err != nil {
		return nil, entities.Agent{}, err
	}
	runs, err := u.runs.ListRunsByAgent(ctx, agentID, limit, beforeID)
	return runs, a, err
}

// UsageDay is one day of usage.
type UsageDay struct {
	Day          time.Time
	InputTokens  int64
	OutputTokens int64
	CostUSD      float64
	Runs         int
}

// Usage returns today plus the last `days` UTC days (oldest first,
// zero-filled).
func (u *UseCase) Usage(ctx context.Context, agentID uint, days int) (UsageDay, []UsageDay, error) {
	if _, err := u.repo.GetAgent(ctx, agentID); err != nil {
		return UsageDay{}, nil, err
	}
	if days <= 0 {
		days = 30
	}
	if days > 365 {
		days = 365
	}
	today := entities.UsageDay(u.now())
	from := today.AddDate(0, 0, -(days - 1))
	rows, err := u.repo.ListUsage(ctx, agentID, from, today)
	if err != nil {
		return UsageDay{}, nil, err
	}
	byDay := map[string]entities.AgentUsage{}
	for _, r := range rows {
		byDay[r.Day.UTC().Format("2006-01-02")] = r
	}
	out := make([]UsageDay, 0, days)
	for d := from; !d.After(today); d = d.AddDate(0, 0, 1) {
		r := byDay[d.Format("2006-01-02")]
		out = append(out, UsageDay{Day: d, InputTokens: r.InputTokens, OutputTokens: r.OutputTokens, CostUSD: r.CostUSD, Runs: r.Runs})
	}
	return out[len(out)-1], out, nil
}

// AgentNames maps agent id -> name.
func (u *UseCase) AgentNames(ctx context.Context) map[uint]string {
	out := map[uint]string{}
	if agents, err := u.repo.ListAgents(ctx); err == nil {
		for _, a := range agents {
			out[a.ID] = a.Name
		}
	}
	return out
}

// KillSwitch is the global kill switch.
func (u *UseCase) KillSwitch(ctx context.Context) (bool, error) {
	s, err := u.settings.Get(ctx)
	if err != nil {
		return false, err
	}
	return s != nil && s.AgentsPaused, nil
}

// SetKillSwitch writes ONLY Settings.AgentsPaused (no drain/swap, no mode
// validation).
func (u *UseCase) SetKillSwitch(ctx context.Context, paused bool) (bool, error) {
	if err := u.settings.SetAgentsPaused(ctx, paused); err != nil {
		return false, err
	}
	return u.KillSwitch(ctx)
}

// --- Reports -----------------------------------------------------------------

// ListReports lists reports (without rendered HTML).
func (u *UseCase) ListReports(ctx context.Context, f ReportFilter) ([]entities.AgentReport, error) {
	if f.Severity != "" && !entities.IsValidReportSeverity(f.Severity) {
		return nil, badRequest("severity must be info, warning or critical")
	}
	return u.repo.ListReports(ctx, f)
}

// GetReport returns one report including its rendered HTML.
func (u *UseCase) GetReport(ctx context.Context, id uint) (entities.AgentReport, error) {
	return u.repo.GetReport(ctx, id)
}

// --- Webhook targets ---------------------------------------------------------

// WebhookTargetInput is the write model for webhook targets. KeepURL /
// KeepSecret mean "keep the stored value" (masked placeholder or empty on
// update).
type WebhookTargetInput struct {
	Name       string
	Kind       string
	URL        string
	Secret     string
	ChatID     string
	Enabled    bool
	KeepURL    bool
	KeepSecret bool
}

func validateTarget(t entities.WebhookTarget) error {
	if strings.TrimSpace(t.Name) == "" {
		return badRequest("name is required")
	}
	if !entities.IsValidWebhookTargetKind(string(t.Kind)) {
		return badRequest("kind must be one of generic, discord, slack, telegram")
	}
	if t.Kind == entities.WebhookTelegram {
		if strings.TrimSpace(t.Secret) == "" {
			return badRequest("telegram targets need the bot token in secret")
		}
		if strings.TrimSpace(t.ChatID) == "" {
			return badRequest("telegram targets need chat_id")
		}
		return nil
	}
	parsed, err := url.Parse(strings.TrimSpace(t.URL))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return badRequest("url must be an absolute http(s) URL")
	}
	return nil
}

// ListWebhookTargets lists all targets.
func (u *UseCase) ListWebhookTargets(ctx context.Context) ([]entities.WebhookTarget, error) {
	return u.repo.ListWebhookTargets(ctx)
}

// CreateWebhookTarget validates and creates a target.
func (u *UseCase) CreateWebhookTarget(ctx context.Context, in WebhookTargetInput) (entities.WebhookTarget, error) {
	t := entities.WebhookTarget{Name: strings.TrimSpace(in.Name), Kind: entities.WebhookTargetKind(in.Kind), ChatID: strings.TrimSpace(in.ChatID), Enabled: in.Enabled}
	if !in.KeepURL {
		t.URL = strings.TrimSpace(in.URL)
	}
	if !in.KeepSecret {
		t.Secret = strings.TrimSpace(in.Secret)
	}
	if err := validateTarget(t); err != nil {
		return entities.WebhookTarget{}, err
	}
	return u.repo.CreateWebhookTarget(ctx, t)
}

// UpdateWebhookTarget validates and updates a target, keeping masked
// secrets.
func (u *UseCase) UpdateWebhookTarget(ctx context.Context, id uint, in WebhookTargetInput) (entities.WebhookTarget, error) {
	t, err := u.repo.GetWebhookTarget(ctx, id)
	if err != nil {
		return entities.WebhookTarget{}, err
	}
	t.Name = strings.TrimSpace(in.Name)
	t.Kind = entities.WebhookTargetKind(in.Kind)
	t.ChatID = strings.TrimSpace(in.ChatID)
	t.Enabled = in.Enabled
	if !in.KeepURL {
		t.URL = strings.TrimSpace(in.URL)
	}
	if !in.KeepSecret {
		t.Secret = strings.TrimSpace(in.Secret)
	}
	if err := validateTarget(t); err != nil {
		return entities.WebhookTarget{}, err
	}
	return u.repo.UpdateWebhookTarget(ctx, t)
}

// ReferencedError is the 409 for deleting a target agents still use.
type ReferencedError struct {
	AgentNames []string
}

func (e *ReferencedError) Error() string {
	return "webhook target is used by agents: " + strings.Join(e.AgentNames, ", ")
}

// DeleteWebhookTarget deletes a target unless an agent references it
// (*ReferencedError, HTTP 409).
func (u *UseCase) DeleteWebhookTarget(ctx context.Context, id uint) error {
	if _, err := u.repo.GetWebhookTarget(ctx, id); err != nil {
		return err
	}
	agents, err := u.repo.ListAgents(ctx)
	if err != nil {
		return err
	}
	var users []string
	for _, a := range agents {
		for _, tid := range a.WebhookTargetIDs {
			if tid == id {
				users = append(users, a.Name)
				break
			}
		}
	}
	if len(users) > 0 {
		sort.Strings(users)
		return &ReferencedError{AgentNames: users}
	}
	return u.repo.DeleteWebhookTarget(ctx, id)
}

// TestWebhookTarget sends one synchronous info test message.
func (u *UseCase) TestWebhookTarget(ctx context.Context, id uint) error {
	t, err := u.repo.GetWebhookTarget(ctx, id)
	if err != nil {
		return err
	}
	if u.sender == nil {
		return fmt.Errorf("notifications are not available on this server")
	}
	return u.sender.SendSync(ctx, t, notifier.AgentMessage{
		AgentName: "go-trade-bot",
		Severity:  string(entities.SeverityInfo),
		Title:     "Test notification",
		Message:   fmt.Sprintf("This is a test message for webhook target %q.", t.Name),
		Timestamp: u.now().UTC(),
	})
}

// --- Strategy memory ---------------------------------------------------------

const maxOperatorNoteLen = 4000

// ListMemory lists a strategy's shared memory, newest first.
func (u *UseCase) ListMemory(ctx context.Context, strategyID uint, kinds []string, limit int, beforeID *uint) ([]entities.StrategyMemoryEntry, error) {
	if s, err := u.strategies.GetByID(ctx, strategyID); err != nil || s.ID == 0 {
		return nil, notFound("strategy %d not found", strategyID)
	}
	mk := make([]entities.MemoryKind, 0, len(kinds))
	for _, k := range kinds {
		if !entities.IsValidMemoryKind(k) {
			return nil, badRequest("unknown memory kind %q", k)
		}
		mk = append(mk, entities.MemoryKind(k))
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	return u.repo.ListMemory(ctx, strategyID, mk, limit, beforeID)
}

// AddOperatorNote appends an operator-authored journal entry.
func (u *UseCase) AddOperatorNote(ctx context.Context, strategyID uint, content string) (entities.StrategyMemoryEntry, error) {
	if s, err := u.strategies.GetByID(ctx, strategyID); err != nil || s.ID == 0 {
		return entities.StrategyMemoryEntry{}, notFound("strategy %d not found", strategyID)
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return entities.StrategyMemoryEntry{}, badRequest("content is required")
	}
	if len([]rune(content)) > maxOperatorNoteLen {
		return entities.StrategyMemoryEntry{}, badRequest("content must be at most %d characters", maxOperatorNoteLen)
	}
	return u.repo.AppendMemory(ctx, entities.StrategyMemoryEntry{StrategyID: strategyID, Kind: entities.MemoryJournal, Content: content})
}
