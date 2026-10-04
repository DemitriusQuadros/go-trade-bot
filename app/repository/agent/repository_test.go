package agent_test

import (
	"context"
	"testing"

	"go-trade-bot/app/entities"
	"go-trade-bot/app/repository/agent"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&entities.AgentInstruction{}, &entities.AgentRun{}))
	return db
}

// TestGetInstruction_NoRowYet is Acceptance Criterion #1: no row -> zero
// value + nil error, never a "not found" error callers must special-case.
func TestGetInstruction_NoRowYet(t *testing.T) {
	db := setupTestDB(t)
	repo := agent.NewGormRepository(db)

	instruction, err := repo.GetInstruction(context.Background())
	require.NoError(t, err)
	assert.Equal(t, entities.AgentInstruction{}, instruction)
}

// TestSaveInstruction_UpsertsSingleton is Acceptance Criterion #2: saving
// twice with different content leaves exactly one row, matching the second
// call's content.
func TestSaveInstruction_UpsertsSingleton(t *testing.T) {
	db := setupTestDB(t)
	repo := agent.NewGormRepository(db)
	ctx := context.Background()

	_, err := repo.SaveInstruction(ctx, "first house rules")
	require.NoError(t, err)
	_, err = repo.SaveInstruction(ctx, "second house rules")
	require.NoError(t, err)

	instruction, err := repo.GetInstruction(ctx)
	require.NoError(t, err)
	assert.Equal(t, "second house rules", instruction.Content)

	var count int64
	require.NoError(t, db.Model(&entities.AgentInstruction{}).Count(&count).Error)
	assert.Equal(t, int64(1), count)
}

// TestCreateRun_PersistsBeforeFinished is Acceptance Criterion #3: a run
// created with Status unset must be retrievable via GetRun before
// FinishedAt is ever set - supports incremental persistence.
func TestCreateRun_PersistsBeforeFinished(t *testing.T) {
	db := setupTestDB(t)
	repo := agent.NewGormRepository(db)
	ctx := context.Background()

	created, err := repo.CreateRun(ctx, entities.AgentRun{
		Provider: "anthropic",
		Model:    "claude-sonnet-5",
		Trigger:  "mcp_tool",
	})
	require.NoError(t, err)
	require.NotZero(t, created.ID)
	assert.NotZero(t, created.StartedAt)
	assert.True(t, created.FinishedAt.IsZero())

	fetched, err := repo.GetRun(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, created.ID, fetched.ID)
	assert.True(t, fetched.FinishedAt.IsZero())
}

// TestListRuns_PreservesNullableStrategyID is Acceptance Criterion #4:
// StrategyID stays correctly typed as *uint (nil for read-only runs, set
// for strategy-affecting runs) across a round trip.
func TestListRuns_PreservesNullableStrategyID(t *testing.T) {
	db := setupTestDB(t)
	repo := agent.NewGormRepository(db)
	ctx := context.Background()

	strategyID := uint(42)
	_, err := repo.CreateRun(ctx, entities.AgentRun{
		Provider:      "anthropic",
		Trigger:       "mcp_tool",
		Status:        entities.AgentRunOK,
		StrategyID:    &strategyID,
		ToolCallsJSON: datatypes.JSON(`[{"tool":"save_strategy_script"}]`),
	})
	require.NoError(t, err)

	_, err = repo.CreateRun(ctx, entities.AgentRun{
		Provider: "anthropic",
		Trigger:  "mcp_tool",
		Status:   entities.AgentRunOK,
	})
	require.NoError(t, err)

	runs, err := repo.ListRuns(ctx, 10, nil)
	require.NoError(t, err)
	require.Len(t, runs, 2)
	// Newest first.
	assert.Nil(t, runs[0].StrategyID)
	require.NotNil(t, runs[1].StrategyID)
	assert.Equal(t, strategyID, *runs[1].StrategyID)
}

// TestUpdateRun_AllowsLargeToolCallsJSON is Acceptance Criterion #6: the
// repository does not truncate or error on a large-but-reasonable
// ToolCallsJSON payload.
func TestUpdateRun_AllowsLargeToolCallsJSON(t *testing.T) {
	db := setupTestDB(t)
	repo := agent.NewGormRepository(db)
	ctx := context.Background()

	created, err := repo.CreateRun(ctx, entities.AgentRun{Provider: "anthropic", Trigger: "mcp_tool"})
	require.NoError(t, err)

	large := make([]byte, 0, 200*1024)
	large = append(large, '[')
	for i := 0; i < 2000; i++ {
		if i > 0 {
			large = append(large, ',')
		}
		large = append(large, []byte(`{"tool":"run_backtest","result":"trimmed summary padding padding padding"}`)...)
	}
	large = append(large, ']')

	created.ToolCallsJSON = datatypes.JSON(large)
	require.NoError(t, repo.UpdateRun(ctx, created))

	fetched, err := repo.GetRun(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, len(large), len(fetched.ToolCallsJSON))
}

