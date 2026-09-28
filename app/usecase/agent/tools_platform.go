package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"go-trade-bot/app/entities"
	"go-trade-bot/app/repository/agentplatform"
	"go-trade-bot/internal/modelprovider"
	"go-trade-bot/internal/notifier"
	"go-trade-bot/internal/report/agentreport"
)

// Agents platform tools (A-01 §4.6). None of these can modify
// entities.Strategy, place/cancel orders, or touch settings/credentials:
// they only read/write the platform's own tables (memory, reports) or send
// a webhook message.

const (
	notifyToolName          = "notify"
	maxJournalChars         = 4000
	maxNotifyChars          = 1000
	maxReadMemoryLimit      = 100
	defaultReadMemoryLimit  = 30
	maxListReportsLimit     = 50
	defaultListReportsLimit = 10
)

// toolAgent returns the persona a tool call acts as, and the run id (0 over
// MCP, where there is no run): the run's agent inside Run, otherwise the
// default agent.
func (u AgentUseCase) toolAgent(ctx context.Context) (entities.Agent, *uint, error) {
	if s, ok := scopeFrom(ctx); ok {
		return s.agent, s.runIDPtr(), nil
	}
	a, err := u.DefaultAgent(ctx)
	return a, nil, err
}

func compactJSON(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (u AgentUseCase) requirePlatform(tool string) error {
	if u.Platform == nil {
		return fmt.Errorf("%s: the agents platform is not available on this server", tool)
	}
	return nil
}

func (u AgentUseCase) requireStrategy(ctx context.Context, tool string, id uint) (entities.Strategy, error) {
	if id == 0 {
		return entities.Strategy{}, fmt.Errorf("%s: strategy_id is required", tool)
	}
	s, err := u.Strategy.GetByID(ctx, id)
	if err != nil || s.ID == 0 {
		return entities.Strategy{}, fmt.Errorf("%s: strategy %d does not exist", tool, id)
	}
	return s, nil
}

// --- read_memory -------------------------------------------------------------

var readMemorySchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"strategy_id": {"type": "integer"},
		"kinds": {"type": "array", "items": {"type": "string", "enum": ["journal", "chat_user", "chat_agent", "report_ref", "finding"]}},
		"limit": {"type": "integer", "description": "max entries, 1-100, default 30"},
		"before_id": {"type": "integer", "description": "only entries with id < before_id (pagination)"}
	},
	"required": ["strategy_id"]
}`)

type memoryEntryOut struct {
	ID        uint   `json:"id"`
	Author    string `json:"author"`
	Kind      string `json:"kind"`
	Content   string `json:"content"`
	RefID     *uint  `json:"ref_id,omitempty"`
	CreatedAt string `json:"created_at"`
}

func (u AgentUseCase) readMemoryTool() Tool {
	return Tool{
		Def: modelprovider.ToolDefinition{
			Name:        "read_memory",
			Description: "Read a strategy's shared memory (journal notes, findings, chat, report references) written by any agent or the operator. Newest first.",
			InputSchema: readMemorySchema,
		},
		Execute: func(ctx context.Context, raw json.RawMessage) (string, error) {
			if err := u.requirePlatform("read_memory"); err != nil {
				return "", err
			}
			var in struct {
				StrategyID uint     `json:"strategy_id"`
				Kinds      []string `json:"kinds"`
				Limit      int      `json:"limit"`
				BeforeID   *uint    `json:"before_id"`
			}
			if err := json.Unmarshal(raw, &in); err != nil {
				return "", fmt.Errorf("read_memory: invalid args: %w", err)
			}
			if in.StrategyID == 0 {
				return "", fmt.Errorf("read_memory: strategy_id is required")
			}
			if in.Limit <= 0 {
				in.Limit = defaultReadMemoryLimit
			}
			if in.Limit > maxReadMemoryLimit {
				in.Limit = maxReadMemoryLimit
			}
			kinds := make([]entities.MemoryKind, 0, len(in.Kinds))
			for _, k := range in.Kinds {
				if !entities.IsValidMemoryKind(k) {
					return "", fmt.Errorf("read_memory: unknown kind %q", k)
				}
				kinds = append(kinds, entities.MemoryKind(k))
			}
			entries, err := u.Platform.ListMemory(ctx, in.StrategyID, kinds, in.Limit, in.BeforeID)
			if err != nil {
				return "", err
			}
			names := u.agentNames(ctx)
			out := make([]memoryEntryOut, 0, len(entries))
			for _, e := range entries {
				out = append(out, memoryEntryOut{
					ID: e.ID, Author: authorName(names, e.AuthorAgentID), Kind: string(e.Kind),
					Content: e.Content, RefID: e.RefID, CreatedAt: e.CreatedAt.UTC().Format(time.RFC3339),
				})
			}
			return compactJSON(out)
		},
	}
}

func (u AgentUseCase) agentNames(ctx context.Context) map[uint]string {
	names := map[uint]string{}
	if u.Platform == nil {
		return names
	}
	if agents, err := u.Platform.ListAgents(ctx); err == nil {
		for _, a := range agents {
			names[a.ID] = a.Name
		}
	}
	return names
}

func authorName(names map[uint]string, id *uint) string {
	if id == nil {
		return "operator"
	}
	if n, ok := names[*id]; ok {
		return n
	}
	return "agent #" + uintToString(*id)
}

// --- write_journal -----------------------------------------------------------

var writeJournalSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"strategy_id": {"type": "integer"},
		"content": {"type": "string", "description": "the note, at most 4000 characters"},
		"kind": {"type": "string", "enum": ["journal", "finding"], "description": "default journal"}
	},
	"required": ["strategy_id", "content"]
}`)

