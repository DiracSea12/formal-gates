package validate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPackageReceiptRejectsSymlinkAndOverlappingRoots(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "payload"), []byte("stable"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "mutable")
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "payload"), []byte("worktree"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "payload"), link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := PackageReceipt(root); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("expected symlink rejection, got %v", err)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if _, err := PackageReceipt(root); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("expected directory symlink rejection, got %v", err)
	}
	cleanRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(cleanRoot, "payload"), []byte("stable"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := PackageReceipt(cleanRoot, cleanRoot); err == nil || !strings.Contains(err.Error(), "overlaps") {
		t.Fatalf("expected disjoint proof rejection, got %v", err)
	}
}

func TestPackageReceiptRealPathsThroughAncestorAlias(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "package")
	payload := filepath.Join(root, "nested", "payload")
	if err := os.MkdirAll(filepath.Dir(payload), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(payload, []byte("stable"), 0o600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "parent-alias")
	if err := os.Symlink(parent, alias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	wantRealPath, err := filepath.EvalSymlinks(payload)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := PackageReceipt(filepath.Join(alias, "package"))
	if err != nil {
		t.Fatal(err)
	}
	if len(receipt.Entries) != 1 || receipt.Entries[0].Path != "nested/payload" || receipt.Entries[0].RealPath != wantRealPath {
		t.Fatalf("receipt through ancestor alias = %+v; want payload at %q", receipt.Entries, wantRealPath)
	}
}


func TestAdmitRegistryLeavesMachineReadableUnregisteredReceipt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.json")
	receipt, err := AdmitRegistry(path, "missing")
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Accepted || receipt.Code != "UNREGISTERED_INSTALL" {
		t.Fatalf("receipt=%+v", receipt)
	}
	if _, err := os.Stat(path + ".admission.json"); err != nil {
		t.Fatal(err)
	}
}

func TestBuildBaselineReceiptBindsCanonicalPathsAndDigest(t *testing.T) {
	root := t.TempDir()
	hostConfig := filepath.Join(t.TempDir(), "host", "config.json")
	if err := os.WriteFile(filepath.Join(root, "payload"), []byte("stable"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(hostConfig), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hostConfig, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	receipt, err := BuildBaselineReceipt("git:abc123", root, "", map[string]string{"hostConfig": hostConfig})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.VCSIdentity != "git:abc123" || receipt.PackageDigest == "" || receipt.CanonicalPaths["hostConfig"] != canonicalRegistryPath(hostConfig) {
		t.Fatalf("unexpected baseline receipt: %+v", receipt)
	}
	path := filepath.Join(t.TempDir(), "baseline.json")
	if err := WriteBaselineReceipt(path, receipt); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}
