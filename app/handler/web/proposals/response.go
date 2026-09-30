package proposals

import (
	"encoding/json"
	"errors"
	"time"

	"go-trade-bot/app/entities"
	agentusecase "go-trade-bot/app/usecase/agent"
	usecase "go-trade-bot/app/usecase/proposal"
)

// ListItem is one GET /proposals entry (B-01 §5).
type ListItem struct {
	ID                   uint       `json:"id"`
	Kind                 string     `json:"kind"`
	TargetStrategyID     uint       `json:"target_strategy_id"`
	TargetStrategyName   string     `json:"target_strategy_name"`
	ChallengerStrategyID *uint      `json:"challenger_strategy_id"`
	AgentID              uint       `json:"agent_id"`
	AgentName            string     `json:"agent_name"`
	Rationale            string     `json:"rationale"`
	Status               string     `json:"status"`
	Early                bool       `json:"early"`
	GatePassed           *bool      `json:"gate_passed"`
	CreatedAt            time.Time  `json:"created_at"`
	DecidedAt            *time.Time `json:"decided_at"`
	// DecidedBy is the deciding user's display name (auth-01 §7), null
	// when unknown (legacy rows, service token).
	DecidedBy     *string    `json:"decided_by"`
	AppliedAt     *time.Time `json:"applied_at"`
	FailureReason string     `json:"failure_reason"`
}

// Detail is GET /proposals/{id} (and the approve/reject response).
type Detail struct {
	ListItem
	BaseSource                     string          `json:"base_source"`
	ProposedSource                 string          `json:"proposed_source"`
	Evidence                       json.RawMessage `json:"evidence"`
	ReportID                       *uint           `json:"report_id"`
	DecisionNote                   string          `json:"decision_note"`
	TargetHasOpenPosition          bool            `json:"target_has_open_position"`
	TargetCurrentSourceMatchesBase bool            `json:"target_current_source_matches_base"`
	LastFlatCheckAt                *time.Time      `json:"last_flat_check_at"`
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// ToListItem maps a usecase view.
func ToListItem(v usecase.View) ListItem {
	p := v.Proposal
	return ListItem{
		ID: p.ID, Kind: string(p.Kind), TargetStrategyID: p.TargetStrategyID, TargetStrategyName: v.TargetStrategyName,
		ChallengerStrategyID: p.ChallengerStrategyID, AgentID: p.AgentID, AgentName: v.AgentName, Rationale: p.Rationale,
		Status: string(p.Status), Early: p.IsEarly(), GatePassed: agentusecase.GatePassedFromEvidence(p.EvidenceJSON),
		CreatedAt: p.CreatedAt.UTC(), DecidedAt: utcPtr(p.DecidedAt), DecidedBy: nonEmpty(v.DecidedByName), AppliedAt: utcPtr(p.AppliedAt), FailureReason: p.FailureReason,
	}
}

// ToDetail maps a usecase view with its detail fields.
func ToDetail(v usecase.View) Detail {
	p := v.Proposal
	evidence := json.RawMessage("null")
	if len(p.EvidenceJSON) > 0 && json.Valid(p.EvidenceJSON) {
		evidence = json.RawMessage(p.EvidenceJSON)
	}
	return Detail{
		ListItem: ToListItem(v), BaseSource: p.BaseSource, ProposedSource: p.ProposedSource, Evidence: evidence,
		ReportID: p.ReportID, DecisionNote: p.DecisionNote, TargetHasOpenPosition: v.TargetHasOpenPosition,
		TargetCurrentSourceMatchesBase: v.TargetCurrentSourceMatchesBase, LastFlatCheckAt: utcPtr(p.LastFlatCheckAt),
	}
}

// GateConfigResponse is GET/PUT /deploy-gate.
type GateConfigResponse struct {
	MinSharpeDelta   float64   `json:"min_sharpe_delta"`
	MaxDrawdownRatio float64   `json:"max_drawdown_ratio"`
	MinTrades        int       `json:"min_trades"`
	MinProfitFactor  float64   `json:"min_profit_factor"`
	LookbackMonths   int       `json:"lookback_months"`
	TrainMonths      int       `json:"train_months"`
	TestMonths       int       `json:"test_months"`
	Timeframe        string    `json:"timeframe"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// ToGateConfig maps the entity.
func ToGateConfig(c entities.DeployGateConfig) GateConfigResponse {
	return GateConfigResponse{
		MinSharpeDelta: c.MinSharpeDelta, MaxDrawdownRatio: c.MaxDrawdownRatio, MinTrades: c.MinTrades,
		MinProfitFactor: c.MinProfitFactor, LookbackMonths: c.LookbackMonths, TrainMonths: c.TrainMonths,
		TestMonths: c.TestMonths, Timeframe: c.Timeframe, UpdatedAt: c.UpdatedAt.UTC(),
	}
}

// GateConfigRequest is the PUT /deploy-gate body. Every numeric field is
// required; timeframe is optional ("" = the strategy's cycle interval).
type GateConfigRequest struct {
	MinSharpeDelta   *float64 `json:"min_sharpe_delta"`
	MaxDrawdownRatio *float64 `json:"max_drawdown_ratio"`
	MinTrades        *int     `json:"min_trades"`
	MinProfitFactor  *float64 `json:"min_profit_factor"`
	LookbackMonths   *int     `json:"lookback_months"`
	TrainMonths      *int     `json:"train_months"`
	TestMonths       *int     `json:"test_months"`
	Timeframe        string   `json:"timeframe"`
}

// ToEntity checks presence; value validation is the usecase's.
func (r GateConfigRequest) ToEntity() (entities.DeployGateConfig, error) {
	if r.MinSharpeDelta == nil || r.MaxDrawdownRatio == nil || r.MinTrades == nil || r.MinProfitFactor == nil ||
		r.LookbackMonths == nil || r.TrainMonths == nil || r.TestMonths == nil {
		return entities.DeployGateConfig{}, errors.New("min_sharpe_delta, max_drawdown_ratio, min_trades, min_profit_factor, lookback_months, train_months and test_months are required")
	}
	return entities.DeployGateConfig{
		MinSharpeDelta: *r.MinSharpeDelta, MaxDrawdownRatio: *r.MaxDrawdownRatio, MinTrades: *r.MinTrades,
		MinProfitFactor: *r.MinProfitFactor, LookbackMonths: *r.LookbackMonths, TrainMonths: *r.TrainMonths,
		TestMonths: *r.TestMonths, Timeframe: r.Timeframe,
	}, nil
}
