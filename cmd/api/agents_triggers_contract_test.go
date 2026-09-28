package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"go-trade-bot/app/entities"
	repoagent "go-trade-bot/app/repository/agent"
	"go-trade-bot/internal/marketstatus"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
)

// agents-platform C-01 REST contract (§1 triggers, §4 run DTO, §6).

func (e *contractEnv) createAgent(t *testing.T, name string, triggers map[string]any) map[string]any {
	t.Helper()
	req := e.agentRequest(name)
	req["triggers"] = triggers
	rec := e.do(t, http.MethodPost, "/api/agents", req)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	return decodeMap(t, rec)
}

func TestAgentsAPI_TriggersRoundTripAndSummary(t *testing.T) {
	e := newContractEnv(t)
	src := e.createAgent(t, "Source", map[string]any{"cron": []string{"0 * * * *"}})
	srcID := src["id"].(float64)
	got := e.createAgent(t, "Watcher", map[string]any{
		"cron":       []string{"0 */6 * * *"},
		"events":     []any{map[string]any{"type": "stoploss.hit"}, map[string]any{"type": "drawdown", "threshold_pct": 5}, map[string]any{"type": "no_signal", "window_hours": 12}},
		"market":     []any{map[string]any{"id": "rule-1", "symbol": "BTCUSDT", "kind": "pct_move", "window_minutes": 60, "threshold_pct": 3}},
		"chain_from": []any{map[string]any{"agent_id": srcID, "on": "notify"}},
	})
	tr := got["triggers"].(map[string]any)
	assert.Equal(t, []any{"0 */6 * * *"}, tr["cron"])
	assert.Equal(t, []any{
		map[string]any{"type": "stoploss.hit", "cooldown_minutes": float64(15)},
		map[string]any{"type": "drawdown", "threshold_pct": float64(5), "window_hours": float64(24), "cooldown_minutes": float64(240)},
		map[string]any{"type": "no_signal", "window_hours": float64(12), "cooldown_minutes": float64(240)},
	}, tr["events"], "defaults are filled in and echoed")
	assert.Equal(t, []any{map[string]any{"id": "rule-1", "symbol": "BTCUSDT", "kind": "pct_move", "window_minutes": float64(60), "threshold_pct": float64(3), "cooldown_minutes": float64(60)}}, tr["market"])
	assert.Equal(t, []any{map[string]any{"agent_id": srcID, "on": "notify"}}, tr["chain_from"])
	assert.Equal(t, map[string]any{"cron": float64(1), "events": float64(3), "market": float64(1), "chain_from": float64(1)}, got["trigger_summary"])

	// A trigger-less agent still returns all four arrays.
	plain := decodeMap(t, e.do(t, http.MethodGet, fmt.Sprintf("/api/agents/%d", int(srcID)), nil))
	ptr := plain["triggers"].(map[string]any)
	for _, k := range []string{"cron", "events", "market", "chain_from"} {
		assert.NotNil(t, ptr[k], k)
	}
	assert.Equal(t, map[string]any{"cron": float64(1), "events": float64(0), "market": float64(0), "chain_from": float64(0)}, plain["trigger_summary"])
}

// C-01 AC#1 through the API: an old-format stored row loads in the new shape.
func TestAgentsAPI_OldFormatRowLoads(t *testing.T) {
	e := newContractEnv(t)
	src := e.createAgent(t, "Src", map[string]any{"cron": []string{}})
	legacy := entities.Agent{Name: "Legacy", Triggers: datatypes.JSON(fmt.Sprintf(`{"cron":["0 * * * *"],"events":["position.closed"],"chain_from":[%d]}`, int(src["id"].(float64))))}
	require.NoError(t, e.db.Create(&legacy).Error)
	rec := e.do(t, http.MethodGet, fmt.Sprintf("/api/agents/%d", legacy.ID), nil)
	require.Equal(t, http.StatusOK, rec.Code)
	tr := decodeMap(t, rec)["triggers"].(map[string]any)
	assert.Equal(t, []any{map[string]any{"type": "position.closed"}}, tr["events"])
	assert.Equal(t, []any{map[string]any{"agent_id": src["id"], "on": "report"}}, tr["chain_from"])

	// The old forms are also accepted on write.
	req := e.agentRequest("OldClient")
	req["triggers"] = map[string]any{"cron": []string{}, "events": []string{"position.closed"}, "chain_from": []any{src["id"]}}
	rec = e.do(t, http.MethodPost, "/api/agents", req)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	tr = decodeMap(t, rec)["triggers"].(map[string]any)
	assert.Equal(t, "position.closed", tr["events"].([]any)[0].(map[string]any)["type"])
	assert.Equal(t, "report", tr["chain_from"].([]any)[0].(map[string]any)["on"])
}

