package repository_test

import (
	"context"
	"testing"

	"go-trade-bot/app/entities"
	repository "go-trade-bot/app/repository/strategy"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func replaceDB(t *testing.T) (*gorm.DB, repository.StrategyRepository, entities.Strategy) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&entities.Strategy{}, &entities.Signal{}, &entities.Order{}, &entities.ScriptVersion{}, &entities.ScriptState{}))
	s := entities.Strategy{Name: "Live", StrategyName: "script", ScriptSource: "old", Mode: "live", Status: entities.Productive}
	require.NoError(t, db.Create(&s).Error)
	require.NoError(t, db.Create(&entities.ScriptState{StrategyID: s.ID, Symbol: "BTCUSDT", StateJSON: datatypes.JSON(`{"n":1}`)}).Error)
	return db, repository.NewStrategyRepository(db), s
}

// B-01 §5 step 4: only script_source changes (never mode/status), a
// ScriptVersion is written and ScriptState is cleared when flat.
func TestReplaceScriptSource_WritesSourceOnly(t *testing.T) {
	db, repo, s := replaceDB(t)
	res, err := repo.ReplaceScriptSource(context.Background(), s.ID, "old", "new", true)
	require.NoError(t, err)
	assert.True(t, res.StateCleared)

	got, err := repo.GetByID(context.Background(), s.ID)
	require.NoError(t, err)
	assert.Equal(t, "new", got.ScriptSource)
	assert.Equal(t, "live", got.Mode)
	assert.Equal(t, entities.Productive, got.Status)
	assert.Equal(t, "Live", got.Name)

	var versions []entities.ScriptVersion
	require.NoError(t, db.Where("strategy_id = ?", s.ID).Find(&versions).Error)
	require.Len(t, versions, 1)
	assert.Equal(t, "new", versions[0].Source)
	var states int64
	db.Model(&entities.ScriptState{}).Where("strategy_id = ?", s.ID).Count(&states)
	assert.Zero(t, states)
}

func TestReplaceScriptSource_RefusesOpenPositionWhenFlatRequired(t *testing.T) {
	db, repo, s := replaceDB(t)
	require.NoError(t, db.Create(&entities.Signal{StrategyID: s.ID, Symbol: "BTCUSDT", Status: entities.Open}).Error)

	_, err := repo.ReplaceScriptSource(context.Background(), s.ID, "old", "new", true)
	assert.ErrorIs(t, err, repository.ErrOpenPosition)
	got, _ := repo.GetByID(context.Background(), s.ID)
	assert.Equal(t, "old", got.ScriptSource)
	var states int64
	db.Model(&entities.ScriptState{}).Where("strategy_id = ?", s.ID).Count(&states)
	assert.EqualValues(t, 1, states)

	// Not requiring flat (non-live gate_failed_change): writes, keeps state.
	res, err := repo.ReplaceScriptSource(context.Background(), s.ID, "old", "new", false)
	require.NoError(t, err)
	assert.False(t, res.StateCleared)
	db.Model(&entities.ScriptState{}).Where("strategy_id = ?", s.ID).Count(&states)
	assert.EqualValues(t, 1, states)
}

func TestReplaceScriptSource_RefusesChangedBase(t *testing.T) {
	db, repo, s := replaceDB(t)
	_, err := repo.ReplaceScriptSource(context.Background(), s.ID, "something else", "new", true)
	assert.ErrorIs(t, err, repository.ErrSourceChanged)
	got, _ := repo.GetByID(context.Background(), s.ID)
	assert.Equal(t, "old", got.ScriptSource)
	var versions int64
	db.Model(&entities.ScriptVersion{}).Count(&versions)
	assert.Zero(t, versions)
}
