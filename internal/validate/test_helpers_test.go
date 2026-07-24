package validate

import (
	"path/filepath"
	"testing"
)

func repoRootForCanaryTest(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}
