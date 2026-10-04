// Package deploygate is the hard metric gate an agent's code change must
// pass before it is auto-deployed to a non-live strategy (agents-platform
// Phase B-01 §3). Pure Go, no I/O: the caller (app/usecase/agent's
// GateRunner) supplies metrics read from PERSISTED walk-forward backtest
// runs and thresholds read from entities.DeployGateConfig - never numbers
// supplied by the model.
package deploygate

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
)

// Metrics is the subset of a walk-forward run's aggregate OOS metrics the
// gate evaluates.
type Metrics struct {
	Sharpe         float64
	MaxDrawdownPct float64
	ProfitFactor   float64
	TotalReturnPct float64
	Trades         int
}

// Thresholds come only from entities.DeployGateConfig.
type Thresholds struct {
	MinSharpeDelta   float64
	MaxDrawdownRatio float64
	MinProfitFactor  float64
	MinTrades        int
}

// Check is one rule's outcome.
type Check struct {
	Name      string
	Passed    bool
	Candidate float64
	Baseline  float64
	Threshold float64
	Detail    string
}

// GateResult is the gate's verdict. Passed is true only if every check
// passed (and there is at least one check).
type GateResult struct {
	Passed         bool
	Checks         []Check
	BaselineRunID  uint
	CandidateRunID uint
}

// Check names.
const (
	CheckTrades              = "trades"
	CheckSharpe              = "sharpe"
	CheckMaxDrawdown         = "max_drawdown"
	CheckProfitFactor        = "profit_factor"
	CheckFiniteMetrics       = "finite_metrics"
	CheckInsufficientHistory = "insufficient_history"
	CheckGateError           = "gate_error"
)

// Evaluate applies every rule; all must pass:
//   - candidate trades >= MinTrades
//   - Sharpe_c >= Sharpe_b + MinSharpeDelta
//   - MaxDD_c <= MaxDD_b * MaxDrawdownRatio; if MaxDD_b == 0, MaxDD_c must be 0
//   - PF_c >= MinProfitFactor (+Inf passes)
//   - any NaN (metric or threshold) fails, with detail
//
// A baseline with 0 trades is treated as Sharpe 0, max drawdown 0.
func Evaluate(candidate, baseline Metrics, t Thresholds) GateResult {
	if baseline.Trades == 0 {
		baseline.Sharpe = 0
		baseline.MaxDrawdownPct = 0
	}

	var checks []Check

	checks = append(checks, Check{
		Name:      CheckTrades,
		Passed:    candidate.Trades >= t.MinTrades,
		Candidate: float64(candidate.Trades),
		Baseline:  float64(baseline.Trades),
		Threshold: float64(t.MinTrades),
		Detail:    fmt.Sprintf("candidate out-of-sample trades %d, need >= %d", candidate.Trades, t.MinTrades),
	})

	sharpeNeed := baseline.Sharpe + t.MinSharpeDelta
	checks = append(checks, Check{
		Name:      CheckSharpe,
		Passed:    candidate.Sharpe >= sharpeNeed, // false for NaN
		Candidate: candidate.Sharpe,
		Baseline:  baseline.Sharpe,
		Threshold: sharpeNeed,
		Detail:    fmt.Sprintf("candidate Sharpe %.4f, need >= baseline %.4f + delta %.4f", candidate.Sharpe, baseline.Sharpe, t.MinSharpeDelta),
	})

	var ddPass bool
	var ddLimit float64
	var ddDetail string
	if baseline.MaxDrawdownPct == 0 {
		ddLimit = 0
		ddPass = candidate.MaxDrawdownPct == 0
		ddDetail = fmt.Sprintf("baseline max drawdown is 0%%, so the candidate's must be 0%% too (got %.2f%%)", candidate.MaxDrawdownPct)
	} else {
		ddLimit = baseline.MaxDrawdownPct * t.MaxDrawdownRatio
		ddPass = candidate.MaxDrawdownPct <= ddLimit // false for NaN
		ddDetail = fmt.Sprintf("candidate max drawdown %.2f%%, need <= baseline %.2f%% x %.2f = %.2f%%", candidate.MaxDrawdownPct, baseline.MaxDrawdownPct, t.MaxDrawdownRatio, ddLimit)
	}
	checks = append(checks, Check{
		Name:      CheckMaxDrawdown,
		Passed:    ddPass,
		Candidate: candidate.MaxDrawdownPct,
		Baseline:  baseline.MaxDrawdownPct,
		Threshold: ddLimit,
		Detail:    ddDetail,
	})

	pfPass := math.IsInf(candidate.ProfitFactor, 1) || candidate.ProfitFactor >= t.MinProfitFactor
	if math.IsNaN(t.MinProfitFactor) {
		pfPass = false
	}
	checks = append(checks, Check{
		Name:      CheckProfitFactor,
		Passed:    pfPass,
		Candidate: candidate.ProfitFactor,
		Baseline:  baseline.ProfitFactor,
		Threshold: t.MinProfitFactor,
		Detail:    fmt.Sprintf("candidate profit factor %s, need >= %.4f (+Inf passes)", fmtFloat(candidate.ProfitFactor), t.MinProfitFactor),
	})

	if nans := nanFields(candidate, baseline, t); len(nans) > 0 {
		checks = append(checks, Check{
			Name:   CheckFiniteMetrics,
			Passed: false,
			Detail: fmt.Sprintf("NaN in %v - the gate never passes on undefined metrics", nans),
		})
	}

	return result(checks)
}

