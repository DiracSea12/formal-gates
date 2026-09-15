package validate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPackageReceiptExcludesLocalDogfoodEnvironment(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.txt"), []byte("immutable"), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := PackageReceipt(root)
	if err != nil {
		t.Fatal(err)
	}
	local := filepath.Join(root, ".dogfood", "venv", "bin")
	if err := os.MkdirAll(local, 0700); err != nil {
		t.Fatal(err)
	}
	// A local venv is allowed to contain symlinks because it is not distributed.
	if err := os.Symlink(filepath.Join(root, "package.txt"), filepath.Join(local, "python")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".dogfood", "private-evidence.json"), []byte("not a package input"), 0600); err != nil {
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
