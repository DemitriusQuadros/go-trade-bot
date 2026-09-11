package script

import (
	"context"
	"strings"
	"testing"

	"go-trade-bot/app/entities"

	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// fakeScriptStateRepo is a hand-rolled in-package fake for the unexported
// scriptstateRepo interface (mockery cannot target an unexported,
// package-internal interface, so a small fake is the house-appropriate
// substitute here).
type fakeScriptStateRepo struct {
	getRow   entities.ScriptState
	getErr   error
	upserted *entities.ScriptState
	upsertErr error
}

func (f *fakeScriptStateRepo) Get(_ context.Context, _ uint, _ string) (entities.ScriptState, error) {
	return f.getRow, f.getErr
}

func (f *fakeScriptStateRepo) Upsert(_ context.Context, state entities.ScriptState) error {
	if f.upsertErr != nil {
		return f.upsertErr
	}
	cp := state
	f.upserted = &cp
	return nil
}

func TestScriptStateStore_LoadEmptyOnNotFound(t *testing.T) {
	repo := &fakeScriptStateRepo{getErr: gorm.ErrRecordNotFound}
	store := NewScriptStateStore(repo)

	state, err := store.Load(context.Background(), 7, "BTCUSDT")
	require.NoError(t, err)
	require.NotNil(t, state)
	require.Empty(t, state)
}

func TestScriptStateStore_LoadRoundTrip(t *testing.T) {
	repo := &fakeScriptStateRepo{getRow: entities.ScriptState{StateJSON: datatypes.JSON(`{"ladder_level":2}`)}}
	store := NewScriptStateStore(repo)

	state, err := store.Load(context.Background(), 7, "BTCUSDT")
	require.NoError(t, err)
	require.Equal(t, 2.0, state["ladder_level"])
}

func TestScriptStateStore_LoadMalformedJSONErrors(t *testing.T) {
	repo := &fakeScriptStateRepo{getRow: entities.ScriptState{StateJSON: datatypes.JSON(`not valid json`)}}
	store := NewScriptStateStore(repo)

	_, err := store.Load(context.Background(), 7, "BTCUSDT")
	require.Error(t, err)
}

func TestScriptStateStore_SaveThenUpsertCalled(t *testing.T) {
	repo := &fakeScriptStateRepo{}
	store := NewScriptStateStore(repo)

	require.NoError(t, store.Save(context.Background(), 7, "BTCUSDT", map[string]interface{}{"ladder_level": 2.0}))
	require.NotNil(t, repo.upserted)
	require.Equal(t, uint(7), repo.upserted.StrategyID)
	require.Equal(t, "BTCUSDT", repo.upserted.Symbol)
	require.JSONEq(t, `{"ladder_level":2}`, string(repo.upserted.StateJSON))
}

func TestScriptStateStore_SaveOversizedRejectedBeforeDB(t *testing.T) {
	repo := &fakeScriptStateRepo{}
	store := NewScriptStateStore(repo)

	huge := strings.Repeat("x", maxStateJSONBytes+1)
	err := store.Save(context.Background(), 7, "BTCUSDT", map[string]interface{}{"blob": huge})
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds")
	require.Contains(t, err.Error(), "byte cap")
	// The oversized save must never touch the DB - Upsert was never called.
	require.Nil(t, repo.upserted)
}
