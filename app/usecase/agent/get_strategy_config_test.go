package agent_test

import (
	"context"
	"testing"

	"go-trade-bot/app/entities"
	agentusecase "go-trade-bot/app/usecase/agent"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
)

// get_strategy exposes the strategy's configuration JSON (the risk knobs:
// stop_loss_pct, position_sizing, max_daily_loss_usd) next to the script.
func TestGetStrategyTool_IncludesConfiguration(t *testing.T) {
	strategyUC := &mockStrategyUseCase{}
	uc := agentusecase.NewAgentUseCase(&mockModelProvider{}, &mockAgentRepository{}, strategyUC, &mockBacktestUseCase{}, &mockSignalUseCase{}, &mockSnapshotUseCase{})
	tool := findTool(t, uc, "get_strategy")

	s := entities.Strategy{ID: 7, Name: "s", StrategyName: "script", ScriptSource: "-- lua"}
	s.StrategyConfiguration.Configuration = datatypes.JSON(`{"stop_loss_pct":2,"max_daily_loss_usd":50}`)
	strategyUC.On("GetByID", mock.Anything, uint(7)).Return(s, nil).Once()

	out, err := tool.Execute(context.Background(), []byte(`{"strategy_id":7}`))
	require.NoError(t, err)
	assert.Contains(t, out, `configuration: {"stop_loss_pct":2,"max_daily_loss_usd":50}`)
	assert.Contains(t, out, "script_source:\n-- lua")

	// An unset configuration reads as an empty object, not a blank line.
	strategyUC.On("GetByID", mock.Anything, uint(8)).Return(entities.Strategy{ID: 8}, nil).Once()
	out, err = tool.Execute(context.Background(), []byte(`{"strategy_id":8}`))
	require.NoError(t, err)
	assert.Contains(t, out, "configuration: {}")
}
