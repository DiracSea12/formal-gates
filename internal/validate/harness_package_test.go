package validate

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPackageReceiptExcludesLocalHarnessEnvironment(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.txt"), []byte("immutable"), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := PackageReceipt(root)
	if err != nil {
		t.Fatal(err)
	}
	local := filepath.Join(root, ".harness", "venv", "bin")
	if err := os.MkdirAll(local, 0700); err != nil {
		t.Fatal(err)
	}
	// A local venv is allowed to contain symlinks because it is not distributed.
	if err := os.Symlink(filepath.Join(root, "package.txt"), filepath.Join(local, "python")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".harness", "private-evidence.json"), []byte("not a package input"), 0600); err != nil {
		t.Fatal(err)
	}
	after, err := PackageReceipt(root)
	if err != nil {
		t.Fatal(err)
	}
	if after.Digest != before.Digest || len(after.Entries) != 1 {
		t.Fatalf("local runtime affected receipt: %#v", after)
	}
	// The exemption is root-local, not a relaxation for real package resources.
	dir := filepath.Join(root, "prompts")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "package.txt"), filepath.Join(dir, "rule.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := PackageReceipt(root); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("packaged symlink must still be rejected: %v", err)
	}
}

func TestPackageReceiptRespectsLocalExclusionsWithoutHidingTrackedSources(t *testing.T) {
	root := t.TempDir()
	runGit := func(args ...string) {
		t.Helper()
		if output, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	runGit("init", "--quiet")
	if err := os.WriteFile(filepath.Join(root, "tracked.md"), []byte("source"), 0600); err != nil {
		t.Fatal(err)
	}
	runGit("add", "tracked.md")
	if err := os.WriteFile(filepath.Join(root, ".git", "info", "exclude"), []byte("/.private-runtime/\n/tracked.md\n"), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := PackageReceipt(root)
	if err != nil || len(before.Entries) != 1 || before.Entries[0].Path != "tracked.md" {
		t.Fatalf("tracked source missing: %#v: %v", before, err)
	}
	local := filepath.Join(root, ".private-runtime", "venv")
	if err := os.MkdirAll(local, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "tracked.md"), filepath.Join(local, "python")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	after, err := PackageReceipt(root)
	if err != nil || after.Digest != before.Digest {
		t.Fatalf("private runtime changed package receipt: %#v: %v", after, err)
	}
}
