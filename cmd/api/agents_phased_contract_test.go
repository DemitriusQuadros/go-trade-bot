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
	"gorm.io/datatypes"
)

// Phase D-01 §2/§3: GET /api/agents/{id}/runs uses the same run DTO as
// /api/agent/runs - tool calls carry `refs` and input_summary is the
// operator's raw text.
func TestAgentsAPI_RunsCarryRefsAndCleanInput(t *testing.T) {
	e := newContractEnv(t)
	var def entities.Agent
	require.NoError(t, e.db.Where("is_default = ?", true).First(&def).Error)
	sid := e.strategy.ID
	calls := `[{"tool":"create_challenger","result":"{\"challenger_strategy_id\":12,\"champion_strategy_id\":5,\"created\":true}"},{"tool":"notify","error":"boom"}]`
	_, err := repoagent.NewGormRepository(e.db).CreateRun(context.Background(), entities.AgentRun{
		AgentID: &def.ID, Trigger: "chat_ui", Status: entities.AgentRunOK, StrategyID: &sid,
		InputSummary:  fmt.Sprintf("[Context: the operator currently has strategy #%d open in the workbench editor. Assume questions refer to it unless stated otherwise.]\n\nhello", sid),
		ToolCallsJSON: datatypes.JSON(calls),
	})
	require.NoError(t, err)

	rec := e.do(t, http.MethodGet, fmt.Sprintf("/api/agents/%d/runs", def.ID), nil)
	require.Equal(t, http.StatusOK, rec.Code)
	var list []struct {
		InputSummary string `json:"input_summary"`
		ToolCalls    []struct {
			Refs []map[string]any `json:"refs"`
		} `json:"tool_calls"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list))
	require.Len(t, list, 1)
	assert.Equal(t, "hello", list[0].InputSummary)
	require.Len(t, list[0].ToolCalls, 2)
	assert.Equal(t, []map[string]any{
		{"kind": "strategy", "id": float64(12), "role": "challenger"},
		{"kind": "strategy", "id": float64(5), "role": "champion"},
	}, list[0].ToolCalls[0].Refs)
	require.NotNil(t, list[0].ToolCalls[1].Refs)
	assert.Empty(t, list[0].ToolCalls[1].Refs)
}
