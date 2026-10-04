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
	agentreports "go-trade-bot/app/handler/web/agentreports"
	agentshandler "go-trade-bot/app/handler/web/agents"
	webhooktargets "go-trade-bot/app/handler/web/webhooktargets"
	repoagent "go-trade-bot/app/repository/agent"
	"go-trade-bot/app/repository/agentplatform"
	settings_repo "go-trade-bot/app/repository/settings"
	strategy_repo "go-trade-bot/app/repository/strategy"
	platformusecase "go-trade-bot/app/usecase/agentplatform"
	agentworker "go-trade-bot/app/workers/agent"
	"go-trade-bot/internal/configuration"
	"go-trade-bot/internal/notifier"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// This is the A-02 §5 REST contract test: real handlers, real usecase and
// real GORM repositories (SQLite) mounted through cmd/api's own NewServeMux
// (so the /api prefix, bearer/?token auth and route ordering are the
// production ones). Only the asynq enqueuer and webhook sender are faked.

const testToken = "test-token"

type fakeEnqueuer struct{ payloads []agentworker.RunPayload }

func (f *fakeEnqueuer) EnqueueRun(_ context.Context, p agentworker.RunPayload) (string, error) {
	f.payloads = append(f.payloads, p)
	return fmt.Sprintf("task-%d", len(f.payloads)), nil
}

type fakeSender struct{ err error }

func (f fakeSender) SendSync(context.Context, entities.WebhookTarget, notifier.AgentMessage) error {
	return f.err
}

type contractEnv struct {
	router   *mux.Router
	db       *gorm.DB
	enqueuer *fakeEnqueuer
	strategy entities.Strategy
	platform *platformusecase.UseCase
}

func newContractEnv(t *testing.T) *contractEnv {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, Migrate(db))

	strat := entities.Strategy{Name: "RSI Momentum", StrategyName: "script", Status: entities.Testing, Mode: "dryrun"}
	require.NoError(t, db.Create(&strat).Error)

	enq := &fakeEnqueuer{}
	uc := platformusecase.NewUseCase(
		agentplatform.NewGormRepository(db),
		strategy_repo.NewStrategyRepository(db),
		repoagent.NewGormRepository(db),
		enq,
		settings_repo.NewRepository(db),
		fakeSender{},
	)
	routes := []Route{
		agentshandler.NewAgentsHandler(uc),
		agentreports.NewAgentReportsHandler(uc),
		webhooktargets.NewWebhookTargetsHandler(uc),
	}
	router := NewServeMux(routes, &configuration.Configuration{APIToken: testToken}, nil)
	return &contractEnv{router: router, db: db, enqueuer: enq, strategy: strat, platform: uc}
}

func (e *contractEnv) do(t *testing.T, method, path string, body any) *httptest.ResponseRecorder {
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

func decodeMap(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &m), rec.Body.String())
	return m
}

func (e *contractEnv) agentRequest(name string) map[string]any {
	return map[string]any{
		"name": name, "goal": "Watch drawdowns.", "provider": "", "model": "",
		"permissions":  []string{"read", "backtest", "notify"},
		"triggers":     map[string]any{"cron": []string{"0 */6 * * *"}},
		"strategy_ids": []uint{e.strategy.ID}, "webhook_target_ids": []uint{},
		"daily_budget_usd": 2.5, "max_auto_deploys_per_day": 0,
	}
}

