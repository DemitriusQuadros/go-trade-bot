package handler

import (
	"encoding/json"
	"testing"

	"go-trade-bot/app/entities"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// challenger_of_id is always present: the champion id, or null.
func TestToStrategyResponse_ChallengerOfID(t *testing.T) {
	champion := uint(4)
	b, err := json.Marshal(ToStrategyResponse(entities.Strategy{ID: 9, ChallengerOfID: &champion}))
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	assert.EqualValues(t, 4, m["challenger_of_id"])

	b, err = json.Marshal(ToStrategyResponse(entities.Strategy{ID: 4}))
	require.NoError(t, err)
	m = map[string]any{}
	require.NoError(t, json.Unmarshal(b, &m))
	v, ok := m["challenger_of_id"]
	assert.True(t, ok)
	assert.Nil(t, v)
}
