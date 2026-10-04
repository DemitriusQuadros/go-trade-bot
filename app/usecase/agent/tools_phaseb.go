package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"strings"
	"time"

	"go-trade-bot/app/entities"
	proposalrepo "go-trade-bot/app/repository/proposal"
	"go-trade-bot/app/strategies"
	"go-trade-bot/app/strategies/script"
	"go-trade-bot/app/usecase/agent/deploygate"
	"go-trade-bot/internal/modelprovider"
	"go-trade-bot/internal/notifier"

	"gorm.io/datatypes"
)

// Agents platform Phase B-01 §4 tools: challengers, the gated
// deploy_to_testing, promotion proposals, create_strategy and read-only
// proposal/gate tools.
//
// SAFETY: no tool here can change a live-mode or productive strategy's
// source, mode or status. deploy_to_testing refuses them; create_challenger
// only READS the champion and creates a separate dryrun/testing clone;
// propose_promotion only files a pending proposal. The only path that
// changes a live strategy's code is cmd/agent's agent:apply_proposal task
// after an operator approves over authenticated REST - this package has no
// dependency on it (see phaseb_isolation_test.go).

// ProposalStore is the slice of app/repository/proposal the tools need. It
// cannot apply a proposal.
type ProposalStore interface {
	Create(ctx context.Context, p entities.StrategyChangeProposal) (entities.StrategyChangeProposal, error)
	List(ctx context.Context, f proposalrepo.Filter) ([]entities.StrategyChangeProposal, error)
	SupersedePending(ctx context.Context, targetID, exceptID uint, reason string) (int64, error)
}

// ClosedSignalReader reads closed signals for forward-test evidence
// (app/repository/signal.SignalRepository).
type ClosedSignalReader interface {
	ListClosedSince(ctx context.Context, strategyID uint, since time.Time) ([]entities.Signal, error)
}

// ScriptValidator compiles Lua without running it.
type ScriptValidator interface {
	Validate(source string) error
}

// defaultValidator is the script runner's compile-only validation (the
// same one the REPL/fast-rerun path uses).
var defaultValidator ScriptValidator = script.NewRunner(script.DefaultHookTimeout, nil)

const (
	// MaxStrategiesCreatedPerDay is create_strategy's per-agent, per-UTC-day
	// limit, counted in AgentUsage.StrategiesCreated.
	MaxStrategiesCreatedPerDay = 3
	// MinChallengerForwardTestAge is the forward-test age a challenger needs
	// before propose_promotion, unless the rationale starts with "EARLY:".
	MinChallengerForwardTestAge = 7 * 24 * time.Hour
	maxRationaleChars           = 4000
	listProposalsLimit          = 20
)

func (u AgentUseCase) now() time.Time {
	if u.Clock != nil {
		return u.Clock()
	}
	return time.Now()
}

func (u AgentUseCase) validator() ScriptValidator {
	if u.Validator != nil {
		return u.Validator
	}
	return defaultValidator
}

// ProposalURL is the operator deep link for a proposal.
func (u AgentUseCase) ProposalURL(id uint) string {
	return strings.TrimRight(u.APIBaseURL, "/") + "/agents/proposals/" + uintToString(id)
}

// clampAgentMode is the Phase A clamp: only the literal "backtest" passes,
// everything else becomes "dryrun".
func clampAgentMode(mode string) string {
	if mode == strategies.ModeBacktest.String() {
		return mode
	}
	return strategies.ModeDryRun.String()
}

// --- evidence --------------------------------------------------------------

type gateContextJSON struct {
	Symbol    string         `json:"symbol"`
	Timeframe string         `json:"timeframe"`
	From      time.Time      `json:"from"`
	To        time.Time      `json:"to"`
	Config    gateConfigJSON `json:"thresholds"`
}

type gateConfigJSON struct {
	MinSharpeDelta   deploygate.JSONFloat `json:"min_sharpe_delta"`
	MaxDrawdownRatio deploygate.JSONFloat `json:"max_drawdown_ratio"`
	MinTrades        int                  `json:"min_trades"`
	MinProfitFactor  deploygate.JSONFloat `json:"min_profit_factor"`
	LookbackMonths   int                  `json:"lookback_months"`
	TrainMonths      int                  `json:"train_months"`
	TestMonths       int                  `json:"test_months"`
	Timeframe        string               `json:"timeframe"`
}

func toGateConfigJSON(c entities.DeployGateConfig) gateConfigJSON {
	return gateConfigJSON{
		MinSharpeDelta: deploygate.JSONFloat(c.MinSharpeDelta), MaxDrawdownRatio: deploygate.JSONFloat(c.MaxDrawdownRatio),
		MinTrades: c.MinTrades, MinProfitFactor: deploygate.JSONFloat(c.MinProfitFactor),
		LookbackMonths: c.LookbackMonths, TrainMonths: c.TrainMonths, TestMonths: c.TestMonths, Timeframe: c.Timeframe,
	}
}

// ForwardTestStats summarises closed trades. MaxAdverse is the worst single
// closed-trade PnL (most negative; 0 when there are no losing trades).
type ForwardTestStats struct {
	Trades     int                  `json:"trades"`
	WinRatePct deploygate.JSONFloat `json:"win_rate_pct"`
	NetPnl     deploygate.JSONFloat `json:"net_pnl"`
	MaxAdverse deploygate.JSONFloat `json:"max_adverse"`
}

