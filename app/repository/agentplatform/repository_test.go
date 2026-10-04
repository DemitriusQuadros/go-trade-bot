package agentplatform_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"go-trade-bot/app/entities"
	"go-trade-bot/app/repository/agentplatform"
	"go-trade-bot/internal/customerror"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// PlatformEntities is every table the agents platform adds.
var platformEntities = []any{
	&entities.Agent{},
	&entities.AgentStrategyBinding{},
	&entities.StrategyMemoryEntry{},
	&entities.AgentReport{},
	&entities.WebhookTarget{},
	&entities.AgentUsage{},
}

func setupDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	all := append([]any{&entities.Strategy{}, &entities.ScriptVersion{}, &entities.BacktestRun{}, &entities.Signal{}, &entities.Order{}, &entities.AgentRun{}}, platformEntities...)
	require.NoError(t, db.AutoMigrate(all...))
	return db
}

func statusOf(err error) int {
	var ce *customerror.CustomError
	if errors.As(err, &ce) {
		return ce.Code
	}
	return 0
}

// A-01 AC#1 (repository half): EnsureDefaultAgent is idempotent.
func TestEnsureDefaultAgent_Idempotent(t *testing.T) {
	repo := agentplatform.NewGormRepository(setupDB(t))
	ctx := context.Background()

	first, err := repo.EnsureDefaultAgent(ctx)
	require.NoError(t, err)
	second, err := repo.EnsureDefaultAgent(ctx)
	require.NoError(t, err)
	assert.Equal(t, first.ID, second.ID)

	agents, err := repo.ListAgents(ctx)
	require.NoError(t, err)
	require.Len(t, agents, 1)
	assert.True(t, agents[0].IsDefault)
	assert.Equal(t, entities.DefaultAgentName, agents[0].Name)
	assert.ElementsMatch(t, agentplatform.DefaultAgentPermissions(), []string(agents[0].Permissions))
	assert.Empty(t, agents[0].ParsedTriggers().Cron)
}

func TestAgentCRUD_AndDefaultCannotBeDeleted(t *testing.T) {
	repo := agentplatform.NewGormRepository(setupDB(t))
	ctx := context.Background()
	def, err := repo.EnsureDefaultAgent(ctx)
	require.NoError(t, err)

	a, err := repo.CreateAgent(ctx, entities.Agent{
		Name: "Risk Monitor", Goal: "watch risk", Permissions: []string{"read"},
		Triggers:         agentplatform.MarshalTriggers(entities.AgentTriggers{Cron: []string{"0 * * * *"}}),
		WebhookTargetIDs: []uint{4}, DailyBudgetUSD: 2.5,
	})
	require.NoError(t, err)
	require.NotZero(t, a.ID)

	got, err := repo.GetAgent(ctx, a.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{"0 * * * *"}, got.ParsedTriggers().Cron)
	assert.Equal(t, datatypes.JSONSlice[uint]{4}, got.WebhookTargetIDs)

	got.Goal = "updated"
	updated, err := repo.UpdateAgent(ctx, got)
	require.NoError(t, err)
	assert.Equal(t, "updated", updated.Goal)

	paused, err := repo.SetAgentPaused(ctx, a.ID, true)
	require.NoError(t, err)
	assert.True(t, paused.Paused)

	_, err = repo.CreateAgent(ctx, entities.Agent{Name: "Risk Monitor"})
	assert.Error(t, err, "name is unique")

	assert.Equal(t, http.StatusConflict, statusOf(repo.DeleteAgent(ctx, def.ID)))
	require.NoError(t, repo.SetBindings(ctx, a.ID, []uint{1, 2}))
	require.NoError(t, repo.DeleteAgent(ctx, a.ID))
	_, err = repo.GetAgent(ctx, a.ID)
	assert.Equal(t, http.StatusNotFound, statusOf(err))
	bindings, err := repo.ListBindingsByAgent(ctx, a.ID)
	require.NoError(t, err)
	assert.Empty(t, bindings, "delete removes the agent's bindings")
}