// C-01 AC#2.
func TestAgentsAPI_TriggerValidation(t *testing.T) {
	e := newContractEnv(t)
	bad := func(name string, triggers map[string]any) map[string]any {
		t.Helper()
		req := e.agentRequest(name)
		req["triggers"] = triggers
		rec := e.do(t, http.MethodPost, "/api/agents", req)
		require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		return decodeMap(t, rec)
	}
	body := bad("E", map[string]any{"events": []any{map[string]any{"type": "position.teleported"}}})
	assert.Equal(t, "validation_error", body["error"])
	assert.Contains(t, body["message"], `unknown event type "position.teleported"`)
	assert.Contains(t, bad("E2", map[string]any{"events": []any{map[string]any{"type": "drawdown"}}})["message"], "threshold_pct")
	assert.Contains(t, bad("E3", map[string]any{"events": []any{map[string]any{"type": "no_signal"}}})["message"], "window_hours")

	rule := func(over map[string]any) []any {
		r := map[string]any{"id": "r", "symbol": "BTCUSDT", "kind": "pct_move", "window_minutes": 60, "threshold_pct": 3}
		for k, v := range over {
			r[k] = v
		}
		return []any{r}
	}
	assert.Contains(t, bad("M1", map[string]any{"market": rule(map[string]any{"symbol": "btcusdt"})})["message"], "uppercase")
	assert.Contains(t, bad("M2", map[string]any{"market": rule(map[string]any{"symbol": ""})})["message"], "symbol")
	assert.Contains(t, bad("M3", map[string]any{"market": rule(map[string]any{"kind": "moon"})})["message"], "kind")
	assert.Contains(t, bad("M4", map[string]any{"market": rule(map[string]any{"window_minutes": 1441})})["message"], "window_minutes")
	assert.Contains(t, bad("M5", map[string]any{"market": rule(map[string]any{"cooldown_minutes": 2})})["message"], "cooldown_minutes")
	assert.Contains(t, bad("M6", map[string]any{"market": rule(map[string]any{"threshold_pct": 0})})["message"], "threshold_pct")
	assert.Contains(t, bad("M7", map[string]any{"market": append(rule(nil), rule(nil)...)})["message"], "used twice")
	assert.Contains(t, bad("M8", map[string]any{"market": rule(map[string]any{"kind": "volatility_spike", "threshold_pct": 0, "multiplier": 0.5})})["message"], "multiplier")
	assert.Contains(t, bad("C0", map[string]any{"chain_from": []any{map[string]any{"agent_id": 9999, "on": "report"}}})["message"], "does not exist")

	// Self-chain and a 2-agent cycle: 400 {"error":"chain_cycle"}.
	a := e.createAgent(t, "Alpha", map[string]any{"cron": []string{}})
	aID := a["id"].(float64)
	self := e.agentRequest("Alpha")
	self["triggers"] = map[string]any{"chain_from": []any{map[string]any{"agent_id": aID, "on": "report"}}}
	rec := e.do(t, http.MethodPut, fmt.Sprintf("/api/agents/%d", int(aID)), self)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, map[string]any{"error": "chain_cycle", "message": "chain cycle: Alpha → Alpha"}, decodeMap(t, rec))

	b := e.createAgent(t, "Beta", map[string]any{"chain_from": []any{map[string]any{"agent_id": aID, "on": "report"}}}) // A -> B
	cyc := e.agentRequest("Alpha")
	cyc["triggers"] = map[string]any{"chain_from": []any{map[string]any{"agent_id": b["id"], "on": "success"}}} // B -> A
	rec = e.do(t, http.MethodPut, fmt.Sprintf("/api/agents/%d", int(aID)), cyc)
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	assert.Equal(t, map[string]any{"error": "chain_cycle", "message": "chain cycle: Alpha → Beta → Alpha"}, decodeMap(t, rec))
}

