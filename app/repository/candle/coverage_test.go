package candle_test

import (
	"context"
	"testing"
	"time"

	"go-trade-bot/app/entities"
	"go-trade-bot/app/repository/candle"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedSeries(t *testing.T, repo candle.CandleRepository, symbol, tf string, start time.Time, step time.Duration, n int) {
	t.Helper()
	rows := make([]entities.Candle, n)
	for i := range rows {
		rows[i] = entities.Candle{Symbol: symbol, Timeframe: tf, OpenTime: start.Add(time.Duration(i) * step), Open: 1, High: 1, Low: 1, Close: 1, Volume: 1}
	}
	require.NoError(t, repo.Upsert(context.Background(), rows))
}

func TestCandleRepository_Coverage(t *testing.T) {
	db := setupTestDB(t)
	repo := candle.NewCandleRepository(db)
	ctx := context.Background()

	seedSeries(t, repo, "BTCUSDT", "1h", time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC), time.Hour, 48)
	seedSeries(t, repo, "BTCUSDT", "1d", time.Date(2022, 1, 1, 0, 0, 0, 0, time.UTC), 24*time.Hour, 5)
	seedSeries(t, repo, "ETHUSDT", "5m", time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC), 5*time.Minute, 3)

	cov, err := repo.Coverage(ctx, "BTCUSDT")
	require.NoError(t, err)
	require.Len(t, cov, 2)
	// Ordered by count desc within a symbol.
	assert.Equal(t, candle.Coverage{Symbol: "BTCUSDT", Timeframe: "1h",
		From: time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2021, 1, 2, 23, 0, 0, 0, time.UTC), Count: 48}, cov[0])
	assert.Equal(t, "1d", cov[1].Timeframe)
	assert.Equal(t, time.Date(2022, 1, 5, 0, 0, 0, 0, time.UTC), cov[1].To)
	assert.Equal(t, int64(5), cov[1].Count)
	assert.Equal(t, "1h 2021-01-01..2021-01-02 (48), 1d 2022-01-01..2022-01-05 (5)", candle.FormatCoverage(cov))

	all, err := repo.Coverage(ctx, "")
	require.NoError(t, err)
	require.Len(t, all, 3)
	assert.Equal(t, "ETHUSDT", all[2].Symbol)
	assert.Equal(t, int64(3), all[2].Count)

	none, err := repo.Coverage(ctx, "DOGEUSDT")
	require.NoError(t, err)
	assert.Empty(t, none)
	assert.Equal(t, "none", candle.FormatCoverage(none))
}