func TestAgentsAPI_CreateGetListShape(t *testing.T) {
	e := newContractEnv(t)
	rec := e.do(t, http.MethodPost, "/api/agents", e.agentRequest("Risk Monitor"))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	got := decodeMap(t, rec)
	for _, k := range []string{"id", "name", "goal", "provider", "model", "permissions", "triggers", "strategy_ids", "webhook_target_ids",
		"daily_budget_usd", "max_auto_deploys_per_day", "paused", "is_default", "created_at", "updated_at", "today_cost_usd", "last_run", "next_run_at"} {
		assert.Contains(t, got, k)
	}
	assert.Equal(t, []any{"0 */6 * * *"}, got["triggers"].(map[string]any)["cron"])
	assert.Equal(t, []any{float64(e.strategy.ID)}, got["strategy_ids"])
	assert.Nil(t, got["last_run"])
	require.NotNil(t, got["next_run_at"], "cron + binding + not paused -> next run computed")
	_, err := time.Parse(time.RFC3339, got["next_run_at"].(string))
	require.NoError(t, err)

	id := int(got["id"].(float64))
	rec = e.do(t, http.MethodPost, fmt.Sprintf("/api/agents/%d/pause", id), map[string]bool{"paused": true})
	require.Equal(t, http.StatusOK, rec.Code)
	paused := decodeMap(t, rec)
	assert.Equal(t, true, paused["paused"])
	assert.Nil(t, paused["next_run_at"], "paused -> no next run")

	rec = e.do(t, http.MethodGet, "/api/agents", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	var list []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list))
	require.Len(t, list, 2)
	assert.Equal(t, "Copilot", list[0]["name"])
	assert.Equal(t, true, list[0]["is_default"])
}

// A-02 AC#5: invalid cron / unknown permission / duplicate name.
func TestAgentsAPI_Validation(t *testing.T) {
	e := newContractEnv(t)
	bad := e.agentRequest("X")
	bad["triggers"] = map[string]any{"cron": []string{"every tuesday"}}
	rec := e.do(t, http.MethodPost, "/api/agents", bad)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	body := decodeMap(t, rec)
	assert.Equal(t, "validation_error", body["error"])
	assert.Contains(t, body["message"], "cron")

	bad = e.agentRequest("X")
	bad["permissions"] = []string{"read", "trade_live"}
	rec = e.do(t, http.MethodPost, "/api/agents", bad)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, decodeMap(t, rec)["message"], "trade_live")

	bad = e.agentRequest("X")
	bad["strategy_ids"] = []uint{9999}
	assert.Equal(t, http.StatusBadRequest, e.do(t, http.MethodPost, "/api/agents", bad).Code)

	bad = e.agentRequest("X")
	bad["provider"] = "openai"
	assert.Equal(t, http.StatusBadRequest, e.do(t, http.MethodPost, "/api/agents", bad).Code)

	bad = e.agentRequest("X")
	bad["daily_budget_usd"] = -1
	assert.Equal(t, http.StatusBadRequest, e.do(t, http.MethodPost, "/api/agents", bad).Code)

	require.Equal(t, http.StatusCreated, e.do(t, http.MethodPost, "/api/agents", e.agentRequest("Dup")).Code)
	rec = e.do(t, http.MethodPost, "/api/agents", e.agentRequest("Dup"))
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Equal(t, "conflict", decodeMap(t, rec)["error"])
	assert.Equal(t, http.StatusConflict, e.do(t, http.MethodPost, "/api/agents", e.agentRequest("Copilot")).Code)
}

// A-02 AC#6: the default agent cannot be deleted or renamed; a referenced
// webhook target cannot be deleted.
func TestAgentsAPI_DefaultAgentAndReferencedTargetConflicts(t *testing.T) {
	e := newContractEnv(t)
	var def entities.Agent
	require.NoError(t, e.db.Where("is_default = ?", true).First(&def).Error)

	assert.Equal(t, http.StatusConflict, e.do(t, http.MethodDelete, fmt.Sprintf("/api/agents/%d", def.ID), nil).Code)
	rename := e.agentRequest("Not Copilot")
	assert.Equal(t, http.StatusBadRequest, e.do(t, http.MethodPut, fmt.Sprintf("/api/agents/%d", def.ID), rename).Code)
	edit := e.agentRequest("Copilot")
	assert.Equal(t, http.StatusOK, e.do(t, http.MethodPut, fmt.Sprintf("/api/agents/%d", def.ID), edit).Code, "the default agent may be edited")

	rec := e.do(t, http.MethodPost, "/api/webhook-targets", map[string]any{"name": "ops", "kind": "slack", "url": "https://hooks.slack.com/services/T/B/SECRET1234", "enabled": true})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	targetID := uint(decodeMap(t, rec)["id"].(float64))

	req := e.agentRequest("Pager")
	req["webhook_target_ids"] = []uint{targetID}
	require.Equal(t, http.StatusCreated, e.do(t, http.MethodPost, "/api/agents", req).Code)

	rec = e.do(t, http.MethodDelete, fmt.Sprintf("/api/webhook-targets/%d", targetID), nil)
	assert.Equal(t, http.StatusConflict, rec.Code)
	body := decodeMap(t, rec)
	assert.Equal(t, []any{"Pager"}, body["agents"])
	assert.Contains(t, body["message"], "Pager")

	// A non-default agent deletes fine (204).
	var pager entities.Agent
	require.NoError(t, e.db.Where("name = ?", "Pager").First(&pager).Error)
	assert.Equal(t, http.StatusNoContent, e.do(t, http.MethodDelete, fmt.Sprintf("/api/agents/%d", pager.ID), nil).Code)
	assert.Equal(t, http.StatusNoContent, e.do(t, http.MethodDelete, fmt.Sprintf("/api/webhook-targets/%d", targetID), nil).Code)
}

