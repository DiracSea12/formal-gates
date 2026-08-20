package validate

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGlobalProjectBindingSurvivesUpgradeAndBootstrap(t *testing.T) {
	source := copyPackageFixture(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	project := repairGitProject(t)
	registry := filepath.Join(home, ".formal-gates", "registry.json")
	launcher := filepath.Join(home, ".local", "bin", nativeBinaryName())

	first, err := Install(InstallOptions{
		Source: source, Host: "claude", Scope: "global", RegistryPath: registry,
		BinaryTarget: launcher, Force: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Targets) != 1 {
		t.Fatalf("unexpected global target report: %+v", first)
	}
	if _, err := Start(StartOptions{
		Root: project, PackageRoot: first.Targets[0].TargetPath, RunID: "global-binding",
		Flow: "formal", RequirementSource: "requirements.md", VCS: "git", Split: "no",
		AdmissionRegistry: registry, AdmissionRecordID: first.Targets[0].RegistryRecordID,
	}); err != nil {
		t.Fatal(err)
	}
	state, err := LoadRunState(project, "global-binding")
	if err != nil {
		t.Fatal(err)
	}
	if state.AdmissionRecordID == first.Targets[0].RegistryRecordID {
		t.Fatalf("global workflow did not create the project-local admission sibling: %+v", state)
	}
	if err := DeleteRun(project, state.RunID); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(source, "README.md"), []byte("upgraded package\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := Install(InstallOptions{
		Source: source, Host: "claude", Scope: "global", RegistryPath: registry,
		BinaryTarget: launcher, Force: true,
	})
	if err != nil {
		t.Fatalf("global upgrade rejected the project-local sibling: %v", err)
	}
	document, err := LoadRegistry(registry)
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range document.Records {
		if strings.HasPrefix(record.ID, first.Targets[0].RegistryRecordID+"-project-") &&
			(record.PackageDigest != second.PackageDigest || record.InstalledDigest != second.Targets[0].InstalledDigest) {
			t.Fatalf("global project-local sibling was not refreshed: %+v", record)
		}
	}
	if _, err := Install(InstallOptions{
		Source: source, Host: "claude", Scope: "global", RegistryPath: registry,
		BinaryTarget: launcher, Bootstrap: true, Force: true,
	}); err != nil {
		t.Fatalf("bootstrap rejected the refreshed global sibling: %v", err)
	}
}

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
