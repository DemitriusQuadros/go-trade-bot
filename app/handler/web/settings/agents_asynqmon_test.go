package settings

import (
	"encoding/json"
	"testing"

	"go-trade-bot/app/entities"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fix-02 B5: agents_asynqmon_url round-trips through PUT (request JSON ->
// MergeInto) and GET (ToSettingsResponse -> JSON), like asynqmon_url.
func TestAgentsAsynqmonURL_RoundTrip(t *testing.T) {
	var req PlatformSettingsUpdateRequestDTO
	require.NoError(t, json.Unmarshal([]byte(`{
		"mode": "dryrun",
		"asynqmon_url": "http://localhost:9191/tasks/monitoring",
		"agents_asynqmon_url": "http://localhost:9194/tasks/monitoring"
	}`), &req))
	assert.Equal(t, "http://localhost:9194/tasks/monitoring", req.AgentsAsynqmonURL)

	merged := req.MergeInto(entities.Settings{ID: 1, AgentsAsynqmonURL: "http://old"})
	assert.Equal(t, "http://localhost:9194/tasks/monitoring", merged.AgentsAsynqmonURL)
	assert.Equal(t, "http://localhost:9191/tasks/monitoring", merged.AsynqmonURL)

	body, err := json.Marshal(ToSettingsResponse(merged))
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(body, &got))
	assert.Equal(t, "http://localhost:9194/tasks/monitoring", got["agents_asynqmon_url"])
	assert.Equal(t, "http://localhost:9191/tasks/monitoring", got["asynqmon_url"])
}

func TestAgentsAsynqmonURL_DefaultsEmpty(t *testing.T) {
	body, err := json.Marshal(ToSettingsResponse(entities.Settings{}))
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(body, &got))
	v, ok := got["agents_asynqmon_url"]
	assert.True(t, ok, "the key is always present")
	assert.Equal(t, "", v)

	// Like asynqmon_url, a PUT without the field clears it (non-secret
	// fields always take the request's value).
	merged := PlatformSettingsUpdateRequestDTO{}.MergeInto(entities.Settings{AgentsAsynqmonURL: "http://x"})
	assert.Equal(t, "", merged.AgentsAsynqmonURL)
}