func (u AgentUseCase) writeJournalTool() Tool {
	return Tool{
		Def: modelprovider.ToolDefinition{
			Name:        "write_journal",
			Description: "Append a note to a strategy's shared memory (visible to every agent and the operator). Use kind=finding for conclusions worth keeping, journal for working notes.",
			InputSchema: writeJournalSchema,
		},
		Execute: func(ctx context.Context, raw json.RawMessage) (string, error) {
			if err := u.requirePlatform("write_journal"); err != nil {
				return "", err
			}
			var in struct {
				StrategyID uint   `json:"strategy_id"`
				Content    string `json:"content"`
				Kind       string `json:"kind"`
			}
			if err := json.Unmarshal(raw, &in); err != nil {
				return "", fmt.Errorf("write_journal: invalid args: %w", err)
			}
			if strings.TrimSpace(in.Content) == "" {
				return "", fmt.Errorf("write_journal: content is required")
			}
			if len([]rune(in.Content)) > maxJournalChars {
				return "", fmt.Errorf("write_journal: content exceeds %d characters", maxJournalChars)
			}
			kind := entities.MemoryJournal
			switch in.Kind {
			case "", string(entities.MemoryJournal):
			case string(entities.MemoryFinding):
				kind = entities.MemoryFinding
			default:
				return "", fmt.Errorf("write_journal: kind must be journal or finding")
			}
			if _, err := u.requireStrategy(ctx, "write_journal", in.StrategyID); err != nil {
				return "", err
			}
			agent, runID, err := u.toolAgent(ctx)
			if err != nil {
				return "", err
			}
			entry := entities.StrategyMemoryEntry{StrategyID: in.StrategyID, AgentRunID: runID, Kind: kind, Content: in.Content}
			if agent.ID != 0 {
				id := agent.ID
				entry.AuthorAgentID = &id
			}
			saved, err := u.Platform.AppendMemory(ctx, entry)
			if err != nil {
				return "", err
			}
			return compactJSON(map[string]any{"memory_id": saved.ID, "strategy_id": saved.StrategyID, "kind": saved.Kind})
		},
	}
}

// --- list_reports ------------------------------------------------------------

var listReportsSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"strategy_id": {"type": "integer", "description": "optional - only reports covering this strategy"},
		"limit": {"type": "integer", "description": "1-50, default 10"}
	}
}`)

func (u AgentUseCase) listReportsTool() Tool {
	return Tool{
		Def: modelprovider.ToolDefinition{
			Name:        "list_reports",
			Description: "List recent agent reports (id, title, severity, created_at, agent), optionally for one strategy.",
			InputSchema: listReportsSchema,
		},
		Execute: func(ctx context.Context, raw json.RawMessage) (string, error) {
			if err := u.requirePlatform("list_reports"); err != nil {
				return "", err
			}
			var in struct {
				StrategyID *uint `json:"strategy_id"`
				Limit      int   `json:"limit"`
			}
			if len(raw) > 0 {
				if err := json.Unmarshal(raw, &in); err != nil {
					return "", fmt.Errorf("list_reports: invalid args: %w", err)
				}
			}
			if in.Limit <= 0 {
				in.Limit = defaultListReportsLimit
			}
			if in.Limit > maxListReportsLimit {
				in.Limit = maxListReportsLimit
			}
			if in.StrategyID != nil && *in.StrategyID == 0 {
				in.StrategyID = nil
			}
			reps, err := u.Platform.ListReports(ctx, agentplatform.ReportFilter{StrategyID: in.StrategyID, Limit: in.Limit})
			if err != nil {
				return "", err
			}
			names := u.agentNames(ctx)
			type out struct {
				ID          uint   `json:"id"`
				Title       string `json:"title"`
				Severity    string `json:"severity"`
				CreatedAt   string `json:"created_at"`
				Agent       string `json:"agent"`
				StrategyIDs []uint `json:"strategy_ids"`
			}
			res := make([]out, 0, len(reps))
			for _, r := range reps {
				agentID := r.AgentID
				res = append(res, out{ID: r.ID, Title: r.Title, Severity: string(r.Severity), CreatedAt: r.CreatedAt.UTC().Format(time.RFC3339),
					Agent: authorName(names, &agentID), StrategyIDs: append([]uint{}, r.StrategyIDs...)})
			}
			return compactJSON(res)
		},
	}
}

// --- write_report ------------------------------------------------------------

var writeReportSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"title": {"type": "string"},
		"severity": {"type": "string", "enum": ["info", "warning", "critical"]},
		"strategy_ids": {"type": "array", "items": {"type": "integer"}},
		"blocks": {
			"type": "array",
			"description": "At most 40 typed blocks, rendered server-side. Allowed types and data: summary {text}; callout {severity: info|warning|critical, title, text}; kpi_grid {source, metrics: [sharpe, max_drawdown, win_rate, profit_factor, total_trades, net_pnl]}; equity_chart {source}; trade_table {source, limit <= 50}; code_diff {strategy_id, from_version_id?, to_version_id?} or {strategy_id, from_source, to_source}; recommendation {items: [{title, rationale, action?}]}; text_table {columns: [..], rows: [[..]]} (qualitative text only). source is {kind: \"backtest_run\", id} or {kind: \"strategy_live\", strategy_id, days}. Numbers for kpi_grid/equity_chart/trade_table are read from the database by the renderer - never put metric values in block data.",
			"items": {
				"type": "object",
				"properties": {
					"type": {"type": "string"},
					"data": {"type": "object"}
				},
				"required": ["type", "data"]
			}
		}
	},
	"required": ["title", "severity", "strategy_ids", "blocks"]
}`)

func (u AgentUseCase) reportURL(id uint) string {
	return strings.TrimRight(u.APIBaseURL, "/") + "/agents/reports/" + uintToString(id)
}

