package proposal_test

import (
	"context"
	"testing"

	"go-trade-bot/app/entities"
	"go-trade-bot/app/usecase/proposal"
	"go-trade-bot/internal/authz"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeUserNames map[uint]string

func (f fakeUserNames) UserNames(context.Context) (map[uint]string, error) { return f, nil }

// Auth-01 §7: approve/reject record DecidedByUserID; views carry the name.
func TestDecidedBy(t *testing.T) {
	e := newEnv(t)
	e.uc.SetUserNamer(fakeUserNames{42: "Ana"})
	ctx := authz.WithUser(context.Background(), authz.Principal{UserID: 42, Username: "ana", Caps: authz.RolePreset(authz.RoleFriend)})

	p := e.promotion(t)
	v, err := e.uc.Approve(ctx, p.ID, "ok")
	require.NoError(t, err)
	require.NotNil(t, v.Proposal.DecidedByUserID)
	assert.Equal(t, uint(42), *v.Proposal.DecidedByUserID)
	assert.Equal(t, "Ana", v.DecidedByName)

	list, err := e.uc.List(context.Background(), proposal.ListFilter{})
	require.NoError(t, err)
	require.NotEmpty(t, list)
	assert.Equal(t, "Ana", list[0].DecidedByName)

	// The service token (no user id) leaves it nil.
	e.db.Model(&entities.StrategyChangeProposal{}).Where("id = ?", p.ID).Update("status", entities.ProposalPending)
	e.db.Model(&entities.StrategyChangeProposal{}).Where("id = ?", p.ID).Update("decided_by_user_id", nil)
	v, err = e.uc.Reject(authz.WithUser(context.Background(), authz.ServicePrincipal()), p.ID, "")
	require.NoError(t, err)
	assert.Nil(t, v.Proposal.DecidedByUserID)
	assert.Empty(t, v.DecidedByName)
}
