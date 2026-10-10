package validate

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/vcs"
)

func TestGlobalProjectBindingSurvivesUpgradeAndBootstrap(t *testing.T) {
	source := copyPackageFixture(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	project := repairGitProject(t)
	registry := filepath.Join(home, ".formal-gates", "registry.json")
	launcher := filepath.Join(home, ".local", "bin", nativeBinaryName())
	fixtureLauncherInvocation(t, launcher)

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
	// 文档化首启边界：普通 install 只提交 registry record，第一次 workflow start
	// 之前必须先提交 bootstrap receipt。
	if _, err := Install(InstallOptions{
		Source: source, Host: "claude", Scope: "global", RegistryPath: registry,
		BinaryTarget: launcher, Bootstrap: true, Force: true,
	}); err != nil {
		t.Fatalf("first bootstrap rejected the fresh global install: %v", err)
	}
	document, err := LoadRegistry(registry)
	if err != nil {
		t.Fatal(err)
	}
	var sibling RegistryRecord
	for _, record := range document.Records {
		if record.ID == first.Targets[0].RegistryRecordID {
			sibling = record
		}
	}
	sibling.ID += "-project-fixture"
	sibling.ProjectRoot = canonicalRegistryPath(project)
	sibling.StateRoot = canonicalRegistryPath(filepath.Join(project, ".gates"))
	sibling.ResourceRoot = canonicalRegistryPath(filepath.Join(project, ".formal-gates-resources"))
	sibling.CanonicalPaths = map[string]string{}
	for key, value := range document.Records[0].CanonicalPaths {
		sibling.CanonicalPaths[key] = value
	}
	sibling.CanonicalPaths["projectRoot"] = sibling.ProjectRoot
	sibling.CanonicalPaths["stateRoot"] = sibling.StateRoot
	sibling.CanonicalPaths["resourceRoot"] = sibling.ResourceRoot
	repairCommitRegistry(t, registry, sibling)
	admission, err := ResolveEngineAdmission(project, first.Targets[0].TargetPath, registry)
	if err != nil {
		t.Fatal(err)
	}
	if admission.RecordID == first.Targets[0].RegistryRecordID || admission.RecordID != sibling.ID {
		t.Fatalf("global admission lost its existing project-local sibling: %+v", admission)
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
	document, err = LoadRegistry(registry)
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

// The documented release installer first installs from the unpacked source
// while staging a release root, then invokes bootstrap with that release root
// as its source.  Archive/extraction tools may normalize the native
// executable's mode in the copied release tree; that normalization must not
// rotate the already-registered source identity.  The target digest remains
// the tamper fence and the resulting bootstrap receipt must still admit the
// first workflow start.
func TestReleaseRootBootstrapReconcilesNormalizedExecutableMode(t *testing.T) {
	source := copyPackageFixture(t)
	binary := filepath.Join(source, "bin", nativeBinaryName())
	// The source receipt may carry owner-only execute permission.  The
	// installer normalizes the copied release executable by adding execute
	// bits for all users, so the release-tree package digest is intentionally
	// different from the source package digest.
	if err := os.Chmod(binary, 0o700); err != nil {
		t.Fatal(err)
	}
	project := repairGitProject(t)
	registry := filepath.Join(t.TempDir(), "registry.json")
	launcher := filepath.Join(t.TempDir(), "bin", nativeBinaryName())
	releaseRoot := filepath.Join(t.TempDir(), "releases", "v-test")

	first, err := Install(InstallOptions{
		Source: source, Host: "claude", Scope: "project", Project: project,
		ReleaseRoot: releaseRoot, RegistryPath: registry, BinaryTarget: launcher, Force: true,
	})
	if err != nil {
		t.Fatalf("documented release-root install failed: %v", err)
	}
	document, err := LoadRegistry(registry)
	if err != nil || len(document.Records) != 1 {
		t.Fatalf("initial registry=%+v err=%v", document, err)
	}
	before := document.Records[0]

	// The release executable is 0711 after copyFile's native-binary
	// normalization, while the source receipt above recorded 0700.
	releaseInfo, err := os.Stat(filepath.Join(releaseRoot, "bin", nativeBinaryName()))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && releaseInfo.Mode().Perm() != 0o711 {
		t.Fatalf("unexpected normalized release executable mode: %v", releaseInfo.Mode().Perm())
	}
	beforeDigest, err := PackageDigest(source)
	if err != nil {
		t.Fatal(err)
	}
	releaseDigest, err := PackageDigest(releaseRoot)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && releaseDigest == beforeDigest {
		t.Fatalf("release normalization did not change package digest: %s", releaseDigest)
	}
	if _, err := Install(InstallOptions{
		Source: releaseRoot, Host: "claude", Scope: "project", Project: project,
		ReleaseRoot: releaseRoot, RegistryPath: registry, BinaryTarget: launcher,
		Bootstrap: true, Force: true,
	}); err != nil {
		t.Fatalf("release-root bootstrap rejected normalized executable mode: %v", err)
	}

	document, err = LoadRegistry(registry)
	if err != nil || len(document.Records) != 1 {
		t.Fatalf("bootstrapped registry=%+v err=%v", document, err)
	}
	after := document.Records[0]
	if after.ID != before.ID || after.VCSIdentity != before.VCSIdentity ||
		after.PackageDigest != before.PackageDigest || after.InstalledDigest != before.InstalledDigest {
		t.Fatalf("bootstrap rotated registered identity: before=%+v after=%+v", before, after)
	}
	restore := fixtureRegisteredInvocation(registry, first.Targets[0].TargetPath, "")
	defer restore()
	admission, err := ResolveEngineAdmission(project, first.Targets[0].TargetPath, registry)
	if err != nil {
		t.Fatalf("admission after release-root bootstrap failed: %v", err)
	}
	if admission.RecordID != before.ID || admission.PackageDigest != before.PackageDigest {
		t.Fatalf("bootstrap admission lost installed identity: %+v", admission)
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
	if err := os.WriteFile(filepath.Join(root, "requirements.md"), []byte(testRequirementDocument("Sample requirement.")), 0o600); err != nil {
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

// Start requires the committed bootstrap receipt before creating engine state.
func TestWorkflowStartRequiresCommittedBootstrapReceipt(t *testing.T) {
	source := copyPackageFixture(t)
	project := repairGitProject(t)
	registry := filepath.Join(t.TempDir(), "registry.json")
	launcher := filepath.Join(t.TempDir(), "bin", nativeBinaryName())
	options := InstallOptions{Source: source, Host: "claude", Scope: "project", Project: project, RegistryPath: registry, BinaryTarget: launcher, Force: true}
	report, err := Install(options)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := repairStartEngine(project, report.Targets[0].TargetPath, registry, "bootstrap-receipt"); err == nil || !strings.Contains(err.Error(), "bootstrap receipt") {
		t.Fatalf("start accepted missing bootstrap receipt: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, facade.EngineNamespace, "bootstrap-receipt")); !os.IsNotExist(err) {
		t.Fatalf("rejected start created authority: %v", err)
	}
	options.Bootstrap = true
	if _, err = Install(options); err != nil {
		t.Fatal(err)
	}
	if _, _, err = repairStartEngine(project, report.Targets[0].TargetPath, registry, "bootstrap-receipt"); err != nil {
		t.Fatalf("start refused committed bootstrap: %v", err)
	}
}

// Drive checks current installation admission before writing worker materials.
func TestEngineDriveRejectsDisabledAdmissionBeforeArtifactWrite(t *testing.T) {
	source := copyPackageFixture(t)
	project := repairGitProject(t)
	registry := filepath.Join(t.TempDir(), "registry.json")
	launcher := filepath.Join(t.TempDir(), "bin", nativeBinaryName())
	options := InstallOptions{Source: source, Host: "claude", Scope: "project", Project: project, RegistryPath: registry, BinaryTarget: launcher, Force: true}
	report, err := Install(options)
	if err != nil {
		t.Fatal(err)
	}
	options.Bootstrap = true
	if _, err = Install(options); err != nil {
		t.Fatal(err)
	}
	f, run, err := repairStartEngine(project, report.Targets[0].TargetPath, registry, "hard-reject-before-write")
	if err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(project, facade.EngineNamespace, run.RunID, "state.json")
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	document, err := LoadRegistry(registry)
	if err != nil {
		t.Fatal(err)
	}
	record := document.Records[0]
	record.Status = "disabled"
	repairCommitRegistry(t, registry, record)
	if _, err = f.Drive(); err == nil || !strings.HasPrefix(err.Error(), "STALE_FENCING_TOKEN:") {
		t.Fatalf("drive did not reject disabled admission: %v", err)
	}
	if _, err = os.Stat(filepath.Join(project, facade.EngineNamespace, run.RunID, "materials")); !os.IsNotExist(err) {
		t.Fatalf("rejected drive wrote worker artifacts: %v", err)
	}
	after, err := os.ReadFile(statePath)
	if err != nil || string(before) != string(after) {
		t.Fatalf("rejected drive changed authority: %v", err)
	}
	record.Status = "active"
	repairCommitRegistry(t, registry, record)
	restored, err := f.Drive()
	if err != nil {
		t.Fatalf("restored admission refused current drive: %v", err)
	}
	if len(restored.WorkerMaterials) != 1 {
		t.Fatalf("restored regular drive did not issue material: %+v", restored.WorkerMaterials)
	}
	for _, ref := range restored.WorkerMaterials {
		body, err := os.ReadFile(ref.Path)
		if err != nil || !strings.Contains(string(body), "Sample requirement.") {
			t.Fatalf("restored material lost actual requirement: %s %v", body, err)
		}
	}
}

func repairStartEngine(project, target, registry, runID string) (*facade.Facade, facade.Run, error) {
	restore := fixtureRegisteredInvocation(registry, target, "")
	defer restore()
	admission, err := ResolveEngineAdmission(project, target, registry)
	if err != nil {
		return nil, facade.Run{}, err
	}
	raw, err := os.ReadFile(filepath.Join(project, "requirements.md"))
	if err != nil {
		return nil, facade.Run{}, err
	}
	revision := fmt.Sprintf("%x", sha256.Sum256(raw))
	receipt := protocol.IntakeConfirmationReceipt{Source: facade.DefaultIntakeSource, Authority: facade.DefaultIntakeAuthority, Transport: facade.DefaultIntakeTransport, RequirementSource: "requirements.md", RequirementRevision: revision, Artifacts: []protocol.IntakeArtifact{{Path: "requirements.md", Revision: revision}}, SolutionRevision: revision, SolutionDigest: "sha256:" + revision}
	base, err := vcs.New(project, nil).Resolve()
	if err != nil {
		return nil, facade.Run{}, err
	}
	return facade.Start(facade.StartOptions{Root: project, Capacity: 1, PackageRoot: target, ArtifactRoot: project, Request: facade.StartRequest{RunID: runID, Route: "regular", VCS: "git", BaseSnapshot: base, Provider: admission.Provider, IntakeConfirmationReceipt: receipt}, Admission: &facade.Admission{RegistryPath: admission.RegistryPath, Target: admission.Target, Provider: admission.Provider, PackageDigest: admission.PackageDigest, InstalledTargetIdentity: admission.RecordID, Generation: admission.Generation, Lease: admission.Lease, Token: admission.Token}})
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
