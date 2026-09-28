package agentreport

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecodeBlockLenient(t *testing.T) {
	b, moved, err := DecodeBlockLenient(json.RawMessage(`{"type":"summary","data":{"text":"a"}}`))
	require.NoError(t, err)
	assert.False(t, moved)
	assert.JSONEq(t, `{"text":"a"}`, string(b.Data))

	b, moved, err = DecodeBlockLenient(json.RawMessage(`{"type":"summary","text":"flat"}`))
	require.NoError(t, err)
	assert.True(t, moved)
	assert.Equal(t, "summary", b.Type)
	assert.JSONEq(t, `{"text":"flat"}`, string(b.Data))
	require.NoError(t, Validate([]Block{b}))

	b, moved, err = DecodeBlockLenient(json.RawMessage(`{"type":"summary","data":null,"text":"flat"}`))
	require.NoError(t, err)
	assert.True(t, moved)
	assert.JSONEq(t, `{"text":"flat"}`, string(b.Data))

	b, moved, err = DecodeBlockLenient(json.RawMessage(`{"type":"summary"}`))
	require.NoError(t, err)
	assert.False(t, moved)
	assert.ErrorContains(t, Validate([]Block{b}), "missing data")

	_, _, err = DecodeBlockLenient(json.RawMessage(`[1]`))
	assert.Error(t, err)
	_, _, err = DecodeBlockLenient(json.RawMessage(`{"type":3}`))
	assert.Error(t, err)

	blocks, n, err := DecodeBlocksLenient([]json.RawMessage{
		json.RawMessage(`{"type":"summary","text":"x"}`),
		json.RawMessage(`{"type":"summary","data":{"text":"y"}}`),
	})
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.Len(t, blocks, 2)
}
