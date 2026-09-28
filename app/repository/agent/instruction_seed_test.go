package agent_test

import (
	"bytes"
	"context"
	"log"
	"testing"

	"go-trade-bot/app/entities"
	"go-trade-bot/app/repository/agent"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// fix-02 B4: a missing house-rules row is empty content and logs nothing
// (First used to log "record not found" on every agent run).
func TestGetInstruction_MissingRowLogsNothing(t *testing.T) {
	var buf bytes.Buffer
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.New(log.New(&buf, "", 0), logger.Config{LogLevel: logger.Warn}),
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&entities.AgentInstruction{}))
	buf.Reset()

	instruction, err := agent.NewGormRepository(db).GetInstruction(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "", instruction.Content)
	assert.NotContains(t, buf.String(), "record not found")
	assert.Empty(t, buf.String())
}

func TestEnsureInstruction_IdempotentAndKeepsContent(t *testing.T) {
	db := setupTestDB(t)
	repo := agent.NewGormRepository(db)
	ctx := context.Background()

	require.NoError(t, repo.EnsureInstruction(ctx))
	require.NoError(t, repo.EnsureInstruction(ctx))
	var count int64
	require.NoError(t, db.Model(&entities.AgentInstruction{}).Count(&count).Error)
	assert.Equal(t, int64(1), count)
	got, err := repo.GetInstruction(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint(1), got.ID)
	assert.Equal(t, "", got.Content)

	_, err = repo.SaveInstruction(ctx, "house rules")
	require.NoError(t, err)
	require.NoError(t, repo.EnsureInstruction(ctx), "re-seeding must not clobber operator content")
	got, err = repo.GetInstruction(ctx)
	require.NoError(t, err)
	assert.Equal(t, "house rules", got.Content)
}

// fix-02 B1: HitIterationCap persists.
func TestUpdateRun_PersistsHitIterationCap(t *testing.T) {
	db := setupTestDB(t)
	repo := agent.NewGormRepository(db)
	ctx := context.Background()
	run, err := repo.CreateRun(ctx, entities.AgentRun{Trigger: "cron", Status: entities.AgentRunOK})
	require.NoError(t, err)
	run.HitIterationCap = true
	require.NoError(t, repo.UpdateRun(ctx, run))
	got, err := repo.GetRun(ctx, run.ID)
	require.NoError(t, err)
	assert.True(t, got.HitIterationCap)
}
