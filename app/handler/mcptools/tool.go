// Package mcptools is the MCP inbound adapter for the platform's operator-
// facing reads and low-risk writes. Like app/handler/web it sits in front of
// the usecases and talks to them through its own narrow, consumer-side
// interfaces (ports.go) - but it returns compact, LLM-oriented results
// instead of the web UI's DTOs, and it never sees a method that is not
// listed there: the ports ARE the allowlist, enforced by the compiler.
//
// Deliberately not reachable from here (REST-only, human approved): settings
// and account writes, closing positions, strategy status/mode, the agents
// kill-switch write, proposal approve/reject, deploy-gate writes, users,
// webhook targets, and manually running an agent (it spends model budget).
package mcptools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Tool is one MCP tool. Run returns any JSON-encodable value; the transport
// encodes it.
type Tool struct {
	Name        string
	Description string
	InputSchema json.RawMessage
	// Write marks tools that change state (pinned by a test so adding one is
	// a conscious edit).
	Write bool
	Run   func(ctx context.Context, args json.RawMessage) (any, error)
}

// Deps are the ports the tools need. A nil port omits its tools.
type Deps struct {
	Settings   SettingsReader
	Accounts   AccountReader
	Signals    SignalReader
	Versions   VersionReader
	MonteCarlo MonteCarlo
	Market     MarketData
	Datasets   DatasetManager
	Scripts    ScriptRunner
	Agents     AgentManager
	Webhooks   WebhookTargetReader
	// WalkForward + Tasks enable the asynchronous run_walk_forward tool.
	WalkForward WalkForwardQueue
	Tasks       BacktestTasks
	// Reports/Backtests + BaseURL enable the report link tools; BaseURL is
	// the dashboard's public URL (API_BASE_URL).
	Reports   ReportReader
	Backtests BacktestReader
	BaseURL   string
}

// New builds every tool whose dependency is wired.
func New(d Deps) []Tool {
	var tools []Tool
	if d.Settings != nil {
		tools = append(tools, settingsTools(d.Settings)...)
	}
	if d.Accounts != nil {
		tools = append(tools, accountTools(d.Accounts)...)
	}
	if d.Signals != nil {
		tools = append(tools, signalTools(d.Signals)...)
	}
	if d.Versions != nil {
		tools = append(tools, versionTools(d.Versions)...)
	}
	if d.MonteCarlo != nil {
		tools = append(tools, monteCarloTools(d.MonteCarlo)...)
	}
	if d.Market != nil {
		tools = append(tools, marketTools(d.Market)...)
	}
	if d.Datasets != nil {
		tools = append(tools, datasetTools(d.Datasets)...)
	}
	if d.Scripts != nil {
		tools = append(tools, scriptTools(d.Scripts)...)
	}
	if d.Agents != nil {
		tools = append(tools, agentTools(d.Agents)...)
	}
	if d.WalkForward != nil && d.Tasks != nil {
		tools = append(tools, walkForwardTools(d.WalkForward, d.Tasks)...)
	}
	tools = append(tools, reportTools(d.BaseURL, d.Reports, d.Backtests)...)
	if d.Webhooks != nil {
		tools = append(tools, webhookTools(d.Webhooks)...)
	}
	return tools
}

// decode unmarshals tool arguments (empty = {}).
func decode(name string, raw json.RawMessage, into any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return fmt.Errorf("%s: invalid arguments: %w", name, err)
	}
	return nil
}

// clamp bounds an optional limit: <=0 -> def, > max -> max.
func clamp(v, def, max int) int {
	if v <= 0 {
		return def
	}
	if v > max {
		return max
	}
	return v
}

func requireID(name, field string, v uint) error {
	if v == 0 {
		return fmt.Errorf("%s: %s is required", name, field)
	}
	return nil
}

// ts renders a time as RFC3339 UTC ("" for the zero time).
func ts(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func tsPtr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return ts(*t)
}

// truncate shortens s to at most n runes, marking the cut.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "...[truncated]"
}