type forwardTestJSON struct {
	Since      time.Time        `json:"since"`
	AgeDays    float64          `json:"age_days"`
	Challenger ForwardTestStats `json:"challenger"`
	Champion   ForwardTestStats `json:"champion"`
}

// Evidence is StrategyChangeProposal.EvidenceJSON's shape.
type Evidence struct {
	Gate        *deploygate.GateResult `json:"gate"`
	GateContext *gateContextJSON       `json:"gate_context,omitempty"`
	ForwardTest *forwardTestJSON       `json:"forward_test,omitempty"`
}

func gateEvidence(o GateOutcome) (*deploygate.GateResult, *gateContextJSON) {
	res := o.Result
	return &res, &gateContextJSON{Symbol: o.Symbol, Timeframe: o.Timeframe, From: o.From, To: o.To, Config: toGateConfigJSON(o.Config)}
}

// ComputeForwardTestStats aggregates closed signals.
func ComputeForwardTestStats(signals []entities.Signal) ForwardTestStats {
	var st ForwardTestStats
	wins := 0
	net, worst := 0.0, 0.0
	for _, s := range signals {
		p := 0.0
		for _, o := range s.Orders {
			p += float64(o.Profit)
		}
		st.Trades++
		net += p
		if p > 0 {
			wins++
		}
		if p < worst {
			worst = p
		}
	}
	if st.Trades > 0 {
		st.WinRatePct = deploygate.JSONFloat(float64(wins) / float64(st.Trades) * 100)
	}
	st.NetPnl = deploygate.JSONFloat(net)
	st.MaxAdverse = deploygate.JSONFloat(worst)
	return st
}

func gateSummary(r deploygate.GateResult) string {
	var parts []string
	for _, c := range r.Checks {
		status := "pass"
		if !c.Passed {
			status = "FAIL"
		}
		parts = append(parts, fmt.Sprintf("%s %s (%s)", c.Name, status, c.Detail))
	}
	return strings.Join(parts, "; ")
}

// --- create_challenger -------------------------------------------------------

var createChallengerSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"champion_strategy_id": {"type": "integer", "description": "a live-mode or productive strategy bound to you"},
		"name_suffix": {"type": "string", "description": "optional suffix for the challenger's name"}
	},
	"required": ["champion_strategy_id"]
}`)

func (u AgentUseCase) createChallengerTool() Tool {
	return Tool{
		Def: modelprovider.ToolDefinition{
			Name: "create_challenger",
			Description: "Create (or return the existing) challenger for a live or productive strategy bound to you: a dryrun/testing clone " +
				"linked to its champion. Iterate on the challenger with deploy_to_testing, gather forward-test evidence, then propose_promotion. " +
				"The champion itself is never modified.",
			InputSchema: createChallengerSchema,
		},
		Execute: func(ctx context.Context, raw json.RawMessage) (string, error) {
			if err := u.requirePlatform("create_challenger"); err != nil {
				return "", err
			}
			var in struct {
				ChampionStrategyID uint   `json:"champion_strategy_id"`
				NameSuffix         string `json:"name_suffix"`
			}
			if err := json.Unmarshal(raw, &in); err != nil {
				return "", fmt.Errorf("create_challenger: invalid args: %w", err)
			}
			agent, runID, err := u.toolAgent(ctx)
			if err != nil {
				return "", err
			}
			champion, err := u.requireStrategy(ctx, "create_challenger", in.ChampionStrategyID)
			if err != nil {
				return "", err
			}
			if agent.ID == 0 {
				return "", fmt.Errorf("create_challenger: agent %q is not a saved persona", agent.Name)
			}
			bound, err := u.isBound(ctx, agent.ID, champion.ID)
			if err != nil {
				return "", fmt.Errorf("create_challenger: %w", err)
			}
			if !bound {
				return "", fmt.Errorf("create_challenger: strategy %d is not bound to agent %q", champion.ID, agent.Name)
			}
			if !champion.IsLiveOrProductive() {
				return "", fmt.Errorf("create_challenger: strategy %d is %s/%s - challengers are only for live or productive strategies; change it directly with deploy_to_testing", champion.ID, champion.Status, champion.Mode)
			}
			if champion.ChallengerOfID != nil {
				return "", fmt.Errorf("create_challenger: strategy %d is itself a challenger", champion.ID)
			}

			// Serialise concurrent create_challenger calls for the same
			// champion (the agent writer lock - the champion is only read).
			release, err := u.acquireStrategyLock(ctx, champion.ID)
			if err != nil {
				return "", err
			}
			defer release()

			all, err := u.Strategy.GetAll(ctx)
			if err != nil {
				return "", fmt.Errorf("create_challenger: %w", err)
			}
			for _, s := range all {
				if s.ChallengerOfID != nil && *s.ChallengerOfID == champion.ID && s.Status != entities.Disabled {
					if err := u.Platform.AddBinding(ctx, agent.ID, s.ID); err != nil {
						log.Printf("create_challenger: could not bind existing challenger %d to agent %d: %v", s.ID, agent.ID, err)
					}
					return compactJSON(map[string]any{
						"challenger_strategy_id": s.ID, "champion_strategy_id": champion.ID, "created": false,
						"note": "an active challenger already exists for this champion; iterate on it with deploy_to_testing",
					})
				}
			}

			name := champion.Name + " · challenger"
			if suffix := strings.TrimSpace(in.NameSuffix); suffix != "" {
				name += " · " + suffix
			}
			championID, agentID := champion.ID, agent.ID
			clone := entities.Strategy{
				Name:             name,
				Description:      fmt.Sprintf("Challenger of #%d %s (created by agent %q)", champion.ID, champion.Name, agent.Name),
				StrategyName:     champion.StrategyName,
				ScriptSource:     champion.ScriptSource,
				Status:           entities.Testing,
				Mode:             strategies.ModeDryRun.String(),
				MonitoredSymbols: append(datatypes.JSONSlice[string]{}, champion.MonitoredSymbols...),
				StrategyConfiguration: entities.StrategyConfiguration{
					Cycle:         champion.StrategyConfiguration.Cycle,
					Configuration: append(datatypes.JSON{}, champion.StrategyConfiguration.Configuration...),
				},
				ChallengerOfID:   &championID,
				CreatedByAgentID: &agentID,
			}
			saved, err := u.Strategy.Save(ctx, clone)
			if err != nil {
				return "", fmt.Errorf("create_challenger: %w", err)
			}
			if err := u.Platform.AddBinding(ctx, agent.ID, saved.ID); err != nil {
				return "", fmt.Errorf("create_challenger: challenger %d created but binding it to agent %q failed: %w", saved.ID, agent.Name, err)
			}
			u.appendMemory(ctx, entities.StrategyMemoryEntry{
				StrategyID: champion.ID, AuthorAgentID: &agentID, AgentRunID: runID, Kind: entities.MemoryFinding,
				Content: fmt.Sprintf("Challenger #%d %q created by agent %q (dryrun, testing). Changes are iterated there and reach this strategy only through an operator-approved promotion.", saved.ID, name, agent.Name),
			})
			return compactJSON(map[string]any{"challenger_strategy_id": saved.ID, "champion_strategy_id": champion.ID, "created": true})
		},
	}
}

// --- deploy_to_testing -------------------------------------------------------

var deployToTestingSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"strategy_id": {"type": "integer", "description": "a non-live, non-productive strategy in your scope (e.g. a challenger or dryrun strategy)"},
		"script_source": {"type": "string", "description": "the complete new Lua source"},
		"rationale": {"type": "string", "description": "why this change should be better"},
		"skip_gate": {"type": "boolean", "description": "set to true to skip the backtest gate and apply code directly when the operator explicitly approves in chat without testing"}
	},
	"required": ["strategy_id", "script_source", "rationale"]
}`)

