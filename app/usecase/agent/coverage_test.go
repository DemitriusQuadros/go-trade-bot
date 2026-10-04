package agent_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"go-trade-bot/app/entities"
	"go-trade-bot/app/repository/candle"
	agentusecase "go-trade-bot/app/usecase/agent"
	"go-trade-bot/app/usecase/agent/deploygate"
	"go-trade-bot/internal/modelprovider"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// fakeCoverage is a CandleCoverageReader (fix-02 B2).
type fakeCoverage struct {
	rows      []candle.Coverage
	err       error
	gotSymbol *string
}

func (f *fakeCoverage) Coverage(_ context.Context, symbol string) ([]candle.Coverage, error) {
	s := symbol
	f.gotSymbol = &s
	if f.err != nil {
		return nil, f.err
	}
	var out []candle.Coverage
	for _, r := range f.rows {
		if symbol == "" || r.Symbol == symbol {
			out = append(out, r)
		}
	}
	return out, nil
}

func d(y int, m time.Month, day int) time.Time { return time.Date(y, m, day, 0, 0, 0, 0, time.UTC) }

var sampleCoverage = []candle.Coverage{
	{Symbol: "BTCUSDT", Timeframe: "1h", From: d(2021, 1, 1), To: d(2026, 9, 20), Count: 36267},
	{Symbol: "BTCUSDT", Timeframe: "1d", From: d(2022, 1, 1), To: d(2026, 9, 19), Count: 192},
	{Symbol: "ETHUSDT", Timeframe: "5m", From: d(2026, 9, 7), To: d(2026, 9, 11), Count: 1000},
}

func coverageTool(t *testing.T, uc *agentusecase.AgentUseCase) agentusecase.Tool {
	t.Helper()
	for _, tool := range uc.Tools() {
		if tool.Def.Name == "get_candle_coverage" {
			assert.Equal(t, entities.PermRead, tool.Permission)
			return tool
		}
	}
	t.Fatal("get_candle_coverage not registered")
	return agentusecase.Tool{}
}

func TestGetCandleCoverageTool_SymbolAndAll(t *testing.T) {
	uc, _, _, _, _ := newBareAgentUseCase()
	cov := &fakeCoverage{rows: sampleCoverage}
	uc.Coverage = cov
	tool := coverageTool(t, uc)

	out, err := tool.Execute(context.Background(), rawArgs(map[string]any{"symbol": "btcusdt"}))
	require.NoError(t, err)
	assert.Equal(t, "BTCUSDT", *cov.gotSymbol, "symbol is upper-cased")
	assert.Contains(t, out, "symbol=BTCUSDT timeframe=1h from=2021-01-01T00:00:00Z to=2026-09-20T00:00:00Z candles=36267")
	assert.Contains(t, out, "timeframe=1d")
	assert.NotContains(t, out, "ETHUSDT")

	out, err = tool.Execute(context.Background(), rawArgs(map[string]any{}))
	require.NoError(t, err)
	assert.Equal(t, "", *cov.gotSymbol, "no symbol = every symbol")
	assert.Contains(t, out, "ETHUSDT")
	assert.Contains(t, out, "BTCUSDT")

	out, err = tool.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Contains(t, out, "ETHUSDT")

	out, err = tool.Execute(context.Background(), rawArgs(map[string]any{"symbol": "DOGEUSDT"}))
	require.NoError(t, err)
	assert.Contains(t, out, "no candles stored for DOGEUSDT")
}

func TestGetCandleCoverageTool_NotWiredAndErrors(t *testing.T) {
	uc, _, _, _, _ := newBareAgentUseCase()
	_, err := coverageTool(t, uc).Execute(context.Background(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not available")

	uc.Coverage = &fakeCoverage{err: errors.New("db down")}
	_, err = coverageTool(t, uc).Execute(context.Background(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "db down")

	_, err = coverageTool(t, uc).Execute(context.Background(), []byte(`{"symbol": 5}`))
	require.Error(t, err)
}

func TestSystemPrompt_TellsModelToCheckCoverageBeforeBacktesting(t *testing.T) {
	uc, model, repo, _, _ := newBareAgentUseCase()
	repo.On("GetInstruction", mock.Anything).Return(entities.AgentInstruction{}, nil)
	repo.On("CreateRun", mock.Anything, mock.Anything).Return(entities.AgentRun{ID: 1}, nil)
	repo.On("UpdateRun", mock.Anything, mock.Anything).Return(nil)
	var system string
	model.On("Complete", mock.Anything, mock.MatchedBy(func(req modelprovider.CompletionRequest) bool {
		system = req.System
		return true
	})).Return(modelprovider.CompletionResult{StopReason: "end_turn", Text: "ok"}, nil).Once()
	_, err := uc.RunToolLoop(context.Background(), "chat_ui", "hi", nil, nil)
	require.NoError(t, err)
	assert.Contains(t, system, "Before backtesting, call get_candle_coverage and pick a timeframe and date range that has data.")
}

// The Phase B gate still fails on insufficient history, and its message now
// lists the stored coverage for the symbol.
type countWithCoverage struct {
	noCandles
	fakeCoverage
}

func TestGateRunner_InsufficientHistoryIncludesCoverage(t *testing.T) {
	e := newPhaseBEnv(t)
	wf := &fakeWalkForward{}
	candles := &countWithCoverage{fakeCoverage: fakeCoverage{rows: sampleCoverage}}
	gate := agentusecase.NewGateRunner(wf, candles, e.proposals)
	s := e.strategy(t, "Dry", "dryrun", entities.Testing)

	out, err := gate.Run(context.Background(), s, newSource)
	require.NoError(t, err)
	assert.False(t, out.Result.Passed)
	require.Len(t, out.Result.Checks, 1)
	assert.Equal(t, deploygate.CheckInsufficientHistory, out.Result.Checks[0].Name)
	assert.Contains(t, out.Result.Checks[0].Detail, "import more history first; available: 1h 2021-01-01..2026-09-20 (36267), 1d 2022-01-01..2026-09-19 (192)")
	assert.Empty(t, wf.reqs)

	// Without a coverage-capable counter the old message is unchanged.
	out, err = agentusecase.NewGateRunner(wf, noCandles{}, e.proposals).Run(context.Background(), s, newSource)
	require.NoError(t, err)
	assert.NotContains(t, out.Result.Checks[0].Detail, "available:")
}
