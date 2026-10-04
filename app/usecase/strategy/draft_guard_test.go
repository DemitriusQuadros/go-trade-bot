package usecase_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"go-trade-bot/app/entities"
	usecase "go-trade-bot/app/usecase/strategy"
	"go-trade-bot/app/usecase/strategy/mocks"
	"go-trade-bot/internal/authz"
	"go-trade-bot/internal/customerror"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// Auth-01 §3 draft-only guard.

func friendCtx() context.Context {
	return authz.WithUser(context.Background(), authz.Principal{UserID: 5, Username: "ana", Role: authz.RoleFriend, Caps: authz.RolePreset(authz.RoleFriend)})
}

func adminCtx() context.Context {
	return authz.WithUser(context.Background(), authz.Principal{UserID: 1, Username: "root", Role: authz.RoleAdmin, Caps: authz.RolePreset(authz.RoleAdmin)})
}

func draft(mode string, status entities.StrategyStatus) entities.Strategy {
	return entities.Strategy{
		ID: 9, Name: "S", Description: "d", MonitoredSymbols: []string{"BTCUSDT"}, StrategyName: "grid",
		Mode: mode, Status: status, StrategyConfiguration: entities.StrategyConfiguration{Cycle: 15},
	}
}

func assertForbidden(t *testing.T, err error) {
	t.Helper()
	var ce *customerror.CustomError
	require.True(t, errors.As(err, &ce), "want CustomError, got %v", err)
	assert.Equal(t, http.StatusForbidden, ce.Code)
	assert.Equal(t, "forbidden", ce.ErrorCode)
	assert.Equal(t, "only admins can change non-backtest strategies", ce.Message)
}

func TestDraftGuard_FriendCannotCreateDryrun(t *testing.T) {
	repo := new(mocks.StrategyRepository)
	uc := usecase.NewStrategyUseCase(repo, nil)
	_, err := uc.Save(friendCtx(), draft("dryrun", entities.Testing))
	assertForbidden(t, err)
	_, err = uc.Save(friendCtx(), draft("backtest", entities.Productive))
	assertForbidden(t, err)
	repo.AssertNotCalled(t, "Save", mock.Anything, mock.Anything)
}

func TestDraftGuard_FriendCanCreateBacktestDraft_RecordsCreator(t *testing.T) {
	repo := new(mocks.StrategyRepository)
	worker := new(mocks.StrategyWorker)
	uc := usecase.NewStrategyUseCase(repo, worker)
	repo.On("Save", mock.Anything, mock.MatchedBy(func(s entities.Strategy) bool {
		return s.CreatedByUserID != nil && *s.CreatedByUserID == 5
	})).Return(entities.Strategy{ID: 3}, nil).Once()
	worker.On("EnqueueStrategyTask", mock.Anything).Return(nil).Once()
	_, err := uc.Save(friendCtx(), draft("backtest", entities.Testing))
	require.NoError(t, err)
	repo.AssertExpectations(t)
}

func TestDraftGuard_FriendCannotUpdateDryrun(t *testing.T) {
	repo := new(mocks.StrategyRepository)
	uc := usecase.NewStrategyUseCase(repo, nil)
	repo.On("GetByID", mock.Anything, uint(9)).Return(draft("dryrun", entities.Testing), nil)
	// Even a request that tries to turn it into a backtest draft.
	assertForbidden(t, uc.Update(friendCtx(), draft("backtest", entities.Testing)))
	repo.AssertNotCalled(t, "Update", mock.Anything, mock.Anything)
}

func TestDraftGuard_FriendCannotSetLiveOnBacktestDraft(t *testing.T) {
	repo := new(mocks.StrategyRepository)
	uc := usecase.NewStrategyUseCase(repo, nil)
	repo.On("GetByID", mock.Anything, uint(9)).Return(draft("backtest", entities.Testing), nil)
	assertForbidden(t, uc.Update(friendCtx(), draft("live", entities.Testing)))
	assertForbidden(t, uc.Update(friendCtx(), draft("backtest", entities.Productive)))
	repo.AssertNotCalled(t, "Update", mock.Anything, mock.Anything)
}

func TestDraftGuard_FriendCanUpdateBacktestDraft(t *testing.T) {
	repo := new(mocks.StrategyRepository)
	uc := usecase.NewStrategyUseCase(repo, nil)
	repo.On("GetByID", mock.Anything, uint(9)).Return(draft("backtest", entities.Testing), nil)
	repo.On("Update", mock.Anything, mock.Anything).Return(nil).Once()
	require.NoError(t, uc.Update(friendCtx(), draft("backtest", entities.Disabled)))
	repo.AssertExpectations(t)
}

func TestDraftGuard_FriendCannotDeleteProductive(t *testing.T) {
	repo := new(mocks.StrategyRepository)
	uc := usecase.NewStrategyUseCase(repo, nil)
	repo.On("GetByID", mock.Anything, uint(9)).Return(draft("backtest", entities.Productive), nil)
	assertForbidden(t, uc.Delete(friendCtx(), 9))
	repo.AssertNotCalled(t, "Delete", mock.Anything, mock.Anything)
}

func TestDraftGuard_FriendCannotRevertDryrun(t *testing.T) {
	repo := new(mocks.StrategyRepository)
	uc := usecase.NewStrategyUseCase(repo, nil)
	repo.On("GetByID", mock.Anything, uint(9)).Return(draft("dryrun", entities.Testing), nil)
	assertForbidden(t, uc.RevertScriptVersion(friendCtx(), 9, 1))
	repo.AssertNotCalled(t, "GetScriptVersions", mock.Anything, mock.Anything)
}

func TestDraftGuard_FriendCannotChangeStatusOrMode(t *testing.T) {
	repo := new(mocks.StrategyRepository)
	uc := usecase.NewStrategyUseCase(repo, nil)
	_, err := uc.UpdateStatus(friendCtx(), 9, entities.Disabled)
	assertForbidden(t, err)
	_, err = uc.UpdateMode(friendCtx(), 9, "backtest")
	assertForbidden(t, err)
}

func TestDraftGuard_AdminAndSystemAreNotGuarded(t *testing.T) {
	for name, ctx := range map[string]context.Context{"admin": adminCtx(), "system": context.Background()} {
		t.Run(name, func(t *testing.T) {
			repo := new(mocks.StrategyRepository)
			worker := new(mocks.StrategyWorker)
			uc := usecase.NewStrategyUseCase(repo, worker)

			repo.On("Save", mock.Anything, mock.Anything).Return(entities.Strategy{ID: 3}, nil).Once()
			worker.On("EnqueueStrategyTask", mock.Anything).Return(nil).Once()
			_, err := uc.Save(ctx, draft("dryrun", entities.Testing))
			require.NoError(t, err)

			repo.On("Update", mock.Anything, mock.Anything).Return(nil).Once()
			require.NoError(t, uc.Update(ctx, draft("live", entities.Testing)))
			repo.AssertNotCalled(t, "GetByID", mock.Anything, mock.Anything)

			repo.On("GetByID", mock.Anything, uint(9)).Return(draft("dryrun", entities.Disabled), nil).Once()
			repo.On("CountOpenSignals", mock.Anything, mock.Anything).Return(int64(0), nil).Once()
			repo.On("Delete", mock.Anything, uint(9)).Return(nil).Once()
			require.NoError(t, uc.Delete(ctx, 9))
			repo.AssertExpectations(t)
		})
	}
}
