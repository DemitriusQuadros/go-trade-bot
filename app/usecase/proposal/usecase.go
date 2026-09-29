// Package proposal is agents-platform Phase B-01 §5: the operator side of
// strategy change proposals (list, detail, approve, reject, deploy gate
// thresholds - cmd/api REST) and the Applier behind cmd/agent's
// agent:apply_proposal task.
//
// SAFETY: this package is NOT an LLM tool and app/usecase/agent must never
// depend on it (asserted by app/usecase/agent/phaseb_isolation_test.go).
// The only path that changes a live strategy's code is: operator approves
// over authenticated REST (UseCase.Approve) -> agent:apply_proposal ->
// Applier.Apply -> StrategyRepository.ReplaceScriptSource, which writes
// script_source only (never Mode or Status).
package proposal

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"go-trade-bot/app/entities"
	proposalrepo "go-trade-bot/app/repository/proposal"
	"go-trade-bot/internal/customerror"
)

// Repository is the slice of app/repository/proposal the operator usecase needs.
type Repository interface {
	Get(ctx context.Context, id uint) (entities.StrategyChangeProposal, error)
	List(ctx context.Context, f proposalrepo.Filter) ([]entities.StrategyChangeProposal, error)
	CountByStatus(ctx context.Context, status entities.ProposalStatus) (int64, error)
	TransitionStatus(ctx context.Context, id uint, from []entities.ProposalStatus, to entities.ProposalStatus, t proposalrepo.Transition) (bool, error)
	GetGateConfig(ctx context.Context) (entities.DeployGateConfig, error)
	SaveGateConfig(ctx context.Context, c entities.DeployGateConfig) (entities.DeployGateConfig, error)
}

// StrategyReader reads strategies and their open positions.
type StrategyReader interface {
	GetByID(ctx context.Context, id uint) (entities.Strategy, error)
	CountOpenSignals(ctx context.Context, strategy entities.Strategy) (int64, error)
}

// AgentNamer resolves agent names.
type AgentNamer interface {
	ListAgents(ctx context.Context) ([]entities.Agent, error)
}

// ApplyEnqueuer enqueues agent:apply_proposal (app/workers/agent).
type ApplyEnqueuer interface {
	EnqueueApplyProposal(ctx context.Context, proposalID uint, delay time.Duration) error
}

// ErrSuperseded marks the 409 returned when approving a proposal whose
// target changed since it was created.
var ErrSuperseded = errors.New("superseded")

// SupersededError is returned by Approve (HTTP 409, error code
// "superseded").
type SupersededError struct{ Message string }

func (e *SupersededError) Error() string { return e.Message }

// Unwrap lets errors.Is(err, ErrSuperseded) match.
func (e *SupersededError) Unwrap() error { return ErrSuperseded }

// View is a proposal plus the derived fields the API returns.
type View struct {
	Proposal           entities.StrategyChangeProposal
	TargetStrategyName string
	AgentName          string
	// Detail-only (live values at read time).
	TargetHasOpenPosition          bool
	TargetCurrentSourceMatchesBase bool
}

// ListFilter is the GET /proposals query.
type ListFilter struct {
	Statuses   []entities.ProposalStatus
	StrategyID *uint
	AgentID    *uint
	Limit      int
	BeforeID   *uint
}

// UseCase is the operator-facing proposals API (cmd/api).
type UseCase struct {
	repo       Repository
	strategies StrategyReader
	agents     AgentNamer
	enqueuer   ApplyEnqueuer
	now        func() time.Time
}

// NewUseCase builds a UseCase.
func NewUseCase(r Repository, s StrategyReader, a AgentNamer, e ApplyEnqueuer) *UseCase {
	return &UseCase{repo: r, strategies: s, agents: a, enqueuer: e, now: time.Now}
}

func badRequest(format string, args ...any) error {
	return &customerror.CustomError{Code: http.StatusBadRequest, Message: fmt.Sprintf(format, args...)}
}

func conflict(format string, args ...any) error {
	return &customerror.CustomError{Code: http.StatusConflict, Message: fmt.Sprintf(format, args...)}
}

