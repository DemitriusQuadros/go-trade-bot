package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"go-trade-bot/app/entities"
	"go-trade-bot/internal/metrics_provider"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
)

// TestSummarizeBacktestForModel_TrimsToLastNTradesAndStaysBounded is
// Backend Spec 06 Acceptance Criterion #3: a large trade log must be
// trimmed to the trailing N trades plus aggregate stats, staying under a
// fixed size bound rather than including the full trace verbatim.
func TestSummarizeBacktestForModel_TrimsToLastNTradesAndStaysBounded(t *testing.T) {
	trades := make([]metrics_provider.TradeLogEntry, 0, 5000)
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 5000; i++ {
		trades = append(trades, metrics_provider.TradeLogEntry{
			Symbol:     "BTCUSDT",
			EntryTime:  base.Add(time.Duration(i) * time.Minute),
			EntryPrice: 100,
			ExitTime:   base.Add(time.Duration(i+1) * time.Minute),
			ExitPrice:  101,
			Quantity:   1,
			Profit:     1,
			ExitReason: "take_profit",
		})
	}
	b, err := json.Marshal(trades)
	require.NoError(t, err)

	run := entities.BacktestRun{
		ID:             1,
		StrategyID:     2,
		Symbol:         "BTCUSDT",
		Sharpe:         1.2,
		MaxDrawdownPct: 5.5,
		WinRatePct:     60,
		ProfitFactor:   2.1,
		TotalTrades:    len(trades),
		TotalReturnPct: 42,
		TradeLogJSON:   datatypes.JSON(b),
	}

	summary := summarizeBacktestForModel(run)
	assert.LessOrEqual(t, len(summary), 8*1024+64)
	assert.Contains(t, summary, "sharpe=1.2000")
	assert.Contains(t, summary, "last 20 of 5000 trades")
}

// TestSaveStrategyScriptTool_ClampsModeAtTheExecuteLevel exercises the gate
// directly against the unexported saveStrategyScriptTool - a white-box
// complement to safety_gate_test.go's full-loop coverage, pinning down that
// THE GATE (Backend Spec 03) lives in Execute itself, not somewhere further
// up the call stack that a refactor could accidentally remove.
func TestSaveStrategyScriptTool_ClampsModeAtTheExecuteLevel(t *testing.T) {
	var savedStrategy entities.Strategy
	stub := stubStrategyUseCase{
		save: func(s entities.Strategy) (entities.Strategy, error) {
			savedStrategy = s
			return s, nil
		},
	}
	u := AgentUseCase{Strategy: stub}
	tool := u.saveStrategyScriptTool()

	_, err := tool.Execute(context.Background(), mustMarshal(map[string]any{
		"name":          "x",
		"description":   "y",
		"script_source": "function GoLong() end",
		"symbols":       []string{"BTCUSDT"},
		"cycle_minutes": 15,
		"mode":          "live",
	}))
	require.NoError(t, err)
	assert.Equal(t, "dryrun", savedStrategy.Mode)
	assert.Equal(t, entities.Testing, savedStrategy.Status)
}

func mustMarshal(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// stubStrategyUseCase is a minimal hand-written stub (no testify/mock
// needed for a single-method assertion) satisfying the local
// StrategyUseCase interface.
type stubStrategyUseCase struct {
	save func(entities.Strategy) (entities.Strategy, error)
}

func (s stubStrategyUseCase) Save(_ context.Context, strategy entities.Strategy) (entities.Strategy, error) {
	return s.save(strategy)
}

func (s stubStrategyUseCase) Update(_ context.Context, _ entities.Strategy) error {
	panic("not used by this test")
}

func (s stubStrategyUseCase) GetByID(_ context.Context, _ uint) (entities.Strategy, error) {
	panic("not used by this test")
}

func (s stubStrategyUseCase) GetAll(_ context.Context) ([]entities.Strategy, error) {
	panic("not used by this test")
}
