package docgen_test

import (
	"os"
	"path/filepath"
	"testing"

	"go-trade-bot/internal/docgen"

	"github.com/stretchr/testify/require"
)

// TestGeneratedDocIsUpToDate is Backend Spec 05's concrete mechanism
// preventing config.example.yml-style drift: if
// docs/generated/strategy-authoring.md was hand-edited (or source changed)
// without re-running `make gen-agent-docs`, this test fails loudly.
func TestGeneratedDocIsUpToDate(t *testing.T) {
	root, err := docgen.FindRepoRoot()
	require.NoError(t, err)

	generated, err := docgen.GenerateStrategyAuthoringDoc(root)
	require.NoError(t, err)

	onDisk, err := os.ReadFile(filepath.Join(root, "docs", "generated", "strategy-authoring.md"))
	require.NoError(t, err)
	require.Equal(t, string(onDisk), generated, "run `make gen-agent-docs` - the committed doc is stale relative to interface.go/indicators.go")

	// app/usecase/agent/strategy_authoring_doc.md is the copy AgentUseCase
	// actually go:embeds (a //go:embed directive cannot reference
	// docs/generated/ via a ".." path) - it must never drift from the
	// canonical copy above (Backend Spec 05 AC#4).
	embedded, err := os.ReadFile(filepath.Join(root, "app", "usecase", "agent", "strategy_authoring_doc.md"))
	require.NoError(t, err)
	require.Equal(t, string(embedded), generated, "run `make gen-agent-docs` - app/usecase/agent's embedded copy is stale relative to interface.go/indicators.go")
}