func (u *UseCase) agentNames(ctx context.Context) map[uint]string {
	names := map[uint]string{}
	if u.agents == nil {
		return names
	}
	if all, err := u.agents.ListAgents(ctx); err == nil {
		for _, a := range all {
			names[a.ID] = a.Name
		}
	}
	return names
}

func (u *UseCase) view(ctx context.Context, p entities.StrategyChangeProposal, names map[uint]string, strategyNames map[uint]string) View {
	v := View{Proposal: p, AgentName: names[p.AgentID]}
	if n, ok := strategyNames[p.TargetStrategyID]; ok {
		v.TargetStrategyName = n
	} else if s, err := u.strategies.GetByID(ctx, p.TargetStrategyID); err == nil {
		v.TargetStrategyName = s.Name
		strategyNames[p.TargetStrategyID] = s.Name
	}
	return v
}

// List returns proposals newest first.
func (u *UseCase) List(ctx context.Context, f ListFilter) ([]View, error) {
	ps, err := u.repo.List(ctx, proposalrepo.Filter{Statuses: f.Statuses, StrategyID: f.StrategyID, AgentID: f.AgentID, Limit: f.Limit, BeforeID: f.BeforeID})
	if err != nil {
		return nil, err
	}
	names := u.agentNames(ctx)
	strategyNames := map[uint]string{}
	out := make([]View, 0, len(ps))
	for _, p := range ps {
		out = append(out, u.view(ctx, p, names, strategyNames))
	}
	return out, nil
}

// PendingCount is the number of pending proposals.
func (u *UseCase) PendingCount(ctx context.Context) (int64, error) {
	return u.repo.CountByStatus(ctx, entities.ProposalPending)
}

// Get returns one proposal with its live detail fields.
func (u *UseCase) Get(ctx context.Context, id uint) (View, error) {
	p, err := u.repo.Get(ctx, id)
	if err != nil {
		return View{}, err
	}
	return u.detail(ctx, p)
}

func (u *UseCase) detail(ctx context.Context, p entities.StrategyChangeProposal) (View, error) {
	v := u.view(ctx, p, u.agentNames(ctx), map[uint]string{})
	target, err := u.strategies.GetByID(ctx, p.TargetStrategyID)
	if err != nil {
		// The target was deleted: nothing is open and nothing matches.
		return v, nil
	}
	v.TargetStrategyName = target.Name
	v.TargetCurrentSourceMatchesBase = target.ScriptSource == p.BaseSource
	open, err := u.strategies.CountOpenSignals(ctx, target)
	if err != nil {
		return View{}, err
	}
	v.TargetHasOpenPosition = open > 0
	return v, nil
}

// Approve moves a pending proposal to approved and enqueues
// agent:apply_proposal. If the target's code changed since the proposal
// was created it is marked superseded instead (409 superseded).
func (u *UseCase) Approve(ctx context.Context, id uint, note string) (View, error) {
	p, err := u.repo.Get(ctx, id)
	if err != nil {
		return View{}, err
	}
	if p.Status != entities.ProposalPending {
		return View{}, conflict("proposal %d is %s; only pending proposals can be approved", id, p.Status)
	}
	target, err := u.strategies.GetByID(ctx, p.TargetStrategyID)
	if err != nil {
		return View{}, conflict("proposal %d's target strategy %d no longer exists", id, p.TargetStrategyID)
	}
	now := u.now()
	if target.ScriptSource != p.BaseSource {
		reason := "the target strategy's code changed since the proposal was created"
		if _, err := u.repo.TransitionStatus(ctx, id, []entities.ProposalStatus{entities.ProposalPending}, entities.ProposalSuperseded,
			proposalrepo.Transition{DecidedAt: &now, DecisionNote: &note, FailureReason: &reason}); err != nil {
			return View{}, err
		}
		return View{}, &SupersededError{Message: fmt.Sprintf("proposal %d is superseded: %s", id, reason)}
	}
	ok, err := u.repo.TransitionStatus(ctx, id, []entities.ProposalStatus{entities.ProposalPending}, entities.ProposalApproved,
		proposalrepo.Transition{DecidedAt: &now, DecisionNote: &note})
	if err != nil {
		return View{}, err
	}
	if !ok {
		return View{}, conflict("proposal %d was decided concurrently", id)
	}
	if err := u.enqueuer.EnqueueApplyProposal(ctx, id, 0); err != nil {
		// Put it back so the operator can retry - an approved proposal with
		// no apply task would otherwise be stuck.
		empty := ""
		if _, rErr := u.repo.TransitionStatus(ctx, id, []entities.ProposalStatus{entities.ProposalApproved}, entities.ProposalPending,
			proposalrepo.Transition{DecisionNote: &empty}); rErr != nil {
			return View{}, fmt.Errorf("proposal %d approved but enqueueing the apply task failed (%v) and reverting failed: %w", id, err, rErr)
		}
		return View{}, fmt.Errorf("could not enqueue the apply task, proposal %d is still pending: %w", id, err)
	}
	return u.Get(ctx, id)
}

