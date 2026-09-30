package agent

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"go-trade-bot/app/entities"
	"go-trade-bot/app/repository/agentplatform"
	"go-trade-bot/internal/i18n"
)

// promptMemoryKinds are the memory kinds shown in the system prompt:
// deliberate notes only. Legacy chat_user/chat_agent rows (written by chat
// before the Phase D follow-up) stay in the DB and in read_memory, but no
// longer take slots from findings in the 30-entry window.
var promptMemoryKinds = []entities.MemoryKind{entities.MemoryJournal, entities.MemoryFinding, entities.MemoryReportRef}

const (
	memoryEntriesPerStrategy = 30
	memoryEntryMaxChars      = 800
	memorySectionMaxChars    = 24000
	recentReportsPerStrategy = 5
)

// permissionDescriptions is the plain-words rendering of each permission
// for the operating-context block.
var permissionDescriptions = map[entities.AgentPermission]string{
	entities.PermRead:           "read - inspect strategies, backtests, open positions, performance and optimization results",
	entities.PermBacktest:       "backtest - run backtests",
	entities.PermOptimize:       "optimize - start parameter optimizations",
	entities.PermEditTesting:    "edit_testing - save_strategy_script (create strategies, update backtest-mode drafts), deploy_to_testing (gated deploys to non-live strategies), create_challenger",
	entities.PermNotify:         "notify - send webhook notifications to the operator",
	entities.PermCreateStrategy: "create_strategy - create new testing strategies (backtest/dryrun), bound to you, max 3 per UTC day",
	entities.PermProposeLive:    "propose_live - propose_promotion: file a challenger promotion for the operator to approve",
	entities.PermChain:          "chain - trigger_agent: start another agent's run with a message (max 3 per run; chains stop after 3 hops and never revisit an agent)",
}

var triggerDescriptions = map[string]string{
	"chat_ui":  "chat_ui - a human operator is chatting with you right now",
	"cron":     "cron - a scheduled run; no human is watching, so record what matters in memory and reports",
	"manual":   "manual - the operator queued this run; no human is watching it live",
	"mcp_tool": "mcp_tool - invoked through the MCP server",
	"event":    "event - a strategy event triggered this run; no human is watching, so record what matters in memory and reports",
	"market":   "market - a market watch rule triggered this run; no human is watching, so record what matters in memory and reports",
	"chain":    "chain - another agent's run triggered this run; no human is watching, so record what matters in memory and reports",
}

func uintToString(v uint) string { return strconv.FormatUint(uint64(v), 10) }

