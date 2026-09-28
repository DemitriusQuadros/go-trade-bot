package entities

import (
	"strings"
	"time"

	"gorm.io/datatypes"
)

// ProposalStatus is a StrategyChangeProposal's lifecycle state
// (agents-platform Phase B-01 §1).
//
//	pending -> approved -> applied
//	pending -> rejected | superseded
//	approved -> superseded | failed
type ProposalStatus string

const (
	ProposalPending    ProposalStatus = "pending"
	ProposalApproved   ProposalStatus = "approved"
	ProposalRejected   ProposalStatus = "rejected"
	ProposalApplied    ProposalStatus = "applied"
	ProposalSuperseded ProposalStatus = "superseded"
	ProposalFailed     ProposalStatus = "failed"
)

// IsValidProposalStatus reports whether s is a known status.
func IsValidProposalStatus(s string) bool {
	switch ProposalStatus(s) {
	case ProposalPending, ProposalApproved, ProposalRejected, ProposalApplied, ProposalSuperseded, ProposalFailed:
		return true
	}
	return false
}

// ProposalKind distinguishes why a proposal exists.
type ProposalKind string

const (
	// ProposalPromoteChallenger: replace a live/productive champion's code
	// with its challenger's (propose_promotion).
	ProposalPromoteChallenger ProposalKind = "promote_challenger"
	// ProposalGateFailedChange: a deploy_to_testing change on a non-live
	// strategy that failed the deploy gate, kept so the operator can still
	// accept it.
	ProposalGateFailedChange ProposalKind = "gate_failed_change"
)

// EarlyRationalePrefix marks a promotion filed before the challenger has
// the minimum forward-test age (shown as a warning in the UI).
const EarlyRationalePrefix = "EARLY:"

// StrategyChangeProposal is a code change for a strategy that needs the
// operator's approval in the web UI. It is applied only by the
// agent:apply_proposal task (cmd/agent) after an authenticated REST
// approve - never by an LLM tool.
type StrategyChangeProposal struct {
	ID                   uint `gorm:"primaryKey"`
	Kind                 ProposalKind
	TargetStrategyID     uint  `gorm:"index"` // the strategy whose code would change
	ChallengerStrategyID *uint // promote_challenger only
	AgentID              uint  `gorm:"index"`
	AgentRunID           *uint
	BaseSource           string         `gorm:"type:text"` // target's ScriptSource when the proposal was created
	ProposedSource       string         `gorm:"type:text"`
	Rationale            string         `gorm:"type:text"` // agent-written
	EvidenceJSON         datatypes.JSON `gorm:"type:jsonb"`
	ReportID             *uint
	Status               ProposalStatus `gorm:"index"`
	DecisionNote         string
	DecidedAt            *time.Time
	AppliedAt            *time.Time
	// LastFlatCheckAt is stamped by agent:apply_proposal every time it
	// checks whether the target is flat (no open position).
	LastFlatCheckAt *time.Time
	FailureReason   string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// IsEarly reports whether the rationale carries the EARLY: marker.
func (p StrategyChangeProposal) IsEarly() bool {
	return strings.HasPrefix(strings.TrimSpace(p.Rationale), EarlyRationalePrefix)
}

// DeployGateConfig is the singleton (ID=1) holding the deploy gate
// thresholds (agents-platform Phase B-01 §1). The model can read these
// (get_deploy_gate_config) but never supply or override them.
type DeployGateConfig struct {
	ID               uint `gorm:"primaryKey"`
	MinSharpeDelta   float64
	MaxDrawdownRatio float64
	MinTrades        int
	MinProfitFactor  float64
	LookbackMonths   int
	TrainMonths      int
	TestMonths       int
	Timeframe        string // "" = the strategy's own cycle-derived timeframe
	UpdatedAt        time.Time
}

// DeployGateConfigID is the singleton row id.
const DeployGateConfigID = 1

// DefaultDeployGateConfig returns the seeded thresholds.
func DefaultDeployGateConfig() DeployGateConfig {
	return DeployGateConfig{
		ID:               DeployGateConfigID,
		MinSharpeDelta:   0,
		MaxDrawdownRatio: 1.10,
		MinTrades:        20,
		MinProfitFactor:  1.0,
		LookbackMonths:   6,
		TrainMonths:      3,
		TestMonths:       1,
		Timeframe:        "",
	}
}