// A-02 AC#7: secrets masked in every response; PUT with the masked value
// keeps the stored secret.
func TestWebhookTargetsAPI_MaskingAndKeep(t *testing.T) {
	e := newContractEnv(t)
	rec := e.do(t, http.MethodPost, "/api/webhook-targets", map[string]any{"name": "tg", "kind": "telegram", "secret": "123456:BOTTOKEN-XYZ", "chat_id": "-1001", "enabled": true})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	created := decodeMap(t, rec)
	assert.NotContains(t, rec.Body.String(), "BOTTOKEN")
	assert.Equal(t, "••••-XYZ", created["secret"])
	id := uint(created["id"].(float64))

	rec = e.do(t, http.MethodGet, "/api/webhook-targets", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.NotContains(t, rec.Body.String(), "BOTTOKEN")

	// Round-trip the masked secret, change the name -> secret kept.
	rec = e.do(t, http.MethodPut, fmt.Sprintf("/api/webhook-targets/%d", id), map[string]any{"name": "tg2", "kind": "telegram", "secret": created["secret"], "chat_id": "-1001", "enabled": true})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var stored entities.WebhookTarget
	require.NoError(t, e.db.First(&stored, id).Error)
	assert.Equal(t, "123456:BOTTOKEN-XYZ", stored.Secret)
	assert.Equal(t, "tg2", stored.Name)

	// A new value overwrites.
	rec = e.do(t, http.MethodPut, fmt.Sprintf("/api/webhook-targets/%d", id), map[string]any{"name": "tg2", "kind": "telegram", "secret": "999:NEW", "chat_id": "-1001", "enabled": false})
	require.Equal(t, http.StatusOK, rec.Code)
	stored = entities.WebhookTarget{}
	require.NoError(t, e.db.First(&stored, id).Error)
	assert.Equal(t, "999:NEW", stored.Secret)

	// URL targets: url masked too, and kept on masked round-trip.
	rec = e.do(t, http.MethodPost, "/api/webhook-targets", map[string]any{"name": "dc", "kind": "discord", "url": "https://discord.com/api/webhooks/1/ABCDEFGH", "enabled": true})
	require.Equal(t, http.StatusCreated, rec.Code)
	dc := decodeMap(t, rec)
	assert.Equal(t, "••••EFGH", dc["url"])
	dcID := uint(dc["id"].(float64))
	require.Equal(t, http.StatusOK, e.do(t, http.MethodPut, fmt.Sprintf("/api/webhook-targets/%d", dcID), map[string]any{"name": "dc", "kind": "discord", "url": dc["url"], "enabled": true}).Code)
	var storedDC entities.WebhookTarget
	require.NoError(t, e.db.First(&storedDC, dcID).Error)
	assert.Equal(t, "https://discord.com/api/webhooks/1/ABCDEFGH", storedDC.URL)

	// Validation.
	assert.Equal(t, http.StatusBadRequest, e.do(t, http.MethodPost, "/api/webhook-targets", map[string]any{"name": "x", "kind": "generic", "url": "not a url"}).Code)
	assert.Equal(t, http.StatusBadRequest, e.do(t, http.MethodPost, "/api/webhook-targets", map[string]any{"name": "x", "kind": "sms", "url": "https://x"}).Code)

	rec = e.do(t, http.MethodPost, fmt.Sprintf("/api/webhook-targets/%d/test", dcID), nil)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, true, decodeMap(t, rec)["ok"])
}

