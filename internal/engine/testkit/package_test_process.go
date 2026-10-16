package testkit

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// RunNativePackageTest runs the parent test in parallel in its own native package.
// Windows uses independent executable bytes so the parent image cannot pin the
// child's package.
// It returns false in the child process so the caller executes the test body.
func RunNativePackageTest(t *testing.T) bool {
	t.Helper()
	const marker = "FORMAL_GATES_NATIVE_TEST_CHILD"
	if os.Getenv(marker) == t.Name() {
		return false
	}
	t.Parallel()
	root := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	name := "native.test"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	target := filepath.Join(root, "bin", name)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		payload, err := os.ReadFile(executable)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, payload, 0o700); err != nil {
			t.Fatal(err)
		}
	} else if err := os.Link(executable, target); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(target, "-test.timeout=0", "-test.v", "-test.run=^"+t.Name()+"$")
	cmd.Env = append(os.Environ(), marker+"="+t.Name())
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("native package fixture: %v\n%s", err, output)
	}
	t.Logf("%s", output)
	return true
}