func (u AgentUseCase) deployToTestingTool() Tool {
	return Tool{
		Def: modelprovider.ToolDefinition{
			Name: "deploy_to_testing",
			Description: "Deploy new code to a non-live, non-productive strategy in your scope. Set skip_gate=true if the operator explicitly approved applying the code via chat without testing, which deploys directly without running the slow walk-forward backtest gate. Otherwise, runs the server-side metric gate (two walk-forward backtests). Live/productive strategies are never modified directly.",
			InputSchema: deployToTestingSchema,
		},
		Execute: func(ctx context.Context, raw json.RawMessage) (string, error) {
			if err := u.requirePlatform("deploy_to_testing"); err != nil {
				return "", err
			}
			if u.Gate == nil || u.Proposals == nil {
				return "", fmt.Errorf("deploy_to_testing: the deploy gate is not available on this server")
			}
			// Only these fields are read; any threshold/metric-like
			// keys in the args are ignored (the gate reads DeployGateConfig).
			var in struct {
				StrategyID   uint   `json:"strategy_id"`
				ScriptSource string `json:"script_source"`
				Rationale    string `json:"rationale"`
				SkipGate     bool   `json:"skip_gate"`
			}
			if err := json.Unmarshal(raw, &in); err != nil {
				return "", fmt.Errorf("deploy_to_testing: invalid args: %w", err)
			}
			if strings.TrimSpace(in.ScriptSource) == "" {
				return "", fmt.Errorf("deploy_to_testing: script_source is required")
			}
			in.Rationale = strings.TrimSpace(in.Rationale)
			if in.Rationale == "" || len([]rune(in.Rationale)) > maxRationaleChars {
				return "", fmt.Errorf("deploy_to_testing: rationale is required (at most %d characters)", maxRationaleChars)
			}
			agent, runID, err := u.toolAgent(ctx)
			if err != nil {
				return "", err
			}
			target, err := u.requireStrategy(ctx, "deploy_to_testing", in.StrategyID)
			if err != nil {
				return "", err
			}
			if target.IsLiveOrProductive() {
				return "", fmt.Errorf("deploy_to_testing: strategy %d is %s/%s - agents never change live or productive strategies; use create_challenger, iterate on the challenger, then propose_promotion", target.ID, target.Status, target.Mode)
			}
			if err := u.inWriteScope(ctx, agent, target); err != nil {
				return "", fmt.Errorf("deploy_to_testing: %w", err)
			}

			if in.SkipGate {
				if sc, ok := scopeFrom(ctx); ok && isScheduledTrigger(sc.trigger) {
					return "", fmt.Errorf("deploy_to_testing: skip_gate is only permitted during interactive chat when approved by the operator")
				}
				release, err := u.acquireStrategyLock(ctx, target.ID)
				if err != nil {
					return "", err
				}
				defer release()

				if err := u.validator().Validate(in.ScriptSource); err != nil {
					return "", fmt.Errorf("deploy_to_testing: script_source does not compile: %v", err)
				}
				if in.ScriptSource == target.ScriptSource {
					return "", fmt.Errorf("deploy_to_testing: script_source is identical to strategy %d's current source", target.ID)
				}

				current, err := u.Strategy.GetByID(ctx, target.ID)
				if err != nil {
					return "", fmt.Errorf("deploy_to_testing: could not reload strategy %d: %w", target.ID, err)
				}
				if current.IsLiveOrProductive() {
					return "", fmt.Errorf("deploy_to_testing: strategy %d became %s/%s - live/productive strategies cannot be modified directly", current.ID, current.Status, current.Mode)
				}

				updated := current
				updated.ScriptSource = in.ScriptSource
				updated.Mode = clampAgentMode(updated.Mode)
				if updated.Status != entities.Disabled {
					updated.Status = entities.Testing
				}
				if err := u.Strategy.Update(ctx, updated); err != nil {
					return "", fmt.Errorf("deploy_to_testing: %w", err)
				}

				agentID := agent.ID
				u.appendMemory(ctx, entities.StrategyMemoryEntry{
					StrategyID: target.ID, AuthorAgentID: &agentID, AgentRunID: runID, Kind: entities.MemoryFinding,
					Content: truncate(fmt.Sprintf("Directly applied new code without testing (operator approved via chat). Rationale: %s", in.Rationale), maxJournalChars),
				})
				return compactJSON(map[string]any{"deployed": true, "strategy_id": target.ID, "gate_skipped": true})
			}

			// 1. Daily auto-deploy cap (re-read the persona: the operator may
			// have changed it since the run started).
			fresh, err := u.Platform.GetAgent(ctx, agent.ID)
			if err != nil {
				return "", fmt.Errorf("deploy_to_testing: could not load agent %q: %w", agent.Name, err)
			}
			if fresh.MaxAutoDeploysPerDay <= 0 {
				return "", fmt.Errorf("deploy_to_testing: auto-deploy is disabled for agent %q (max_auto_deploys_per_day = 0) - do not retry; file a proposal instead: record the candidate change and rationale with write_journal/write_report for the operator (for a live strategy use create_challenger + propose_promotion)", fresh.Name)
			}
			usage, err := u.Platform.GetUsage(ctx, fresh.ID, u.now())
			if err != nil {
				return "", fmt.Errorf("deploy_to_testing: could not read today's usage: %w", err)
			}
			if usage.AutoDeploys >= fresh.MaxAutoDeploysPerDay {
				return "", fmt.Errorf("deploy_to_testing: daily auto-deploy limit reached for agent %q (%d/%d today, UTC) - do not retry today; file a proposal instead: record the candidate change and rationale with write_journal/write_report for the operator", fresh.Name, usage.AutoDeploys, fresh.MaxAutoDeploysPerDay)
			}

			// 2. One writer per strategy.
			release, err := u.acquireStrategyLock(ctx, target.ID)
			if err != nil {
				return "", err
			}
			defer release()

			// 3. The Lua must compile.
			if err := u.validator().Validate(in.ScriptSource); err != nil {
				return "", fmt.Errorf("deploy_to_testing: script_source does not compile: %v", err)
			}
			if in.ScriptSource == target.ScriptSource {
				return "", fmt.Errorf("deploy_to_testing: script_source is identical to strategy %d's current source", target.ID)
			}

			// 4. The gate (thresholds from DeployGateConfig only).
			outcome, err := u.Gate.Run(ctx, target, in.ScriptSource)
			if err != nil {
				return "", fmt.Errorf("deploy_to_testing: %w", err)
			}
			gate, gctx := gateEvidence(outcome)

			if !outcome.Result.Passed {
				// 6. Fail: no write, file a gate_failed_change proposal.
				return u.fileGateFailedProposal(ctx, agent, runID, target, in.ScriptSource, in.Rationale, gate, gctx)
			}

			// 5. Pass. The gate took minutes: re-check the target before
			// writing - never deploy onto a strategy that became live/
			// productive or whose code changed meanwhile.
			current, err := u.Strategy.GetByID(ctx, target.ID)
			if err != nil {
				return "", fmt.Errorf("deploy_to_testing: could not reload strategy %d: %w", target.ID, err)
			}
			if current.IsLiveOrProductive() {
				return "", fmt.Errorf("deploy_to_testing: strategy %d became %s/%s while the gate ran - nothing was deployed", current.ID, current.Status, current.Mode)
			}
			if current.ScriptSource != target.ScriptSource {
				return "", fmt.Errorf("deploy_to_testing: strategy %d's code changed while the gate ran - nothing was deployed; re-run against the new code", current.ID)
			}
			ok, err := u.Platform.TryIncAutoDeploys(ctx, fresh.ID, u.now(), fresh.MaxAutoDeploysPerDay)
			if err != nil {
				return "", fmt.Errorf("deploy_to_testing: could not record the auto-deploy: %w", err)
			}
			if !ok {
				return "", fmt.Errorf("deploy_to_testing: daily auto-deploy limit reached for agent %q while the gate ran - nothing was deployed; file a proposal instead", fresh.Name)
			}

			updated := current
			updated.ScriptSource = in.ScriptSource
			updated.Mode = clampAgentMode(updated.Mode) // Phase A clamp: backtest stays, anything else -> dryrun
			if updated.Status != entities.Disabled {
				updated.Status = entities.Testing
			}
			if err := u.Strategy.Update(ctx, updated); err != nil {
				return "", fmt.Errorf("deploy_to_testing: %w", err)
			}

			agentID := agent.ID
			u.appendMemory(ctx, entities.StrategyMemoryEntry{
				StrategyID: target.ID, AuthorAgentID: &agentID, AgentRunID: runID, Kind: entities.MemoryFinding,
				Content: truncate(fmt.Sprintf("Auto-deployed new code (deploy gate passed; baseline run #%d, candidate run #%d, %s %s). %s. Rationale: %s",
					gate.BaselineRunID, gate.CandidateRunID, gctx.Symbol, gctx.Timeframe, gateSummary(*gate), in.Rationale), maxJournalChars),
			})
			return compactJSON(map[string]any{"deployed": true, "strategy_id": target.ID, "gate": gate, "gate_context": gctx})
		},
	}
}