func TestAgentsAPI_RunsCarryChainFieldsAndTriggerDetail(t *testing.T) {
	e := newContractEnv(t)
	var def entities.Agent
	require.NoError(t, e.db.Where("is_default = ?", true).First(&def).Error)
	runs := repoagent.NewGormRepository(e.db)
	parent := uint(41)
	detail := `{"source_agent_id":2,"source_run_id":41,"report_ids":[7],"on":"report","chain_path":[2]}`
	_, err := runs.CreateRun(context.Background(), entities.AgentRun{AgentID: &def.ID, Trigger: "chain", Status: entities.AgentRunOK,
		StartedAt: time.Now(), ChainDepth: 1, ParentRunID: &parent, TriggerDetail: datatypes.JSON(detail)})
	require.NoError(t, err)
	_, err = runs.CreateRun(context.Background(), entities.AgentRun{AgentID: &def.ID, Trigger: "cron", Status: entities.AgentRunOK, StartedAt: time.Now()})
	require.NoError(t, err)

	rec := e.do(t, http.MethodGet, fmt.Sprintf("/api/agents/%d/runs", def.ID), nil)
	require.Equal(t, http.StatusOK, rec.Code)
	var list []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list))
	require.Len(t, list, 2)
	byTrigger := map[string]map[string]any{}
	for _, r := range list {
		byTrigger[r["trigger"].(string)] = r
	}
	chain := byTrigger["chain"]
	assert.Equal(t, float64(1), chain["chain_depth"])
	assert.Equal(t, float64(41), chain["parent_run_id"])
	assert.Equal(t, float64(7), chain["trigger_detail"].(map[string]any)["report_ids"].([]any)[0])
	cron := byTrigger["cron"]
	assert.Contains(t, cron, "parent_run_id")
	assert.Nil(t, cron["parent_run_id"])
	assert.Equal(t, float64(0), cron["chain_depth"])
}

type staticStatus struct{ snap *marketstatus.Snapshot }

func (s staticStatus) Read(context.Context) (*marketstatus.Snapshot, error) { return s.snap, nil }

func TestAgentsAPI_MarketSymbols(t *testing.T) {
	e := newContractEnv(t)
	rec := e.do(t, http.MethodGet, "/api/agents/market-symbols", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"symbols":[],"runtime_seen_at":null}`, rec.Body.String(), "no runtime snapshot")

	price := 61000.5
	at := time.Date(2026, 5, 1, 12, 1, 0, 0, time.UTC)
	e.platform.SetMarketStatusReader(staticStatus{snap: &marketstatus.Snapshot{
		UpdatedAt: time.Date(2026, 5, 1, 12, 1, 30, 0, time.UTC),
		Symbols:   []marketstatus.SymbolStatus{{Symbol: "BTCUSDT", Watchers: 2, LastPrice: &price, LastCandleAt: &at}, {Symbol: "ETHUSDT", Watchers: 1}},
	}})
	rec = e.do(t, http.MethodGet, "/api/agents/market-symbols", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"symbols":[
		{"symbol":"BTCUSDT","watchers":2,"last_price":61000.5,"last_candle_at":"2026-05-01T12:01:00Z"},
		{"symbol":"ETHUSDT","watchers":1,"last_price":null,"last_candle_at":null}],
		"runtime_seen_at":"2026-05-01T12:01:30Z"}`, rec.Body.String())
}
