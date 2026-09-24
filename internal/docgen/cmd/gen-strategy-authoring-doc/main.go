// Command gen-strategy-authoring-doc regenerates
// docs/generated/strategy-authoring.md from source (Backend Spec 05). Run
// via `make gen-agent-docs`; never hand-edit the generated file.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"go-trade-bot/internal/docgen"
)

// This command writes the generated document to TWO paths that must always
// stay byte-identical (Backend Spec 05 AC#4):
//   - docs/generated/strategy-authoring.md - the committed, human-reviewable
//     copy diffs are checked against (see docgen.strategy_authoring_test.go).
//   - app/usecase/agent/strategy_authoring_doc.md - the copy AgentUseCase
//     actually go:embeds. Go's //go:embed directive cannot reference a path
//     containing ".." (verified: `pattern ../../docs/x.md: invalid pattern
//     syntax`), so the embedded copy cannot simply point back at
//     docs/generated/ - both are written from the same in-memory string
//     every time this command runs, which is what keeps them identical.
func main() {
	root, err := docgen.FindRepoRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	doc, err := docgen.GenerateStrategyAuthoringDoc(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	targets := []string{
		filepath.Join(root, "docs", "generated", "strategy-authoring.md"),
		filepath.Join(root, "app", "usecase", "agent", "strategy_authoring_doc.md"),
	}
	for _, path := range targets {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
}
