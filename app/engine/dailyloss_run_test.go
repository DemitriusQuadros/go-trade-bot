package engine_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"go-trade-bot/app/engine"
	"go-trade-bot/app/entities"
	"go-trade-bot/app/repository/candle"
	"go-trade-bot/app/strategies"
	"go-trade-bot/app/strategies/script"
	usecase "go-trade-bot/app/usecase/signal"
	"go-trade-bot/internal/feed"
	"go-trade-bot/internal/indicators"
	"go-trade-bot/internal/memcache"

	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type dailyLossEntry struct {
	at     time.Time
	profit float64
}

type dailyLossResult struct {
	entries []dailyLossEntry
	labels  []string
}

// dailyLossRun replays ~30h of 1m candles with a script that buys on every
// flat candle and sells on the next one, so every round trip costs fees.
// limit <= 0 means no max_daily_loss_usd.
func dailyLossRun(t *testing.T, name string, limit float64) dailyLossResult {
	t.Helper()
	candles := generateSyntheticCandles(1800)
	symbol := "BTCUSDT"

	db, err := gorm.Open(sqlite.Open("file:"+name+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&entities.Candle{}))
	repo := candle.NewCandleRepository(db)
	require.NoError(t, repo.Upsert(context.Background(), candles))

	f, err := feed.NewReplayFeed(context.Background(), repo, symbol, "1m", candles[0].OpenTime, candles[len(candles)-1].OpenTime.Add(time.Minute))
	require.NoError(t, err)

	sim := engine.NewSimulatedFillExchange(engine.NewCandleRepoMarketDataSource(repo), engine.FillPolicy{})
	signalRepo := &memorySignalRepo{}
	acct := &memoryAccountUseCase{amount: 1000}
	signalUC := usecase.NewSignalUseCase(signalRepo, acct, sim, nil, nil, nil)
	eng := engine.NewEngine(sim, indicators.NewTalibAdapter(), signalUC, acct, nil, memcache.NewInMemoryCache(), nil)

	cfg := `{}`
	if limit > 0 {
		cfg = fmt.Sprintf(`{"max_daily_loss_usd": %v}`, limit)
	}
	dbStrat := entities.Strategy{
		ID: 1, Name: "churn", StrategyName: "script",
		ScriptSource: `
function should_long(ctx) return true end
function go_long(ctx) return {buy = {qty = 5, price = ctx.price}} end
function update_position(ctx) return {sell = {price = ctx.price}} end
`,
		MonitoredSymbols:      datatypes.JSONSlice[string]{symbol},
		StrategyConfiguration: entities.StrategyConfiguration{Cycle: entities.OneMinute, Configuration: datatypes.JSON(cfg)},
	}
	strat, ok := strategies.Get("script", dbStrat)
	require.True(t, ok)
	var res dailyLossResult
	strat.(*script.ScriptStrategy).SetTraceSink(func(r script.TraceRecord) {
		for _, l := range r.Log {
			res.labels = append(res.labels, l.Label)
		}
	})

	_, err = engine.NewReplayDriver(f, sim, eng, strat, dbStrat, symbol, strategies.ModeBacktest, signalRepo).Run(context.Background())
	require.NoError(t, err)

	for _, s := range signalRepo.signals {
		if s.Status == entities.Closed && len(s.Orders) > 0 {
			res.entries = append(res.entries, dailyLossEntry{at: s.Orders[0].UpdatedAt, profit: float64(s.Orders[0].Profit)})
		}
	}
	return res
}
