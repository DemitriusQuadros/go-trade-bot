package deploygate

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var defaults = Thresholds{MinSharpeDelta: 0, MaxDrawdownRatio: 1.10, MinProfitFactor: 1.0, MinTrades: 20}

func good() Metrics {
	return Metrics{Sharpe: 1.5, MaxDrawdownPct: 10, ProfitFactor: 1.6, TotalReturnPct: 12, Trades: 40}
}

func base() Metrics {
	return Metrics{Sharpe: 1.2, MaxDrawdownPct: 10, ProfitFactor: 1.3, TotalReturnPct: 8, Trades: 35}
}

func checkByName(r GateResult, name string) (Check, bool) {
	for _, c := range r.Checks {
		if c.Name == name {
			return c, true
		}
	}
	return Check{}, false
}

// AC#1: table tests over every rule and the §3 edge cases.
func TestEvaluate(t *testing.T) {
	nan := math.NaN()
	inf := math.Inf(1)

	cases := []struct {
		name       string
		candidate  Metrics
		baseline   Metrics
		thresholds Thresholds
		wantPass   bool
		failing    []string // checks that must fail
	}{
		{name: "everything passes", candidate: good(), baseline: base(), thresholds: defaults, wantPass: true},
		{name: "equal sharpe passes with delta 0", candidate: func() Metrics { m := good(); m.Sharpe = 1.2; return m }(), baseline: base(), thresholds: defaults, wantPass: true},

		{name: "trades below minimum", candidate: func() Metrics { m := good(); m.Trades = 19; return m }(), baseline: base(), thresholds: defaults, failing: []string{CheckTrades}},
		{name: "trades exactly minimum passes", candidate: func() Metrics { m := good(); m.Trades = 20; return m }(), baseline: base(), thresholds: defaults, wantPass: true},

		{name: "sharpe below baseline", candidate: func() Metrics { m := good(); m.Sharpe = 1.19; return m }(), baseline: base(), thresholds: defaults, failing: []string{CheckSharpe}},
		{name: "sharpe below baseline plus delta", candidate: good(), baseline: base(), thresholds: Thresholds{MinSharpeDelta: 0.5, MaxDrawdownRatio: 1.1, MinProfitFactor: 1, MinTrades: 20}, failing: []string{CheckSharpe}},

		{name: "drawdown above ratio", candidate: func() Metrics { m := good(); m.MaxDrawdownPct = 11.01; return m }(), baseline: base(), thresholds: defaults, failing: []string{CheckMaxDrawdown}},
		{name: "drawdown exactly at ratio passes", candidate: func() Metrics { m := good(); m.MaxDrawdownPct = 11; return m }(), baseline: base(), thresholds: defaults, wantPass: true},

		{name: "zero baseline drawdown requires zero candidate drawdown", candidate: good(), baseline: func() Metrics { m := base(); m.MaxDrawdownPct = 0; return m }(), thresholds: defaults, failing: []string{CheckMaxDrawdown}},
		{name: "zero baseline drawdown and zero candidate drawdown passes", candidate: func() Metrics { m := good(); m.MaxDrawdownPct = 0; return m }(), baseline: func() Metrics { m := base(); m.MaxDrawdownPct = 0; return m }(), thresholds: defaults, wantPass: true},

		{name: "profit factor below minimum", candidate: func() Metrics { m := good(); m.ProfitFactor = 0.99; return m }(), baseline: base(), thresholds: defaults, failing: []string{CheckProfitFactor}},
		{name: "+Inf profit factor passes", candidate: func() Metrics { m := good(); m.ProfitFactor = inf; return m }(), baseline: base(), thresholds: defaults, wantPass: true},
		{name: "-Inf profit factor fails", candidate: func() Metrics { m := good(); m.ProfitFactor = math.Inf(-1); return m }(), baseline: base(), thresholds: defaults, failing: []string{CheckProfitFactor}},

		{name: "NaN candidate sharpe fails", candidate: func() Metrics { m := good(); m.Sharpe = nan; return m }(), baseline: base(), thresholds: defaults, failing: []string{CheckSharpe, CheckFiniteMetrics}},
		{name: "NaN candidate drawdown fails", candidate: func() Metrics { m := good(); m.MaxDrawdownPct = nan; return m }(), baseline: base(), thresholds: defaults, failing: []string{CheckMaxDrawdown, CheckFiniteMetrics}},
		{name: "NaN candidate profit factor fails", candidate: func() Metrics { m := good(); m.ProfitFactor = nan; return m }(), baseline: base(), thresholds: defaults, failing: []string{CheckProfitFactor, CheckFiniteMetrics}},
		{name: "NaN baseline sharpe fails", candidate: good(), baseline: func() Metrics { m := base(); m.Sharpe = nan; return m }(), thresholds: defaults, failing: []string{CheckSharpe, CheckFiniteMetrics}},
		{name: "NaN threshold fails", candidate: good(), baseline: base(), thresholds: Thresholds{MinSharpeDelta: nan, MaxDrawdownRatio: 1.1, MinProfitFactor: 1, MinTrades: 20}, failing: []string{CheckFiniteMetrics}},

		{
			name:      "baseline with 0 trades is treated as sharpe 0 and drawdown 0",
			candidate: func() Metrics { m := good(); m.MaxDrawdownPct = 0; m.Sharpe = 0.01; return m }(),
			// garbage baseline metrics that must be ignored
			baseline:   Metrics{Sharpe: 99, MaxDrawdownPct: 50, Trades: 0},
			thresholds: defaults,
			wantPass:   true,
		},
		{
			name:       "baseline with 0 trades: candidate with drawdown fails (baseline DD treated as 0)",
			candidate:  good(),
			baseline:   Metrics{Sharpe: nan, MaxDrawdownPct: 50, Trades: 0},
			thresholds: defaults,
			failing:    []string{CheckMaxDrawdown},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := Evaluate(tc.candidate, tc.baseline, tc.thresholds)
			assert.Equal(t, tc.wantPass, r.Passed)
			if tc.wantPass {
				for _, c := range r.Checks {
					assert.Truef(t, c.Passed, "check %s should pass: %s", c.Name, c.Detail)
				}
			}
			for _, name := range tc.failing {
				c, ok := checkByName(r, name)
				require.Truef(t, ok, "check %s missing", name)
				assert.Falsef(t, c.Passed, "check %s should fail", name)
				assert.NotEmpty(t, c.Detail)
			}
		})
	}
}

