package docgen

import (
	"fmt"
	"os"
	"path/filepath"
)

// FindRepoRoot walks up from the current working directory looking for
// go.mod, so both the generator command and its "is the committed doc
// stale" test work regardless of which directory `go run`/`go test` is
// invoked from.
func FindRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("docgen: go.mod not found above %s", dir)
		}
		dir = parent
	}
}