// InsufficientHistory is the failing result when there is not enough
// candle history to run the gate. It never passes.
func InsufficientHistory(detail string) GateResult {
	return result([]Check{{Name: CheckInsufficientHistory, Passed: false, Detail: detail}})
}

// Errored is the failing result when the gate could not be computed (e.g.
// a backtest error). It never passes.
func Errored(detail string) GateResult {
	return result([]Check{{Name: CheckGateError, Passed: false, Detail: detail}})
}

func result(checks []Check) GateResult {
	passed := len(checks) > 0
	for _, c := range checks {
		passed = passed && c.Passed
	}
	return GateResult{Passed: passed, Checks: checks}
}

func nanFields(c, b Metrics, t Thresholds) []string {
	var out []string
	for name, v := range map[string]float64{
		"candidate.sharpe": c.Sharpe, "candidate.max_drawdown_pct": c.MaxDrawdownPct, "candidate.profit_factor": c.ProfitFactor,
		"baseline.sharpe": b.Sharpe, "baseline.max_drawdown_pct": b.MaxDrawdownPct,
		"thresholds.min_sharpe_delta": t.MinSharpeDelta, "thresholds.max_drawdown_ratio": t.MaxDrawdownRatio,
		"thresholds.min_profit_factor": t.MinProfitFactor,
	} {
		if math.IsNaN(v) {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func fmtFloat(v float64) string {
	switch {
	case math.IsInf(v, 1):
		return "+Inf"
	case math.IsInf(v, -1):
		return "-Inf"
	case math.IsNaN(v):
		return "NaN"
	}
	return fmt.Sprintf("%.4f", v)
}

// JSONFloat marshals non-finite values as the strings "+Inf", "-Inf" and
// "NaN" (JSON has no representation for them).
type JSONFloat float64

// MarshalJSON implements json.Marshaler.
func (f JSONFloat) MarshalJSON() ([]byte, error) {
	v := float64(f)
	if math.IsInf(v, 0) || math.IsNaN(v) {
		return json.Marshal(fmtFloat(v))
	}
	return json.Marshal(v)
}

// MarshalJSON renders the snake_case evidence shape:
// {"name","passed","candidate","baseline","threshold","detail"}.
func (c Check) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Name      string    `json:"name"`
		Passed    bool      `json:"passed"`
		Candidate JSONFloat `json:"candidate"`
		Baseline  JSONFloat `json:"baseline"`
		Threshold JSONFloat `json:"threshold"`
		Detail    string    `json:"detail"`
	}{c.Name, c.Passed, JSONFloat(c.Candidate), JSONFloat(c.Baseline), JSONFloat(c.Threshold), c.Detail})
}

// MarshalJSON renders {"passed","checks","baseline_run_id","candidate_run_id"}.
func (g GateResult) MarshalJSON() ([]byte, error) {
	checks := g.Checks
	if checks == nil {
		checks = []Check{}
	}
	return json.Marshal(struct {
		Passed         bool    `json:"passed"`
		Checks         []Check `json:"checks"`
		BaselineRunID  uint    `json:"baseline_run_id"`
		CandidateRunID uint    `json:"candidate_run_id"`
	}{g.Passed, checks, g.BaselineRunID, g.CandidateRunID})
}
