package agents

import (
	"encoding/json"
	"time"

	"go-trade-bot/app/entities"
	usecase "go-trade-bot/app/usecase/agentplatform"
)

// TriggersDTO mirrors entities.AgentTriggers. cron is always present
// (possibly empty); the Phase C fields are accepted and echoed back.
type TriggersDTO struct {
	Cron      []string          `json:"cron"`
	Events    []string          `json:"events,omitempty"`
	Market    []json.RawMessage `json:"market,omitempty"`
	ChainFrom []uint            `json:"chain_from,omitempty"`
}

// AgentRequest is the POST/PUT /agents body (A-02 §5).
type AgentRequest struct {
	Name                 string      `json:"name"`
	Goal                 string      `json:"goal"`
	Provider             string      `json:"provider"`
	Model                string      `json:"model"`
	Permissions          []string    `json:"permissions"`
	Triggers             TriggersDTO `json:"triggers"`
	StrategyIDs          []uint      `json:"strategy_ids"`
	WebhookTargetIDs     []uint      `json:"webhook_target_ids"`
	DailyBudgetUSD       float64     `json:"daily_budget_usd"`
	MaxAutoDeploysPerDay int         `json:"max_auto_deploys_per_day"`
}

// ToInput maps the request to the usecase write model.
func (r AgentRequest) ToInput() usecase.AgentInput {
	return usecase.AgentInput{
		Name:        r.Name,
		Goal:        r.Goal,
		Provider:    r.Provider,
		Model:       r.Model,
		Permissions: r.Permissions,
		Triggers: entities.AgentTriggers{
			Cron:      append([]string(nil), r.Triggers.Cron...),
			Events:    r.Triggers.Events,
			Market:    r.Triggers.Market,
			ChainFrom: r.Triggers.ChainFrom,
		},
		StrategyIDs:          r.StrategyIDs,
		WebhookTargetIDs:     r.WebhookTargetIDs,
		DailyBudgetUSD:       r.DailyBudgetUSD,
		MaxAutoDeploysPerDay: r.MaxAutoDeploysPerDay,
	}
}

// LastRunDTO is AgentResponse.last_run.
type LastRunDTO struct {
	ID        uint   `json:"id"`
	Status    string `json:"status"`
	Trigger   string `json:"trigger"`
	StartedAt string `json:"started_at"`
}

// AgentResponse = AgentRequest fields + derived fields (A-02 §5).
type AgentResponse struct {
	AgentRequest
	ID           uint        `json:"id"`
	Paused       bool        `json:"paused"`
	IsDefault    bool        `json:"is_default"`
	CreatedAt    string      `json:"created_at"`
	UpdatedAt    string      `json:"updated_at"`
	TodayCostUSD float64     `json:"today_cost_usd"`
	LastRun      *LastRunDTO `json:"last_run"`
	NextRunAt    *string     `json:"next_run_at"`
}

func rfc3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// ToAgentResponse maps an AgentView to the DTO.
func ToAgentResponse(v usecase.AgentView) AgentResponse {
	a := v.Agent
	t := a.ParsedTriggers()
	cron := t.Cron
	if cron == nil {
		cron = []string{}
	}
	perms := []string(a.Permissions)
	if perms == nil {
		perms = []string{}
	}
	targets := []uint(a.WebhookTargetIDs)
	if targets == nil {
		targets = []uint{}
	}
	strategyIDs := v.StrategyIDs
	if strategyIDs == nil {
		strategyIDs = []uint{}
	}
	resp := AgentResponse{
		AgentRequest: AgentRequest{
			Name: a.Name, Goal: a.Goal, Provider: a.Provider, Model: a.Model, Permissions: perms,
			Triggers:    TriggersDTO{Cron: cron, Events: t.Events, Market: t.Market, ChainFrom: t.ChainFrom},
			StrategyIDs: strategyIDs, WebhookTargetIDs: targets,
			DailyBudgetUSD: a.DailyBudgetUSD, MaxAutoDeploysPerDay: a.MaxAutoDeploysPerDay,
		},
		ID: a.ID, Paused: a.Paused, IsDefault: a.IsDefault,
		CreatedAt: rfc3339(a.CreatedAt), UpdatedAt: rfc3339(a.UpdatedAt),
		TodayCostUSD: v.TodayCostUSD,
	}
	if v.LastRun != nil {
		resp.LastRun = &LastRunDTO{ID: v.LastRun.ID, Status: string(v.LastRun.Status), Trigger: v.LastRun.Trigger, StartedAt: rfc3339(v.LastRun.StartedAt)}
	}
	if v.NextRunAt != nil {
		s := rfc3339(*v.NextRunAt)
		resp.NextRunAt = &s
	}
	return resp
}

// UsageDayResponse is one day of usage.
type UsageDayResponse struct {
	Day          string  `json:"day"` // YYYY-MM-DD (UTC)
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	CostUSD      float64 `json:"cost_usd"`
	Runs         int     `json:"runs"`
}

// UsageResponse is GET /agents/{id}/usage.
type UsageResponse struct {
	Today UsageDayResponse   `json:"today"`
	Days  []UsageDayResponse `json:"days"`
}

func toUsageDay(d usecase.UsageDay) UsageDayResponse {
	return UsageDayResponse{Day: d.Day.UTC().Format("2006-01-02"), InputTokens: d.InputTokens, OutputTokens: d.OutputTokens, CostUSD: d.CostUSD, Runs: d.Runs}
}

// MemoryEntryResponse is one strategy memory entry.
type MemoryEntryResponse struct {
	ID            uint   `json:"id"`
	StrategyID    uint   `json:"strategy_id"`
	AuthorAgentID *uint  `json:"author_agent_id"`
	AuthorName    string `json:"author_name"` // agent name, or "operator"
	AgentRunID    *uint  `json:"agent_run_id"`
	Kind          string `json:"kind"`
	Content       string `json:"content"`
	RefID         *uint  `json:"ref_id"`
	CreatedAt     string `json:"created_at"`
}

// ToMemoryEntryResponse maps an entry, resolving the author name.
func ToMemoryEntryResponse(e entities.StrategyMemoryEntry, names map[uint]string) MemoryEntryResponse {
	author := "operator"
	if e.AuthorAgentID != nil {
		if n, ok := names[*e.AuthorAgentID]; ok {
			author = n
		} else {
			author = "agent #" + uintString(*e.AuthorAgentID)
		}
	}
	return MemoryEntryResponse{
		ID: e.ID, StrategyID: e.StrategyID, AuthorAgentID: e.AuthorAgentID, AuthorName: author,
		AgentRunID: e.AgentRunID, Kind: string(e.Kind), Content: e.Content, RefID: e.RefID, CreatedAt: rfc3339(e.CreatedAt),
	}
}

func uintString(v uint) string {
	b, _ := json.Marshal(v)
	return string(b)
}
