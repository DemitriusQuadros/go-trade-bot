package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go-trade-bot/app/entities"
	proposalshandler "go-trade-bot/app/handler/web/proposals"
	"go-trade-bot/app/repository/agentplatform"
	proposalrepo "go-trade-bot/app/repository/proposal"
	strategy_repo "go-trade-bot/app/repository/strategy"
	proposalusecase "go-trade-bot/app/usecase/proposal"
	"go-trade-bot/internal/configuration"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// B-01 §5 REST contract: real handler + usecase + GORM repos (SQLite)
// mounted through cmd/api's NewServeMux (the /api prefix and bearer auth
// are the production ones). Only the asynq enqueuer is faked.

type fakeApplyEnqueuer struct{ ids []uint }

func (f *fakeApplyEnqueuer) EnqueueApplyProposal(_ context.Context, id uint, _ time.Duration) error {
	f.ids = append(f.ids, id)
	return nil
}

type proposalEnv struct {
	router   *mux.Router
	db       *gorm.DB
	enqueuer *fakeApplyEnqueuer
	champion entities.Strategy
	agent    entities.Agent
}

func newProposalEnv(t *testing.T) *proposalEnv {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, Migrate(db))
	platform := agentplatform.NewGormRepository(db)
	agent, err := platform.CreateAgent(context.Background(), entities.Agent{Name: "Improver"})
	require.NoError(t, err)
	champion := entities.Strategy{Name: "Champion", StrategyName: "script", ScriptSource: "old", Status: entities.Productive, Mode: "live"}
	require.NoError(t, db.Create(&champion).Error)

	enq := &fakeApplyEnqueuer{}
	uc := proposalusecase.NewUseCase(proposalrepo.NewGormRepository(db), strategy_repo.NewStrategyRepository(db), platform, enq)
	router := NewServeMux([]Route{proposalshandler.NewProposalsHandler(uc)}, &configuration.Configuration{APIToken: testToken})
	return &proposalEnv{router: router, db: db, enqueuer: enq, champion: champion, agent: agent}
}

func (e *proposalEnv) do(t *testing.T, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Authorization", "Bearer "+testToken)
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	return rec
}

func (e *proposalEnv) create(t *testing.T, kind entities.ProposalKind, rationale string, evidence string) entities.StrategyChangeProposal {
	t.Helper()
	challenger := entities.Strategy{Name: "Champion · challenger", StrategyName: "script", ScriptSource: "new", Status: entities.Testing, Mode: "dryrun", ChallengerOfID: &e.champion.ID}
	require.NoError(t, e.db.Create(&challenger).Error)
	p := entities.StrategyChangeProposal{Kind: kind, TargetStrategyID: e.champion.ID, AgentID: e.agent.ID, BaseSource: "old", ProposedSource: "new",
		Rationale: rationale, Status: entities.ProposalPending}
	if kind == entities.ProposalPromoteChallenger {
		p.ChallengerStrategyID = &challenger.ID
	}
	if evidence != "" {
		p.EvidenceJSON = datatypes.JSON(evidence)
	}
	require.NoError(t, e.db.Create(&p).Error)
	return p
}

const evidenceFixture = `{"gate":{"passed":false,"checks":[{"name":"trades","passed":false,"candidate":5,"baseline":30,"threshold":20,"detail":"x"}],"baseline_run_id":1,"candidate_run_id":2},` +
	`"forward_test":{"since":"2026-09-01T00:00:00Z","challenger":{"trades":2,"win_rate_pct":50,"net_pnl":6,"max_adverse":-4},"champion":{"trades":1,"win_rate_pct":0,"net_pnl":-2,"max_adverse":-2}}}`