// A-02 AC#4 (manual half) + coordinator's kill-switch endpoint.
func TestAgentsAPI_KillSwitchAndManualRun(t *testing.T) {
	e := newContractEnv(t)
	rec := e.do(t, http.MethodPost, "/api/agents", e.agentRequest("Runner"))
	require.Equal(t, http.StatusCreated, rec.Code)
	id := int(decodeMap(t, rec)["id"].(float64))

	rec = e.do(t, http.MethodPut, "/api/agents/kill-switch", map[string]bool{"paused": true})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, map[string]any{"agents_paused": true}, decodeMap(t, rec))

	rec = e.do(t, http.MethodPost, fmt.Sprintf("/api/agents/%d/run", id), map[string]string{"prompt": "check BTC"})
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, decodeMap(t, rec)["message"], "kill switch")

	rec = e.do(t, http.MethodGet, fmt.Sprintf("/api/agents/%d", id), nil)
	assert.Nil(t, decodeMap(t, rec)["next_run_at"], "kill switch -> nothing scheduled")

	require.Equal(t, http.StatusOK, e.do(t, http.MethodPut, "/api/agents/kill-switch", map[string]bool{"paused": false}).Code)
	rec = e.do(t, http.MethodPost, fmt.Sprintf("/api/agents/%d/run", id), map[string]string{"prompt": "check BTC"})
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	assert.Equal(t, map[string]any{"enqueued": true, "task_id": "task-1"}, decodeMap(t, rec))
	require.Len(t, e.enqueuer.payloads, 1)
	assert.Equal(t, "manual", e.enqueuer.payloads[0].Trigger)
	assert.Equal(t, "check BTC", e.enqueuer.payloads[0].Prompt)

	// A paused agent -> 409 too.
	require.Equal(t, http.StatusOK, e.do(t, http.MethodPost, fmt.Sprintf("/api/agents/%d/pause", id), map[string]bool{"paused": true}).Code)
	assert.Equal(t, http.StatusConflict, e.do(t, http.MethodPost, fmt.Sprintf("/api/agents/%d/run", id), nil).Code)

	assert.Equal(t, http.StatusBadRequest, e.do(t, http.MethodPut, "/api/agents/kill-switch", map[string]any{}).Code)
}

// A-02 AC#9: the HTML endpoint works with ?token= and sends the CSP.
func TestAgentReportsAPI_HTMLWithBearerAndCSP(t *testing.T) {
	e := newContractEnv(t)
	var def entities.Agent
	require.NoError(t, e.db.Where("is_default = ?", true).First(&def).Error)
	rep := entities.AgentReport{AgentID: def.ID, AgentRunID: 0, StrategyIDs: []uint{e.strategy.ID}, Title: "Weekly", Severity: "warning",
		Summary: "All quiet.", BlocksJSON: []byte(`[{"type":"summary","data":{"text":"All quiet."}}]`),
		RenderedHTML: "<!DOCTYPE html>\n<html lang=\"en\">\n<head></head><body>report</body></html>", CreatedAt: time.Now()}
	require.NoError(t, e.db.Create(&rep).Error)

	// Auth-01: the ?token= query fallback is gone - iframes send the session
	// cookie; scripts use the bearer header.
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/agent-reports/%d/html?theme=dark", rep.ID), nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "default-src 'none'; style-src 'unsafe-inline'; img-src data:", rec.Header().Get("Content-Security-Policy"))
	assert.Equal(t, "SAMEORIGIN", rec.Header().Get("X-Frame-Options"))
	assert.True(t, strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html"))
	assert.Contains(t, rec.Body.String(), `<html lang="en" class="dark">`)

	unauth := httptest.NewRecorder()
	e.router.ServeHTTP(unauth, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/agent-reports/%d/html?token=%s", rep.ID, testToken), nil))
	assert.Equal(t, http.StatusUnauthorized, unauth.Code, "?token= is no longer accepted")

	rec = e.do(t, http.MethodGet, fmt.Sprintf("/api/agent-reports?strategy_id=%d", e.strategy.ID), nil)
	require.Equal(t, http.StatusOK, rec.Code)
	var list []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list))
	require.Len(t, list, 1)
	assert.Equal(t, "Copilot", list[0]["agent_name"])
	assert.Nil(t, list[0]["agent_run_id"])
	assert.NotContains(t, list[0], "blocks")

	rec = e.do(t, http.MethodGet, fmt.Sprintf("/api/agent-reports/%d", rep.ID), nil)
	detail := decodeMap(t, rec)
	assert.Equal(t, "summary", detail["blocks"].([]any)[0].(map[string]any)["type"])

	assert.Equal(t, http.StatusBadRequest, e.do(t, http.MethodGet, fmt.Sprintf("/api/agent-reports/%d/html?theme=neon", rep.ID), nil).Code)
}

