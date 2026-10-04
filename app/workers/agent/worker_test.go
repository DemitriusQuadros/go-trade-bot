package agent

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTaskAndOptions(t *testing.T) {
	task, err := NewTask(RunPayload{AgentID: 3, Trigger: "cron", Detail: json.RawMessage(`{"cron":"0 * * * *"}`)})
	require.NoError(t, err)
	assert.Equal(t, TaskAgentRun, task.Type())
	var p RunPayload
	require.NoError(t, json.Unmarshal(task.Payload(), &p))
	assert.Equal(t, uint(3), p.AgentID)

	cron := Options(RunPayload{Trigger: "cron"})
	manual := Options(RunPayload{Trigger: "manual"})
	assert.Len(t, cron, 4, "queue, max retry 0, timeout, unique")
	assert.Len(t, manual, 3, "manual runs are not deduplicated")
	assert.Equal(t, "Queue(\"agents\")", cron[0].String())
	assert.Equal(t, "MaxRetry(0)", cron[1].String())
}
