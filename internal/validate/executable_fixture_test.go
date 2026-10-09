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
	buildDir := t.TempDir()
	source := filepath.Join(buildDir, "main.go")
	if err := os.WriteFile(source, []byte(program), 0o600); err != nil {
		t.Fatal(err)
	}
	built := filepath.Join(buildDir, filepath.Base(path))
	build := exec.Command("go", "build", "-o", built, source)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build command fixture: %v\n%s", err, output)
	}
	body, err := os.ReadFile(built)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o700); err != nil {
		t.Fatal(err)
	}
}
