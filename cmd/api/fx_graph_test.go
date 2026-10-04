package main

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
)

// The API's dependency graph must resolve: a missing provider otherwise only
// fails when the process starts.
func TestAPIFxGraphResolves(t *testing.T) {
	require.NoError(t, fx.ValidateApp(appOptions()))
}
