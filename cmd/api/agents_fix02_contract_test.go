package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"go-trade-bot/app/entities"
	repoagent "go-trade-bot/app/repository/agent"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fix-02 B1: max_iterations is part of the agent request/response contract
// (0 = trigger default, otherwise 4..50).
func TestAgentsAPI_MaxIterations(t *testing.T) {
	e := newContractEnv(t)

	rec := e.do(t, http.MethodPost, "/api/agents", e.agentRequest("Defaults"))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	got := decodeMap(t, rec)
	assert.Equal(t, float64(0), got["max_iterations"], "omitted = 0 = trigger default")

	req := e.agentRequest("Analyst")
	req["max_iterations"] = 30
	rec = e.do(t, http.MethodPost, "/api/agents", req)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	got = decodeMap(t, rec)
	assert.Equal(t, float64(30), got["max_iterations"])
	id := int(got["id"].(float64))

	req["max_iterations"] = 50
	rec = e.do(t, http.MethodPut, fmt.Sprintf("/api/agents/%d", id), req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, float64(50), decodeMap(t, rec)["max_iterations"])

	rec = e.do(t, http.MethodGet, fmt.Sprintf("/api/agents/%d", id), nil)
	assert.Equal(t, float64(50), decodeMap(t, rec)["max_iterations"])

	for _, bad := range []int{-1, 1, 3, 51, 1000} {
		req["max_iterations"] = bad
		rec = e.do(t, http.MethodPut, fmt.Sprintf("/api/agents/%d", id), req)
		assert.Equal(t, http.StatusBadRequest, rec.Code, "max_iterations=%d", bad)
		assert.Contains(t, decodeMap(t, rec)["message"], "max_iterations")
	}
	for _, ok := range []int{0, 4} {
		req["max_iterations"] = ok
		rec = e.do(t, http.MethodPut, fmt.Sprintf("/api/agents/%d", id), req)
		assert.Equal(t, http.StatusOK, rec.Code, "max_iterations=%d: %s", ok, rec.Body.String())
	}
}

// fix-02 B1: runs expose hit_iteration_cap.
func TestAgentsAPI_RunsExposeHitIterationCap(t *testing.T) {
	e := newContractEnv(t)
	var def entities.Agent
	require.NoError(t, e.db.Where("is_default = ?", true).First(&def).Error)
	runs := repoagent.NewGormRepository(e.db)
	_, err := runs.CreateRun(context.Background(), entities.AgentRun{AgentID: &def.ID, Trigger: "cron", Status: entities.AgentRunOK})
	require.NoError(t, err)
	_, err = runs.CreateRun(context.Background(), entities.AgentRun{AgentID: &def.ID, Trigger: "cron", Status: entities.AgentRunOK, HitIterationCap: true})
	require.NoError(t, err)

	rec := e.do(t, http.MethodGet, fmt.Sprintf("/api/agents/%d/runs", def.ID), nil)
	require.Equal(t, http.StatusOK, rec.Code)
	var list []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list))
	require.Len(t, list, 2)
	seen := map[bool]int{}
	for _, r := range list {
		v, ok := r["hit_iteration_cap"].(bool)
		require.True(t, ok, "hit_iteration_cap is always present: %v", r)
		seen[v]++
	}
	assert.Equal(t, map[bool]int{true: 1, false: 1}, seen)
}

// fix-02 B4: Migrate seeds the empty house-rules row, idempotently.
func TestMigrate_SeedsEmptyInstruction(t *testing.T) {
	e := newContractEnv(t)
	require.NoError(t, Migrate(e.db))
	var rows []entities.AgentInstruction
	require.NoError(t, e.db.Find(&rows).Error)
	require.Len(t, rows, 1)
	assert.Equal(t, uint(1), rows[0].ID)
	assert.Equal(t, "", rows[0].Content)
}
