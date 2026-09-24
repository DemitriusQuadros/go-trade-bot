package script_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"go-trade-bot/app/entities"
	backtest_repo "go-trade-bot/app/repository/backtest"
	candle_repo "go-trade-bot/app/repository/candle"
	scriptstate_repo "go-trade-bot/app/repository/scriptstate"
	"go-trade-bot/app/strategies"
	strategyscript "go-trade-bot/app/strategies/script"
	backtest_uc "go-trade-bot/app/usecase/backtest"
	scriptuc "go-trade-bot/app/usecase/script"
	"go-trade-bot/internal/exchange"
	"go-trade-bot/internal/metrics_provider"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func init() {
	// Register "script" for the real-backtest comparison path (fast-rerun
	// itself constructs ScriptStrategy directly, not via the registry).
	runner := strategyscript.NewRunner(strategyscript.DefaultHookTimeout, nil)
	strategies.Register("script", func(db entities.Strategy) strategies.Strategy {
		return strategyscript.NewScriptStrategy(db, consistencyNopStore{}, runner)
	})
}

type consistencyNopStore struct{}

func (consistencyNopStore) Load(context.Context, uint, string) (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}
func (consistencyNopStore) Save(context.Context, uint, string, map[string]interface{}) error {
	return nil
}

type consistencyStrategyRepo struct{ s entities.Strategy }

func (r consistencyStrategyRepo) GetByID(context.Context, uint) (entities.Strategy, error) {
	return r.s, nil
}

func toEntityCandles(cs []exchange.Candle) []entities.Candle {
	out := make([]entities.Candle, len(cs))
	for i, c := range cs {
		out[i] = entities.Candle{
			Symbol:    c.Symbol,
			Timeframe: c.Timeframe,
			OpenTime:  c.OpenTime,
			Open:      c.Open,
			High:      c.High,
			Low:       c.Low,
			Close:     c.Close,
			Volume:    c.Volume,
		}
	}
	return out
}

// AC5 (consistency) + AC6 (no script_state) + AC7 (no signals): a fast-rerun
// over a 100-candle window agrees with a real backtest over the identical
// fixture/script/window on which cycles produced signals, and leaves the
// script_state and signals tables untouched.
func TestFastRerun_ConsistentWithBacktest_AndNoPersistence(t *testing.T) {
	candles := fixtureCandles("BTCUSDT", 100)
	strat := entities.Strategy{
		ID:           1,
		Name:         "s",
		StrategyName: "script",
		ScriptSource: entryExitScript,
		StrategyConfiguration: entities.StrategyConfiguration{
			Cycle: entities.OneMinute,
			Configuration: datatypes.JSON(`{
				"stop_loss_pct": 50.0,
				"position_sizing": {"type": "pct_capital", "value": 10}
			}`),
		},
	}

	// --- Real backtest path over the identical fixture ---
	db, err := gorm.Open(sqlite.Open("file:fastrerun_consistency?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&entities.Candle{}, &entities.BacktestRun{}, &entities.ScriptState{}, &entities.Signal{}, &entities.Order{}))

	candleRepo := candle_repo.NewCandleRepository(db)
	require.NoError(t, candleRepo.Upsert(context.Background(), toEntityCandles(candles)))

	btUC := backtest_uc.NewBacktestUseCase(
		candleRepo,
		backtest_repo.NewBacktestRepository(db),
		consistencyStrategyRepo{s: strat},
		metrics_provider.NewCinarMetricsAdapter(),
		backtest_uc.DefaultThresholdPolicy(),
		t.TempDir(),
		20,
	)
	run, err := btUC.Run(context.Background(), backtest_uc.RunRequest{
		StrategyID: 1,
		Symbol:     "BTCUSDT",
		Timeframe:  "1m",
		StartDate:  candles[0].OpenTime,
		EndDate:    candles[len(candles)-1].OpenTime.Add(time.Minute),
	})
	require.NoError(t, err)

	var btTrace []strategyscript.TraceRecord
	require.NoError(t, json.Unmarshal(run.ExecutionTraceJSON, &btTrace))
	backtestSignalTimes := signalTimestamps(btTrace)

	// --- Fast-rerun path over the identical fixture ---
	hist := &spyHistoricalSource{candles: candles}
	uc := scriptuc.NewUseCase(nil, hist, consistencyStrategyRepo{s: strat}, strategyscript.NewRunner(strategyscript.DefaultHookTimeout, nil))
	end := candles[len(candles)-1].OpenTime.Add(time.Minute)
	resp, err := uc.FastRerun(context.Background(), scriptuc.FastRerunRequest{
		StrategyID: 1,
		Source:     entryExitScript,
		Symbol:     "BTCUSDT",
		EndTime:    &end,
	})
	require.NoError(t, err)
	fastRerunSignalTimes := signalTimestamps(resp.Trace)

	// AC5: both paths agree on which cycles produced signals.
	assert.Equal(t, backtestSignalTimes, fastRerunSignalTimes, "fast-rerun and backtest should agree on signal-producing cycles")
	assert.NotEmpty(t, fastRerunSignalTimes, "the fixture/script should produce at least one signal")

	// AC6: no script_state row exists for the strategy after fast-rerun.
	ssRepo := scriptstate_repo.NewRepository(db)
	_, ssErr := ssRepo.Get(context.Background(), 1, "BTCUSDT")
	assert.ErrorIs(t, ssErr, gorm.ErrRecordNotFound, "fast-rerun must not write a script_state row")

	// AC7: fast-rerun never touched the signals table (backtest uses an
	// in-memory signal repo, so this DB table is only touched if fast-rerun
	// wrongly persisted).
	var signalCount int64
	require.NoError(t, db.Model(&entities.Signal{}).Count(&signalCount).Error)
	assert.Equal(t, int64(0), signalCount, "fast-rerun must not create signals")
}

func signalTimestamps(trace []strategyscript.TraceRecord) []int64 {
	var out []int64
	for _, r := range trace {
		if r.Signal != nil {
			out = append(out, r.Timestamp.Unix())
		}
	}
	return out
}