func TestBindings_ReplaceAll(t *testing.T) {
	repo := agentplatform.NewGormRepository(setupDB(t))
	ctx := context.Background()
	a, err := repo.CreateAgent(ctx, entities.Agent{Name: "A"})
	require.NoError(t, err)
	b, err := repo.CreateAgent(ctx, entities.Agent{Name: "B"})
	require.NoError(t, err)

	require.NoError(t, repo.SetBindings(ctx, a.ID, []uint{3, 4, 3}))
	require.NoError(t, repo.SetBindings(ctx, b.ID, []uint{4}))
	require.NoError(t, repo.SetBindings(ctx, a.ID, []uint{4, 5}))

	bindings, err := repo.ListBindingsByAgent(ctx, a.ID)
	require.NoError(t, err)
	var ids []uint
	for _, bd := range bindings {
		ids = append(ids, bd.StrategyID)
	}
	assert.Equal(t, []uint{4, 5}, ids)

	agents, err := repo.ListAgentsByStrategy(ctx, 4)
	require.NoError(t, err)
	assert.Len(t, agents, 2)
	agents, err = repo.ListAgentsByStrategy(ctx, 3)
	require.NoError(t, err)
	assert.Empty(t, agents)
}

func TestMemory_NewestFirstWithKindsAndCursor(t *testing.T) {
	repo := agentplatform.NewGormRepository(setupDB(t))
	ctx := context.Background()
	agentID := uint(9)
	for i, kind := range []entities.MemoryKind{entities.MemoryJournal, entities.MemoryChatUser, entities.MemoryFinding, entities.MemoryJournal} {
		_, err := repo.AppendMemory(ctx, entities.StrategyMemoryEntry{StrategyID: 3, AuthorAgentID: &agentID, Kind: kind, Content: string(rune('a' + i))})
		require.NoError(t, err)
	}
	_, err := repo.AppendMemory(ctx, entities.StrategyMemoryEntry{StrategyID: 4, Kind: entities.MemoryJournal, Content: "other"})
	require.NoError(t, err)

	all, err := repo.ListMemory(ctx, 3, nil, 0, nil)
	require.NoError(t, err)
	require.Len(t, all, 4)
	assert.Equal(t, "d", all[0].Content)
	assert.Equal(t, "a", all[3].Content)

	journals, err := repo.ListMemory(ctx, 3, []entities.MemoryKind{entities.MemoryJournal}, 10, nil)
	require.NoError(t, err)
	assert.Len(t, journals, 2)

	older, err := repo.ListMemory(ctx, 3, nil, 2, &all[1].ID)
	require.NoError(t, err)
	require.Len(t, older, 2)
	assert.Equal(t, "b", older[0].Content)
}

func TestReports_FiltersIncludingStrategyContainment(t *testing.T) {
	repo := agentplatform.NewGormRepository(setupDB(t))
	ctx := context.Background()
	mk := func(agentID uint, sev entities.ReportSeverity, sids ...uint) entities.AgentReport {
		r, err := repo.CreateReport(ctx, entities.AgentReport{AgentID: agentID, Severity: sev, StrategyIDs: sids, Title: "t", RenderedHTML: "<html>big</html>"})
		require.NoError(t, err)
		return r
	}
	r1 := mk(1, entities.SeverityInfo, 3, 4)
	mk(1, entities.SeverityCritical, 4)
	r3 := mk(2, entities.SeverityInfo, 3)
	mk(2, entities.SeverityWarning, 13)

	sid := uint(3)
	got, err := repo.ListReports(ctx, agentplatform.ReportFilter{StrategyID: &sid})
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, r3.ID, got[0].ID)
	assert.Equal(t, r1.ID, got[1].ID)
	assert.Empty(t, got[0].RenderedHTML, "list omits rendered HTML")

	agentID := uint(1)
	got, err = repo.ListReports(ctx, agentplatform.ReportFilter{AgentID: &agentID, Severity: "critical"})
	require.NoError(t, err)
	require.Len(t, got, 1)

	before := r3.ID
	got, err = repo.ListReports(ctx, agentplatform.ReportFilter{StrategyID: &sid, BeforeID: &before, Limit: 5})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, r1.ID, got[0].ID)

	full, err := repo.GetReport(ctx, r1.ID)
	require.NoError(t, err)
	assert.Equal(t, "<html>big</html>", full.RenderedHTML)
	assert.Equal(t, datatypes.JSONSlice[uint]{3, 4}, full.StrategyIDs)
}

