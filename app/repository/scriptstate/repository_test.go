package scriptstate_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"go-trade-bot/app/entities"
	repository "go-trade-bot/app/repository/scriptstate"

	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newTestRepo(t *testing.T) (repository.Repository, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&entities.ScriptState{}))
	return repository.NewRepository(db), db
}

func TestScriptStateRepository_GetNotFound(t *testing.T) {
	repo, _ := newTestRepo(t)
	_, err := repo.Get(context.Background(), 7, "BTCUSDT")
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestScriptStateRepository_UpsertRoundTrip(t *testing.T) {
	repo, _ := newTestRepo(t)
	ctx := context.Background()

	require.NoError(t, repo.Upsert(ctx, entities.ScriptState{
		StrategyID: 7,
		Symbol:     "BTCUSDT",
		StateJSON:  datatypes.JSON(`{"ladder_level":2}`),
	}))

	row, err := repo.Get(ctx, 7, "BTCUSDT")
	require.NoError(t, err)
	require.JSONEq(t, `{"ladder_level":2}`, string(row.StateJSON))
}

func TestScriptStateRepository_UpsertKeepsSingleRow(t *testing.T) {
	repo, db := newTestRepo(t)
	ctx := context.Background()

	require.NoError(t, repo.Upsert(ctx, entities.ScriptState{StrategyID: 7, Symbol: "BTCUSDT", StateJSON: datatypes.JSON(`{"n":1}`)}))
	require.NoError(t, repo.Upsert(ctx, entities.ScriptState{StrategyID: 7, Symbol: "BTCUSDT", StateJSON: datatypes.JSON(`{"n":2}`)}))

	var count int64
	require.NoError(t, db.Model(&entities.ScriptState{}).
		Where("strategy_id = ? AND symbol = ?", 7, "BTCUSDT").Count(&count).Error)
	require.Equal(t, int64(1), count)

	row, err := repo.Get(ctx, 7, "BTCUSDT")
	require.NoError(t, err)
	require.JSONEq(t, `{"n":2}`, string(row.StateJSON))
}

func TestScriptStateRepository_TwoSymbolsIndependent(t *testing.T) {
	repo, db := newTestRepo(t)
	ctx := context.Background()

	require.NoError(t, repo.Upsert(ctx, entities.ScriptState{StrategyID: 7, Symbol: "BTCUSDT", StateJSON: datatypes.JSON(`{"s":"btc"}`)}))
	require.NoError(t, repo.Upsert(ctx, entities.ScriptState{StrategyID: 7, Symbol: "ETHUSDT", StateJSON: datatypes.JSON(`{"s":"eth"}`)}))

	var count int64
	require.NoError(t, db.Model(&entities.ScriptState{}).Where("strategy_id = ?", 7).Count(&count).Error)
	require.Equal(t, int64(2), count)

	btc, err := repo.Get(ctx, 7, "BTCUSDT")
	require.NoError(t, err)
	require.JSONEq(t, `{"s":"btc"}`, string(btc.StateJSON))

	eth, err := repo.Get(ctx, 7, "ETHUSDT")
	require.NoError(t, err)
	require.JSONEq(t, `{"s":"eth"}`, string(eth.StateJSON))
}

func TestScriptStateRepository_ConcurrentSaveSingleRow(t *testing.T) {
	// Shared-cache in-memory DB with a single connection so concurrent
	// writers serialize on the same database file rather than each getting a
	// private :memory: instance.
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&entities.ScriptState{}))
	repo := repository.NewRepository(db)

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_ = repo.Upsert(context.Background(), entities.ScriptState{
				StrategyID: 7,
				Symbol:     "BTCUSDT",
				StateJSON:  datatypes.JSON(fmt.Sprintf(`{"n":%d}`, n)),
			})
		}(i)
	}
	wg.Wait()

	var count int64
	require.NoError(t, db.Model(&entities.ScriptState{}).
		Where("strategy_id = ? AND symbol = ?", 7, "BTCUSDT").Count(&count).Error)
	require.Equal(t, int64(1), count)
}