func TestListRunsByAgentAndLastRun(t *testing.T) {
	db := setupTestDB(t)
	repo := agent.NewGormRepository(db)
	ctx := context.Background()
	a, b := uint(1), uint(2)
	for i := 0; i < 3; i++ {
		_, err := repo.CreateRun(ctx, entities.AgentRun{AgentID: &a, Trigger: "cron", Status: entities.AgentRunOK})
		require.NoError(t, err)
	}
	_, err := repo.CreateRun(ctx, entities.AgentRun{AgentID: &b, Trigger: "manual"})
	require.NoError(t, err)

	runs, err := repo.ListRunsByAgent(ctx, a, 2, nil)
	require.NoError(t, err)
	require.Len(t, runs, 2)
	assert.Greater(t, runs[0].ID, runs[1].ID)
	older, err := repo.ListRunsByAgent(ctx, a, 10, &runs[1].ID)
	require.NoError(t, err)
	assert.Len(t, older, 1)

	last, err := repo.LastRunByAgent(ctx, a)
	require.NoError(t, err)
	require.NotNil(t, last)
	assert.Equal(t, runs[0].ID, last.ID)
	none, err := repo.LastRunByAgent(ctx, 99)
	require.NoError(t, err)
	assert.Nil(t, none)
}

// TestListRunsFiltered covers every RunFilter field alone and combined
// (Phase D-01 §1).
func TestListRunsFiltered(t *testing.T) {
	db := setupTestDB(t)
	repo := agent.NewGormRepository(db)
	ctx := context.Background()
	u := func(v uint) *uint { return &v }

	type seed struct {
		trigger    string
		strategyID *uint
		agentID    *uint
	}
	seeds := []seed{
		{"chat_ui", u(5), u(1)}, // 1
		{"cron", u(5), u(2)},    // 2
		{"chat_ui", nil, u(1)},  // 3
		{"manual", u(6), u(2)},  // 4
		{"chat_ui", u(5), u(2)}, // 5
		{"mcp_tool", nil, nil},  // 6
	}
	for _, s := range seeds {
		_, err := repo.CreateRun(ctx, entities.AgentRun{Trigger: s.trigger, StrategyID: s.strategyID, AgentID: s.agentID, Status: entities.AgentRunOK})
		require.NoError(t, err)
	}

	ids := func(f agent.RunFilter) []uint {
		runs, err := repo.ListRunsFiltered(ctx, f)
		require.NoError(t, err)
		out := make([]uint, len(runs))
		for i, r := range runs {
			out[i] = r.ID
		}
		return out
	}

	assert.Equal(t, []uint{6, 5, 4, 3, 2, 1}, ids(agent.RunFilter{}), "no filter, newest first")
	assert.Equal(t, []uint{6, 5}, ids(agent.RunFilter{Limit: 2}))
	assert.Equal(t, []uint{5, 2, 1}, ids(agent.RunFilter{StrategyID: u(5)}))
	assert.Equal(t, []uint{6, 3}, ids(agent.RunFilter{NoStrategy: true}))
	assert.Equal(t, []uint{5, 4, 3, 1}, ids(agent.RunFilter{Triggers: []string{"chat_ui", "manual"}}))
	assert.Equal(t, []uint{5, 4, 2}, ids(agent.RunFilter{AgentID: u(2)}))
	assert.Equal(t, []uint{3, 2, 1}, ids(agent.RunFilter{BeforeID: u(4)}))

	// Combinations.
	assert.Equal(t, []uint{5, 1}, ids(agent.RunFilter{StrategyID: u(5), Triggers: []string{"chat_ui"}}))
	assert.Equal(t, []uint{3}, ids(agent.RunFilter{NoStrategy: true, Triggers: []string{"chat_ui"}}))
	assert.Equal(t, []uint{1}, ids(agent.RunFilter{StrategyID: u(5), Triggers: []string{"chat_ui"}, BeforeID: u(5)}))
	assert.Equal(t, []uint{5}, ids(agent.RunFilter{StrategyID: u(5), Triggers: []string{"chat_ui"}, AgentID: u(2)}))
	assert.Equal(t, []uint{2}, ids(agent.RunFilter{AgentID: u(2), BeforeID: u(4), Limit: 1}))
	assert.Empty(t, ids(agent.RunFilter{Triggers: []string{"event"}}))

	// ListRuns stays a thin wrapper with its old semantics.
	runs, err := repo.ListRuns(ctx, 2, u(5))
	require.NoError(t, err)
	require.Len(t, runs, 2)
	assert.Equal(t, uint(5), runs[0].ID)
	assert.Equal(t, uint(2), runs[1].ID)
}