func TestWebhookTargets_CRUD(t *testing.T) {
	repo := agentplatform.NewGormRepository(setupDB(t))
	ctx := context.Background()
	a, err := repo.CreateWebhookTarget(ctx, entities.WebhookTarget{Name: "a", Kind: entities.WebhookGeneric, URL: "https://x", Enabled: true})
	require.NoError(t, err)
	b, err := repo.CreateWebhookTarget(ctx, entities.WebhookTarget{Name: "b", Kind: entities.WebhookTelegram, Secret: "tok", ChatID: "1"})
	require.NoError(t, err)

	b.Enabled = true
	b, err = repo.UpdateWebhookTarget(ctx, b)
	require.NoError(t, err)
	assert.True(t, b.Enabled)

	list, err := repo.ListWebhookTargetsByIDs(ctx, []uint{b.ID, 999})
	require.NoError(t, err)
	require.Len(t, list, 1)
	empty, err := repo.ListWebhookTargetsByIDs(ctx, nil)
	require.NoError(t, err)
	assert.Empty(t, empty)

	require.NoError(t, repo.DeleteWebhookTarget(ctx, a.ID))
	assert.Equal(t, http.StatusNotFound, statusOf(repo.DeleteWebhookTarget(ctx, a.ID)))
	all, err := repo.ListWebhookTargets(ctx)
	require.NoError(t, err)
	assert.Len(t, all, 1)
}

func TestUsage_UpsertAccumulatesAndRunsSeparately(t *testing.T) {
	repo := agentplatform.NewGormRepository(setupDB(t))
	ctx := context.Background()
	now := time.Date(2026, 9, 27, 15, 30, 0, 0, time.UTC)

	require.NoError(t, repo.AddUsage(ctx, 1, now, 1000, 500, 0.5))
	require.NoError(t, repo.AddUsage(ctx, 1, now.Add(time.Hour), 10, 5, 0.25))
	require.NoError(t, repo.IncRuns(ctx, 1, now))
	require.NoError(t, repo.AddUsage(ctx, 1, now.AddDate(0, 0, -1), 1, 1, 1))
	require.NoError(t, repo.AddUsage(ctx, 2, now, 7, 7, 7))

	u, err := repo.GetUsage(ctx, 1, now)
	require.NoError(t, err)
	assert.Equal(t, int64(1010), u.InputTokens)
	assert.Equal(t, int64(505), u.OutputTokens)
	assert.InDelta(t, 0.75, u.CostUSD, 1e-9)
	assert.Equal(t, 1, u.Runs)

	none, err := repo.GetUsage(ctx, 1, now.AddDate(0, 0, -10))
	require.NoError(t, err)
	assert.Zero(t, none.CostUSD)

	days, err := repo.ListUsage(ctx, 1, now.AddDate(0, 0, -30), now)
	require.NoError(t, err)
	require.Len(t, days, 2)
	assert.True(t, days[0].Day.Before(days[1].Day))
}

func TestUsage_MarkBudgetAlertSentExactlyOnce(t *testing.T) {
	repo := agentplatform.NewGormRepository(setupDB(t))
	ctx := context.Background()
	now := time.Now()

	won, err := repo.MarkBudgetAlertSent(ctx, 1, now)
	require.NoError(t, err)
	assert.True(t, won)
	won, err = repo.MarkBudgetAlertSent(ctx, 1, now)
	require.NoError(t, err)
	assert.False(t, won)
	won, err = repo.MarkBudgetAlertSent(ctx, 1, now.AddDate(0, 0, 1))
	require.NoError(t, err)
	assert.True(t, won, "a new day re-arms the alert")
}

// Phase B: conditional usage counters never exceed the cap; AddBinding is
// idempotent.
func TestPhaseB_TryIncCountersAndAddBinding(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&entities.AgentUsage{}, &entities.AgentStrategyBinding{}))
	r := agentplatform.NewGormRepository(db)
	ctx := context.Background()
	now := time.Now()

	for i := 0; i < 2; i++ {
		ok, err := r.TryIncAutoDeploys(ctx, 1, now, 2)
		require.NoError(t, err)
		assert.True(t, ok)
	}
	ok, err := r.TryIncAutoDeploys(ctx, 1, now, 2)
	require.NoError(t, err)
	assert.False(t, ok)
	ok, _ = r.TryIncAutoDeploys(ctx, 2, now, 0)
	assert.False(t, ok, "max 0 = disabled")
	ok, _ = r.TryIncStrategiesCreated(ctx, 1, now, 3)
	assert.True(t, ok)
	u, err := r.GetUsage(ctx, 1, now)
	require.NoError(t, err)
	assert.Equal(t, 2, u.AutoDeploys)
	assert.Equal(t, 1, u.StrategiesCreated)

	require.NoError(t, r.AddBinding(ctx, 1, 5))
	require.NoError(t, r.AddBinding(ctx, 1, 5))
	bs, err := r.ListBindingsByAgent(ctx, 1)
	require.NoError(t, err)
	assert.Len(t, bs, 1)
}
