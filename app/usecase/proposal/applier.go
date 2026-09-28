package proposal

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"go-trade-bot/app/entities"
	proposalrepo "go-trade-bot/app/repository/proposal"
	strategyrepo "go-trade-bot/app/repository/strategy"
	"go-trade-bot/internal/notifier"
)

// ApplyRepository is the slice of app/repository/proposal the applier needs.
type ApplyRepository interface {
	Get(ctx context.Context, id uint) (entities.StrategyChangeProposal, error)
	TransitionStatus(ctx context.Context, id uint, from []entities.ProposalStatus, to entities.ProposalStatus, t proposalrepo.Transition) (bool, error)
	TouchFlatCheck(ctx context.Context, id uint, at time.Time) error
}

// StrategyApplier is the strategy side of an apply: reads plus the ONE
// write path, StrategyRepository.ReplaceScriptSource.
type StrategyApplier interface {
	GetByID(ctx context.Context, id uint) (entities.Strategy, error)
	CountOpenSignals(ctx context.Context, strategy entities.Strategy) (int64, error)
	ReplaceScriptSource(ctx context.Context, id uint, expectedBase, newSource string, requireFlat bool) (strategyrepo.ReplaceResult, error)
}

// StatusUpdater disables the challenger after a promotion
// (app/usecase/strategy.StrategyUseCase.UpdateStatus).
type StatusUpdater interface {
	UpdateStatus(ctx context.Context, id uint, status entities.StrategyStatus) (entities.Strategy, error)
}

// CycleLock is the strategy-cycle:<id> Redis lock the trading worker holds
// around every strategy cycle (internal/lock with lock.CycleKeyPrefix).
type CycleLock interface {
	Acquire(ctx context.Context, strategyID uint, holder string, ttl time.Duration) (bool, error)
	Release(ctx context.Context, strategyID uint, holder string) error
}

// PlatformReader is the slice of app/repository/agentplatform used for
// memory entries and notifications.
type PlatformReader interface {
	GetAgent(ctx context.Context, id uint) (entities.Agent, error)
	ListWebhookTargetsByIDs(ctx context.Context, ids []uint) ([]entities.WebhookTarget, error)
	AppendMemory(ctx context.Context, entry entities.StrategyMemoryEntry) (entities.StrategyMemoryEntry, error)
}

// Outcome is what one Apply attempt did.
type Outcome string

const (
	OutcomeApplied     Outcome = "applied"
	OutcomeRescheduled Outcome = "rescheduled" // not flat (or cycle lock busy): try again after RetryIn
	OutcomeSuperseded  Outcome = "superseded"
	OutcomeFailed      Outcome = "failed"
	OutcomeSkipped     Outcome = "skipped" // not approved (any more)
)

// ApplyResult reports an Apply attempt.
type ApplyResult struct {
	Outcome Outcome
	RetryIn time.Duration
	Reason  string
}

const (
	// FlatRecheckInterval is how long agent:apply_proposal waits before
	// re-checking a target that has an open position.
	FlatRecheckInterval = time.Minute
	// CycleLockBusyRetry is the wait when the worker is mid-cycle.
	CycleLockBusyRetry = 10 * time.Second
	// NeverFlatTimeout: an approved proposal whose target never goes flat
	// within this long (from approval) fails with "never flat".
	NeverFlatTimeout = 7 * 24 * time.Hour
	// applyLockTTL bounds how long a crashed apply keeps cycles blocked.
	applyLockTTL = 30 * time.Second
)

// Applier applies approved proposals (cmd/agent only).
type Applier struct {
	Repo       ApplyRepository
	Strategies StrategyApplier
	Status     StatusUpdater
	Lock       CycleLock
	Platform   PlatformReader
	Notifier   notifier.AgentNotifier // may be nil
	APIBaseURL string
	Now        func() time.Time
}

func (a *Applier) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

func (a *Applier) url(id uint) string {
	return strings.TrimRight(a.APIBaseURL, "/") + fmt.Sprintf("/agents/proposals/%d", id)
}

// requiresFlat: promotions and any live/productive target wait for flat.
// A gate_failed_change on a non-live, non-productive target applies
// immediately (B-01 §5).
func requiresFlat(p entities.StrategyChangeProposal, target entities.Strategy) bool {
	return p.Kind == entities.ProposalPromoteChallenger || target.IsLiveOrProductive()
}