func (u AgentUseCase) fileGateFailedProposal(ctx context.Context, agent entities.Agent, runID *uint, target entities.Strategy, source, rationale string, gate *deploygate.GateResult, gctx *gateContextJSON) (string, error) {
	evidence, err := json.Marshal(Evidence{Gate: gate, GateContext: gctx})
	if err != nil {
		return "", fmt.Errorf("deploy_to_testing: could not encode evidence: %w", err)
	}
	p, err := u.Proposals.Create(ctx, entities.StrategyChangeProposal{
		Kind:             entities.ProposalGateFailedChange,
		TargetStrategyID: target.ID,
		AgentID:          agent.ID,
		AgentRunID:       runID,
		BaseSource:       target.ScriptSource,
		ProposedSource:   source,
		Rationale:        rationale,
		EvidenceJSON:     datatypes.JSON(evidence),
		Status:           entities.ProposalPending,
	})
	if err != nil {
		return "", fmt.Errorf("deploy_to_testing: gate failed and filing the proposal failed: %w", err)
	}
	agentID := agent.ID
	u.appendMemory(ctx, entities.StrategyMemoryEntry{
		StrategyID: target.ID, AuthorAgentID: &agentID, AgentRunID: runID, Kind: entities.MemoryFinding,
		Content: truncate(fmt.Sprintf("Deploy gate FAILED - nothing deployed; filed proposal #%d for the operator. %s", p.ID, gateSummary(*gate)), maxJournalChars),
	})
	return compactJSON(map[string]any{"deployed": false, "proposal_id": p.ID, "gate": gate, "gate_context": gctx, "url": u.ProposalURL(p.ID)})
}

