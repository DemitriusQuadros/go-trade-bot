package entities

import (
	"time"

	"gorm.io/datatypes"
)

// AgentInstruction is a single-operator singleton: exactly one row, ID fixed
// at 1, upserted in place (Backend Spec 02). This is a deliberate
// simplification for single-tenant scope (per the resolved PRD decision) -
// do not add a UserID/OrgID column speculatively.
type AgentInstruction struct {
	ID        uint   `gorm:"primaryKey"`
	Content   string `gorm:"type:text"` // free-text house rules, injected verbatim into every agent system prompt
	UpdatedAt time.Time
}

type AgentRunStatus string

const (
	AgentRunOK    AgentRunStatus = "ok"
	AgentRunError AgentRunStatus = "error"
)

// AgentRun is the audit-log entry for one agent invocation (one MCP
// tool-call session in Phase 1; one chat turn or monitor firing in Phase 2).
// ToolCallsJSON is the actual audit trail - every tool the agent called, its
// arguments, and a summary of the result, in call order.
type AgentRun struct {
	ID            uint   `gorm:"primaryKey"`
	Provider      string // "anthropic" | "gemini"
	Model         string
	Trigger       string // "mcp_tool" (Phase 1) | "chat_ui" | "monitor" (Phase 2)
	Status        AgentRunStatus
	ErrorMessage  string
	InputSummary  string         `gorm:"type:text"` // triggering user prompt/event, truncated to a bounded length
	ToolCallsJSON datatypes.JSON `gorm:"type:jsonb"`
	// ResponseText is the model's final natural-language answer text - the
	// content of its last turn, once it stops calling tools
	// (StopReason != "tool_use"). Found missing entirely during manual
	// verification: RunToolLoop computed this value every time but never
	// persisted it anywhere, so the copilot UI could show every tool call
	// it made but never its actual answer. Empty for a run that errors out
	// before ever reaching a final turn.
	ResponseText string `gorm:"type:text"`
	// StrategyID is set once this run's first strategy-affecting tool call
	// succeeds; nullable for read-only runs (Backend Spec 02's "Judgment
	// call" - a single run's tool calls in Phase 1 are scoped to at most one
	// strategy at a time, so no separate join table is needed here).
	StrategyID *uint
	StartedAt  time.Time
	FinishedAt time.Time
}