// Apply runs one attempt of agent:apply_proposal (B-01 §5):
//  1. not approved -> skipped;
//  2. target source != BaseSource -> superseded (+ notify);
//  3. target not flat (when required) -> rescheduled in 1 minute, or failed
//     "never flat" 7 days after approval (+ notify);
//  4. flat -> under the strategy-cycle lock, ReplaceScriptSource (source
//     only; transactional flat + base re-check; ScriptVersion; ScriptState
//     cleared) -> applied, memory entry, notify, challenger disabled.
//
// A returned error means "retry this task" (infrastructure failure); no
// write happened in that case unless ReplaceScriptSource committed, which
// then returns nil.
func (a *Applier) Apply(ctx context.Context, id uint) (ApplyResult, error) {
	p, err := a.Repo.Get(ctx, id)
	if err != nil {
		return ApplyResult{}, err
	}
	if p.Status != entities.ProposalApproved {
		return ApplyResult{Outcome: OutcomeSkipped, Reason: "proposal is " + string(p.Status)}, nil
	}
	target, err := a.Strategies.GetByID(ctx, p.TargetStrategyID)
	if err != nil {
		return a.fail(ctx, p, fmt.Sprintf("target strategy %d could not be loaded: %v", p.TargetStrategyID, err))
	}
	if target.ScriptSource != p.BaseSource {
		return a.supersede(ctx, p, target)
	}

	requireFlat := requiresFlat(p, target)
	now := a.now()
	if requireFlat {
		open, err := a.Strategies.CountOpenSignals(ctx, target)
		if err != nil {
			return ApplyResult{}, err
		}
		if err := a.Repo.TouchFlatCheck(ctx, p.ID, now); err != nil {
			log.Printf("apply_proposal %d: could not record the flat check: %v", p.ID, err)
		}
		if open > 0 {
			return a.notFlat(ctx, p, target, now)
		}
	}

	holder := fmt.Sprintf("apply-proposal:%d:%s", p.ID, randomToken())
	got, err := a.Lock.Acquire(ctx, target.ID, holder, applyLockTTL)
	if err != nil {
		return ApplyResult{}, fmt.Errorf("apply_proposal %d: could not take the strategy-cycle lock: %w", p.ID, err)
	}
	if !got {
		return ApplyResult{Outcome: OutcomeRescheduled, RetryIn: CycleLockBusyRetry, Reason: "the trading worker is mid-cycle"}, nil
	}
	res, replaceErr := a.Strategies.ReplaceScriptSource(ctx, target.ID, p.BaseSource, p.ProposedSource, requireFlat)
	rctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := a.Lock.Release(rctx, target.ID, holder); err != nil {
		log.Printf("apply_proposal %d: could not release the strategy-cycle lock (expires in %s): %v", p.ID, applyLockTTL, err)
	}
	cancel()

	switch {
	case errors.Is(replaceErr, strategyrepo.ErrOpenPosition):
		return a.notFlat(ctx, p, target, now)
	case errors.Is(replaceErr, strategyrepo.ErrSourceChanged):
		return a.supersede(ctx, p, target)
	case replaceErr != nil:
		return ApplyResult{}, fmt.Errorf("apply_proposal %d: %w", p.ID, replaceErr)
	}

	appliedAt := a.now()
	if ok, err := a.Repo.TransitionStatus(ctx, p.ID, []entities.ProposalStatus{entities.ProposalApproved}, entities.ProposalApplied,
		proposalrepo.Transition{AppliedAt: &appliedAt, LastFlatCheckAt: &now}); err != nil || !ok {
		// The code IS applied; only the bookkeeping failed. Never retry the
		// write (a retry would see source != base and supersede it).
		log.Printf("apply_proposal %d: applied, but recording status failed (ok=%v): %v", p.ID, ok, err)
	}

	label := "Proposal"
	if p.Kind == entities.ProposalPromoteChallenger {
		label = "Promotion"
	}
	content := fmt.Sprintf("%s #%d applied: this strategy's code was replaced after operator approval (mode %s and status %s unchanged).", label, p.ID, target.Mode, target.Status)
	if res.StateCleared {
		content += " Script state was cleared (the strategy was flat)."
	}
	a.memory(ctx, target.ID, content)

	if p.Kind == entities.ProposalPromoteChallenger && p.ChallengerStrategyID != nil && a.Status != nil {
		if _, err := a.Status.UpdateStatus(ctx, *p.ChallengerStrategyID, entities.Disabled); err != nil {
			log.Printf("apply_proposal %d: could not disable challenger %d: %v", p.ID, *p.ChallengerStrategyID, err)
		} else {
			a.memory(ctx, *p.ChallengerStrategyID, fmt.Sprintf("Disabled: promoted into strategy #%d by proposal #%d.", target.ID, p.ID))
		}
	}
	a.notify(ctx, p, target, "info", fmt.Sprintf("%s #%d applied: %s", label, p.ID, target.Name),
		fmt.Sprintf("The approved change to %q is now live in its code (mode %s, status %s unchanged).", target.Name, target.Mode, target.Status))
	return ApplyResult{Outcome: OutcomeApplied}, nil
}