// --- propose_promotion -------------------------------------------------------

var proposePromotionSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"challenger_strategy_id": {"type": "integer"},
		"rationale": {"type": "string", "description": "why the challenger should replace its champion. Needs >= 7 days of challenger forward-testing unless it starts with \"EARLY:\" (shown to the operator as a warning)"},
		"report_id": {"type": "integer", "description": "optional supporting report"}
	},
	"required": ["challenger_strategy_id", "rationale"]
}`)

func (u AgentUseCase) proposePromotionTool() Tool {
	return Tool{
		Def: modelprovider.ToolDefinition{
			Name: "propose_promotion",
			Description: "File a proposal to replace a challenger's champion code with the challenger's code. Computes forward-test evidence " +
				"(both strategies' closed trades since the challenger was created) and runs the deploy gate (recorded, not blocking). The " +
				"operator approves or rejects in the web UI; once approved it is applied when the champion is flat. Supersedes any older " +
				"pending proposal for the same champion and notifies the operator. Slow (minutes).",
			InputSchema: proposePromotionSchema,
		},
		Execute: func(ctx context.Context, raw json.RawMessage) (string, error) {
			if err := u.requirePlatform("propose_promotion"); err != nil {
				return "", err
			}
			if u.Proposals == nil || u.Gate == nil {
				return "", fmt.Errorf("propose_promotion: proposals are not available on this server")
			}
			var in struct {
				ChallengerStrategyID uint   `json:"challenger_strategy_id"`
				Rationale            string `json:"rationale"`
				ReportID             *uint  `json:"report_id"`
			}
			if err := json.Unmarshal(raw, &in); err != nil {
				return "", fmt.Errorf("propose_promotion: invalid args: %w", err)
			}
			in.Rationale = strings.TrimSpace(in.Rationale)
			if in.Rationale == "" || len([]rune(in.Rationale)) > maxRationaleChars {
				return "", fmt.Errorf("propose_promotion: rationale is required (at most %d characters)", maxRationaleChars)
			}
			agent, runID, err := u.toolAgent(ctx)
			if err != nil {
				return "", err
			}
			challenger, err := u.requireStrategy(ctx, "propose_promotion", in.ChallengerStrategyID)
			if err != nil {
				return "", err
			}
			if challenger.ChallengerOfID == nil {
				return "", fmt.Errorf("propose_promotion: strategy %d is not a challenger", challenger.ID)
			}
			if err := u.inWriteScope(ctx, agent, challenger); err != nil {
				return "", fmt.Errorf("propose_promotion: %w", err)
			}
			champion, err := u.requireStrategy(ctx, "propose_promotion", *challenger.ChallengerOfID)
			if err != nil {
				return "", err
			}
			if challenger.ScriptSource == champion.ScriptSource {
				return "", fmt.Errorf("propose_promotion: challenger %d's code is identical to its champion's", challenger.ID)
			}

			now := u.now()
			age := now.Sub(challenger.CreatedAt)
			early := strings.HasPrefix(in.Rationale, entities.EarlyRationalePrefix)
			if age < MinChallengerForwardTestAge && !early {
				return "", fmt.Errorf("propose_promotion: challenger %d has only %.1f days of forward-testing (need 7) - wait, or start the rationale with %q to file early (shown to the operator as a warning)",
					challenger.ID, age.Hours()/24, entities.EarlyRationalePrefix)
			}
			var reportID *uint
			if in.ReportID != nil && *in.ReportID != 0 {
				if _, err := u.Platform.GetReport(ctx, *in.ReportID); err != nil {
					return "", fmt.Errorf("propose_promotion: report %d not found", *in.ReportID)
				}
				reportID = in.ReportID
			}

			ev := Evidence{ForwardTest: &forwardTestJSON{Since: challenger.CreatedAt.UTC(), AgeDays: math.Round(age.Hours()/24*10) / 10}}
			if u.ForwardTest != nil {
				if sigs, err := u.ForwardTest.ListClosedSince(ctx, challenger.ID, challenger.CreatedAt); err == nil {
					ev.ForwardTest.Challenger = ComputeForwardTestStats(sigs)
				} else {
					return "", fmt.Errorf("propose_promotion: could not read the challenger's trades: %w", err)
				}
				if sigs, err := u.ForwardTest.ListClosedSince(ctx, champion.ID, challenger.CreatedAt); err == nil {
					ev.ForwardTest.Champion = ComputeForwardTestStats(sigs)
				} else {
					return "", fmt.Errorf("propose_promotion: could not read the champion's trades: %w", err)
				}
			}

			// The gate is recorded, never blocking here - the operator decides.
			outcome, gErr := u.Gate.Run(ctx, champion, challenger.ScriptSource)
			if gErr != nil {
				res := deploygate.Errored(gErr.Error())
				ev.Gate = &res
			} else {
				ev.Gate, ev.GateContext = gateEvidence(outcome)
			}
			evidence, err := json.Marshal(ev)
			if err != nil {
				return "", fmt.Errorf("propose_promotion: could not encode evidence: %w", err)
			}

			challengerID := challenger.ID
			p, err := u.Proposals.Create(ctx, entities.StrategyChangeProposal{
				Kind:                 entities.ProposalPromoteChallenger,
				TargetStrategyID:     champion.ID,
				ChallengerStrategyID: &challengerID,
				AgentID:              agent.ID,
				AgentRunID:           runID,
				BaseSource:           champion.ScriptSource,
				ProposedSource:       challenger.ScriptSource,
				Rationale:            in.Rationale,
				EvidenceJSON:         datatypes.JSON(evidence),
				ReportID:             reportID,
				Status:               entities.ProposalPending,
			})
			if err != nil {
				return "", fmt.Errorf("propose_promotion: %w", err)
			}
			superseded, err := u.Proposals.SupersedePending(ctx, champion.ID, p.ID, fmt.Sprintf("superseded by proposal #%d", p.ID))
			if err != nil {
				log.Printf("propose_promotion: could not supersede older proposals for strategy %d: %v", champion.ID, err)
			}

			agentID := agent.ID
			u.appendMemory(ctx, entities.StrategyMemoryEntry{
				StrategyID: champion.ID, AuthorAgentID: &agentID, AgentRunID: runID, Kind: entities.MemoryFinding,
				Content: truncate(fmt.Sprintf("Promotion proposal #%d filed by agent %q: replace this strategy's code with challenger #%d's (awaiting operator approval). Gate passed: %v. Rationale: %s",
					p.ID, agent.Name, challenger.ID, ev.Gate.Passed, in.Rationale), maxJournalChars),
			})
			gatePassed := notifier.Bool(ev.Gate.Passed)
			if early {
				gatePassed = notifier.T("proposal.promotion.early", gatePassed)
			}
			notified := u.notifyProposal(ctx, agent, p, champion, "warning",
				notifier.T("proposal.promotion.title", p.ID, champion.Name),
				notifier.T("proposal.promotion.message", agent.Name, challenger.ID, champion.Name, gatePassed))

			return compactJSON(map[string]any{
				"proposal_id": p.ID, "status": p.Status, "early": early, "gate_passed": ev.Gate.Passed,
				"superseded": superseded, "notified": notified, "url": u.ProposalURL(p.ID),
			})
		},
	}
}

// notifyProposal sends a link-only notification via the agent's webhook
// targets. Reports whether anything was sent.
func (u AgentUseCase) notifyProposal(ctx context.Context, agent entities.Agent, p entities.StrategyChangeProposal, target entities.Strategy, severity string, title, message *notifier.Text) bool {
	if u.Notifier == nil || u.Platform == nil || len(agent.WebhookTargetIDs) == 0 {
		return false
	}
	targets, err := u.Platform.ListWebhookTargetsByIDs(ctx, agent.WebhookTargetIDs)
	if err != nil {
		log.Printf("agent: could not load webhook targets for agent %d: %v", agent.ID, err)
		return false
	}
	ids := []uint{target.ID}
	if p.ChallengerStrategyID != nil {
		ids = append(ids, *p.ChallengerStrategyID)
	}
	for _, e := range u.Notifier.SendToTargets(ctx, targets, notifier.AgentMessage{
		AgentName: agent.Name, Severity: severity,
		Link: u.ProposalURL(p.ID), StrategyIDs: ids, Timestamp: u.now().UTC(),
	}.WithText(title, message)) {
		log.Printf("agent: proposal %d notification: %v", p.ID, e)
	}
	return true
}

// --- create_strategy ---------------------------------------------------------

var createStrategySchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"name": {"type": "string"},
		"description": {"type": "string"},
		"script_source": {"type": "string", "description": "Lua source implementing the Strategy hook contract"},
		"symbols": {"type": "array", "items": {"type": "string"}},
		"cycle_minutes": {"type": "integer", "description": "one of 1, 5, 15, 30, 60"},
		"mode": {"type": "string", "enum": ["backtest", "dryrun"], "description": "anything but backtest is saved as dryrun"}
	},
	"required": ["name", "description", "script_source", "symbols", "cycle_minutes"]
}`)