func TestAgentsAPI_RunsUsageAndMemory(t *testing.T) {
	e := newContractEnv(t)
	var def entities.Agent
	require.NoError(t, e.db.Where("is_default = ?", true).First(&def).Error)
	runs := repoagent.NewGormRepository(e.db)
	for i := 0; i < 3; i++ {
		_, err := runs.CreateRun(context.Background(), entities.AgentRun{AgentID: &def.ID, Trigger: "cron", Status: entities.AgentRunOK,
			TriggerDetail: []byte(`{"cron":"0 * * * *"}`), InputTokens: 100, OutputTokens: 10, CostUSD: 0.01})
		require.NoError(t, err)
	}
	require.NoError(t, agentplatform.NewGormRepository(e.db).AddUsage(context.Background(), def.ID, time.Now(), 300, 30, 0.03))

	rec := e.do(t, http.MethodGet, fmt.Sprintf("/api/agents/%d/runs?limit=2", def.ID), nil)
	require.Equal(t, http.StatusOK, rec.Code)
	var list []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list))
	require.Len(t, list, 2)
	assert.Equal(t, "Copilot", list[0]["agent_name"])
	assert.Equal(t, map[string]any{"cron": "0 * * * *"}, list[0]["trigger_detail"])
	assert.Equal(t, float64(100), list[0]["input_tokens"])
	assert.Equal(t, 0.01, list[0]["cost_usd"])

	rec = e.do(t, http.MethodGet, fmt.Sprintf("/api/agents/%d/usage?days=7", def.ID), nil)
	require.Equal(t, http.StatusOK, rec.Code)
	usage := decodeMap(t, rec)
	today := usage["today"].(map[string]any)
	assert.Equal(t, time.Now().UTC().Format("2006-01-02"), today["day"])
	assert.Equal(t, 0.03, today["cost_usd"])
	assert.Len(t, usage["days"], 7)

	rec = e.do(t, http.MethodGet, fmt.Sprintf("/api/agents/%d", def.ID), nil)
	agent := decodeMap(t, rec)
	assert.Equal(t, 0.03, agent["today_cost_usd"])
	assert.Equal(t, "cron", agent["last_run"].(map[string]any)["trigger"])

	rec = e.do(t, http.MethodPost, fmt.Sprintf("/api/strategies/%d/memory", e.strategy.ID), map[string]string{"content": "Operator: widen stops"})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	note := decodeMap(t, rec)
	assert.Equal(t, "operator", note["author_name"])
	assert.Equal(t, "journal", note["kind"])
	assert.Nil(t, note["author_agent_id"])
	assert.Contains(t, note, "ref_id")

	rec = e.do(t, http.MethodGet, fmt.Sprintf("/api/strategies/%d/memory?kinds=journal,finding", e.strategy.ID), nil)
	require.Equal(t, http.StatusOK, rec.Code)
	var mem []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &mem))
	require.Len(t, mem, 1)

	assert.Equal(t, http.StatusNotFound, e.do(t, http.MethodGet, "/api/strategies/9999/memory", nil).Code)
	assert.Equal(t, http.StatusBadRequest, e.do(t, http.MethodGet, fmt.Sprintf("/api/strategies/%d/memory?kinds=gossip", e.strategy.ID), nil).Code)
	assert.Equal(t, http.StatusBadRequest, e.do(t, http.MethodPost, fmt.Sprintf("/api/strategies/%d/memory", e.strategy.ID), map[string]string{"content": " "}).Code)
}