func TestProposalsAPI_ListDetailShapes(t *testing.T) {
	e := newProposalEnv(t)
	p := e.create(t, entities.ProposalPromoteChallenger, "EARLY: regime shift", evidenceFixture)
	e.create(t, entities.ProposalGateFailedChange, "tweak", "")

	rec := e.do(t, http.MethodGet, "/api/proposals", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var list []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list))
	require.Len(t, list, 2)
	for _, k := range []string{"id", "kind", "target_strategy_id", "target_strategy_name", "challenger_strategy_id", "agent_id", "agent_name",
		"rationale", "status", "early", "gate_passed", "created_at", "decided_at", "applied_at", "failure_reason"} {
		assert.Contains(t, list[0], k)
	}
	promo := list[1] // newest first
	assert.EqualValues(t, p.ID, promo["id"])
	assert.Equal(t, "promote_challenger", promo["kind"])
	assert.Equal(t, "Champion", promo["target_strategy_name"])
	assert.Equal(t, "Improver", promo["agent_name"])
	assert.Equal(t, true, promo["early"])
	assert.Equal(t, false, promo["gate_passed"])
	assert.Nil(t, list[0]["gate_passed"], "no gate evidence -> null")
	assert.Nil(t, list[0]["challenger_strategy_id"])

	rec = e.do(t, http.MethodGet, fmt.Sprintf("/api/proposals/%d", p.ID), nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	d := decodeMap(t, rec)
	for _, k := range []string{"base_source", "proposed_source", "evidence", "report_id", "decision_note", "target_has_open_position",
		"target_current_source_matches_base", "last_flat_check_at"} {
		assert.Contains(t, d, k)
	}
	assert.Equal(t, true, d["target_current_source_matches_base"])
	assert.Equal(t, false, d["target_has_open_position"])
	ev := d["evidence"].(map[string]any)
	assert.Contains(t, ev, "gate")
	assert.Contains(t, ev, "forward_test")

	rec = e.do(t, http.MethodGet, "/api/proposals/pending-count", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"count":2}`, rec.Body.String())

	// status is a comma-separated list; strategy_id matches target or challenger.
	rec = e.do(t, http.MethodGet, "/api/proposals?status=applied,rejected,superseded,failed", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `[]`, rec.Body.String())
	rec = e.do(t, http.MethodGet, fmt.Sprintf("/api/proposals?strategy_id=%d", *p.ChallengerStrategyID), nil)
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list))
	require.Len(t, list, 1)
	assert.EqualValues(t, p.ID, list[0]["id"])
	rec = e.do(t, http.MethodGet, "/api/proposals?status=bogus", nil)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	rec = e.do(t, http.MethodGet, "/api/proposals/999", nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestProposalsAPI_ApproveRejectAndSuperseded(t *testing.T) {
	e := newProposalEnv(t)
	p := e.create(t, entities.ProposalPromoteChallenger, "better", evidenceFixture)

	rec := e.do(t, http.MethodPost, fmt.Sprintf("/api/proposals/%d/approve", p.ID), map[string]string{"note": "go"})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	d := decodeMap(t, rec)
	assert.Equal(t, "approved", d["status"])
	assert.Equal(t, "go", d["decision_note"])
	assert.NotNil(t, d["decided_at"])
	assert.Equal(t, []uint{p.ID}, e.enqueuer.ids)

	rec = e.do(t, http.MethodPost, fmt.Sprintf("/api/proposals/%d/approve", p.ID), nil)
	assert.Equal(t, http.StatusConflict, rec.Code, "only pending proposals can be approved")

	q := e.create(t, entities.ProposalGateFailedChange, "x", "")
	rec = e.do(t, http.MethodPost, fmt.Sprintf("/api/proposals/%d/reject", q.ID), map[string]string{"note": "nope"})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	d = decodeMap(t, rec)
	assert.Equal(t, "rejected", d["status"])
	assert.Contains(t, d, "base_source", "reject returns the detail DTO")

	// AC#8: the target's source changed since the proposal -> 409 superseded.
	r := e.create(t, entities.ProposalPromoteChallenger, "y", "")
	require.NoError(t, e.db.Model(&entities.Strategy{}).Where("id = ?", e.champion.ID).Update("script_source", "edited").Error)
	rec = e.do(t, http.MethodPost, fmt.Sprintf("/api/proposals/%d/approve", r.ID), nil)
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	assert.Equal(t, "superseded", decodeMap(t, rec)["error"])
	var got entities.StrategyChangeProposal
	require.NoError(t, e.db.First(&got, r.ID).Error)
	assert.Equal(t, entities.ProposalSuperseded, got.Status)
	assert.Len(t, e.enqueuer.ids, 1, "no apply task for a superseded proposal")

	// Approval never touches the strategy itself.
	var champ entities.Strategy
	require.NoError(t, e.db.First(&champ, e.champion.ID).Error)
	assert.Equal(t, "live", champ.Mode)
	assert.Equal(t, entities.Productive, champ.Status)
}

func TestDeployGateAPI(t *testing.T) {
	e := newProposalEnv(t)
	rec := e.do(t, http.MethodGet, "/api/deploy-gate", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	d := decodeMap(t, rec)
	for k, v := range map[string]any{"min_sharpe_delta": 0.0, "max_drawdown_ratio": 1.1, "min_trades": 20.0, "min_profit_factor": 1.0,
		"lookback_months": 6.0, "train_months": 3.0, "test_months": 1.0, "timeframe": ""} {
		assert.Equal(t, v, d[k], k)
	}

	body := map[string]any{"min_sharpe_delta": 0.1, "max_drawdown_ratio": 1.2, "min_trades": 30, "min_profit_factor": 1.1,
		"lookback_months": 8, "train_months": 4, "test_months": 2, "timeframe": "1h"}
	rec = e.do(t, http.MethodPut, "/api/deploy-gate", body)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.EqualValues(t, 30, decodeMap(t, rec)["min_trades"])
	rec = e.do(t, http.MethodGet, "/api/deploy-gate", nil)
	assert.EqualValues(t, "1h", decodeMap(t, rec)["timeframe"])

	for name, mutate := range map[string]func(m map[string]any){
		"ratio 0":     func(m map[string]any) { m["max_drawdown_ratio"] = 0 },
		"trades 0":    func(m map[string]any) { m["min_trades"] = 0 },
		"months 0":    func(m map[string]any) { m["test_months"] = 0 },
		"missing":     func(m map[string]any) { delete(m, "min_trades") },
		"short range": func(m map[string]any) { m["lookback_months"] = 2 },
	} {
		b := map[string]any{}
		for k, v := range body {
			b[k] = v
		}
		mutate(b)
		rec = e.do(t, http.MethodPut, "/api/deploy-gate", b)
		assert.Equalf(t, http.StatusBadRequest, rec.Code, "%s: %s", name, rec.Body.String())
	}

	// Auth is required like every other /api route.
	req := httptest.NewRequest(http.MethodGet, "/api/deploy-gate", strings.NewReader(""))
	rec = httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}