func (u AgentUseCase) createStrategyTool() Tool {
	return Tool{
		Def: modelprovider.ToolDefinition{
			Name: "create_strategy",
			Description: "Create a new script strategy (always status testing, mode backtest or dryrun) and bind it to yourself. " +
				"At most 3 per day (UTC).",
			InputSchema: createStrategySchema,
		},
		Execute: func(ctx context.Context, raw json.RawMessage) (string, error) {
			if err := u.requirePlatform("create_strategy"); err != nil {
				return "", err
			}
			var in saveStrategyArgs
			if err := json.Unmarshal(raw, &in); err != nil {
				return "", fmt.Errorf("create_strategy: invalid args: %w", err)
			}
			agent, runID, err := u.toolAgent(ctx)
			if err != nil {
				return "", err
			}
			if agent.ID == 0 {
				return "", fmt.Errorf("create_strategy: agent %q is not a saved persona", agent.Name)
			}
			if strings.TrimSpace(in.ScriptSource) == "" {
				return "", fmt.Errorf("create_strategy: script_source is required")
			}
			if err := u.validator().Validate(in.ScriptSource); err != nil {
				return "", fmt.Errorf("create_strategy: script_source does not compile: %v", err)
			}
			ok, err := u.Platform.TryIncStrategiesCreated(ctx, agent.ID, u.now(), MaxStrategiesCreatedPerDay)
			if err != nil {
				return "", fmt.Errorf("create_strategy: could not check the daily limit: %w", err)
			}
			if !ok {
				return "", fmt.Errorf("create_strategy: agent %q already created %d strategies today (UTC) - the limit is %d per day", agent.Name, MaxStrategiesCreatedPerDay, MaxStrategiesCreatedPerDay)
			}
			agentID := agent.ID
			saved, err := u.Strategy.Save(ctx, entities.Strategy{
				Name:             in.Name,
				Description:      in.Description,
				StrategyName:     "script",
				ScriptSource:     in.ScriptSource,
				Status:           entities.Testing,
				Mode:             clampAgentMode(in.Mode),
				MonitoredSymbols: in.Symbols,
				StrategyConfiguration: entities.StrategyConfiguration{
					Cycle: entities.Cycle(in.CycleMinutes),
				},
				CreatedByAgentID: &agentID,
			})
			if err != nil {
				return "", err
			}
			if err := u.Platform.AddBinding(ctx, agent.ID, saved.ID); err != nil {
				return "", fmt.Errorf("create_strategy: strategy %d created but binding it to agent %q failed: %w", saved.ID, agent.Name, err)
			}
			u.appendMemory(ctx, entities.StrategyMemoryEntry{
				StrategyID: saved.ID, AuthorAgentID: &agentID, AgentRunID: runID, Kind: entities.MemoryFinding,
				Content: fmt.Sprintf("Strategy created by agent %q (status testing, mode %s).", agent.Name, saved.Mode),
			})
			return summarizeStrategyForModel(saved), nil
		},
	}
}

