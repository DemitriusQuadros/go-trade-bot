package main

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
)

// The worker's dependency graph must resolve: a missing provider otherwise
// only fails when the process starts.
func TestWorkerFxGraphResolves(t *testing.T) {
	require.NoError(t, fx.ValidateApp(appOptions()))
}
