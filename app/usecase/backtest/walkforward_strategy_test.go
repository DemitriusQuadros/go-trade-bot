package backtest_test

import (
	"context"
	"testing"
	"time"

	"go-trade-bot/app/entities"
	"go-trade-bot/app/usecase/backtest"
	"go-trade-bot/internal/metrics_provider"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
)

const wfNoTradeScript = `
function should_long(ctx)
  return false
end
`

// walkForwardFixture places 200 oscillating 1m candles inside the first
// walk-forward window's out-of-sample (test) range, so OOS trade counts
// depend only on the script being replayed.
func walkForwardFixture(t *testing.T) (*backtest.BacktestUseCase, *mockBacktestRepo, entities.Strategy, time.Time) {
	t.Helper()
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	candles := oscillatingCandles(200)
	shift := start.Add(91 * 24 * time.Hour).Sub(candles[0].OpenTime)
	for i := range candles {
		candles[i].OpenTime = candles[i].OpenTime.Add(shift)
	}
	saved := entities.Strategy{
		ID: 7, Name: "Saved", StrategyName: "script", ScriptSource: wfNoTradeScript,
		StrategyConfiguration: entities.StrategyConfiguration{
			Cycle:         entities.OneMinute,
			Configuration: datatypes.JSON(`{"stop_loss_pct": 2.0, "position_sizing": {"type": "pct_capital", "value": 10}}`),
		},
	}
	btRepo := &mockBacktestRepo{}
	uc := backtest.NewBacktestUseCase(&mockCandleRepo{candles: candles}, btRepo, &mockStrategyRepo{strategy: saved},
		metrics_provider.NewCinarMetricsAdapter(), backtest.DefaultThresholdPolicy(), t.TempDir(), 20)
	return uc, btRepo, saved, start
}

// B-01 §2: RunWalkForwardForStrategy replays the passed-in (unsaved)
// strategy, links the run to strat.ID and stamps CandidateSourceHash;
// RunWalkForward (load by ID -> delegate) keeps replaying the saved source.
func TestRunWalkForwardForStrategy_UsesInMemorySource(t *testing.T) {
	uc, btRepo, saved, start := walkForwardFixture(t)
	req := backtest.WalkForwardRequest{
		StrategyID: saved.ID, Symbol: "BTCUSDT", Timeframe: "1m",
		StartDate: start, EndDate: start.Add(121 * 24 * time.Hour), TrainMonths: 3, TestMonths: 1,
	}

	byID, err := uc.RunWalkForward(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, 0, byID.TotalTrades, "saved no-trade source")
	assert.Empty(t, byID.CandidateSourceHash)

	candidate := saved
	candidate.ScriptSource = tradeScript
	req.StrategyID = 999 // ignored by ForStrategy
	req.CandidateSourceHash = "abc123"
	run, err := uc.RunWalkForwardForStrategy(context.Background(), candidate, req)
	require.NoError(t, err)
	assert.Greater(t, run.TotalTrades, 0, "candidate source must be the one replayed")
	assert.Equal(t, saved.ID, run.StrategyID, "run linked to the real strategy row")
	assert.True(t, run.IsWalkForward)
	require.Len(t, btRepo.runs, 2)
	assert.Equal(t, "abc123", btRepo.runs[1].CandidateSourceHash)
}
