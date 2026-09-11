package strategies

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExecutionMode_String(t *testing.T) {
	assert.Equal(t, "backtest", ModeBacktest.String())
	assert.Equal(t, "dryrun", ModeDryRun.String())
	assert.Equal(t, "paper", ModePaper.String())
	assert.Equal(t, "live", ModeLive.String())
}

func TestExecutionMode_Ordinals(t *testing.T) {
	assert.True(t, ModeBacktest < ModeDryRun)
	assert.True(t, ModeDryRun < ModePaper)
	assert.True(t, ModePaper < ModeLive)
}

func TestParseExecutionMode(t *testing.T) {
	cases := map[string]ExecutionMode{
		"backtest": ModeBacktest,
		"dryrun":   ModeDryRun,
		"paper":    ModePaper,
		"live":     ModeLive,
	}
	for in, want := range cases {
		got, err := ParseExecutionMode(in)
		assert.NoError(t, err)
		assert.Equal(t, want, got)
	}
}

func TestParseExecutionMode_Unknown(t *testing.T) {
	_, err := ParseExecutionMode("nonsense")
	assert.Error(t, err)
}