func (u AgentUseCase) writeReportTool() Tool {
	return Tool{
		Def: modelprovider.ToolDefinition{
			Name:        "write_report",
			Description: "Write a report for the operator as typed blocks; it is rendered to HTML server-side, stored, and linked from each strategy's shared memory. Returns {report_id, url}.",
			InputSchema: writeReportSchema,
		},
		Execute: func(ctx context.Context, raw json.RawMessage) (string, error) {
			if err := u.requirePlatform("write_report"); err != nil {
				return "", err
			}
			if u.Reports == nil {
				return "", fmt.Errorf("write_report: report rendering is not available on this server")
			}
			var in struct {
				Title       string              `json:"title"`
				Severity    string              `json:"severity"`
				StrategyIDs []uint              `json:"strategy_ids"`
				Blocks      []agentreport.Block `json:"blocks"`
			}
			if err := json.Unmarshal(raw, &in); err != nil {
				return "", fmt.Errorf("write_report: invalid args: %w", err)
			}
			in.Title = strings.TrimSpace(in.Title)
			if in.Title == "" || len([]rune(in.Title)) > 200 {
				return "", fmt.Errorf("write_report: title is required (at most 200 characters)")
			}
			if !entities.IsValidReportSeverity(in.Severity) {
				return "", fmt.Errorf("write_report: severity must be info, warning or critical")
			}
			if err := u.Reports.Validate(in.Blocks); err != nil {
				return "", fmt.Errorf("write_report: %w", err)
			}
			strategyIDs := dedupeUints(in.StrategyIDs)
			var names []string
			for _, sid := range strategyIDs {
				s, err := u.requireStrategy(ctx, "write_report", sid)
				if err != nil {
					return "", err
				}
				names = append(names, fmt.Sprintf("#%d %s", s.ID, s.Name))
			}
			agent, runID, err := u.toolAgent(ctx)
			if err != nil {
				return "", err
			}

			now := time.Now().UTC()
			var runIDVal uint
			if runID != nil {
				runIDVal = *runID
			}
			html, err := u.Reports.Render(ctx, agentreport.ReportMeta{
				Title: in.Title, Severity: in.Severity, AgentName: agent.Name, RunID: runIDVal,
				CreatedAt: now, StrategyNames: names,
			}, in.Blocks)
			if err != nil {
				return "", fmt.Errorf("write_report: %w", err)
			}
			blocksJSON, err := json.Marshal(in.Blocks)
			if err != nil {
				return "", fmt.Errorf("write_report: %w", err)
			}

			rep, err := u.Platform.CreateReport(ctx, entities.AgentReport{
				AgentID: agent.ID, AgentRunID: runIDVal, StrategyIDs: strategyIDs, Title: in.Title,
				Severity: entities.ReportSeverity(in.Severity), Summary: agentreport.FirstSummary(in.Blocks),
				BlocksJSON: blocksJSON, RenderedHTML: html, CreatedAt: now,
			})
			if err != nil {
				return "", err
			}
			for _, sid := range strategyIDs {
				refID := rep.ID
				entry := entities.StrategyMemoryEntry{StrategyID: sid, AgentRunID: runID, Kind: entities.MemoryReportRef, Content: in.Title, RefID: &refID}
				if agent.ID != 0 {
					id := agent.ID
					entry.AuthorAgentID = &id
				}
				if _, err := u.Platform.AppendMemory(ctx, entry); err != nil {
					return "", fmt.Errorf("write_report: report %d saved but linking it to strategy %d failed: %w", rep.ID, sid, err)
				}
			}
			return compactJSON(map[string]any{"report_id": rep.ID, "url": u.reportURL(rep.ID)})
		},
	}
}