func TestEvaluate_NaNDetailNamesTheField(t *testing.T) {
	c := good()
	c.Sharpe = math.NaN()
	r := Evaluate(c, base(), defaults)
	fc, ok := checkByName(r, CheckFiniteMetrics)
	require.True(t, ok)
	assert.Contains(t, fc.Detail, "candidate.sharpe")
}

func TestInsufficientHistoryAndErroredNeverPass(t *testing.T) {
	for _, r := range []GateResult{InsufficientHistory("only 10% of candles"), Errored("boom")} {
		assert.False(t, r.Passed)
		require.Len(t, r.Checks, 1)
		assert.False(t, r.Checks[0].Passed)
	}
	assert.Equal(t, CheckInsufficientHistory, InsufficientHistory("x").Checks[0].Name)
}

func TestGateResultJSON_EncodesNonFiniteAsStrings(t *testing.T) {
	c := good()
	c.ProfitFactor = math.Inf(1)
	r := Evaluate(c, base(), defaults)
	r.BaselineRunID, r.CandidateRunID = 11, 12
	b, err := json.Marshal(r)
	require.NoError(t, err)

	var out struct {
		Passed         bool             `json:"passed"`
		BaselineRunID  uint             `json:"baseline_run_id"`
		CandidateRunID uint             `json:"candidate_run_id"`
		Checks         []map[string]any `json:"checks"`
	}
	require.NoError(t, json.Unmarshal(b, &out))
	assert.True(t, out.Passed)
	assert.Equal(t, uint(11), out.BaselineRunID)
	assert.Equal(t, uint(12), out.CandidateRunID)
	var pf map[string]any
	for _, ch := range out.Checks {
		for _, k := range []string{"name", "passed", "candidate", "baseline", "threshold", "detail"} {
			assert.Contains(t, ch, k)
		}
		if ch["name"] == CheckProfitFactor {
			pf = ch
		}
	}
	require.NotNil(t, pf)
	assert.Equal(t, "+Inf", pf["candidate"])

	nanRes := Evaluate(Metrics{Sharpe: math.NaN(), Trades: 30}, base(), defaults)
	b, err = json.Marshal(nanRes)
	require.NoError(t, err)
	assert.Contains(t, string(b), `"NaN"`)
}
