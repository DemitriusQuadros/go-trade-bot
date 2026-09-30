package agent_test

import (
	"context"
	"testing"

	"go-trade-bot/app/entities"
	"go-trade-bot/app/repository/agent"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Auth-01 §6: runs filtered by AgentRun.UserID.
func TestListRunsFiltered_Owner(t *testing.T) {
	db := setupTestDB(t)
	repo := agent.NewGormRepository(db)
	ctx := context.Background()
	u1, u2 := uint(1), uint(2)
	for _, r := range []entities.AgentRun{{Trigger: "chat_ui", UserID: &u1}, {Trigger: "chat_ui", UserID: &u2}, {Trigger: "chat_ui"}} {
		_, err := repo.CreateRun(ctx, r)
		require.NoError(t, err)
	}
	ids := func(f agent.RunFilter) []uint {
		runs, err := repo.ListRunsFiltered(ctx, f)
		require.NoError(t, err)
		out := []uint{}
		for _, r := range runs {
			out = append(out, r.ID)
		}
		return out
	}
	assert.Equal(t, []uint{3, 2, 1}, ids(agent.RunFilter{}))
	assert.Equal(t, []uint{2}, ids(agent.RunFilter{Owner: &agent.OwnerFilter{UserID: 2}}))
	assert.Equal(t, []uint{3, 1}, ids(agent.RunFilter{Owner: &agent.OwnerFilter{UserID: 1, IncludeUnowned: true}}))
	assert.Equal(t, []uint{3}, ids(agent.RunFilter{Owner: &agent.OwnerFilter{UserID: 0}}))
}