// Reject moves a pending proposal to rejected.
func (u *UseCase) Reject(ctx context.Context, id uint, note string) (View, error) {
	p, err := u.repo.Get(ctx, id)
	if err != nil {
		return View{}, err
	}
	if p.Status != entities.ProposalPending {
		return View{}, conflict("proposal %d is %s; only pending proposals can be rejected", id, p.Status)
	}
	now := u.now()
	ok, err := u.repo.TransitionStatus(ctx, id, []entities.ProposalStatus{entities.ProposalPending}, entities.ProposalRejected,
		proposalrepo.Transition{DecidedAt: &now, DecisionNote: &note})
	if err != nil {
		return View{}, err
	}
	if !ok {
		return View{}, conflict("proposal %d was decided concurrently", id)
	}
	return u.Get(ctx, id)
}

// GateConfig returns the deploy gate thresholds.
func (u *UseCase) GateConfig(ctx context.Context) (entities.DeployGateConfig, error) {
	return u.repo.GetGateConfig(ctx)
}

// ValidTimeframes are the gate timeframes the API accepts ("" = the
// strategy's own cycle interval).
var ValidTimeframes = []string{"", "1m", "5m", "15m", "30m", "1h", "4h", "1d"}

// UpdateGateConfig validates and saves the thresholds.
func (u *UseCase) UpdateGateConfig(ctx context.Context, c entities.DeployGateConfig) (entities.DeployGateConfig, error) {
	if err := ValidateGateConfig(&c); err != nil {
		return entities.DeployGateConfig{}, err
	}
	return u.repo.SaveGateConfig(ctx, c)
}

// ValidateGateConfig enforces: ratios > 0, MinTrades >= 1, months >= 1,
// lookback >= train + test, finite numbers, known timeframe.
func ValidateGateConfig(c *entities.DeployGateConfig) error {
	c.Timeframe = strings.TrimSpace(c.Timeframe)
	for name, v := range map[string]float64{"min_sharpe_delta": c.MinSharpeDelta, "max_drawdown_ratio": c.MaxDrawdownRatio, "min_profit_factor": c.MinProfitFactor} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return badRequest("%s must be a finite number", name)
		}
	}
	if c.MaxDrawdownRatio <= 0 {
		return badRequest("max_drawdown_ratio must be > 0")
	}
	if c.MinProfitFactor <= 0 {
		return badRequest("min_profit_factor must be > 0")
	}
	if c.MinTrades < 1 {
		return badRequest("min_trades must be >= 1")
	}
	if c.LookbackMonths < 1 || c.TrainMonths < 1 || c.TestMonths < 1 {
		return badRequest("lookback_months, train_months and test_months must be >= 1")
	}
	if c.LookbackMonths < c.TrainMonths+c.TestMonths {
		return badRequest("lookback_months (%d) must be >= train_months + test_months (%d)", c.LookbackMonths, c.TrainMonths+c.TestMonths)
	}
	valid := false
	for _, tf := range ValidTimeframes {
		valid = valid || tf == c.Timeframe
	}
	if !valid {
		return badRequest("timeframe must be one of %q", ValidTimeframes)
	}
	return nil
}
