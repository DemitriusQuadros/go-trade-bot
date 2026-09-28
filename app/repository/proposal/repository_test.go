package proposal_test

import (
	"context"
	"testing"
	"time"

	"go-trade-bot/app/entities"
	"go-trade-bot/app/repository/proposal"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newRepo(t *testing.T) *proposal.GormRepository {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&entities.StrategyChangeProposal{}, &entities.DeployGateConfig{}))
	return proposal.NewGormRepository(db)
}

func TestGateConfig_DefaultsSeedAndSave(t *testing.T) {
	r := newRepo(t)
	ctx := context.Background()
	c, err := r.GetGateConfig(ctx)
	require.NoError(t, err)
	assert.Equal(t, entities.DefaultDeployGateConfig().MinTrades, c.MinTrades)

	require.NoError(t, r.EnsureGateConfig(ctx))
	require.NoError(t, r.EnsureGateConfig(ctx)) // idempotent
	c.MinTrades = 42
	saved, err := r.SaveGateConfig(ctx, c)
	require.NoError(t, err)
	assert.Equal(t, 42, saved.MinTrades)
	require.NoError(t, r.EnsureGateConfig(ctx)) // never overwrites
	c, _ = r.GetGateConfig(ctx)
	assert.Equal(t, 42, c.MinTrades)
	assert.Equal(t, 1.10, c.MaxDrawdownRatio)
}

func TestProposals_ListFiltersTransitionsSupersede(t *testing.T) {
	r := newRepo(t)
	ctx := context.Background()
	ch := uint(9)
	a, _ := r.Create(ctx, entities.StrategyChangeProposal{Kind: entities.ProposalPromoteChallenger, TargetStrategyID: 1, ChallengerStrategyID: &ch, AgentID: 3})
	b, _ := r.Create(ctx, entities.StrategyChangeProposal{Kind: entities.ProposalGateFailedChange, TargetStrategyID: 2, AgentID: 4})
	c, _ := r.Create(ctx, entities.StrategyChangeProposal{Kind: entities.ProposalPromoteChallenger, TargetStrategyID: 1, AgentID: 3})
	assert.Equal(t, entities.ProposalPending, a.Status)

	sid := uint(9)
	got, err := r.List(ctx, proposal.Filter{StrategyID: &sid})
	require.NoError(t, err)
	require.Len(t, got, 1, "strategy_id matches the challenger too")
	assert.Equal(t, a.ID, got[0].ID)

	n, err := r.SupersedePending(ctx, 1, c.ID, "superseded by #3")
	require.NoError(t, err)
	assert.EqualValues(t, 1, n)
	cnt, _ := r.CountByStatus(ctx, entities.ProposalPending)
	assert.EqualValues(t, 2, cnt)

	now := time.Now()
	ok, err := r.TransitionStatus(ctx, b.ID, []entities.ProposalStatus{entities.ProposalPending}, entities.ProposalApproved, proposal.Transition{DecidedAt: &now})
	require.NoError(t, err)
	assert.True(t, ok)
	ok, err = r.TransitionStatus(ctx, b.ID, []entities.ProposalStatus{entities.ProposalPending}, entities.ProposalRejected, proposal.Transition{})
	require.NoError(t, err)
	assert.False(t, ok, "compare-and-set: no longer pending")

	got, err = r.List(ctx, proposal.Filter{Statuses: []entities.ProposalStatus{entities.ProposalApproved, entities.ProposalSuperseded}})
	require.NoError(t, err)
	assert.Len(t, got, 2)
	require.NoError(t, r.TouchFlatCheck(ctx, b.ID, now))
	p, _ := r.Get(ctx, b.ID)
	require.NotNil(t, p.LastFlatCheckAt)
	require.NotNil(t, p.DecidedAt)
	_, err = r.Get(ctx, 999)
	assert.Error(t, err)
}