// --- list_proposals / get_deploy_gate_config (always granted) ----------------

var listProposalsSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"strategy_id": {"type": "integer", "description": "optional - proposals targeting this strategy or promoting this challenger"},
		"status": {"type": "string", "description": "optional - pending, approved, rejected, applied, superseded or failed (comma-separated for several)"}
	}
}`)

// ParseProposalStatuses parses a comma-separated status list.
func ParseProposalStatuses(raw string) ([]entities.ProposalStatus, error) {
	var out []entities.ProposalStatus
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if !entities.IsValidProposalStatus(part) {
			return nil, fmt.Errorf("unknown proposal status %q", part)
		}
		out = append(out, entities.ProposalStatus(part))
	}
	return out, nil
}

// GatePassedFromEvidence returns evidence.gate.passed, or nil when there
// is no gate evidence.
func GatePassedFromEvidence(raw []byte) *bool {
	if len(raw) == 0 {
		return nil
	}
	var ev struct {
		Gate *struct {
			Passed bool `json:"passed"`
		} `json:"gate"`
	}
	if err := json.Unmarshal(raw, &ev); err != nil || ev.Gate == nil {
		return nil
	}
	v := ev.Gate.Passed
	return &v
}

func (u AgentUseCase) listProposalsTool() Tool {
	return Tool{
		Def: modelprovider.ToolDefinition{
			Name:        "list_proposals",
			Description: "List recent strategy change proposals (newest first), optionally for one strategy and/or status.",
			InputSchema: listProposalsSchema,
		},
		Execute: func(ctx context.Context, raw json.RawMessage) (string, error) {
			if u.Proposals == nil {
				return "", fmt.Errorf("list_proposals: proposals are not available on this server")
			}
			var in struct {
				StrategyID *uint  `json:"strategy_id"`
				Status     string `json:"status"`
			}
			if len(raw) > 0 {
				if err := json.Unmarshal(raw, &in); err != nil {
					return "", fmt.Errorf("list_proposals: invalid args: %w", err)
				}
			}
			statuses, err := ParseProposalStatuses(in.Status)
			if err != nil {
				return "", fmt.Errorf("list_proposals: %w", err)
			}
			f := proposalrepo.Filter{Statuses: statuses, Limit: listProposalsLimit}
			if in.StrategyID != nil && *in.StrategyID != 0 {
				f.StrategyID = in.StrategyID
			}
			ps, err := u.Proposals.List(ctx, f)
			if err != nil {
				return "", err
			}
			type out struct {
				ID                   uint   `json:"id"`
				Kind                 string `json:"kind"`
				TargetStrategyID     uint   `json:"target_strategy_id"`
				ChallengerStrategyID *uint  `json:"challenger_strategy_id,omitempty"`
				AgentID              uint   `json:"agent_id"`
				Status               string `json:"status"`
				Early                bool   `json:"early"`
				GatePassed           *bool  `json:"gate_passed"`
				Rationale            string `json:"rationale"`
				FailureReason        string `json:"failure_reason,omitempty"`
				CreatedAt            string `json:"created_at"`
			}
			res := make([]out, 0, len(ps))
			for _, p := range ps {
				res = append(res, out{
					ID: p.ID, Kind: string(p.Kind), TargetStrategyID: p.TargetStrategyID, ChallengerStrategyID: p.ChallengerStrategyID,
					AgentID: p.AgentID, Status: string(p.Status), Early: p.IsEarly(), GatePassed: GatePassedFromEvidence(p.EvidenceJSON),
					Rationale: truncate(p.Rationale, 300), FailureReason: p.FailureReason, CreatedAt: p.CreatedAt.UTC().Format(time.RFC3339),
				})
			}
			return compactJSON(res)
		},
	}
}

func (u AgentUseCase) getDeployGateConfigTool() Tool {
	return Tool{
		Def: modelprovider.ToolDefinition{
			Name:        "get_deploy_gate_config",
			Description: "Read the deploy gate thresholds deploy_to_testing and propose_promotion are judged by (operator-set, read-only), and how many auto-deploys you have left today.",
			InputSchema: emptySchema,
		},
		Execute: func(ctx context.Context, _ json.RawMessage) (string, error) {
			if u.Gate == nil {
				return "", fmt.Errorf("get_deploy_gate_config: the deploy gate is not available on this server")
			}
			cfg, err := u.Gate.Config(ctx)
			if err != nil {
				return "", err
			}
			res := map[string]any{"thresholds": toGateConfigJSON(cfg)}
			if remaining, max, ok := u.remainingAutoDeploys(ctx); ok {
				res["auto_deploys_remaining_today"] = remaining
				res["max_auto_deploys_per_day"] = max
			}
			return compactJSON(res)
		},
	}
}

// remainingAutoDeploys returns today's remaining auto-deploys for the
// calling agent.
func (u AgentUseCase) remainingAutoDeploys(ctx context.Context) (remaining, max int, ok bool) {
	if u.Platform == nil {
		return 0, 0, false
	}
	agent, _, err := u.toolAgent(ctx)
	if err != nil || agent.ID == 0 {
		return 0, 0, false
	}
	return u.remainingAutoDeploysFor(ctx, agent.ID)
}

func (u AgentUseCase) remainingAutoDeploysFor(ctx context.Context, agentID uint) (remaining, max int, ok bool) {
	fresh, err := u.Platform.GetAgent(ctx, agentID)
	if err != nil {
		return 0, 0, false
	}
	usage, err := u.Platform.GetUsage(ctx, agentID, u.now())
	if err != nil {
		return 0, 0, false
	}
	remaining = fresh.MaxAutoDeploysPerDay - usage.AutoDeploys
	if remaining < 0 {
		remaining = 0
	}
	return remaining, fresh.MaxAutoDeploysPerDay, true
}

// ProposalBackend is what cmd/* wires as both the proposal store and the
// gate-threshold reader (app/repository/proposal.GormRepository).
type ProposalBackend interface {
	ProposalStore
	GateConfigReader
}

// WirePhaseB sets the Phase B-01 dependencies (gate, proposals, forward-
// test reader, Lua validator) - one call shared by cmd/api, cmd/mcp and
// cmd/agent so the three transports stay identical.
func (u *AgentUseCase) WirePhaseB(wf WalkForwardRunner, candles CandleCounter, proposals ProposalBackend, forward ClosedSignalReader, validator ScriptValidator) {
	u.Gate = NewGateRunner(wf, candles, proposals)
	if cov, ok := candles.(CandleCoverageReader); ok {
		u.Coverage = cov
	}
	u.Proposals = proposals
	u.ForwardTest = forward
	u.Validator = validator
}
