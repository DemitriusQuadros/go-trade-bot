package agent_test

import (
	"context"
	"testing"
	"time"

	"go-trade-bot/app/entities"
	"go-trade-bot/app/repository/agent"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMarkOrphanedRuns(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	now := time.Now()
	old := now.Add(-2 * time.Hour)

	mk := func(status entities.AgentRunStatus, started time.Time, finished time.Time) uint {
		r := entities.AgentRun{Status: status, StartedAt: started, FinishedAt: finished}
		require.NoError(t, db.Create(&r).Error)
		return r.ID
	}
	orphanRunning := mk(entities.AgentRunRunning, old, time.Time{})
	legacyZombie := mk(entities.AgentRunOK, old, time.Time{})
	liveRunning := mk(entities.AgentRunRunning, now, time.Time{}) // may belong to another replica
	finishedOK := mk(entities.AgentRunOK, old, old.Add(time.Minute))
	finishedErr := mk(entities.AgentRunError, old, old.Add(time.Minute))

	n, err := agent.MarkOrphanedRuns(ctx, db, now.Add(-time.Hour))
	require.NoError(t, err)
	assert.EqualValues(t, 2, n)

	status := func(id uint) entities.AgentRunStatus {
		var r entities.AgentRun
		require.NoError(t, db.First(&r, id).Error)
		return r.Status
	}
	assert.Equal(t, entities.AgentRunError, status(orphanRunning))
	assert.Equal(t, entities.AgentRunError, status(legacyZombie))
	assert.Equal(t, entities.AgentRunRunning, status(liveRunning))
	assert.Equal(t, entities.AgentRunOK, status(finishedOK))
	assert.Equal(t, entities.AgentRunError, status(finishedErr))
}