func dedupeUints(in []uint) []uint {
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

// --- notify ------------------------------------------------------------------

var notifySchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"severity": {"type": "string", "enum": ["info", "warning", "critical"]},
		"message": {"type": "string", "description": "at most 1000 characters"},
		"report_id": {"type": "integer", "description": "optional - link the notification to a report"}
	},
	"required": ["severity", "message"]
}`)

func (u AgentUseCase) notifyTool() Tool {
	return Tool{
		Def: modelprovider.ToolDefinition{
			Name:        notifyToolName,
			Description: "Send a short notification to the operator via this agent's configured webhook targets (link-only). At most 10 per run - use it only for things that matter.",
			InputSchema: notifySchema,
		},
		Execute: func(ctx context.Context, raw json.RawMessage) (string, error) {
			if err := u.requirePlatform("notify"); err != nil {
				return "", err
			}
			if u.Notifier == nil {
				return "", fmt.Errorf("notify: notifications are not available on this server")
			}
			scope, ok := scopeFrom(ctx)
			if !ok {
				return "", fmt.Errorf("notify: only available inside an agent run")
			}
			var in struct {
				Severity string `json:"severity"`
				Message  string `json:"message"`
				ReportID *uint  `json:"report_id"`
			}
			if err := json.Unmarshal(raw, &in); err != nil {
				return "", fmt.Errorf("notify: invalid args: %w", err)
			}
			if !entities.IsValidReportSeverity(in.Severity) {
				return "", fmt.Errorf("notify: severity must be info, warning or critical")
			}
			if strings.TrimSpace(in.Message) == "" || len([]rune(in.Message)) > maxNotifyChars {
				return "", fmt.Errorf("notify: message is required (at most %d characters)", maxNotifyChars)
			}
			agent := scope.agent
			if len(agent.WebhookTargetIDs) == 0 {
				return "", fmt.Errorf("notify: agent %q has no webhook targets configured", agent.Name)
			}
			title := "Agent notification"
			link := ""
			var strategyIDs []uint
			if in.ReportID != nil && *in.ReportID != 0 {
				rep, err := u.Platform.GetReport(ctx, *in.ReportID)
				if err != nil {
					return "", fmt.Errorf("notify: report %d not found", *in.ReportID)
				}
				title = rep.Title
				link = u.reportURL(rep.ID)
				strategyIDs = append(strategyIDs, rep.StrategyIDs...)
			}
			if !scope.takeNotificationSlot() {
				return "", fmt.Errorf("notify: rate limit reached (%d notifications per run)", maxNotificationsPerRun)
			}
			targets, err := u.Platform.ListWebhookTargetsByIDs(ctx, agent.WebhookTargetIDs)
			if err != nil {
				return "", err
			}
			errs := u.Notifier.SendToTargets(ctx, targets, notifier.AgentMessage{
				AgentName: agent.Name, Severity: in.Severity, Title: title, Message: in.Message,
				Link: link, StrategyIDs: strategyIDs, Timestamp: time.Now().UTC(),
			})
			enabled := 0
			for _, t := range targets {
				if t.Enabled {
					enabled++
				}
			}
			res := map[string]any{"sent_to_targets": enabled - len(errs)}
			if len(errs) > 0 {
				msgs := make([]string, 0, len(errs))
				for _, e := range errs {
					msgs = append(msgs, e.Error())
				}
				res["errors"] = msgs
			}
			return compactJSON(res)
		},
	}
}

// --- strategy writer lock ----------------------------------------------------

// ErrStrategyLocked is the tool error when another run holds the lock.
func errStrategyLocked(id uint) error {
	return fmt.Errorf("strategy %d is being edited by another agent run; try later", id)
}

// acquireStrategyLock takes the one-writer lock for strategyID. Inside a
// run the lock is held (re-entrantly) until Run returns; outside a run
// (MCP) it is released right after the write via the returned func.
func (u AgentUseCase) acquireStrategyLock(ctx context.Context, strategyID uint) (func(), error) {
	noop := func() {}
	if u.Lock == nil {
		return noop, nil
	}
	if scope, ok := scopeFrom(ctx); ok {
		scope.mu.Lock()
		held := scope.locks[strategyID]
		scope.mu.Unlock()
		if held {
			return noop, nil
		}
		ok, err := u.Lock.Acquire(ctx, strategyID, scope.holder(), StrategyLockTTL)
		if err != nil {
			return noop, fmt.Errorf("could not acquire the edit lock for strategy %d: %v", strategyID, err)
		}
		if !ok {
			return noop, errStrategyLocked(strategyID)
		}
		scope.mu.Lock()
		scope.locks[strategyID] = true
		scope.mu.Unlock()
		return noop, nil
	}

	holder := "mcp:" + randomToken()
	ok, err := u.Lock.Acquire(ctx, strategyID, holder, StrategyLockTTL)
	if err != nil {
		return noop, fmt.Errorf("could not acquire the edit lock for strategy %d: %v", strategyID, err)
	}
	if !ok {
		return noop, errStrategyLocked(strategyID)
	}
	return func() {
		rctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = u.Lock.Release(rctx, strategyID, holder)
	}, nil
}

func randomToken() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
