package validate

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func round12WriteFile(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
}

func round12ReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func round12Run(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	command := exec.Command(name, args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

func round12RepoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func round12GitProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	return round12GitProjectAt(t, root)
}

func round12GitProjectAt(t *testing.T, root string) string {
	t.Helper()
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	round12WriteFile(t, filepath.Join(root, "requirements.md"), []byte("stage-0 requirement\n"), 0o600)
	round12Run(t, root, "git", "init")
	round12Run(t, root, "git", "config", "user.email", "phase0-round12@example.invalid")
	round12Run(t, root, "git", "config", "user.name", "Phase 0 Round 12 QA")
	round12Run(t, root, "git", "add", "requirements.md")
	round12Run(t, root, "git", "commit", "-m", "baseline")
	return root
}

func round12InstallSource(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	skill := "---\nname: formal-gates\n---\n" + hostInstructionsStartMarker + "\nround twelve rule\n" + hostInstructionsEndMarker + "\n"
	round12WriteFile(t, filepath.Join(root, "SKILL.md"), []byte(skill), 0o600)
	round12WriteFile(t, filepath.Join(root, "README.md"), []byte("round twelve runtime\n"), 0o600)
	round12WriteFile(t, filepath.Join(root, "README_EN.md"), []byte("round twelve runtime\n"), 0o600)
	round12WriteFile(t, filepath.Join(root, "formal-gates.manifest.json"), []byte(`{"name":"formal-gates"}`+"\n"), 0o600)
	round12WriteFile(t, filepath.Join(root, "bin", nativeBinaryName()), []byte("#!/bin/sh\nexit 0\n"), 0o700)
	for _, relative := range []string{"agents/agent.md", "prompts/action.md", "gates/gate.md", "references/reference.md"} {
		round12WriteFile(t, filepath.Join(root, filepath.FromSlash(relative)), []byte(relative+"\n"), 0o600)
	}
	return root
}

func round12Record(id, target, launcher, scope, host, projectRoot, status string) RegistryRecord {
	target = canonicalRegistryPath(target)
	launcher = canonicalRegistryPath(launcher)
	projectRoot = canonicalRegistryPath(projectRoot)
	stateRoot := canonicalRegistryPath(filepath.Join(projectRoot, ".gates"))
	resourceRoot := canonicalRegistryPath(filepath.Join(projectRoot, ".formal-gates-resources"))
	runtimeSibling := canonicalRegistryPath(filepath.Dir(target))
	return RegistryRecord{
		ID: id, Target: target, LauncherPath: launcher, Scope: scope, Host: host,
		ProjectRoot: projectRoot, StateRoot: stateRoot, ResourceRoot: resourceRoot,
		RuntimeSibling: runtimeSibling, Status: status,
		Generation: 1, Lease: "lease-" + id, Token: "token-" + id,
		CanonicalPaths: map[string]string{
			"target": target, "launcher": launcher, "projectRoot": projectRoot,
			"stateRoot": stateRoot, "resourceRoot": resourceRoot, "runtimeSibling": runtimeSibling,
		},
	}
}

func round12WriteRegistry(t *testing.T, path string, epoch uint64, records ...RegistryRecord) []byte {
	t.Helper()
	document, err := json.MarshalIndent(RegistryDocument{SchemaVersion: RegistrySchemaVersion, Epoch: epoch, Records: records}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data := append(document, '\n')
	round12WriteFile(t, path, data, 0o600)
	return data
}

func round12RecordByID(t *testing.T, document RegistryDocument, id string) RegistryRecord {
	t.Helper()
	for _, record := range document.Records {
		if record.ID == id {
			return record
		}
	}
	t.Fatalf("registry record %q was not found", id)
	return RegistryRecord{}
}

func round12PathWithin(parent, path string) bool {
	parent = canonicalRegistryPath(parent)
	path = canonicalRegistryPath(path)
	relative, err := filepath.Rel(parent, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

func round12AssertNoTransactionArtifacts(t *testing.T, registry, target string) {
	t.Helper()
	for _, pattern := range []string{
		filepath.Join(filepath.Dir(registry), ".formal-gates-transaction-*"),
		filepath.Join(filepath.Dir(registry), ".formal-gates-install.lock"),
		filepath.Join(filepath.Dir(target), ".formal-gates-stage-*"),
	} {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		if len(matches) != 0 {
			t.Fatalf("transaction residue matched %s: %v", pattern, matches)
		}
	}
	if _, err := os.Stat(installOuterJournalPath(registry)); !os.IsNotExist(err) {
		t.Fatalf("transaction journal remains: %v", err)
	}
}

func TestWhiteboxPhase0Round12PackageReceiptBindsStableAndInstalledIdentities(t *testing.T) {
	source := round12InstallSource(t)
	installed := round12InstallSource(t)
	round12Run(t, source, "git", "init")
	round12Run(t, source, "git", "config", "user.email", "round12-package@example.invalid")
	round12Run(t, source, "git", "config", "user.name", "Round Twelve Package")
	round12Run(t, source, "git", "add", ".")
	round12Run(t, source, "git", "commit", "-m", "package identity")
	vcsIdentity := round12Run(t, source, "git", "rev-parse", "HEAD")
	hostConfig := filepath.Join(t.TempDir(), "host", "config.json")
	round12WriteFile(t, hostConfig, []byte("{}\n"), 0o600)
	unauthorized := filepath.Join(t.TempDir(), "outside", "sentinel")
	round12WriteFile(t, unauthorized, []byte("must remain\n"), 0o600)

	first, err := PackageReceipt(source, installed)
	if err != nil {
		t.Fatal(err)
	}
	second, err := PackageReceipt(source, installed)
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest == "" || first.Digest != second.Digest || first.GeneratedAt != second.GeneratedAt {
		t.Fatalf("package receipt was not deterministic: first=%+v second=%+v", first, second)
	}
	for _, entry := range first.Entries {
		if !round12PathWithin(source, entry.RealPath) {
			t.Fatalf("package entry escaped source identity: %+v", entry)
		}
	}
	baseline, err := BuildBaselineReceipt("git:"+vcsIdentity, source, installed, map[string]string{"hostConfig": hostConfig})
	if err != nil {
		t.Fatal(err)
	}
	if baseline.VCSIdentity != "git:"+vcsIdentity || baseline.SourceRoot != canonicalRegistryPath(source) || baseline.InstalledTarget != canonicalRegistryPath(installed) || baseline.PackageDigest != first.Digest || baseline.InstalledTargetDigest != second.Digest {
		t.Fatalf("baseline did not bind package identities: %+v", baseline)
	}
	if baseline.CanonicalPaths["hostConfig"] != canonicalRegistryPath(hostConfig) || len(baseline.PackageManifest) != len(first.Entries) {
		t.Fatalf("baseline lost canonical or manifest identity: %+v", baseline)
	}
	if got := round12ReadFile(t, unauthorized); string(got) != "must remain\n" {
		t.Fatalf("package receipt touched an unauthorized namespace: %q", got)
	}
	if _, err := os.Stat(filepath.Join(source, ".gates")); !os.IsNotExist(err) {
		t.Fatalf("package receipt created a workflow namespace: %v", err)
	}
}

func TestWhiteboxPhase0Round12BaselineReceiptValidatesManifestPathsAndIdentityBoundaries(t *testing.T) {
	source := round12InstallSource(t)
	installed := round12InstallSource(t)
	stateRoot := t.TempDir()
	resourceRoot := t.TempDir()
	registry := filepath.Join(t.TempDir(), "registry.json")
	round12WriteFile(t, registry, []byte("registry\n"), 0o600)
	hostConfig := filepath.Join(t.TempDir(), "host.json")
	round12WriteFile(t, hostConfig, []byte("{}\n"), 0o600)

	receipt, err := BuildBaselineReceipt("git:round12", source, installed, map[string]string{
		"hostConfig": hostConfig, "stateRoot": stateRoot, "resourceRoot": resourceRoot, "registry": registry,
	})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.InstalledTarget != canonicalRegistryPath(installed) || receipt.InstalledTargetDigest == "" || receipt.PackageDigest == "" {
		t.Fatalf("installed identity is incomplete: %+v", receipt)
	}
	for _, entry := range receipt.PackageManifest {
		if !filepath.IsAbs(entry.RealPath) || !round12PathWithin(source, entry.RealPath) || entry.Size < 0 || entry.SHA256 == "" {
			t.Fatalf("manifest entry lacks realpath/Lstat-like identity: %+v", entry)
		}
	}
	for name, path := range receipt.CanonicalPaths {
		if !filepath.IsAbs(path) || strings.TrimSpace(name) == "" {
			t.Fatalf("canonical path is incomplete: %q=%q", name, path)
		}
	}
	overlapCases := []struct {
		name string
		root string
		path string
	}{
		{"installed equals source", source, source},
		{"canonical path overlaps source", source, source},
		{"canonical path overlaps installed", installed, installed},
	}
	for _, test := range overlapCases {
		t.Run(test.name, func(t *testing.T) {
			if _, err := BuildBaselineReceipt("git:round12", test.root, test.path, nil); err == nil || !strings.Contains(err.Error(), "overlaps") {
				t.Fatalf("overlapping identity was accepted: %v", err)
			}
		})
	}
	for name, invalid := range map[string]BaselineReceipt{
		"missing VCS":     func() BaselineReceipt { value := receipt; value.VCSIdentity = ""; return value }(),
		"missing package": func() BaselineReceipt { value := receipt; value.PackageDigest = ""; return value }(),
	} {
		t.Run(name, func(t *testing.T) {
			if err := WriteBaselineReceipt(filepath.Join(t.TempDir(), "invalid.json"), invalid); err == nil {
				t.Fatal("incomplete identity was persisted")
			}
		})
	}
}

func TestWhiteboxPhase0Round12RegistryOwnerPreservesRecordsAcrossInstallAndUninstall(t *testing.T) {
	source := round12InstallSource(t)
	project := t.TempDir()
	registry := filepath.Join(t.TempDir(), "registry.json")
	launcher := filepath.Join(t.TempDir(), "stable", nativeBinaryName())
	if _, err := Install(InstallOptions{Source: source, Host: "claude", Scope: "project", Project: project, RegistryPath: registry, BinaryTarget: launcher, Force: true}); err != nil {
		t.Fatal(err)
	}
	firstDocument, err := LoadRegistry(registry)
	if err != nil {
		t.Fatal(err)
	}
	claudeBefore := round12RecordByID(t, firstDocument, firstDocument.Records[0].ID)
	claudeTarget, err := resolveInstallTargets("claude", "project", project)
	if err != nil {
		t.Fatal(err)
	}
	claudeDigest, err := PackageDigest(claudeTarget[0].targetPath)
	if err != nil {
		t.Fatal(err)
	}
	claudeHook := round12ReadFile(t, claudeTarget[0].hookConfig)
	claudeRule := round12ReadFile(t, claudeTarget[0].managedRulePath)

	if _, err := Install(InstallOptions{Source: source, Host: "codex", Scope: "project", Project: project, RegistryPath: registry, BinaryTarget: launcher, Force: true}); err != nil {
		t.Fatal(err)
	}
	secondDocument, err := LoadRegistry(registry)
	if err != nil {
		t.Fatal(err)
	}
	claudeAfter := round12RecordByID(t, secondDocument, claudeBefore.ID)
	if !reflect.DeepEqual(claudeBefore, claudeAfter) {
		t.Fatalf("install of a second host changed the existing record: before=%+v after=%+v", claudeBefore, claudeAfter)
	}
	if secondDocument.Epoch <= firstDocument.Epoch {
		t.Fatalf("registry epoch did not advance monotonically: before=%d after=%d", firstDocument.Epoch, secondDocument.Epoch)
	}
	if got, err := PackageDigest(claudeTarget[0].targetPath); err != nil || got != claudeDigest {
		t.Fatalf("second install changed the existing runtime: before=%s after=%s err=%v", claudeDigest, got, err)
	}
	if !bytes.Equal(round12ReadFile(t, claudeTarget[0].hookConfig), claudeHook) || !bytes.Equal(round12ReadFile(t, claudeTarget[0].managedRulePath), claudeRule) {
		t.Fatal("second host transaction changed the first host configuration")
	}

	codexID := ""
	for _, record := range secondDocument.Records {
		if record.Host == "codex" {
			codexID = record.ID
			if record.Generation == 0 || record.Lease == "" || record.Token == "" {
				t.Fatalf("new record lacks monotonic transaction identity: %+v", record)
			}
		}
	}
	if codexID == "" {
		t.Fatal("second host record was not written by the install owner")
	}
	if _, err := Uninstall(UninstallOptions{Host: "codex", Scope: "project", Project: project, RegistryPath: registry}); err != nil {
		t.Fatal(err)
	}
	thirdDocument, err := LoadRegistry(registry)
	if err != nil {
		t.Fatal(err)
	}
	if thirdDocument.Epoch <= secondDocument.Epoch {
		t.Fatalf("uninstall registry epoch regressed: before=%d after=%d", secondDocument.Epoch, thirdDocument.Epoch)
	}
	if got := round12RecordByID(t, thirdDocument, claudeBefore.ID); !reflect.DeepEqual(got, claudeBefore) {
		t.Fatalf("uninstall changed the unrelated record: before=%+v after=%+v", claudeBefore, got)
	}
	codexAfter := round12RecordByID(t, thirdDocument, codexID)
	if codexAfter.Status != "disabled" || codexAfter.Generation == 0 || codexAfter.Lease == "" || codexAfter.Token == "" {
		t.Fatalf("uninstall lost transaction identity: %+v", codexAfter)
	}
}

func TestWhiteboxPhase0Round12BootstrapReceiptBindsExactRegistryIdentity(t *testing.T) {
	source := round12InstallSource(t)
	project := t.TempDir()
	registry := filepath.Join(t.TempDir(), "registry.json")
	launcher := filepath.Join(t.TempDir(), "stable", nativeBinaryName())
	targets, err := resolveInstallTargets("claude", "project", project)
	if err != nil {
		t.Fatal(err)
	}
	if err := copyInstallRuntime(source, targets[0].targetPath, true); err != nil {
		t.Fatal(err)
	}
	round12WriteFile(t, launcher, round12ReadFile(t, filepath.Join(source, "bin", nativeBinaryName())), 0o700)
	report, err := Install(InstallOptions{Source: source, Host: "claude", Scope: "project", Project: project, RegistryPath: registry, BinaryTarget: launcher, Bootstrap: true})
	if err != nil {
		t.Fatal(err)
	}
	document, err := LoadRegistry(registry)
	if err != nil || len(document.Records) != 1 {
		t.Fatalf("bootstrap registry is incomplete: doc=%+v err=%v", document, err)
	}
	var receipt struct {
		Operation     string           `json:"operation"`
		PackageDigest string           `json:"packageDigest"`
		VCSIdentity   string           `json:"vcsIdentity"`
		Records       []RegistryRecord `json:"records"`
		StateCreated  bool             `json:"stateCreated"`
	}
	if err := json.Unmarshal(round12ReadFile(t, report.BootstrapReceiptPath), &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Operation != "bootstrap" || receipt.StateCreated || receipt.PackageDigest == "" || receipt.VCSIdentity == "" || len(receipt.Records) != 1 {
		t.Fatalf("bootstrap receipt omitted boundary identity: %+v", receipt)
	}
	if !reflect.DeepEqual(receipt.Records[0], document.Records[0]) {
		t.Fatalf("bootstrap receipt and registry record differ: receipt=%+v registry=%+v", receipt.Records[0], document.Records[0])
	}
	for key, value := range document.Records[0].CanonicalPaths {
		if value == "" || !filepath.IsAbs(value) || filepath.Clean(value) != canonicalRegistryPath(value) {
			t.Fatalf("registry canonical identity is incomplete: %q=%q", key, value)
		}
	}
	if _, err := os.Stat(filepath.Join(project, ".gates")); !os.IsNotExist(err) {
		t.Fatalf("bootstrap created workflow state before start: %v", err)
	}
}

func TestWhiteboxPhase0Round12AdmissionRejectsInvalidRecordsBeforeNamespaceWrites(t *testing.T) {
	packageRoot := round12RepoRoot(t)
	cases := []struct {
		name   string
		record func(root string) RegistryRecord
		id     string
	}{
		{"incomplete", func(root string) RegistryRecord { return RegistryRecord{ID: "incomplete", Status: "active"} }, "incomplete"},
		{"conflicting canonical target", func(root string) RegistryRecord {
			record := round12Record("conflict", packageRoot, filepath.Join(root, "launcher", nativeBinaryName()), "project", "codex", root, "active")
			record.CanonicalPaths["target"] = canonicalRegistryPath(filepath.Join(root, "other-package"))
			return record
		}, "conflict"},
		{"missing", func(root string) RegistryRecord {
			return round12Record("present", packageRoot, filepath.Join(root, "launcher", nativeBinaryName()), "project", "codex", root, "active")
		}, "missing"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			root := round12GitProject(t)
			registry := filepath.Join(t.TempDir(), "registry.json")
			record := test.record(root)
			before := round12WriteRegistry(t, registry, 7, record)
			if test.name == "missing" {
				test.id = "absent"
			}
			runID := "round12-" + test.id
			_, err := Start(StartOptions{Root: root, PackageRoot: packageRoot, RunID: runID, Flow: "formal", RequirementSource: "requirements.md", VCS: "git", Split: "no", AdmissionRegistry: registry, AdmissionRecordID: test.id})
			if err == nil || !strings.Contains(err.Error(), "UNREGISTERED_INSTALL") {
				t.Fatalf("invalid admission was accepted: %v", err)
			}
			if got := round12ReadFile(t, registry); !bytes.Equal(got, before) {
				t.Fatalf("authoritative registry bytes changed on rejected admission: before=%q after=%q", before, got)
			}
			if _, err := os.Stat(RunDir(root, runID)); !os.IsNotExist(err) {
				t.Fatalf("rejected admission created a run directory: %v", err)
			}
			if _, err := os.Stat(filepath.Join(root, ".gates")); !os.IsNotExist(err) {
				t.Fatalf("rejected admission created a workflow namespace: %v", err)
			}
		})
	}
}

func TestWhiteboxPhase0Round12WorkflowStateUsesNestedProjectBinding(t *testing.T) {
	packageRoot := round12RepoRoot(t)
	parent := t.TempDir()
	nested := filepath.Join(parent, "nested")
	round12GitProjectAt(t, nested)
	registry := filepath.Join(t.TempDir(), "registry.json")
	launcher := filepath.Join(t.TempDir(), "launcher", nativeBinaryName())
	parentRecord := round12Record("parent", packageRoot, launcher, "project", "codex", parent, "active")
	nestedRecord := round12Record("nested", packageRoot, launcher, "project", "codex", nested, "active")
	round12WriteRegistry(t, registry, 12, parentRecord, nestedRecord)
	if _, err := Start(StartOptions{Root: nested, PackageRoot: packageRoot, RunID: "round12-parent", Flow: "formal", RequirementSource: "requirements.md", VCS: "git", Split: "no", AdmissionRegistry: registry, AdmissionRecordID: parentRecord.ID}); err == nil || !strings.Contains(err.Error(), "workflow root") {
		t.Fatalf("less-specific project binding was accepted for nested root: %v", err)
	}
	state, err := Start(StartOptions{Root: nested, PackageRoot: packageRoot, RunID: "round12-nested", Flow: "formal", RequirementSource: "requirements.md", VCS: "git", Split: "no", AdmissionRegistry: registry, AdmissionRecordID: nestedRecord.ID})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadRunState(nested, state.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.AdmissionRecordID != nestedRecord.ID || loaded.AdmissionRoot != nested || loaded.AdmissionTarget != packageRoot || loaded.AdmissionGeneration != nestedRecord.Generation || loaded.AdmissionLease != nestedRecord.Lease || loaded.AdmissionToken != nestedRecord.Token {
		t.Fatalf("reloaded state lost the nested binding identity: %+v", loaded)
	}
}

func TestWhiteboxPhase0Round12GlobalAdmissionDerivesProjectLocalRoots(t *testing.T) {
	packageRoot := round12RepoRoot(t)
	hostRoot := t.TempDir()
	project := round12GitProject(t)
	otherProject := t.TempDir()
	registry := filepath.Join(t.TempDir(), "registry.json")
	launcher := filepath.Join(hostRoot, "bin", nativeBinaryName())
	global := round12Record("global", packageRoot, launcher, "global", "codex", hostRoot, "active")
	round12WriteRegistry(t, registry, 20, global)
	state, err := Start(StartOptions{Root: project, PackageRoot: packageRoot, RunID: "round12-global", Flow: "formal", RequirementSource: "requirements.md", VCS: "git", Split: "no", AdmissionRegistry: registry, AdmissionRecordID: global.ID})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadRunState(project, state.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.AdmissionRecordID == global.ID || loaded.AdmissionRoot != project || loaded.AdmissionTarget != packageRoot {
		t.Fatalf("global invocation did not derive a project binding: %+v", loaded)
	}
	document, err := LoadRegistry(registry)
	if err != nil {
		t.Fatal(err)
	}
	derived := round12RecordByID(t, document, loaded.AdmissionRecordID)
	if derived.Target != global.Target || derived.LauncherPath != global.LauncherPath || derived.ProjectRoot != canonicalRegistryPath(project) || derived.StateRoot != canonicalRegistryPath(filepath.Join(project, ".gates")) || derived.ResourceRoot != canonicalRegistryPath(filepath.Join(project, ".formal-gates-resources")) {
		t.Fatalf("derived global binding has the wrong project-local roots: %+v", derived)
	}
	if _, err := os.Stat(filepath.Join(hostRoot, ".gates")); !os.IsNotExist(err) {
		t.Fatalf("global host namespace received project state: %v", err)
	}
	if _, err := os.Stat(filepath.Join(hostRoot, ".formal-gates-resources")); !os.IsNotExist(err) {
		t.Fatalf("global host namespace received project resources: %v", err)
	}
	if _, err := os.Stat(filepath.Join(otherProject, ".gates")); !os.IsNotExist(err) {
		t.Fatalf("unrelated project namespace was modified: %v", err)
	}
}

func TestWhiteboxPhase0Round12LegacyLifecyclePreservesExistingStateAndNamespaces(t *testing.T) {
	root := round12GitProject(t)
	packageRoot := round12RepoRoot(t)
	resourceSentinel := filepath.Join(t.TempDir(), "resources", "sentinel")
	registrySentinel := filepath.Join(t.TempDir(), "registry.json")
	round12WriteFile(t, resourceSentinel, []byte("resource\n"), 0o600)
	round12WriteFile(t, registrySentinel, []byte("registry\n"), 0o600)
	state, err := Start(StartOptions{Root: root, PackageRoot: packageRoot, RunID: "round12-legacy", Flow: "formal", RequirementSource: "requirements.md", VCS: "git", Split: "no"})
	if err != nil {
		t.Fatal(err)
	}
	statePath := RunStatePath(root, state.RunID)
	beforeState := round12ReadFile(t, statePath)
	if strings.Contains(string(beforeState), "stateSchemaVersion") || strings.Contains(string(beforeState), "workflowDefinitionVersion") || strings.Contains(string(beforeState), "definitionDigest") {
		t.Fatalf("legacy state unexpectedly has future envelope fields: %s", beforeState)
	}
	if _, err := LoadRunState(root, state.RunID); err != nil {
		t.Fatal(err)
	}
	resume, err := ResumeReport(root, packageRoot, state.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if resume.ClassificationRequired || resume.NativeDrifted {
		t.Fatalf("legacy resume reported an unexpected drift: %+v", resume)
	}
	diagnose, err := DiagnoseState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if diagnose.Summary["status"] != "ACTIVE" || diagnose.Summary["runId"] != state.RunID {
		t.Fatalf("legacy diagnose lost terminal fallback fields: %+v", diagnose)
	}
	if got := round12ReadFile(t, statePath); !bytes.Equal(got, beforeState) || !bytes.Equal(round12ReadFile(t, resourceSentinel), []byte("resource\n")) || !bytes.Equal(round12ReadFile(t, registrySentinel), []byte("registry\n")) {
		t.Fatal("legacy show/resume/diagnose changed existing state or unrelated namespaces")
	}
}

func TestWhiteboxPhase0Round12InstallAndUninstallUseTheSameLockOwner(t *testing.T) {
	source := round12InstallSource(t)
	project := t.TempDir()
	registry := filepath.Join(t.TempDir(), "registry.json")
	launcher := filepath.Join(t.TempDir(), "launcher", nativeBinaryName())
	lockPath := installLockPath(registry)
	unlock, err := acquireInstallLock(registry)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Install(InstallOptions{Source: source, Host: "claude", Scope: "project", Project: project, RegistryPath: registry, BinaryTarget: launcher, Force: true}); err == nil || !strings.Contains(err.Error(), "lock is held") {
		t.Fatalf("install did not use the shared lock: %v", err)
	}
	if _, err := Uninstall(UninstallOptions{Host: "claude", Scope: "project", Project: project, RegistryPath: registry}); err == nil || !strings.Contains(err.Error(), "lock is held") {
		t.Fatalf("uninstall did not use the shared lock: %v", err)
	}
	unlock()
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("released install lock remains: %v", err)
	}
	if _, err := Install(InstallOptions{Source: source, Host: "claude", Scope: "project", Project: project, RegistryPath: registry, BinaryTarget: launcher, Force: true}); err != nil {
		t.Fatal(err)
	}
	targets, err := resolveInstallTargets("claude", "project", project)
	if err != nil {
		t.Fatal(err)
	}
	beforeDigest, err := PackageDigest(targets[0].targetPath)
	if err != nil {
		t.Fatal(err)
	}
	registryBefore := round12ReadFile(t, registry)
	unlock, err = acquireInstallLock(registry)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Uninstall(UninstallOptions{Host: "claude", Scope: "project", Project: project, RegistryPath: registry}); err == nil || !strings.Contains(err.Error(), "lock is held") {
		t.Fatalf("installed uninstall did not honor the shared lock: %v", err)
	}
	unlock()
	if got, err := PackageDigest(targets[0].targetPath); err != nil || got != beforeDigest || !bytes.Equal(round12ReadFile(t, registry), registryBefore) {
		t.Fatalf("busy uninstall changed transaction identity: digest=%s err=%v", got, err)
	}
	if _, err := Uninstall(UninstallOptions{Host: "claude", Scope: "project", Project: project, RegistryPath: registry}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("completed uninstall left the shared lock: %v", err)
	}
}

func TestWhiteboxPhase0Round12InstallFaultsRestoreRegistryIdentityAndCleanup(t *testing.T) {
	for _, fault := range []string{"intent", "registry", "prepared", "switched", "post-switch-smoke", "pointer", "hook", "managed-rule", "registry-commit"} {
		t.Run(fault, func(t *testing.T) {
			source := round12InstallSource(t)
			project := t.TempDir()
			registry := filepath.Join(t.TempDir(), "registry.json")
			launcher := filepath.Join(t.TempDir(), "launcher", nativeBinaryName())
			registryBefore := round12WriteRegistry(t, registry, 4)
			targets, err := resolveInstallTargets("claude", "project", project)
			if err != nil {
				t.Fatal(err)
			}
			round12WriteFile(t, filepath.Join(targets[0].targetPath, "old.txt"), []byte("old runtime\n"), 0o600)
			round12WriteFile(t, targets[0].hookConfig, []byte(`{"hooks":{"External":[{"command":"keep"}]}}`+"\n"), 0o600)
			round12WriteFile(t, targets[0].managedRulePath, []byte("external rule\n"), 0o600)
			round12WriteFile(t, launcher, []byte("old launcher\n"), 0o700)
			t.Setenv("FORMAL_GATES_INSTALL_FAULT", fault)
			_, err = Install(InstallOptions{Source: source, Host: "claude", Scope: "project", Project: project, RegistryPath: registry, BinaryTarget: launcher, Force: true})
			if err == nil || !strings.Contains(err.Error(), "deterministic install fault") {
				t.Fatalf("fault %q did not stop installation: %v", fault, err)
			}
			if got := round12ReadFile(t, filepath.Join(targets[0].targetPath, "old.txt")); string(got) != "old runtime\n" || string(round12ReadFile(t, targets[0].hookConfig)) != `{"hooks":{"External":[{"command":"keep"}]}}`+"\n" || string(round12ReadFile(t, targets[0].managedRulePath)) != "external rule\n" || string(round12ReadFile(t, launcher)) != "old launcher\n" {
				t.Fatal("install fault did not restore old runtime/configuration identity")
			}
			if got := round12ReadFile(t, registry); !bytes.Equal(got, registryBefore) {
				t.Fatalf("install fault changed authoritative registry: before=%q after=%q", registryBefore, got)
			}
			var receipt installRecoveryReceipt
			if err := json.Unmarshal(round12ReadFile(t, installOuterJournalPath(registry)+".failure.json"), &receipt); err != nil {
				t.Fatal(err)
			}
			if !receipt.Recovered || receipt.Outcome != "ROLLED_BACK" || receipt.VCSIdentity == "" || receipt.PackageDigest == "" || receipt.InstalledTarget == "" || receipt.Generation == 0 || receipt.Lease == "" || receipt.Token == "" || receipt.RecoveryReceipt == "" || receipt.StableDigest == "" {
				t.Fatalf("recovery identity is incomplete: %+v", receipt)
			}
			round12AssertNoTransactionArtifacts(t, registry, targets[0].targetPath)
		})
	}
}

func TestWhiteboxPhase0Round12MalformedHookRestoresRegistryAndRecoveryEvidence(t *testing.T) {
	source := round12InstallSource(t)
	project := t.TempDir()
	registry := filepath.Join(t.TempDir(), "registry.json")
	launcher := filepath.Join(t.TempDir(), "launcher", nativeBinaryName())
	registryBefore := round12WriteRegistry(t, registry, 5)
	targets, err := resolveInstallTargets("claude", "project", project)
	if err != nil {
		t.Fatal(err)
	}
	hookBefore := []byte("{malformed hook\n")
	ruleBefore := []byte("external rule\n")
	round12WriteFile(t, filepath.Join(targets[0].targetPath, "old.txt"), []byte("old runtime\n"), 0o600)
	round12WriteFile(t, targets[0].hookConfig, hookBefore, 0o600)
	round12WriteFile(t, targets[0].managedRulePath, ruleBefore, 0o600)
	round12WriteFile(t, launcher, []byte("old launcher\n"), 0o700)
	if _, err := Install(InstallOptions{Source: source, Host: "claude", Scope: "project", Project: project, RegistryPath: registry, BinaryTarget: launcher, Force: true}); err == nil || !strings.Contains(strings.ToLower(err.Error()), "json") {
		t.Fatalf("malformed hook did not stop install: %v", err)
	}
	if !bytes.Equal(round12ReadFile(t, targets[0].hookConfig), hookBefore) || !bytes.Equal(round12ReadFile(t, targets[0].managedRulePath), ruleBefore) || !bytes.Equal(round12ReadFile(t, registry), registryBefore) || string(round12ReadFile(t, launcher)) != "old launcher\n" {
		t.Fatal("malformed hook rollback did not preserve all authoritative bytes")
	}
	var receipt installRecoveryReceipt
	if err := json.Unmarshal(round12ReadFile(t, installOuterJournalPath(registry)+".failure.json"), &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Phase != "smoke-passed" || !receipt.Recovered || receipt.Outcome != "ROLLED_BACK" || receipt.VCSIdentity == "" || receipt.PackageDigest == "" || receipt.Generation == 0 || receipt.Lease == "" || receipt.Token == "" || receipt.StableDigest == "" || receipt.RecoveryReceipt == "" {
		t.Fatalf("malformed-hook recovery evidence is incomplete: %+v", receipt)
	}
	round12AssertNoTransactionArtifacts(t, registry, targets[0].targetPath)
}

func TestWhiteboxPhase0Round12UninstallFaultsReconcileAndPreserveExternalContent(t *testing.T) {
	t.Run("install post-switch smoke recovery", func(t *testing.T) {
		source := round12InstallSource(t)
		project := t.TempDir()
		registry := filepath.Join(t.TempDir(), "registry.json")
		launcher := filepath.Join(t.TempDir(), "launcher", nativeBinaryName())
		registryBefore := round12WriteRegistry(t, registry, 2)
		targets, err := resolveInstallTargets("claude", "project", project)
		if err != nil {
			t.Fatal(err)
		}
		round12WriteFile(t, filepath.Join(targets[0].targetPath, "stable.txt"), []byte("stable\n"), 0o600)
		round12WriteFile(t, launcher, []byte("stable launcher\n"), 0o700)
		t.Setenv("FORMAL_GATES_INSTALL_FAULT", "post-switch-smoke")
		if _, err := Install(InstallOptions{Source: source, Host: "claude", Scope: "project", Project: project, RegistryPath: registry, BinaryTarget: launcher, Force: true}); err == nil {
			t.Fatal("post-switch smoke fault unexpectedly succeeded")
		}
		if string(round12ReadFile(t, filepath.Join(targets[0].targetPath, "stable.txt"))) != "stable\n" || string(round12ReadFile(t, launcher)) != "stable launcher\n" || !bytes.Equal(round12ReadFile(t, registry), registryBefore) {
			t.Fatal("post-switch smoke recovery did not preserve the stable identity")
		}
		round12AssertNoTransactionArtifacts(t, registry, targets[0].targetPath)
	})

	for _, fault := range []string{"intent", "switched", "managed-rule", "hook"} {
		t.Run("uninstall "+fault, func(t *testing.T) {
			source := round12InstallSource(t)
			project := t.TempDir()
			registry := filepath.Join(t.TempDir(), "registry.json")
			launcher := filepath.Join(t.TempDir(), "launcher", nativeBinaryName())
			targets, err := resolveInstallTargets("claude", "project", project)
			if err != nil {
				t.Fatal(err)
			}
			externalHook := []byte(`{"hooks":{"External":[{"command":"keep"}]}}` + "\n")
			externalRule := []byte("external rule\n")
			round12WriteFile(t, targets[0].hookConfig, externalHook, 0o600)
			round12WriteFile(t, targets[0].managedRulePath, externalRule, 0o600)
			if _, err := Install(InstallOptions{Source: source, Host: "claude", Scope: "project", Project: project, RegistryPath: registry, BinaryTarget: launcher, Force: true}); err != nil {
				t.Fatal(err)
			}
			registryBefore := round12ReadFile(t, registry)
			hookBefore := round12ReadFile(t, targets[0].hookConfig)
			ruleBefore := round12ReadFile(t, targets[0].managedRulePath)
			t.Setenv("FORMAL_GATES_INSTALL_FAULT", fault)
			if _, err := Uninstall(UninstallOptions{Host: "claude", Scope: "project", Project: project, RegistryPath: registry}); err == nil || !strings.Contains(err.Error(), "deterministic install fault") {
				t.Fatalf("uninstall fault %q did not stop the transaction: %v", fault, err)
			}
			if _, err := os.Stat(filepath.Join(targets[0].targetPath, "README.md")); err != nil {
				t.Fatalf("uninstall rollback did not restore runtime: %v", err)
			}
			if !bytes.Equal(round12ReadFile(t, targets[0].hookConfig), hookBefore) || !bytes.Equal(round12ReadFile(t, targets[0].managedRulePath), ruleBefore) || !bytes.Equal(round12ReadFile(t, registry), registryBefore) {
				t.Fatal("uninstall rollback did not preserve external/configuration bytes")
			}
			var receipt installRecoveryReceipt
			if err := json.Unmarshal(round12ReadFile(t, installOuterJournalPath(registry)+".failure.json"), &receipt); err != nil {
				t.Fatal(err)
			}
			if receipt.Operation != "uninstall" || !receipt.Recovered || receipt.Outcome != "ROLLED_BACK" || receipt.Generation == 0 || receipt.Lease == "" || receipt.Token == "" || receipt.RecoveryReceipt == "" || receipt.StableDigest == "" {
				t.Fatalf("uninstall recovery evidence is incomplete: %+v", receipt)
			}
			round12AssertNoTransactionArtifacts(t, registry, targets[0].targetPath)
		})
	}
}

func TestWhiteboxPhase0Round12SupportedRestartReconcilesCrashJournalAutomatically(t *testing.T) {
	source := round12InstallSource(t)
	project := t.TempDir()
	root := t.TempDir()
	target := filepath.Join(project, ".claude", "skills", "formal-gates")
	registry := filepath.Join(root, "registry.json")
	launcher := filepath.Join(root, "launcher", nativeBinaryName())
	registryBefore := round12WriteRegistry(t, registry, 3)
	round12WriteFile(t, filepath.Join(target, "stable.txt"), []byte("stable\n"), 0o600)
	round12WriteFile(t, registry, registryBefore, 0o600)
	transactionRoot := filepath.Join(root, "crashed-transaction")
	tree, err := snapshotInstallTree(target, filepath.Join(transactionRoot, "target.before"))
	if err != nil {
		t.Fatal(err)
	}
	registrySnapshot, err := snapshotOuterFile(registry, filepath.Join(transactionRoot, "registry.before"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(target); err != nil {
		t.Fatal(err)
	}
	round12WriteFile(t, filepath.Join(target, "candidate.txt"), []byte("candidate\n"), 0o600)
	staged := filepath.Join(project, ".formal-gates-stage-crashed")
	round12WriteFile(t, staged, []byte("staged\n"), 0o600)
	round12WriteFile(t, registry, []byte(`{"schemaVersion":1,"epoch":4,"records":[]}`+"\n"), 0o600)
	journalPath := installOuterJournalPath(registry)
	journal := outerInstallJournal{
		Operation: "install", RegistryPath: registry, TransactionRoot: transactionRoot, Phase: "switched",
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), Registry: registrySnapshot,
		Targets: []outerTargetSnapshot{{TargetPath: target, Tree: outerTreeFromBackup(tree)}},
		Staged:  []string{staged}, Generation: 9, Lease: "lease-crashed", Token: "token-crashed",
	}
	if err := writeJSONAtomically(journalPath, journal); err != nil {
		t.Fatal(err)
	}
	round12WriteFile(t, installLockPath(registry), []byte("pid=999999999\n"), 0o600)
	t.Setenv("FORMAL_GATES_INSTALL_FAULT", "intent")
	if _, err := Install(InstallOptions{Source: source, Host: "claude", Scope: "project", Project: project, RegistryPath: registry, BinaryTarget: launcher, Force: true}); err == nil || !strings.Contains(err.Error(), "deterministic install fault") {
		t.Fatalf("supported restart did not reach the new transaction after reconcile: %v", err)
	}
	if string(round12ReadFile(t, filepath.Join(target, "stable.txt"))) != "stable\n" || !bytes.Equal(round12ReadFile(t, registry), registryBefore) {
		t.Fatal("automatic restart reconcile did not restore stable bytes")
	}
	var recovered installRecoveryReceipt
	if err := json.Unmarshal(round12ReadFile(t, journalPath+".receipt.json"), &recovered); err != nil {
		t.Fatal(err)
	}
	if !recovered.Recovered || recovered.Outcome != "RECOVERED" || recovered.Phase != "switched" || recovered.Generation != journal.Generation || recovered.Lease != journal.Lease || recovered.Token != journal.Token || recovered.StableDigest == "" {
		t.Fatalf("automatic reconcile receipt is incomplete: %+v", recovered)
	}
	if _, err := os.Stat(staged); !os.IsNotExist(err) {
		t.Fatalf("crashed staged path remains after supported restart: %v", err)
	}
	round12AssertNoTransactionArtifacts(t, registry, target)
}

func TestWhiteboxPhase0Round12ReleaseRollbackRestoresAllOuterIdentity(t *testing.T) {
	source := round12InstallSource(t)
	root := t.TempDir()
	project := filepath.Join(root, "project")
	release := filepath.Join(root, "release")
	launcher := filepath.Join(root, "stable", nativeBinaryName())
	registry := filepath.Join(root, "registry", "registry.json")
	targets, err := resolveInstallTargets("claude", "project", project)
	if err != nil {
		t.Fatal(err)
	}
	registryBefore := round12WriteRegistry(t, registry, 6)
	round12WriteFile(t, filepath.Join(release, "old-release.txt"), []byte("old release\n"), 0o600)
	round12WriteFile(t, launcher, []byte("old launcher\n"), 0o700)
	round12WriteFile(t, filepath.Join(targets[0].targetPath, "old.txt"), []byte("old runtime\n"), 0o600)
	hookBefore := []byte(`{"hooks":{"External":[{"command":"keep"}]}}` + "\n")
	ruleBefore := []byte("external rule\n")
	round12WriteFile(t, targets[0].hookConfig, hookBefore, 0o600)
	round12WriteFile(t, targets[0].managedRulePath, ruleBefore, 0o600)
	t.Setenv("FORMAL_GATES_INSTALL_FAULT", "registry-commit")
	if _, err := Install(InstallOptions{Source: source, Host: "claude", Scope: "project", Project: project, ReleaseRoot: release, BinaryTarget: launcher, RegistryPath: registry, Force: true}); err == nil || !strings.Contains(err.Error(), "deterministic install fault") {
		t.Fatalf("release transaction did not fail at registry commit: %v", err)
	}
	if string(round12ReadFile(t, filepath.Join(release, "old-release.txt"))) != "old release\n" || string(round12ReadFile(t, launcher)) != "old launcher\n" || string(round12ReadFile(t, filepath.Join(targets[0].targetPath, "old.txt"))) != "old runtime\n" || !bytes.Equal(round12ReadFile(t, targets[0].hookConfig), hookBefore) || !bytes.Equal(round12ReadFile(t, targets[0].managedRulePath), ruleBefore) || !bytes.Equal(round12ReadFile(t, registry), registryBefore) {
		t.Fatal("release rollback did not restore the complete outer identity")
	}
	if _, err := os.Stat(filepath.Join(release, "README.md")); !os.IsNotExist(err) {
		t.Fatalf("candidate release remained after rollback: %v", err)
	}
	var receipt installRecoveryReceipt
	if err := json.Unmarshal(round12ReadFile(t, installOuterJournalPath(registry)+".failure.json"), &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Phase != "smoke-passed" || !receipt.Recovered || receipt.Outcome != "ROLLED_BACK" || receipt.VCSIdentity == "" || receipt.PackageDigest == "" || receipt.Generation == 0 || receipt.Lease == "" || receipt.Token == "" || receipt.StableDigest == "" || receipt.RecoveryReceipt == "" {
		t.Fatalf("release rollback receipt is incomplete: %+v", receipt)
	}
	round12AssertNoTransactionArtifacts(t, registry, targets[0].targetPath)
}

func TestWhiteboxPhase0Round12ManifestRejectsBeforeAnyTargetOrReceiptWrite(t *testing.T) {
	source := round12InstallSource(t)
	round12WriteFile(t, filepath.Join(source, "formal-gates.manifest.json"), []byte("{invalid manifest\n"), 0o600)
	project := t.TempDir()
	registry := filepath.Join(t.TempDir(), "registry.json")
	launcher := filepath.Join(t.TempDir(), "launcher", nativeBinaryName())
	registryBefore := round12WriteRegistry(t, registry, 8)
	targets, err := resolveInstallTargets("claude", "project", project)
	if err != nil {
		t.Fatal(err)
	}
	round12WriteFile(t, filepath.Join(targets[0].targetPath, "old.txt"), []byte("old\n"), 0o600)
	round12WriteFile(t, launcher, []byte("old launcher\n"), 0o700)
	hookBefore := []byte(`{"hooks":{"External":[{"command":"keep"}]}}` + "\n")
	ruleBefore := []byte("external rule\n")
	round12WriteFile(t, targets[0].hookConfig, hookBefore, 0o600)
	round12WriteFile(t, targets[0].managedRulePath, ruleBefore, 0o600)
	receiptPath := registry + ".install.json"
	receiptBefore := []byte("old receipt\n")
	round12WriteFile(t, receiptPath, receiptBefore, 0o600)
	if _, err := Install(InstallOptions{Source: source, Host: "claude", Scope: "project", Project: project, RegistryPath: registry, BinaryTarget: launcher, Force: true}); err == nil || !strings.Contains(strings.ToLower(err.Error()), "manifest") {
		t.Fatalf("invalid manifest was accepted: %v", err)
	}
	if string(round12ReadFile(t, filepath.Join(targets[0].targetPath, "old.txt"))) != "old\n" || string(round12ReadFile(t, launcher)) != "old launcher\n" || !bytes.Equal(round12ReadFile(t, targets[0].hookConfig), hookBefore) || !bytes.Equal(round12ReadFile(t, targets[0].managedRulePath), ruleBefore) || !bytes.Equal(round12ReadFile(t, registry), registryBefore) || !bytes.Equal(round12ReadFile(t, receiptPath), receiptBefore) {
		t.Fatal("manifest rejection changed an existing install namespace")
	}
	round12AssertNoTransactionArtifacts(t, registry, targets[0].targetPath)
}

func TestWhiteboxPhase0Round12RelativeReleaseOverlapRejectsWithoutNamespaceWrites(t *testing.T) {
	source := round12InstallSource(t)
	releaseInsideSource := filepath.Join(source, "nested-release")
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relativeRelease, err := filepath.Rel(workingDirectory, releaseInsideSource)
	if err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	registry := filepath.Join(t.TempDir(), "registry.json")
	launcher := filepath.Join(t.TempDir(), "launcher", nativeBinaryName())
	registryBefore := round12WriteRegistry(t, registry, 9)
	targets, err := resolveInstallTargets("claude", "project", project)
	if err != nil {
		t.Fatal(err)
	}
	round12WriteFile(t, filepath.Join(targets[0].targetPath, "old.txt"), []byte("old\n"), 0o600)
	round12WriteFile(t, launcher, []byte("old launcher\n"), 0o700)
	if _, err := Install(InstallOptions{Source: source, Host: "claude", Scope: "project", Project: project, ReleaseRoot: relativeRelease, BinaryTarget: launcher, RegistryPath: registry, Force: true}); err == nil || !strings.Contains(err.Error(), "overlaps release root") {
		t.Fatalf("relative source/release overlap was accepted: %v", err)
	}
	if _, err := os.Stat(releaseInsideSource); !os.IsNotExist(err) {
		t.Fatalf("overlap rejection wrote inside source: %v", err)
	}
	if string(round12ReadFile(t, filepath.Join(targets[0].targetPath, "old.txt"))) != "old\n" || string(round12ReadFile(t, launcher)) != "old launcher\n" || !bytes.Equal(round12ReadFile(t, registry), registryBefore) {
		t.Fatal("overlap rejection changed target or registry namespace")
	}
	round12AssertNoTransactionArtifacts(t, registry, targets[0].targetPath)
}

func TestWhiteboxPhase0Round12InstallReceiptCoversMultiHostScopeRootsAndMismatchRejection(t *testing.T) {
	source := round12InstallSource(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	project := t.TempDir()
	registry := filepath.Join(home, ".formal-gates", "registry.json")
	launcher := filepath.Join(home, ".local", "bin", nativeBinaryName())
	projectReport, err := Install(InstallOptions{Source: source, Host: "both", Scope: "project", Project: project, RegistryPath: registry, BinaryTarget: launcher, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	globalReport, err := Install(InstallOptions{Source: source, Host: "both", Scope: "global", RegistryPath: registry, BinaryTarget: launcher, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	for name, report := range map[string]InstallReport{"project": projectReport, "global": globalReport} {
		if len(report.Targets) != 2 || report.VCSIdentity == "" || report.PackageDigest == "" || report.Registry == "" || report.ReceiptPath == "" {
			t.Fatalf("%s report lacks multi-host identity: %+v", name, report)
		}
		for _, target := range report.Targets {
			if target.SourceDigest == "" || target.InstalledDigest == "" || target.SourceLstat.Kind != "directory" || target.InstalledLstat.Kind != "directory" || target.VCSIdentity == "" || target.PackageDigest == "" || len(target.Manifest) == 0 {
				t.Fatalf("%s target receipt lacks runtime identity: %+v", name, target)
			}
			installed, err := PackageReceipt(target.TargetPath)
			if err != nil {
				t.Fatal(err)
			}
			if installed.Digest != target.InstalledDigest || target.CanonicalPaths["sourceRoot"] != canonicalRegistryPath(source) || target.CanonicalPaths["launcher"] != canonicalRegistryPath(launcher) {
				t.Fatalf("%s target receipt has mismatched digest/canonical identity: %+v", name, target)
			}
			for _, key := range []string{"source-target", "source-launcher", "source-project", "target-state-resource"} {
				if target.DisjointProof[key] != "PASS" {
					t.Fatalf("%s target receipt lacks disjoint proof %s: %+v", name, key, target.DisjointProof)
				}
			}
			if pathOverlaps(canonicalRegistryPath(source), canonicalRegistryPath(target.TargetPath)) || pathOverlaps(canonicalRegistryPath(target.TargetPath), target.CanonicalPaths["stateRoot"]) || pathOverlaps(canonicalRegistryPath(target.TargetPath), target.CanonicalPaths["resourceRoot"]) {
				t.Fatalf("%s target receipt contains overlapping runtime namespace: %+v", name, target.CanonicalPaths)
			}
		}
	}
	document, err := LoadRegistry(registry)
	if err != nil || len(document.Records) != 4 {
		t.Fatalf("multi-host/scope registry is incomplete: doc=%+v err=%v", document, err)
	}
	for _, record := range document.Records {
		if !validRegistryRecord(record) || record.Generation == 0 || record.Lease == "" || record.Token == "" {
			t.Fatalf("registry record lacks complete identity: %+v", record)
		}
	}
	for _, record := range document.Records {
		if record.Scope == "project" {
			if err := verifyRegistryBinding(registry, record.ID, project, filepath.Join(project, "wrong-package")); err == nil {
				t.Fatalf("mismatched package identity was accepted for %s", record.ID)
			}
			if err := verifyRegistryBinding(registry, record.ID, filepath.Join(project, "other-root"), record.Target); err == nil {
				t.Fatalf("mismatched project identity was accepted for %s", record.ID)
			}
			break
		}
	}
}

func round12CreateSourceArchive(t *testing.T, path string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	entries := map[string]string{
		"formal-gates-round12/SKILL.md":                   "skill\n",
		"formal-gates-round12/README.md":                  "readme\n",
		"formal-gates-round12/README_EN.md":               "readme\n",
		"formal-gates-round12/formal-gates.manifest.json": `{"name":"formal-gates"}` + "\n",
		"formal-gates-round12/agents/agent.md":            "agent\n",
		"formal-gates-round12/prompts/action.md":          "prompt\n",
		"formal-gates-round12/gates/gate.md":              "gate\n",
		"formal-gates-round12/references/reference.md":    "reference\n",
	}
	for name, content := range entries {
		writer, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestWhiteboxPhase0Round12BootstrapWrappersForwardOnlyToStableOwner(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("wrapper fixture is for supported Unix release platforms")
	}
	arch := runtime.GOARCH
	osName := runtime.GOOS
	if osName == "darwin" {
		osName = "macos"
	}
	suffix := osName + "-" + arch
	if suffix != "macos-arm64" && suffix != "macos-amd64" && suffix != "linux-amd64" {
		t.Skip("unsupported release platform")
	}
	root := round12RepoRoot(t)
	script := filepath.Join(root, "install.command")
	help := round12Run(t, root, "bash", script, "--help")
	for _, flag := range []string{"--version", "--host", "--scope", "--project", "--force", "--skip-hooks"} {
		if !strings.Contains(help, flag) {
			t.Fatalf("wrapper help omits %s: %s", flag, help)
		}
	}
	fixture := t.TempDir()
	sourceZip := filepath.Join(fixture, "source.zip")
	round12CreateSourceArchive(t, sourceZip)
	binaryAsset := filepath.Join(fixture, "formal-gates-"+suffix)
	logPath := filepath.Join(fixture, "owner.log")
	round12WriteFile(t, binaryAsset, []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$ROUND12_OWNER_LOG\"\nexit 0\n"), 0o700)
	canary := filepath.Join(fixture, "portable-canary-"+suffix+".json")
	checksums := filepath.Join(fixture, "SHA256SUMS-"+suffix+".txt")
	round12WriteFile(t, canary, []byte("{}\n"), 0o600)
	round12WriteFile(t, checksums, []byte("fixture\n"), 0o600)
	binDir := filepath.Join(fixture, "bin")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	curl := filepath.Join(binDir, "curl")
	sha256sum := filepath.Join(binDir, "sha256sum")
	round12WriteFile(t, curl, []byte(`#!/bin/sh
set -eu
output=""
url=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = "-o" ]; then output="$2"; shift 2; else url="$1"; shift; fi
done
case "$url" in
  *zipball*) cp "$ROUND12_SOURCE_ZIP" "$output" ;;
  *portable-canary*) cp "$ROUND12_CANARY" "$output" ;;
  *SHA256SUMS*) cp "$ROUND12_CHECKSUMS" "$output" ;;
  *formal-gates-*) cp "$ROUND12_BINARY" "$output" ;;
  *) exit 1 ;;
esac
`), 0o700)
	round12WriteFile(t, sha256sum, []byte("#!/bin/sh\n[ \"$1\" = \"-c\" ]\n"), 0o700)
	home := filepath.Join(fixture, "home")
	project := filepath.Join(fixture, "project")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("bash", script, "--host", "codex", "--scope", "project", "--project", project, "--force", "--skip-hooks")
	command.Dir = root
	command.Env = append(os.Environ(),
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"HOME="+home, "USERPROFILE="+home, "FORMAL_GATES_VERSION=vround12",
		"ROUND12_SOURCE_ZIP="+sourceZip, "ROUND12_BINARY="+binaryAsset, "ROUND12_CANARY="+canary,
		"ROUND12_CHECKSUMS="+checksums, "ROUND12_OWNER_LOG="+logPath,
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("bootstrap wrapper failed: %v\n%s", err, output)
	}
	lines := strings.Split(strings.TrimSpace(string(round12ReadFile(t, logPath))), "\n")
	if len(lines) != 2 {
		t.Fatalf("wrapper did not make exactly install and bootstrap owner calls: %q", lines)
	}
	launcher := filepath.Join(home, ".local", "bin", "formal-gates")
	release := filepath.Join(home, ".formal-gates", "releases", "round12-"+suffix)
	if info, err := os.Lstat(launcher); err != nil || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("wrapper did not leave a regular stable launcher: info=%+v err=%v", info, err)
	}
	if !strings.Contains(lines[0], "install --source") || !strings.Contains(lines[0], "--release-root "+release) || !strings.Contains(lines[0], "--binary-target "+launcher) || !strings.Contains(lines[0], "--host codex --scope project --project "+project+" --force --skip-hooks") {
		t.Fatalf("install arguments were not forwarded to the stable owner: %q", lines[0])
	}
	if !strings.Contains(lines[1], "install --bootstrap --source "+release) || !strings.Contains(lines[1], "--binary-target "+launcher) || !strings.Contains(lines[1], "--host codex --scope project --project "+project) {
		t.Fatalf("bootstrap arguments were not forwarded to the stable owner: %q", lines[1])
	}
	powershell := string(round12ReadFile(t, filepath.Join(root, "install.ps1")))
	batch := string(round12ReadFile(t, filepath.Join(root, "install.bat")))
	for _, required := range []string{"--release-root", "--binary-target", "install", "--bootstrap", "& $formalBinary @args", "& $formalBinary @bootstrapArgs"} {
		if !strings.Contains(powershell, required) {
			t.Fatalf("PowerShell wrapper omits native-owner delegation %q", required)
		}
	}
	if !strings.Contains(batch, "install.ps1") || strings.Contains(powershell, "SymbolicLink") || strings.Contains(powershell, "Remove-Item $installRoot") {
		t.Fatal("Windows wrappers retain a second pointer-mutating transaction")
	}
}

func TestWhiteboxPhase0Round12InstalledInventoryAndStableHelpStayWithinStageZero(t *testing.T) {
	root := round12RepoRoot(t)
	receipt, err := PackageReceipt(root)
	if err != nil {
		t.Fatal(err)
	}
	entrySet := map[string]bool{}
	for _, entry := range receipt.Entries {
		entrySet[entry.Path] = true
	}
	for _, required := range []string{"SKILL.md", "README.md", "README_EN.md", "formal-gates.manifest.json", "definitions/workflow.json", "references/requirements-precedence.md", "prompts/actions/qa-design.md", "gates/implementation-quality-gate.md", "internal/validate/workflow.go"} {
		if !entrySet[required] {
			t.Fatalf("installed package inventory omits %s", required)
		}
	}
	inventory := string(round12ReadFile(t, filepath.Join(root, "references", "requirements-precedence.md")))
	for _, status := range []string{"current-authority", "reference", "orthogonal", "superseded", "historical"} {
		if !strings.Contains(inventory, status) {
			t.Fatalf("requirements inventory omits status %s", status)
		}
	}
	for _, path := range []string{
		"openspec/changes/orchestration-pipeline-engine-phase-0/master-requirements.md",
		"openspec/changes/orchestration-pipeline-engine/master-requirements.md",
		"openspec/changes/blackbox-parallel-seal-squash-qa-mode/master-requirements.md",
		"openspec/changes/deadlock-recovery-and-codex-lifecycle/master-requirements.md",
		"openspec/changes/fix-existing-defects/master-requirements.md",
		"openspec/changes/host-rule-management-and-codex-hook/master-requirements.md",
		"openspec/changes/p1-qa-decouple-and-carries-fix/master-requirements.md",
		"openspec/changes/qa-execution-rerun-scope/master-requirements.md",
		"openspec/changes/runtime-review-guards/master-requirements.md",
		"openspec/changes/sliced-runs-confirmation-and-qa-refactor/master-requirements.md",
		"openspec/changes/two-phase-pre-development-review/master-requirements.md",
		"openspec/changes/universal-modification-intake/master-requirements.md",
		"P2-FIX-REQUIREMENT.md", "QA-INCREMENTAL-ISOLATION-REQUIREMENT.md", "TRIGGER-MODEL-REQUIREMENT.md", "TRIGGER-MODEL-V2-REQUIREMENT.md",
	} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(path))); err != nil {
			t.Fatalf("inventory path is not present in installed tree: %s: %v", path, err)
		}
	}
	docFiles := []string{filepath.Join(root, "SKILL.md"), filepath.Join(root, "README.md"), filepath.Join(root, "README_EN.md"), filepath.Join(root, "references", "requirements-precedence.md")}
	for _, pattern := range []string{filepath.Join(root, "prompts", "*.md"), filepath.Join(root, "prompts", "actions", "*.md"), filepath.Join(root, "references", "*.md")} {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		docFiles = append(docFiles, matches...)
	}
	for _, path := range docFiles {
		content := strings.ToLower(string(round12ReadFile(t, path)))
		for _, forbidden := range []string{"workflow drive", "workflow submit", "drive/submit", "shadow"} {
			if strings.Contains(content, forbidden) {
				t.Fatalf("stable document %s leaks future public surface %q", path, forbidden)
			}
		}
	}
	var help strings.Builder
	for _, args := range [][]string{{"--help"}, {"workflow", "--help"}, {"registry", "--help"}, {"install", "--help"}, {"uninstall", "--help"}, {"canary", "--help"}} {
		help.WriteString(round12Run(t, root, "go", append([]string{"run", "./cmd/formal-gates"}, args...)...))
		help.WriteByte('\n')
	}
	helpText := strings.ToLower(help.String())
	for _, forbidden := range []string{"workflow drive", "workflow submit", "drive/submit", "shadow"} {
		if strings.Contains(helpText, forbidden) {
			t.Fatalf("public help leaks future surface %q: %s", forbidden, helpText)
		}
	}
}

var _ = fmt.Sprintf
