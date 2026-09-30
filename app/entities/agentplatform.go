package entities

import (
	"encoding/json"
	"time"

	"gorm.io/datatypes"
)

// AgentPermission is one capability an agent persona may be granted
// (agents-platform Phase A-01 §1). Tools are exposed to the model only if
// their required permission is in Agent.Permissions - see
// app/usecase/agent/tools.go's buildToolRegistry.
type AgentPermission string

const (
	PermRead           AgentPermission = "read"            // all read tools
	PermBacktest       AgentPermission = "backtest"        // run_backtest
	PermOptimize       AgentPermission = "optimize"        // run_optimization
	PermEditTesting    AgentPermission = "edit_testing"    // save_strategy_script (backtest drafts), deploy_to_testing (gated), create_challenger
	PermCreateStrategy AgentPermission = "create_strategy" // create_strategy (always testing + backtest/dryrun, max 3/day)
	PermProposeLive    AgentPermission = "propose_live"    // propose_promotion (operator approval required in the web UI)
	PermNotify         AgentPermission = "notify"          // notify tool
	PermChain          AgentPermission = "chain"           // trigger_agent (Phase C): start another agent's run with a message, max 3 per run
)

// AllAgentPermissions is every permission the API accepts, in display order.
var AllAgentPermissions = []AgentPermission{
	PermRead, PermBacktest, PermOptimize, PermEditTesting, PermCreateStrategy, PermProposeLive, PermNotify, PermChain,
}

// IsValidAgentPermission reports whether p is a known permission.
func IsValidAgentPermission(p string) bool {
	for _, known := range AllAgentPermissions {
		if string(known) == p {
			return true
		}
	}
	return false
}

// AgentTriggers is the JSON shape stored in Agent.Triggers (and the REST
// `triggers` object - the agents-platform C-01 §1 contract). Cron is served
// by cmd/agent's cron provider; Events by the worker->agents bridge and the
// sweeper; Market by the market watcher; ChainFrom by the agent:run
// processor after a source run finishes. See agenttriggers.go for the element
// types, defaults and the backward-compatible decoding of the old
// `events: ["..."]` / `chain_from: [1,2]` forms.
type AgentTriggers struct {
	Cron      []string       `json:"cron,omitempty"` // standard 5-field cron specs, UTC
	Events    []EventTrigger `json:"events,omitempty"`
	Market    []MarketRule   `json:"market,omitempty"`
	ChainFrom []ChainTrigger `json:"chain_from,omitempty"`
}

