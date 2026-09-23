//go:build qastructure

package validate

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// writeExecutableFixture builds a native behavior-bearing command fixture.
func writeExecutableFixture(t *testing.T, path, program string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "main.go")
	if err := os.WriteFile(source, []byte(program), 0o600); err != nil {
		t.Fatal(err)
	}
	build := exec.Command("go", "build", "-o", path, source)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build command fixture: %v\n%s", err, output)
	}
}
