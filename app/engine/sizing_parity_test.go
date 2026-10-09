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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// sizingRun drives one full replay of a script that buys once with the given
// qty, under the given execution mode and account wiring, and returns the
// opened order's quantity and entry price plus any trace log labels.
func sizingRun(t *testing.T, name string, mode strategies.ExecutionMode, dryRunAccount bool, qty float64) (float64, float64, []string) {
	t.Helper()
	candles := generateSyntheticCandles(40)
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
	mem := &memoryAccountUseCase{amount: 1000}
	var acct usecase.AccountUseCase = mem
	if dryRunAccount {
		acct = usecase.NewDryRunAccount(mem)
	}
	signalUC := usecase.NewSignalUseCase(signalRepo, acct, sim, nil, nil, nil)
	eng := engine.NewEngine(sim, indicators.NewTalibAdapter(), signalUC, mem, nil, memcache.NewInMemoryCache(), nil)

	dbStrat := entities.Strategy{
		ID: 1, Name: "qty_probe", StrategyName: "script",
		ScriptSource: fmt.Sprintf(`
function should_long(ctx) return true end
function go_long(ctx) return {buy = {qty = %v, price = ctx.price}} end
`, qty),
		MonitoredSymbols:      datatypes.JSONSlice[string]{symbol},
		StrategyConfiguration: entities.StrategyConfiguration{Cycle: entities.OneMinute, Configuration: datatypes.JSON(`{}`)},
	}
	strat, ok := strategies.Get("script", dbStrat)
	require.True(t, ok)
	var labels []string
	strat.(*script.ScriptStrategy).SetTraceSink(func(r script.TraceRecord) {
		for _, l := range r.Log {
			labels = append(labels, l.Label)
		}
	})

	_, err = engine.NewReplayDriver(f, sim, eng, strat, dbStrat, symbol, mode, signalRepo).Run(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, signalRepo.signals, "script should have opened a position")
	o := signalRepo.signals[0].Orders[0]
	return float64(o.Quantity), float64(o.EntryPrice), labels
}

// TestSizingParity_ScriptQty (B-03): the same script qty yields the same
// order size in every execution mode and under both the real and the dry-run
// account wiring, and an oversized request is clamped to the free balance
// with a trace entry. All variants share the simulated exchange; the sizing
// path (engine -> SignalUseCase) is what's under test, not order routing.
func TestSizingParity_ScriptQty(t *testing.T) {
	modes := []strategies.ExecutionMode{strategies.ModeBacktest, strategies.ModeDryRun, strategies.ModePaper, strategies.ModeLive}

	for _, tc := range []struct {
		name        string
		qty         float64
		wantClamped bool
	}{
		{"within_ceiling", 3, false},
		{"over_ceiling", 500, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var first float64
			n := 0
			for _, mode := range modes {
				for _, dry := range []bool{false, true} {
					qty, price, labels := sizingRun(t, fmt.Sprintf("%s_%s_%v", t.Name(), mode, dry), mode, dry, tc.qty)
					if n == 0 {
						first = qty
					}
					n++
					assert.InDelta(t, first, qty, 1e-6, "mode=%s dryrun_account=%v", mode, dry)
					if tc.wantClamped {
						assert.InDelta(t, 1000.0, qty*price, 0.5, "clamped to the free balance")
						assert.Contains(t, labels, "qty_clamped")
					} else {
						assert.InDelta(t, tc.qty, qty, 1e-4)
						assert.NotContains(t, labels, "qty_clamped")
					}
				}
			}
		})
	}
}
