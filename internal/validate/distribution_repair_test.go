package validate

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"

	"strings"
	"testing"
)

func TestAdmissionRejectsStaleInstalledDigest(t *testing.T) {
	source := copyPackageFixture(t)
	project := t.TempDir()
	registry := filepath.Join(t.TempDir(), "registry.json")
	launcher := filepath.Join(t.TempDir(), "bin", nativeBinaryName())
	report, err := Install(InstallOptions{
		Source: source, Host: "claude", Scope: "project", Project: project,
		RegistryPath: registry, BinaryTarget: launcher, Force: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	document, err := LoadRegistry(registry)
	if err != nil {
		t.Fatal(err)
	}
	record := document.Records[0]
	record.InstalledDigest = strings.Repeat("0", 64)
	repairCommitRegistry(t, registry, record)
	receipt, err := AdmitRegistry(registry, report.Targets[0].RegistryRecordID)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Accepted || receipt.Code != "UNREGISTERED_INSTALL" || !strings.Contains(receipt.Reason, "stale") {
		t.Fatalf("stale installed digest was admitted: %+v", receipt)
	}
}

func repairGitProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "requirements.md"), []byte("stage-0 requirement\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "formal-gates-repair@example.invalid"},
		{"config", "user.name", "Formal Gates Repair"},
		{"add", "requirements.md"},
		{"commit", "-m", "baseline"},
	} {
		command := exec.Command("git", args...)
		command.Dir = root
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
		}
	}
	return root
}

func repairCommitRegistry(t *testing.T, path string, record RegistryRecord) {
	t.Helper()
	unlock, err := acquireRegistryLock(path)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	document, err := loadRegistryForCommit(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := commitRegistryRecordsUnlocked(path, document, []RegistryRecord{record}); err != nil {
		t.Fatal(err)
	}
}

// CASE-013 回归：payload manifest 未把请求的 host 登记为可安装 host-target
// （缺失或降级为 explanation-level）时，install 必须在创建任何 target/state
// 之前非零拒绝，而不是以 exit 0 接受一个未登记 target。
func TestInstallRejectsManifestUnregisteredHostTargetBeforeTargetCreation(t *testing.T) {
	source := copyPackageFixture(t)
	manifestPath := filepath.Join(source, "formal-gates.manifest.json")
	manifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	// 把 Claude Code 从 host-target 降级为 explanation-level：payload 不再登记
	// 该安装目标。
	updated := strings.Replace(string(manifest), "\"name\": \"Claude Code\",\n      \"support\": \"host-target\"", "\"name\": \"Claude Code\",\n      \"support\": \"explanation-level\"", 1)
	if updated == string(manifest) {
		t.Fatal("manifest fixture did not contain the Claude Code host-target entry")
	}
	if err := os.WriteFile(manifestPath, []byte(updated), 0o600); err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	registry := filepath.Join(t.TempDir(), "registry.json")
	launcher := filepath.Join(t.TempDir(), "bin", nativeBinaryName())
	_, err = Install(InstallOptions{
		Source: source, Host: "claude", Scope: "project", Project: project,
		RegistryPath: registry, BinaryTarget: launcher, Force: true,
	})
	if err == nil || !strings.Contains(err.Error(), "does not register") {
		t.Fatalf("install accepted a manifest-unregistered host target: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(project, ".claude", "skills", "formal-gates")); !os.IsNotExist(statErr) {
		t.Fatalf("rejected install left a target behind: %v", statErr)
	}
	if _, statErr := os.Stat(launcher); !os.IsNotExist(statErr) {
		t.Fatalf("rejected install left a stable launcher behind: %v", statErr)
	}
}

func TestManifestRejectsUnknownPackageTargetBeforeInstall(t *testing.T) {
	source := copyPackageFixture(t)
	manifestPath := filepath.Join(source, "formal-gates.manifest.json")
	manifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	updated := strings.Replace(string(manifest), `"definitions/"`, `"definitions/", "unknown-target"`, 1)
	if updated == string(manifest) {
		t.Fatal("manifest fixture did not contain the definitions package target")
	}
	if err := os.WriteFile(manifestPath, []byte(updated), 0o600); err != nil {
		t.Fatal(err)
	}
	if result := Package(source); result.OK() || !resultHasPath(result, "formal-gates.manifest.json") {
		t.Fatalf("unknown manifest target was accepted by package validation: %#v", result.Failures)
	}
	// Exercise the local runtime-only vocabulary check. Complete packages use
	// their own validator, tested separately at the real Install boundary.
	if err := os.Remove(filepath.Join(source, "internal", "validate", "runner.go")); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	project := t.TempDir()
	registry := filepath.Join(t.TempDir(), "registry.json")
	launcher := filepath.Join(t.TempDir(), "bin", nativeBinaryName())
	if _, err := Install(InstallOptions{
		Source: source, Host: "claude", Scope: "project", Project: project,
		RegistryPath: registry, BinaryTarget: launcher, Force: true,
	}); err == nil || (!strings.Contains(err.Error(), "unsupported package target") && !strings.Contains(err.Error(), "unknown-target")) {
		t.Fatalf("install did not reject the unknown manifest target: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, ".claude", "skills", "formal-gates")); !os.IsNotExist(err) {
		t.Fatalf("manifest rejection left an install target behind: %v", err)
	}
}

func TestFaultMatrixReceiptProvesPreparedPartialCopy(t *testing.T) {
	source := copyPackageFixture(t)
	report, result := InstallFaultMatrix(InstallFaultMatrixOptions{Root: source, Fixture: "copy-component:runtime"})
	if !result.OK() {
		t.Fatalf("prepared partial-copy fixture failed: %#v", result.Failures)
	}
	if len(report.Checks) != 1 || report.Checks[0].Status != "PASS" {
		t.Fatalf("unexpected partial-copy fixture report: %+v", report.Checks)
	}

	// The public matrix consumes and removes its private fixture. Re-run the
	// native transaction directly to inspect the durable failure receipt.
	fixture := t.TempDir()
	project := filepath.Join(fixture, "project")
	release := filepath.Join(fixture, "release")
	launcher := filepath.Join(fixture, "stable", nativeBinaryName())
	registry := filepath.Join(fixture, "registry.json")
	t.Setenv("FORMAL_GATES_INSTALL_FAULT", "copy-component:runtime")
	if _, err := Install(InstallOptions{
		Source: source, Host: "codex", Scope: "project", Project: project,
		ReleaseRoot: release, BinaryTarget: launcher, RegistryPath: registry, Force: true,
	}); err == nil {
		t.Fatal("partial-copy fault unexpectedly succeeded")
	}
	data, err := os.ReadFile(registry + ".transaction.json.failure.json")
	if err != nil {
		t.Fatal(err)
	}
	var receipt installRecoveryReceipt
	if err := json.Unmarshal(data, &receipt); err != nil {
		t.Fatal(err)
	}
	if !receipt.Prepared || !receipt.PartialCopy || len(receipt.CopiedComponents) == 0 {
		t.Fatalf("recovery receipt lacks prepared partial-copy evidence: %+v", receipt)
	}
}