// contextStrategyIDs is the set of strategies whose memory goes into the
// prompt: the run's StrategyID, plus - for cron/manual runs - every bound
// strategy.
func (u AgentUseCase) contextStrategyIDs(ctx context.Context, req RunRequest) (ids []uint, bound []uint) {
	seen := map[uint]bool{}
	add := func(id uint) {
		if id != 0 && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if req.StrategyID != nil {
		add(*req.StrategyID)
	}
	if u.Platform != nil && req.Agent.ID != 0 && isScheduledTrigger(req.Trigger) {
		if bindings, err := u.Platform.ListBindingsByAgent(ctx, req.Agent.ID); err == nil {
			for _, b := range bindings {
				bound = append(bound, b.StrategyID)
				add(b.StrategyID)
			}
		}
	}
	return ids, bound
}

// isScheduledTrigger reports whether trigger is an unattended run (every
// trigger except chat_ui and mcp_tool, C-01 §5): its context is every bound
// strategy and it runs under the same Phase A/B policy as cron.
func isScheduledTrigger(trigger string) bool {
	switch trigger {
	case "cron", "manual", "event", "market", "chain":
		return true
	}
	return false
}

// buildSystemPrompt composes (A-01 §4.1): house rules, persona block,
// operating context, shared strategy memory, strategy authoring doc.
func (u AgentUseCase) buildSystemPrompt(ctx context.Context, houseRules string, req RunRequest) string {
	var sections []string
	if strings.TrimSpace(houseRules) != "" {
		sections = append(sections, houseRules)
	}

	persona := fmt.Sprintf("You are the agent %q.", req.Agent.Name)
	if strings.TrimSpace(req.Agent.Goal) != "" {
		persona += " Your goal:\n" + req.Agent.Goal
	}
	sections = append(sections, persona)
	sections = append(sections, languageLine(u.runLocale(ctx, req), isScheduledTrigger(req.Trigger)))

	strategyIDs, bound := u.contextStrategyIDs(ctx, req)
	// Strategy details are only looked up when the platform is wired;
	// legacy wiring (no Platform) keeps the pre-platform call pattern.
	strategies := map[uint]entities.Strategy{}
	if u.Platform != nil {
		strategies = u.loadStrategies(ctx, strategyIDs)
	}
	sections = append(sections, u.operatingContext(req, bound, strategies))
	if wf := u.improvementWorkflow(ctx, req.Agent); wf != "" {
		sections = append(sections, wf)
	}

	if mem := u.memorySection(ctx, strategyIDs, strategies); mem != "" {
		sections = append(sections, mem)
	}
	sections = append(sections, strategyAuthoringDoc)
	return strings.Join(sections, "\n\n")
}

// languageLine is the i18n-02 §2 instruction placed right after the
// persona: the reply language, plus - for unattended runs - the language of
// report text and notification messages (Settings.DefaultLocale).
func languageLine(loc i18n.Locale, unattended bool) string {
	line := fmt.Sprintf("Always answer in %s (%s). Keep code, tool arguments, symbols and identifiers unchanged.", loc.LanguageName(), loc)
	if unattended {
		line += fmt.Sprintf(" Write report text and notification messages in %s.", loc.LanguageName())
	}
	return line
}

// runLocale is the run's reply language (i18n-02 §2): unattended runs
// (cron/manual/event/market/chain) always use Settings.DefaultLocale; chat
// and MCP runs use req.Locale when it is a supported locale (the chat
// handler resolves it from the user, then Accept-Language), else the
// default.
func (u AgentUseCase) runLocale(ctx context.Context, req RunRequest) i18n.Locale {
	if !isScheduledTrigger(req.Trigger) {
		if loc, ok := i18n.Parse(req.Locale); ok {
			return loc
		}
	}
	return u.defaultLocale(ctx)
}

// defaultLocale is Settings.DefaultLocale through u.Locales (en when unset).
func (u AgentUseCase) defaultLocale(ctx context.Context) i18n.Locale {
	if u.Locales == nil {
		return i18n.Default
	}
	return u.Locales.DefaultLocale(ctx)
}

func (u AgentUseCase) loadStrategies(ctx context.Context, ids []uint) map[uint]entities.Strategy {
	out := map[uint]entities.Strategy{}
	if u.Strategy == nil {
		return out
	}
	for _, id := range ids {
		if s, err := u.Strategy.GetByID(ctx, id); err == nil {
			out[id] = s
		}
	}
	return out
}

func (u AgentUseCase) operatingContext(req RunRequest, bound []uint, strategies map[uint]entities.Strategy) string {
	var b strings.Builder
	b.WriteString("## Operating context\n")
	trig := triggerDescriptions[req.Trigger]
	if trig == "" {
		trig = req.Trigger
	}
	fmt.Fprintf(&b, "Trigger: %s\n", trig)
	if len(req.TriggerDetail) > 0 && req.Trigger != "chat_ui" {
		detail := string(req.TriggerDetail)
		if len(detail) > 2000 {
			detail = detail[:2000] + "...(truncated)"
		}
		fmt.Fprintf(&b, "Trigger detail: %s\n", detail)
	}
	if req.Trigger == "chain" {
		path := make([]string, 0, len(req.ChainPath))
		for _, id := range req.ChainPath {
			path = append(path, "#"+uintToString(id))
		}
		fmt.Fprintf(&b, "Chain: depth %d of max 3; agents before you in this chain: %s. trigger_agent cannot start any of them.\n",
			req.ChainDepth, strings.Join(path, " → "))
	}

	if isScheduledTrigger(req.Trigger) {
		if len(bound) == 0 {
			b.WriteString("Bound strategies: none.\n")
		} else {
			b.WriteString("Bound strategies (the strategies you are responsible for):\n")
			for _, id := range bound {
				if s, ok := strategies[id]; ok {
					fmt.Fprintf(&b, "- strategy_id=%d name=%q status=%s mode=%s\n", s.ID, s.Name, s.Status, s.Mode)
				} else {
					fmt.Fprintf(&b, "- strategy_id=%d (not found)\n", id)
				}
			}
		}
	} else if req.StrategyID != nil {
		if s, ok := strategies[*req.StrategyID]; ok {
			fmt.Fprintf(&b, "Context strategy: strategy_id=%d name=%q status=%s mode=%s\n", s.ID, s.Name, s.Status, s.Mode)
		}
	}

	b.WriteString("Your permissions:\n")
	var granted []string
	for _, p := range entities.AllAgentPermissions {
		if req.Agent.HasPermission(p) {
			granted = append(granted, "- "+permissionDescriptions[p])
		}
	}
	if len(granted) == 0 {
		b.WriteString("- none beyond the always-available memory and report tools\n")
	} else {
		b.WriteString(strings.Join(granted, "\n") + "\n")
	}
	b.WriteString("Always available: read_memory, write_journal, list_reports, write_report, list_proposals, get_deploy_gate_config.\n")
	b.WriteString("Before backtesting, call get_candle_coverage and pick a timeframe and date range that has data.\n")
	b.WriteString("Safety rules (enforced by the platform): live/productive strategies can never be modified by any tool - " +
		"improve them through a challenger and an operator-approved promotion. You may only change strategies in your scope " +
		"(bound to you, challengers of bound strategies, or created by you). No tool can make a " +
		"strategy live or productive, place or cancel orders, or change settings/credentials. Reports must reference data by id " +
		"(backtest_run id, strategy_id) - never type metric numbers into report blocks yourself; text_table is for qualitative notes only.")
	return b.String()
}

type memoryLine struct {
	strategyID uint
	createdAt  time.Time
	text       string
}

// memorySection renders the last N memory entries (oldest -> newest) and
// the last few report titles for each context strategy, capped at
// memorySectionMaxChars by dropping the globally-oldest entries first.
func (u AgentUseCase) memorySection(ctx context.Context, strategyIDs []uint, strategies map[uint]entities.Strategy) string {
	if u.Platform == nil || len(strategyIDs) == 0 {
		return ""
	}
	names := map[uint]string{}
	if agents, err := u.Platform.ListAgents(ctx); err == nil {
		for _, a := range agents {
			names[a.ID] = a.Name
		}
	}

	perStrategy := map[uint][]memoryLine{}
	reports := map[uint][]string{}
	for _, sid := range strategyIDs {
		entries, err := u.Platform.ListMemory(ctx, sid, promptMemoryKinds, memoryEntriesPerStrategy, nil)
		if err == nil {
			for i := len(entries) - 1; i >= 0; i-- { // newest-first -> oldest-first
				e := entries[i]
				author := "operator"
				if e.AuthorAgentID != nil {
					if n, ok := names[*e.AuthorAgentID]; ok {
						author = n
					} else {
						author = "agent #" + uintToString(*e.AuthorAgentID)
					}
				}
				content := e.Content
				if len([]rune(content)) > memoryEntryMaxChars {
					content = string([]rune(content)[:memoryEntryMaxChars]) + "…"
				}
				ref := ""
				if e.RefID != nil {
					ref = " ref=" + uintToString(*e.RefID)
				}
				perStrategy[sid] = append(perStrategy[sid], memoryLine{
					strategyID: sid,
					createdAt:  e.CreatedAt,
					text:       fmt.Sprintf("[%s %s %s%s] %s", e.CreatedAt.UTC().Format(time.RFC3339), author, e.Kind, ref, content),
				})
			}
		}
		sidCopy := sid
		if reps, err := u.Platform.ListReports(ctx, agentplatform.ReportFilter{StrategyID: &sidCopy, Limit: recentReportsPerStrategy}); err == nil {
			for _, r := range reps {
				reports[sid] = append(reports[sid], fmt.Sprintf("report_id=%d %q severity=%s at %s", r.ID, r.Title, r.Severity, r.CreatedAt.UTC().Format(time.RFC3339)))
			}
		}
	}

	render := func() string {
		var b strings.Builder
		b.WriteString("## Shared strategy memory\nMemory is shared by every agent and the operator, per strategy (oldest first). Use write_journal to add to it.\n")
		for _, sid := range strategyIDs {
			label := fmt.Sprintf("strategy_id=%d", sid)
			if s, ok := strategies[sid]; ok {
				label += fmt.Sprintf(" %q", s.Name)
			}
			fmt.Fprintf(&b, "\n### %s\n", label)
			if len(perStrategy[sid]) == 0 {
				b.WriteString("(no memory entries yet)\n")
			}
			for _, l := range perStrategy[sid] {
				b.WriteString(l.text + "\n")
			}
			if len(reports[sid]) > 0 {
				b.WriteString("Recent reports:\n")
				for _, r := range reports[sid] {
					b.WriteString("- " + r + "\n")
				}
			}
		}
		return b.String()
	}

	out := render()
	for len(out) > memorySectionMaxChars {
		// Drop the globally oldest remaining entry.
		var oldestSID uint
		var oldest time.Time
		found := false
		for sid, lines := range perStrategy {
			if len(lines) == 0 {
				continue
			}
			if !found || lines[0].createdAt.Before(oldest) {
				oldest, oldestSID, found = lines[0].createdAt, sid, true
			}
		}
		if !found {
			break
		}
		perStrategy[oldestSID] = perStrategy[oldestSID][1:]
		out = render()
	}
	return out
}

// improvementWorkflow is the Phase B-01 §6 operating-context block: how to
// improve strategies, the current deploy gate thresholds and today's
// remaining auto-deploys. Empty when the gate isn't wired (legacy wiring).
func (u AgentUseCase) improvementWorkflow(ctx context.Context, agent entities.Agent) string {
	if u.Gate == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Improving strategies\n")
	b.WriteString("- Live or productive strategy: create_challenger (a dryrun/testing clone linked to it) -> iterate on the challenger " +
		"with deploy_to_testing -> let it forward-test (>= 7 days; otherwise start the rationale with \"EARLY:\") -> propose_promotion. " +
		"The operator approves in the web UI and the change is applied when the champion has no open position.\n")
	b.WriteString("- Non-live strategy: deploy_to_testing (gated). If the gate fails nothing changes and a proposal is filed for the operator.\n")
	b.WriteString("- save_strategy_script only creates strategies or updates backtest-mode drafts.\n")
	b.WriteString("- The gate runs two walk-forward backtests (current vs new code) server-side; you cannot supply metrics or thresholds.\n")
	if cfg, err := u.Gate.Config(ctx); err == nil {
		tf := cfg.Timeframe
		if tf == "" {
			tf = "the strategy's cycle interval"
		}
		fmt.Fprintf(&b, "Deploy gate thresholds: candidate out-of-sample trades >= %d; Sharpe >= baseline + %.2f; max drawdown <= baseline x %.2f "+
			"(0 if the baseline's is 0); profit factor >= %.2f. Walk-forward over the last %d months (train %d, test %d) at %s on the first symbol.\n",
			cfg.MinTrades, cfg.MinSharpeDelta, cfg.MaxDrawdownRatio, cfg.MinProfitFactor, cfg.LookbackMonths, cfg.TrainMonths, cfg.TestMonths, tf)
	}
	if u.Platform != nil && agent.ID != 0 {
		if remaining, max, ok := u.remainingAutoDeploysFor(ctx, agent.ID); ok {
			if max <= 0 {
				b.WriteString("Auto-deploy is disabled for you (max_auto_deploys_per_day = 0): deploy_to_testing will refuse.\n")
			} else {
				fmt.Fprintf(&b, "Auto-deploys remaining today (UTC): %d of %d.\n", remaining, max)
			}
		}
	}
	return b.String()
}
