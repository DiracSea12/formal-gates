package validate

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"formal-gates/internal/engine/testkit"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/vcs"
)

func TestReleaseRootBootstrapPreservesExecutableModeAndIdentity(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	source := copyPackageFixture(t)
	binary := filepath.Join(source, "bin", nativeBinaryName())
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

	sourceInfo, err := os.Stat(binary)
	if err != nil {
		t.Fatal(err)
	}
	releaseInfo, err := os.Stat(filepath.Join(releaseRoot, "bin", nativeBinaryName()))
	if err != nil {
		t.Fatal(err)
	}
	if releaseInfo.Mode().Perm() != sourceInfo.Mode().Perm() {
		t.Fatalf("release changed source executable mode: source=%v release=%v", sourceInfo.Mode().Perm(), releaseInfo.Mode().Perm())
	}
	if _, err := Install(InstallOptions{
		Source: releaseRoot, Host: "claude", Scope: "project", Project: project,
		ReleaseRoot: releaseRoot, RegistryPath: registry, BinaryTarget: launcher,
		Bootstrap: true, Force: true,
	}); err != nil {
		t.Fatalf("release-root bootstrap rejected the registered release: %v", err)
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
	t.Parallel()
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
	testkit.ConfigureNativePaths(t, root, "claude")
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

// payload manifest 未把请求的 host 登记为可安装 host-target
// （缺失或降级为 explanation-level）时，install 必须在创建任何 target/state
// 之前非零拒绝，而不是以 exit 0 接受一个未登记 target。
func TestInstallRejectsManifestUnregisteredHostTargetBeforeTargetCreation(t *testing.T) {
	t.Parallel()
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

// Start requires the committed bootstrap receipt before creating engine
// state. The install transaction commits that receipt when it commits the
// registry records, so start accepts a freshly installed registry directly
// and still rejects a missing receipt.
func TestWorkflowStartRequiresCommittedBootstrapReceipt(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	source := copyPackageFixture(t)
	project := repairGitProject(t)
	registry := filepath.Join(t.TempDir(), "registry.json")
	launcher := filepath.Join(t.TempDir(), "bin", nativeBinaryName())
	options := InstallOptions{Source: source, Host: "claude", Scope: "project", Project: project, RegistryPath: registry, BinaryTarget: launcher, Force: true}
	report, err := Install(options)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := repairStartEngine(project, report.Targets[0].TargetPath, registry, "bootstrap-receipt"); err != nil {
		t.Fatalf("install-committed bootstrap receipt did not admit start: %v", err)
	}
	if err := os.Remove(registry + ".bootstrap.json"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repairStartEngine(project, report.Targets[0].TargetPath, registry, "bootstrap-receipt-missing"); err == nil || !strings.Contains(err.Error(), "bootstrap receipt") {
		t.Fatalf("start accepted missing bootstrap receipt: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, facade.EngineNamespace, "bootstrap-receipt-missing")); !os.IsNotExist(err) {
		t.Fatalf("rejected start created authority: %v", err)
	}
	options.Bootstrap = true
	if _, err = Install(options); err != nil {
		t.Fatal(err)
	}
	if _, _, err = repairStartEngine(project, report.Targets[0].TargetPath, registry, "bootstrap-receipt-committed"); err != nil {
		t.Fatalf("start refused committed bootstrap: %v", err)
	}
}

// Drive checks current installation admission before writing worker materials.
func TestEngineDriveRejectsDisabledAdmissionBeforeArtifactWrite(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
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
		t.Fatalf("restored drive did not issue material: %+v", restored.WorkerMaterials)
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
	return facade.Start(facade.StartOptions{Root: project, Capacity: 1, ArtifactRoot: project, Request: facade.StartRequest{RunID: runID, VCS: "git", BaseSnapshot: base, Provider: admission.Provider, IntakeConfirmationReceipt: receipt}, Admission: &facade.Admission{RegistryPath: admission.RegistryPath, Target: admission.Target, Provider: admission.Provider, PackageDigest: admission.PackageDigest, InstalledTargetIdentity: admission.RecordID, Generation: admission.Generation, Lease: admission.Lease, Token: admission.Token}})
}

func TestManifestRejectsUnknownPackageTargetBeforeInstall(t *testing.T) {
	t.Parallel()
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
	if testkit.RunNativePackageTest(t) {
		return
	}
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

func TestGlobalInstallRetiresDerivedAdmissionsWithoutChangingRunFiles(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	source := copyPackageFixture(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	registry := filepath.Join(home, ".formal-gates", "registry.json")
	launcher := filepath.Join(home, ".local", "bin", nativeBinaryName())
	fixtureLauncherInvocation(t, launcher)
	options := InstallOptions{Source: source, Host: "codex", Scope: "global", RegistryPath: registry, BinaryTarget: launcher, Force: true, SkipHooks: true}
	installed, err := Install(options)
	if err != nil {
		t.Fatal(err)
	}
	document, err := LoadRegistry(registry)
	if err != nil {
		t.Fatal(err)
	}
	canonical := document.Records[0]
	project := t.TempDir()
	state := filepath.Join(project, ".gates", "engine", "retained", "state.json")
	if err := os.MkdirAll(filepath.Dir(state), 0700); err != nil {
		t.Fatal(err)
	}
	original := []byte("retained user run bytes\n")
	if err := os.WriteFile(state, original, 0600); err != nil {
		t.Fatal(err)
	}
	retired := canonical
	retired.ID += "-project-retained"
	repairCommitRegistry(t, registry, retired)
	if _, err := Install(options); err != nil {
		t.Fatal(err)
	}
	document, err = LoadRegistry(registry)
	if err != nil || len(document.Records) != 1 || document.Records[0].ID != canonical.ID {
		t.Fatal("native install retained derived registry authority", document, err)
	}
	retired.ID += "-another"
	repairCommitRegistry(t, registry, retired)
	options.Bootstrap = true
	if _, err := Install(options); err != nil {
		t.Fatal(err)
	}
	document, err = LoadRegistry(registry)
	if err != nil || len(document.Records) != 1 {
		t.Fatal("native bootstrap retained derived registry authority", err)
	}
	if got, err := os.ReadFile(state); err != nil || string(got) != string(original) {
		t.Fatal("retirement rewrote retained user state", err)
	}
	foreign := canonical
	foreign.ID = "unrelated-global-family"
	repairCommitRegistry(t, registry, foreign)
	before, err := os.ReadFile(registry)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Install(options); err == nil || !strings.Contains(err.Error(), "unaccounted registry identity") {
		t.Fatal("unknown family bypassed normal bootstrap identity", err)
	}
	if after, err := os.ReadFile(registry); err != nil || string(before) != string(after) {
		t.Fatal("unknown family rejection changed registry", err)
	}
	if !isFile(filepath.Join(installed.Targets[0].TargetPath, "bin", nativeBinaryName())) {
		t.Fatal("rejected unknown family removed runtime")
	}
}