// Agent is a configurable agent persona. Exactly one row has IsDefault=true
// (the "Copilot" persona used by the chat widget when no agent is chosen).
type Agent struct {
	ID                   uint                        `gorm:"primaryKey"`
	Name                 string                      `gorm:"uniqueIndex"`
	Goal                 string                      `gorm:"type:text"` // persona system prompt
	Provider             string                      // "" = process default (configuration.Agent.Provider)
	Model                string                      // "" = provider default
	Permissions          datatypes.JSONSlice[string] `gorm:"type:jsonb"`
	Triggers             datatypes.JSON              `gorm:"type:jsonb"` // AgentTriggers
	WebhookTargetIDs     datatypes.JSONSlice[uint]   `gorm:"type:jsonb"`
	DailyBudgetUSD       float64                     // 0 = no budget limit
	MaxAutoDeploysPerDay int                         // deploy_to_testing auto-deploys per UTC day; 0 = auto-deploy disabled
	// MaxIterations caps the run's tool loop (fix-02 B1). 0 = the trigger
	// default (8 for chat_ui/mcp_tool, 24 for unattended triggers); otherwise
	// 4..50 (validated in app/usecase/agentplatform).
	MaxIterations int
	Paused        bool
	IsDefault     bool // exactly one row: the "Copilot" persona
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// HasPermission reports whether the agent was granted p.
func (a Agent) HasPermission(p AgentPermission) bool {
	for _, granted := range a.Permissions {
		if granted == string(p) {
			return true
		}
	}
	return false
}

// ParsedTriggers decodes Triggers leniently: an empty column is the zero
// value, and a malformed element of one list is skipped without losing the
// others (a bad market rule must never unschedule the agent's cron).
func (a Agent) ParsedTriggers() AgentTriggers {
	return ParseTriggersLenient(a.Triggers)
}

// DefaultAgentName is the fixed name of the IsDefault persona.
const DefaultAgentName = "Copilot"

// AgentStrategyBinding binds an agent to a strategy it watches. Composite
// primary key (AgentID, StrategyID).
type AgentStrategyBinding struct {
	AgentID    uint `gorm:"primaryKey;autoIncrement:false"`
	StrategyID uint `gorm:"primaryKey;autoIncrement:false;index"`
	CreatedAt  time.Time
}

// MemoryKind classifies a StrategyMemoryEntry.
type MemoryKind string

const (
	MemoryJournal   MemoryKind = "journal"
	MemoryChatUser  MemoryKind = "chat_user"
	MemoryChatAgent MemoryKind = "chat_agent"
	MemoryReportRef MemoryKind = "report_ref"
	MemoryFinding   MemoryKind = "finding"
)

// IsValidMemoryKind reports whether k is a known memory kind.
func IsValidMemoryKind(k string) bool {
	switch MemoryKind(k) {
	case MemoryJournal, MemoryChatUser, MemoryChatAgent, MemoryReportRef, MemoryFinding:
		return true
	}
	return false
}

// StrategyMemoryEntry is one entry of the memory shared PER STRATEGY across
// all agents (tagged by author agent; nil author = the human operator).
type StrategyMemoryEntry struct {
	ID            uint `gorm:"primaryKey"`
	StrategyID    uint `gorm:"index"`
	AuthorAgentID *uint
	// AuthorUserID is the app user who wrote an operator note (auth-01 §7);
	// nil for agent entries and legacy operator notes.
	AuthorUserID *uint
	AgentRunID   *uint
	Kind         MemoryKind
	Content      string    `gorm:"type:text"`
	RefID        *uint     // e.g. AgentReport.ID for report_ref
	CreatedAt    time.Time `gorm:"index"`
}

// ReportSeverity is an AgentReport's severity.
type ReportSeverity string

const (
	SeverityInfo     ReportSeverity = "info"
	SeverityWarning  ReportSeverity = "warning"
	SeverityCritical ReportSeverity = "critical"
)

// IsValidReportSeverity reports whether s is a known severity.
func IsValidReportSeverity(s string) bool {
	switch ReportSeverity(s) {
	case SeverityInfo, SeverityWarning, SeverityCritical:
		return true
	}
	return false
}

// AgentReport is a typed-block report written by an agent (write_report),
// rendered server-side into RenderedHTML once, at write time (data
// snapshot semantics).
type AgentReport struct {
	ID           uint `gorm:"primaryKey"`
	AgentID      uint `gorm:"index"`
	AgentRunID   uint
	StrategyIDs  datatypes.JSONSlice[uint] `gorm:"type:jsonb"`
	Title        string
	Severity     ReportSeverity
	Summary      string         `gorm:"type:text"` // first summary block text, for list views/webhooks
	BlocksJSON   datatypes.JSON `gorm:"type:jsonb"`
	RenderedHTML string         `gorm:"type:text"`
	// RenderedHTMLByLocale holds one write-time snapshot per locale
	// (i18n-02 §3): {"en": "<!DOCTYPE html>...", "es": ..., "pt-BR": ...}.
	// Only chrome/labels differ; RenderedHTML stays the Settings.DefaultLocale
	// snapshot for back-compat. Reports written before i18n-02 have none.
	RenderedHTMLByLocale datatypes.JSON `gorm:"type:jsonb"`
	CreatedAt            time.Time      `gorm:"index"`
}

// SetLocaleSnapshots stores per-locale snapshots (locale -> HTML).
func (r *AgentReport) SetLocaleSnapshots(byLocale map[string]string) error {
	if len(byLocale) == 0 {
		r.RenderedHTMLByLocale = nil
		return nil
	}
	b, err := json.Marshal(byLocale)
	if err != nil {
		return err
	}
	r.RenderedHTMLByLocale = datatypes.JSON(b)
	return nil
}

// HTMLForLocale returns the snapshot for locale, falling back to
// RenderedHTML (reports without per-locale snapshots, or a missing locale).
func (r AgentReport) HTMLForLocale(locale string) string {
	if locale != "" && len(r.RenderedHTMLByLocale) > 0 {
		var m map[string]string
		if err := json.Unmarshal(r.RenderedHTMLByLocale, &m); err == nil {
			if html, ok := m[locale]; ok && html != "" {
				return html
			}
		}
	}
	return r.RenderedHTML
}

// WebhookTargetKind selects the payload formatter for a WebhookTarget.
type WebhookTargetKind string

const (
	WebhookGeneric  WebhookTargetKind = "generic"
	WebhookDiscord  WebhookTargetKind = "discord"
	WebhookSlack    WebhookTargetKind = "slack"
	WebhookTelegram WebhookTargetKind = "telegram"
)

// IsValidWebhookTargetKind reports whether k is a known kind.
func IsValidWebhookTargetKind(k string) bool {
	switch WebhookTargetKind(k) {
	case WebhookGeneric, WebhookDiscord, WebhookSlack, WebhookTelegram:
		return true
	}
	return false
}

// WebhookTarget is one per-agent notification destination.
type WebhookTarget struct {
	ID        uint `gorm:"primaryKey"`
	Name      string
	Kind      WebhookTargetKind
	URL       string // generic/discord/slack: full webhook URL. telegram: unused (bot token lives in Secret)
	Secret    string // telegram bot token; masked in API responses
	ChatID    string // telegram only
	Enabled   bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// AgentUsage is one row per (AgentID, Day) of model-usage accounting.
type AgentUsage struct {
	AgentID      uint      `gorm:"primaryKey;autoIncrement:false"`
	Day          time.Time `gorm:"primaryKey;type:date"` // UTC date
	InputTokens  int64
	OutputTokens int64
	CostUSD      float64
	Runs         int
	AutoDeploys  int // deploy_to_testing auto-deploys (Phase B)
	// StrategiesCreated counts create_strategy calls (Phase B rate limit:
	// at most MaxStrategiesCreatedPerDay per agent per UTC day).
	StrategiesCreated int
	// BudgetAlertSent is the cross-process dedupe flag for the once-per-day
	// "budget exhausted" critical notification (A-01 §4.3). Flipped with a
	// conditional UPDATE (false -> true) so exactly one process wins even
	// when cmd/api (chat) and cmd/agent (cron) both hit the budget.
	BudgetAlertSent bool
}

// UsageDay normalizes t to its UTC calendar date at midnight - the key used
// for AgentUsage.Day everywhere.
func UsageDay(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
}
