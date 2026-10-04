package configuration

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestAgentRuntime_WithDefaults(t *testing.T) {
	assert.Equal(t, AgentRuntime{Concurrency: 2, MetricsPort: "9194", SyncInterval: 30 * time.Second}, AgentRuntime{}.WithDefaults())
	custom := AgentRuntime{Concurrency: 5, MetricsPort: "9999", SyncInterval: time.Minute}
	assert.Equal(t, custom, custom.WithDefaults())
}