func (a *Applier) notFlat(ctx context.Context, p entities.StrategyChangeProposal, target entities.Strategy, now time.Time) (ApplyResult, error) {
	since := p.CreatedAt
	if p.DecidedAt != nil {
		since = *p.DecidedAt
	}
	if now.Sub(since) >= NeverFlatTimeout {
		return a.fail(ctx, p, fmt.Sprintf("never flat: strategy %q kept an open position for 7 days after approval", target.Name))
	}
	return ApplyResult{Outcome: OutcomeRescheduled, RetryIn: FlatRecheckInterval, Reason: "the target has an open position"}, nil
}

func (a *Applier) supersede(ctx context.Context, p entities.StrategyChangeProposal, target entities.Strategy) (ApplyResult, error) {
	reason := "the target strategy's code changed after approval"
	if _, err := a.Repo.TransitionStatus(ctx, p.ID, []entities.ProposalStatus{entities.ProposalApproved}, entities.ProposalSuperseded,
		proposalrepo.Transition{FailureReason: &reason}); err != nil {
		return ApplyResult{}, err
	}
	a.notify(ctx, p, target, "warning", fmt.Sprintf("Proposal #%d superseded", p.ID),
		fmt.Sprintf("Proposal #%d for %q was not applied: %s.", p.ID, target.Name, reason))
	return ApplyResult{Outcome: OutcomeSuperseded, Reason: reason}, nil
}

func (a *Applier) fail(ctx context.Context, p entities.StrategyChangeProposal, reason string) (ApplyResult, error) {
	if _, err := a.Repo.TransitionStatus(ctx, p.ID, []entities.ProposalStatus{entities.ProposalApproved}, entities.ProposalFailed,
		proposalrepo.Transition{FailureReason: &reason}); err != nil {
		return ApplyResult{}, err
	}
	a.notify(ctx, p, entities.Strategy{ID: p.TargetStrategyID, Name: fmt.Sprintf("strategy #%d", p.TargetStrategyID)}, "warning",
		fmt.Sprintf("Proposal #%d failed", p.ID), fmt.Sprintf("Proposal #%d was not applied: %s.", p.ID, reason))
	return ApplyResult{Outcome: OutcomeFailed, Reason: reason}, nil
}

func (a *Applier) memory(ctx context.Context, strategyID uint, content string) {
	if a.Platform == nil {
		return
	}
	if _, err := a.Platform.AppendMemory(ctx, entities.StrategyMemoryEntry{StrategyID: strategyID, Kind: entities.MemoryFinding, Content: content}); err != nil {
		log.Printf("apply_proposal: could not write memory for strategy %d: %v", strategyID, err)
	}
}

func (a *Applier) notify(ctx context.Context, p entities.StrategyChangeProposal, target entities.Strategy, severity, title, message string) {
	if a.Notifier == nil || a.Platform == nil {
		return
	}
	agent, err := a.Platform.GetAgent(ctx, p.AgentID)
	if err != nil || len(agent.WebhookTargetIDs) == 0 {
		return
	}
	targets, err := a.Platform.ListWebhookTargetsByIDs(ctx, agent.WebhookTargetIDs)
	if err != nil {
		log.Printf("apply_proposal %d: could not load webhook targets: %v", p.ID, err)
		return
	}
	ids := []uint{target.ID}
	if p.ChallengerStrategyID != nil {
		ids = append(ids, *p.ChallengerStrategyID)
	}
	for _, e := range a.Notifier.SendToTargets(ctx, targets, notifier.AgentMessage{
		AgentName: agent.Name, Severity: severity, Title: title, Message: message,
		Link: a.url(p.ID), StrategyIDs: ids, Timestamp: a.now().UTC(),
	}) {
		log.Printf("apply_proposal %d notification: %v", p.ID, e)
	}
}

func randomToken() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
