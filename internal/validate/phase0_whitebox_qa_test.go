//go:build phase0whitebox

package validate

// This file is the independently authored whitebox QA delivery for stage 0.
// Each exported TestWhiteboxPhase0* function is bound to exactly one QA case.
// The tests exercise the implementation at the lowest responsibility that owns
// the behavior; they do not call or name development-worker tests.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

func phase0WriteFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func phase0ReadFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func phase0Run(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	command := exec.Command(name, args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

func phase0RepoRoot(t *testing.T) string {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok || !filepath.IsAbs(sourceFile) {
		t.Fatal("could not locate the whitebox test source as an absolute path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "..", ".."))
}

func phase0StartFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	phase0WriteFile(t, filepath.Join(root, "requirements.md"), "stage zero requirement\n", 0o600)
	phase0Run(t, root, "git", "init")
	phase0Run(t, root, "git", "config", "user.email", "phase0-whitebox@example.invalid")
	phase0Run(t, root, "git", "config", "user.name", "Phase0 Whitebox")
	phase0Run(t, root, "git", "add", "requirements.md")
	phase0Run(t, root, "git", "commit", "-m", "baseline")
	return root, phase0RepoRoot(t)
}

// phase0TestManifest 是测试 fixture 的最小 manifest：登记四个可安装 host
// target。install 会拒绝 manifest 未登记为 host-target 的安装目标（unknown
// target 在写 target/state 前非零拒绝），fixture 必须携带该登记。
const phase0TestManifest = `{"name":"formal-gates","hosts":[` +
	`{"name":"Claude Code","support":"host-target"},` +
	`{"name":"Codex","support":"host-target"},` +
	`{"name":"Cursor","support":"host-target"},` +
	`{"name":"DeepSeek Harness","support":"host-target"}]}` + "\n"

func phase0InstallSource(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	phase0WriteFile(t, filepath.Join(root, "SKILL.md"), "---\nname: formal-gates\n---\n"+hostInstructionsStartMarker+"\nstage zero rule\n"+hostInstructionsEndMarker+"\n", 0o600)
	phase0WriteFile(t, filepath.Join(root, "README.md"), "runtime\n", 0o600)
	phase0WriteFile(t, filepath.Join(root, "README_EN.md"), "runtime\n", 0o600)
	phase0WriteFile(t, filepath.Join(root, "formal-gates.manifest.json"), phase0TestManifest, 0o600)
	phase0WriteFile(t, filepath.Join(root, "bin", nativeBinaryName()), "#!/bin/sh\nexit 0\n", 0o700)
	for _, entry := range []string{"agents/agent.md", "prompts/action.md", "gates/gate.md", "references/reference.md"} {
		phase0WriteFile(t, filepath.Join(root, filepath.FromSlash(entry)), entry+"\n", 0o600)
	}
	return root
}

func phase0RegistryRecord(id, target, launcher, scope, host, project, status string) RegistryRecord {
	target = canonicalRegistryPath(target)
	launcher = canonicalRegistryPath(launcher)
	project = canonicalRegistryPath(project)
	state := canonicalRegistryPath(filepath.Join(project, ".gates"))
	resources := canonicalRegistryPath(filepath.Join(project, ".formal-gates-resources"))
	runtime := canonicalRegistryPath(filepath.Dir(target))
	return RegistryRecord{
		ID: id, Target: target, LauncherPath: launcher, Scope: scope, Host: host,
		ProjectRoot: project, StateRoot: state, ResourceRoot: resources,
		RuntimeSibling: runtime, Status: status,
		CanonicalPaths: map[string]string{
			"target": target, "launcher": launcher, "projectRoot": project,
			"stateRoot": state, "resourceRoot": resources, "runtimeSibling": runtime,
		},
	}
}

// phase0CommitRegistry exercises the one production semantic registry owner
// while supplying the lock that Install/Uninstall normally hold around it.
func phase0CommitRegistry(t *testing.T, path string, records ...RegistryRecord) RegistryDocument {
	t.Helper()
	unlock, err := acquireRegistryLock(path)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	doc, err := loadRegistryForCommit(path)
	if err != nil {
		t.Fatal(err)
	}
	doc, err = commitRegistryRecordsUnlocked(path, doc, records)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

// phase0WriteBootstrapReceipt materializes the committed bootstrap receipt
// workflow start requires beside a test registry. Test fixtures commit
// registry records directly instead of running the install --bootstrap
// maintenance entry, so they must provide the same first-start boundary the
// documented flow produces.
func phase0WriteBootstrapReceipt(t *testing.T, registryPath string) {
	t.Helper()
	if err := writeJSONAtomically(registryPath+".bootstrap.json", BootstrapReceipt{Operation: "bootstrap", Accepted: true, Registry: registryPath, Epoch: 1, ObservedAt: nowReceiptTime()}); err != nil {
		t.Fatal(err)
	}
}

func phase0AssertOldInstallRestored(t *testing.T, target installTarget, hookBefore, ruleBefore string) {
	t.Helper()
	if got := phase0ReadFile(t, filepath.Join(target.targetPath, "old.txt")); got != "old runtime\n" {
		t.Fatalf("old runtime was not restored: %q", got)
	}
	if _, err := os.Stat(filepath.Join(target.targetPath, "README.md")); !os.IsNotExist(err) {
		t.Fatalf("candidate runtime remained after rollback: %v", err)
	}
	if got := phase0ReadFile(t, target.hookConfig); got != hookBefore {
		t.Fatalf("hook config was not restored byte-for-byte:\n%s", got)
	}
	if got := phase0ReadFile(t, target.managedRulePath); got != ruleBefore {
		t.Fatalf("managed rule was not restored byte-for-byte:\n%s", got)
	}
}

func phase0InstallLockChild(t *testing.T) bool {
	t.Helper()
	mode := os.Getenv("PHASE0_INSTALL_LOCK_CHILD")
	if mode == "" {
		return false
	}
	target := os.Getenv("PHASE0_INSTALL_LOCK_TARGET")
	unlock, err := acquireInstallLock(target)
	switch mode {
	case "held":
		if err == nil {
			unlock()
			fmt.Fprint(os.Stdout, "unexpected-acquire")
			return true
		}
		fmt.Fprintf(os.Stdout, "held-error:%v", err)
	case "free":
		if err != nil {
			fmt.Fprintf(os.Stdout, "free-error:%v", err)
			return true
		}
		unlock()
		fmt.Fprint(os.Stdout, "free-acquired")
	default:
		fmt.Fprintf(os.Stdout, "unknown-mode:%s", mode)
	}
	return true
}

func phase0RunInstallLockChild(t *testing.T, target, mode string) string {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run", "^TestWhiteboxPhase0InstallLockSerializesInstallAndUninstall$", "-test.v")
	command.Env = append(os.Environ(), "PHASE0_INSTALL_LOCK_CHILD="+mode, "PHASE0_INSTALL_LOCK_TARGET="+target)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("lock child %s failed: %v\n%s", mode, err, output)
	}
	return string(output)
}

func TestWhiteboxPhase0VersionEnvelopeExactBarrier(t *testing.T) {
	valid := VersionEnvelope{
		Writer:                    "engine",
		StateSchemaVersion:        CurrentStateSchemaVersion,
		WorkflowDefinitionVersion: CurrentWorkflowDefinitionVersion,
		DefinitionSource:          "definitions/workflow.json",
		DefinitionDigest:          CurrentWorkflowDefinitionDigest,
		PackageDigest:             "sha256:package",
	}
	if err := ValidateVersionEnvelope(valid); err != nil {
		t.Fatalf("current complete candidate envelope was rejected: %v", err)
	}

	mutations := map[string]func(*VersionEnvelope){
		"missing writer":              func(value *VersionEnvelope) { value.Writer = "" },
		"legacy writer":               func(value *VersionEnvelope) { value.Writer = "legacy" },
		"missing schema":              func(value *VersionEnvelope) { value.StateSchemaVersion = "" },
		"older schema":                func(value *VersionEnvelope) { value.StateSchemaVersion = "0" },
		"newer schema":                func(value *VersionEnvelope) { value.StateSchemaVersion = "2" },
		"missing definition version":  func(value *VersionEnvelope) { value.WorkflowDefinitionVersion = "" },
		"older definition version":    func(value *VersionEnvelope) { value.WorkflowDefinitionVersion = "0" },
		"newer definition version":    func(value *VersionEnvelope) { value.WorkflowDefinitionVersion = "2" },
		"missing definition source":   func(value *VersionEnvelope) { value.DefinitionSource = "" },
		"missing definition digest":   func(value *VersionEnvelope) { value.DefinitionDigest = "" },
		"stale definition source":     func(value *VersionEnvelope) { value.DefinitionSource = "definitions/old-workflow.json" },
		"incorrect definition source": func(value *VersionEnvelope) { value.DefinitionSource = "definitions/other-workflow.json" },
		"stale definition digest":     func(value *VersionEnvelope) { value.DefinitionDigest = "sha256:stale-definition" },
		"incorrect definition digest": func(value *VersionEnvelope) { value.DefinitionDigest = "sha256:incorrect-definition" },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			candidate := valid
			mutate(&candidate)
			err := ValidateVersionEnvelope(candidate)
			var unsupported *UnsupportedRunVersionError
			if !errors.As(err, &unsupported) || !IsUnsupportedRunVersion(err) || !strings.Contains(err.Error(), UnsupportedRunVersionCode) {
				t.Fatalf("invalid envelope did not return the typed stable error: %v", err)
			}
		})
	}
}

func TestWhiteboxPhase0DiagnoseRawReadOnlyFallback(t *testing.T) {
	root := t.TempDir()
	malformedPath := filepath.Join(root, "malformed.json")
	malformed := []byte(`{"writer":"engine","stateSchemaVersion":`)
	if err := os.WriteFile(malformedPath, malformed, 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(malformedPath)
	if err != nil {
		t.Fatal(err)
	}
	report, err := DiagnoseState(malformedPath)
	if err != nil {
		t.Fatal(err)
	}
	if report.JSONReadable || report.Integrity != "unknown" || report.Recommendation == "" {
		t.Fatalf("malformed raw report did not retain the safe unknown result: %+v", report)
	}
	if got := phase0ReadFile(t, malformedPath); got != string(malformed) {
		t.Fatalf("diagnose rewrote malformed state: %q", got)
	}
	after, err := os.Stat(malformedPath)
	if err != nil {
		t.Fatal(err)
	}
	if !before.ModTime().Equal(after.ModTime()) || before.Size() != after.Size() {
		t.Fatalf("diagnose changed state metadata: before=%+v after=%+v", before, after)
	}

	legacyPath := filepath.Join(root, "terminal.json")
	legacy := `{"writer":"future-engine","stateSchemaVersion":"9","workflowDefinitionVersion":"8","definitionSource":"definitions/future.json","definitionDigest":"sha256:old","packageDigest":"sha256:pkg","status":"SEALED","runId":"legacy-run"}`
	phase0WriteFile(t, legacyPath, legacy, 0o600)
	futureBefore, err := os.Stat(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	report, err = DiagnoseState(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	if !report.JSONReadable || report.Integrity != "readable" || report.DetectedVersions["writer"] != "future-engine" || report.DetectedVersions["stateSchemaVersion"] != "9" || report.DetectedVersions["workflowDefinitionVersion"] != "8" || report.DetectedVersions["definitionSource"] != "definitions/future.json" || report.DetectedVersions["definitionDigest"] != "sha256:old" || report.DetectedVersions["packageDigest"] != "sha256:pkg" {
		t.Fatalf("raw envelope fields were not reported: %+v", report)
	}
	if report.Summary["status"] != "SEALED" || report.Summary["runId"] != "legacy-run" {
		t.Fatalf("terminal summary fallback was lost: %+v", report.Summary)
	}
	if got := phase0ReadFile(t, legacyPath); got != legacy {
		t.Fatal("diagnose rewrote an unsupported terminal state")
	}
	futureAfter, err := os.Stat(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	if !futureBefore.ModTime().Equal(futureAfter.ModTime()) || futureBefore.Size() != futureAfter.Size() {
		t.Fatalf("diagnose changed future-state metadata: before=%+v after=%+v", futureBefore, futureAfter)
	}
}

func TestWhiteboxPhase0PackageReceiptDeterminismAndIsolation(t *testing.T) {
	root := t.TempDir()
	phase0WriteFile(t, filepath.Join(root, "z.txt"), "z\n", 0o600)
	phase0WriteFile(t, filepath.Join(root, "nested", "a.txt"), "a\n", 0o640)
	phase0WriteFile(t, filepath.Join(root, ".git", "ignored"), "mutable\n", 0o600)
	phase0WriteFile(t, filepath.Join(root, ".gates", "ignored"), "runtime\n", 0o600)

	first, err := PackageReceipt(root)
	if err != nil {
		t.Fatal(err)
	}
	second, err := PackageReceipt(root)
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest == "" || first.Digest != second.Digest {
		t.Fatalf("unchanged package digest was not deterministic: %q vs %q", first.Digest, second.Digest)
	}
	paths := make([]string, 0, len(first.Entries))
	for _, entry := range first.Entries {
		paths = append(paths, entry.Path)
		if !filepath.IsAbs(entry.RealPath) {
			t.Fatalf("entry lacks canonical real path: %+v", entry)
		}
	}
	if !sort.StringsAreSorted(paths) || strings.Join(paths, ",") != "nested/a.txt,z.txt" {
		t.Fatalf("receipt entries are not canonical and sorted: %v", paths)
	}
	phase0WriteFile(t, filepath.Join(root, "nested", "a.txt"), "changed\n", 0o640)
	changed, err := PackageReceipt(root)
	if err != nil {
		t.Fatal(err)
	}
	if changed.Digest == first.Digest {
		t.Fatal("package content changed without changing the receipt digest")
	}

	overlap := filepath.Join(root, "nested")
	if _, err := PackageReceipt(root, overlap); err == nil || !strings.Contains(err.Error(), "overlaps") {
		t.Fatalf("overlapping development/package roots were accepted: %v", err)
	}
	outside := t.TempDir()
	phase0WriteFile(t, filepath.Join(outside, "payload"), "outside\n", 0o600)
	if err := os.Symlink(filepath.Join(outside, "payload"), filepath.Join(root, "live-link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := PackageReceipt(root); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("live package symlink was accepted: %v", err)
	}
}

func TestWhiteboxPhase0BaselineReceiptBindsAllIdentities(t *testing.T) {
	source := t.TempDir()
	installed := t.TempDir()
	phase0WriteFile(t, filepath.Join(source, "payload"), "same bytes\n", 0o600)
	phase0WriteFile(t, filepath.Join(installed, "payload"), "same bytes\n", 0o600)
	canonical := t.TempDir()
	alias := filepath.Join(t.TempDir(), "canonical-alias")
	if err := os.Symlink(canonical, alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	receipt, err := BuildBaselineReceipt("  git:abc123  ", source, installed, map[string]string{
		"hostConfig": alias,
		"empty":      "",
	})
	if err != nil {
		t.Fatal(err)
	}
	canonicalReal, err := filepath.EvalSymlinks(canonical)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.VCSIdentity != "git:abc123" || receipt.PackageDigest == "" || receipt.InstalledTargetDigest == "" {
		t.Fatalf("baseline identity fields are incomplete: %+v", receipt)
	}
	if receipt.PackageDigest != receipt.InstalledTargetDigest {
		t.Fatalf("identical source/install copies received different digests: %+v", receipt)
	}
	if receipt.CanonicalPaths["hostConfig"] != filepath.Clean(canonicalReal) {
		t.Fatalf("canonical path was not resolved: %+v", receipt.CanonicalPaths)
	}
	if _, exists := receipt.CanonicalPaths["empty"]; exists {
		t.Fatal("empty canonical path was recorded")
	}

	output := filepath.Join(t.TempDir(), "baseline.json")
	if err := WriteBaselineReceipt(output, receipt); err != nil {
		t.Fatal(err)
	}
	var persisted BaselineReceipt
	if err := json.Unmarshal([]byte(phase0ReadFile(t, output)), &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.VCSIdentity != receipt.VCSIdentity || persisted.SourceRoot != receipt.SourceRoot || persisted.PackageDigest != receipt.PackageDigest || persisted.InstalledTarget != receipt.InstalledTarget || persisted.InstalledTargetDigest != receipt.InstalledTargetDigest || len(persisted.CanonicalPaths) != len(receipt.CanonicalPaths) || persisted.CanonicalPaths["hostConfig"] != receipt.CanonicalPaths["hostConfig"] || persisted.GeneratedAt != receipt.GeneratedAt {
		t.Fatalf("persisted baseline lost identities: %+v", persisted)
	}
	data := phase0ReadFile(t, output)
	if !strings.HasPrefix(data, "{") || !strings.HasSuffix(data, "}\n") {
		t.Fatalf("baseline receipt was not persisted as one complete JSON document: %q", data)
	}
	entries, err := os.ReadDir(filepath.Dir(output))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".phase0-") {
			t.Fatalf("atomic baseline write left a temporary file: %s", entry.Name())
		}
	}
	if err := WriteBaselineReceipt(filepath.Join(t.TempDir(), "invalid.json"), BaselineReceipt{}); err == nil {
		t.Fatal("baseline receipt without VCS/package identity was persisted")
	}
	invalidVCS := receipt
	invalidVCS.VCSIdentity = ""
	if err := WriteBaselineReceipt(filepath.Join(t.TempDir(), "invalid-vcs.json"), invalidVCS); err == nil {
		t.Fatal("baseline receipt without VCS identity was persisted")
	}
	invalidPackage := receipt
	invalidPackage.PackageDigest = ""
	if err := WriteBaselineReceipt(filepath.Join(t.TempDir(), "invalid-package.json"), invalidPackage); err == nil {
		t.Fatal("baseline receipt without package identity was persisted")
	}
}

func TestWhiteboxPhase0RegistryTransactionOwnerPreservesRecords(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "registry.json")
	initial := phase0RegistryRecord("target-1", filepath.Join(root, "install", "stable"), filepath.Join(root, "bin", nativeBinaryName()), "global", "codex", filepath.Join(root, "home"), "active")
	doc := phase0CommitRegistry(t, path, initial)
	if doc.SchemaVersion != RegistrySchemaVersion || doc.Epoch != 1 || len(doc.Records) != 1 {
		t.Fatalf("initial transaction document is incomplete: %+v", doc)
	}
	if doc.Records[0].ID != "target-1" || doc.Records[0].Status != "active" || doc.Records[0].Generation == 0 || doc.Records[0].Lease == "" || doc.Records[0].Token == "" {
		t.Fatalf("transaction identity was not materialized: %+v", doc.Records[0])
	}

	replacement := initial
	replacement.Status = "disabled"
	doc = phase0CommitRegistry(t, path, replacement)
	if doc.Epoch != 2 || len(doc.Records) != 1 || doc.Records[0].ID != replacement.ID || doc.Records[0].Status != replacement.Status || doc.Records[0].Target != replacement.Target || doc.Records[0].RuntimeSibling != replacement.RuntimeSibling || len(doc.Records[0].CanonicalPaths) == 0 || doc.Records[0].CanonicalPaths["target"] != replacement.CanonicalPaths["target"] {
		t.Fatalf("same-id transaction appended or lost the epoch: %+v", doc)
	}

	second := phase0RegistryRecord("project-target", filepath.Join(root, "install", "project"), filepath.Join(root, "bin", nativeBinaryName()), "project", "claude", filepath.Join(root, "project"), "active")
	doc = phase0CommitRegistry(t, path, second)
	loaded, err := LoadRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Epoch != 3 || loaded.Epoch != doc.Epoch || len(loaded.Records) != 2 {
		t.Fatalf("new transaction was not durably appended: doc=%+v loaded=%+v", doc, loaded)
	}
	var replacementLoaded, secondLoaded *RegistryRecord
	for index := range loaded.Records {
		record := &loaded.Records[index]
		switch record.ID {
		case replacement.ID:
			replacementLoaded = record
		case second.ID:
			secondLoaded = record
		}
	}
	if replacementLoaded == nil || replacementLoaded.Target != replacement.Target || replacementLoaded.Scope != replacement.Scope || replacementLoaded.Host != replacement.Host || replacementLoaded.ProjectRoot != replacement.ProjectRoot || replacementLoaded.StateRoot != replacement.StateRoot || replacementLoaded.ResourceRoot != replacement.ResourceRoot || replacementLoaded.Status != replacement.Status || replacementLoaded.RuntimeSibling != replacement.RuntimeSibling || len(replacementLoaded.CanonicalPaths) == 0 || replacementLoaded.CanonicalPaths["target"] != replacement.CanonicalPaths["target"] {
		t.Fatalf("replacement fields were not persisted on reload: %+v", loaded)
	}
	if secondLoaded == nil || secondLoaded.Target != second.Target || secondLoaded.Scope != second.Scope || secondLoaded.Host != second.Host || secondLoaded.ProjectRoot != second.ProjectRoot || secondLoaded.StateRoot != second.StateRoot || secondLoaded.ResourceRoot != second.ResourceRoot || secondLoaded.RuntimeSibling != second.RuntimeSibling || !strings.EqualFold(secondLoaded.Status, "active") || len(secondLoaded.CanonicalPaths) == 0 || secondLoaded.CanonicalPaths["target"] != second.CanonicalPaths["target"] {
		t.Fatalf("new record fields were not persisted on reload: %+v", loaded)
	}
}

func TestWhiteboxPhase0InstallBootstrapReceiptBindsRecordAndCreatesNoState(t *testing.T) {
	source := phase0InstallSource(t)
	project := t.TempDir()
	registry := filepath.Join(t.TempDir(), "registry.json")
	launcher := filepath.Join(t.TempDir(), "stable", nativeBinaryName())
	phase0WriteFile(t, launcher, phase0ReadFile(t, filepath.Join(source, "bin", nativeBinaryName())), 0o700)
	targets, err := resolveInstallTargets("claude", "project", project)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range targets {
		if err := copyInstallRuntime(source, target.targetPath, true); err != nil {
			t.Fatal(err)
		}
	}
	codexTargets, err := resolveInstallTargets("codex", "project", project)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range codexTargets {
		if err := copyInstallRuntime(source, target.targetPath, true); err != nil {
			t.Fatal(err)
		}
	}
	beforeTarget, err := PackageDigest(targets[0].targetPath)
	if err != nil {
		t.Fatal(err)
	}
	report, err := Install(InstallOptions{
		Source:       source,
		Host:         "claude",
		Scope:        "project",
		Project:      project,
		RegistryPath: registry,
		BinaryTarget: launcher,
		Bootstrap:    true,
	})
	if err != nil {
		t.Fatalf("native bootstrap failed: %v", err)
	}
	if report.BootstrapReceiptPath != registry+".bootstrap.json" || report.ReceiptPath != report.BootstrapReceiptPath {
		t.Fatalf("bootstrap report did not expose its durable receipt: %+v", report)
	}
	afterTarget, err := PackageDigest(targets[0].targetPath)
	if err != nil || afterTarget != beforeTarget {
		t.Fatalf("bootstrap mutated the existing runtime: before=%s after=%s err=%v", beforeTarget, afterTarget, err)
	}
	if _, err := os.Stat(filepath.Join(project, ".gates")); !os.IsNotExist(err) {
		t.Fatalf("bootstrap created workflow state root: %v", err)
	}
	doc, err := LoadRegistry(registry)
	if err != nil || len(doc.Records) != 1 {
		t.Fatalf("bootstrap registry record missing: doc=%+v err=%v", doc, err)
	}
	record := doc.Records[0]
	if record.Generation == 0 || record.Lease == "" || record.Token == "" || record.HookConfig == "" || !validRegistryRecord(record) {
		t.Fatalf("bootstrap registry identity is incomplete: %+v", record)
	}
	var receipt struct {
		PackageDigest string           `json:"packageDigest"`
		Records       []RegistryRecord `json:"records"`
		StateCreated  bool             `json:"stateCreated"`
	}
	if err := json.Unmarshal([]byte(phase0ReadFile(t, report.BootstrapReceiptPath)), &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.PackageDigest == "" || len(receipt.Records) != 1 || receipt.Records[0].Generation == 0 || receipt.Records[0].Lease == "" || receipt.Records[0].Token == "" || receipt.StateCreated {
		t.Fatalf("bootstrap receipt lost identity or state boundary: %+v", receipt)
	}
	firstEpoch := doc.Epoch
	firstIdentity := record
	if _, err := Install(InstallOptions{Source: source, Host: "claude", Scope: "project", Project: project, RegistryPath: registry, BinaryTarget: launcher, Bootstrap: true}); err != nil {
		t.Fatalf("idempotent bootstrap failed: %v", err)
	}
	doc, err = LoadRegistry(registry)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Epoch != firstEpoch || len(doc.Records) != 1 || doc.Records[0].Generation != firstIdentity.Generation || doc.Records[0].Lease != firstIdentity.Lease || doc.Records[0].Token != firstIdentity.Token {
		t.Fatalf("idempotent bootstrap replaced its existing admission identity: %+v", doc)
	}
	if _, err := Install(InstallOptions{Source: source, Host: "codex", Scope: "project", Project: project, RegistryPath: registry, BinaryTarget: launcher, Bootstrap: true}); err != nil {
		t.Fatalf("second target bootstrap failed: %v", err)
	}
	doc, err = LoadRegistry(registry)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Epoch != firstEpoch+1 || len(doc.Records) != 2 {
		t.Fatalf("second target bootstrap replaced the registry instead of merging: %+v", doc)
	}
	for _, current := range doc.Records {
		if current.ID == firstIdentity.ID && (current.Generation != firstIdentity.Generation || current.Lease != firstIdentity.Lease || current.Token != firstIdentity.Token) {
			t.Fatalf("second target bootstrap changed the unrelated record: %+v", current)
		}
	}
}

func TestWhiteboxPhase0AdmissionRejectsIncompleteDisabledAndMissingRecords(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "registry.json")
	complete := phase0RegistryRecord("complete", filepath.Join(root, "install"), filepath.Join(root, "bin", nativeBinaryName()), "global", "codex", filepath.Join(root, "home"), "active")
	disabled := phase0RegistryRecord("disabled", filepath.Join(root, "install-disabled"), filepath.Join(root, "bin", nativeBinaryName()), "project", "claude", filepath.Join(root, "project"), "disabled")
	phase0CommitRegistry(t, path, complete, disabled)
	// The unique transaction owner rejects incomplete records before they can
	// become registry state; admission then covers only normal disabled/missing
	// operation rather than a manually corrupted registry.
	doc, err := LoadRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := commitRegistryRecordsUnlocked(path, doc, []RegistryRecord{{ID: "incomplete", Status: "active"}}); err == nil {
		t.Fatal("semantic registry owner accepted an incomplete record")
	}
	accepted, err := AdmitRegistry(path, "complete")
	if err != nil {
		t.Fatal(err)
	}
	if !accepted.Accepted || accepted.Code != "ADMITTED" {
		t.Fatalf("complete active registration was not admitted: %+v", accepted)
	}

	for _, id := range []string{"disabled", "missing"} {
		receipt, err := AdmitRegistry(path, id)
		if err != nil {
			t.Fatal(err)
		}
		if receipt.Accepted || receipt.Code != "UNREGISTERED_INSTALL" || receipt.Reason == "" {
			t.Fatalf("%s registration was not rejected with a stable receipt: %+v", id, receipt)
		}
		var persisted AdmissionReceipt
		if err := json.Unmarshal([]byte(phase0ReadFile(t, path+".admission.json")), &persisted); err != nil {
			t.Fatal(err)
		}
		if persisted.RecordID != id || persisted.Code != "UNREGISTERED_INSTALL" || persisted.Accepted {
			t.Fatalf("denial receipt does not match attempted registration %q: %+v", id, persisted)
		}
	}
}

func TestWhiteboxPhase0WorkflowAdmissionPrecedesStateCreation(t *testing.T) {
	root, packageRoot := phase0StartFixture(t)
	registryRoot := t.TempDir()
	registry := filepath.Join(registryRoot, "registry.json")
	record := phase0RegistryRecord("candidate", packageRoot, filepath.Join(registryRoot, "bin", nativeBinaryName()), "project", "codex", root, "disabled")
	phase0CommitRegistry(t, registry, record)
	options := StartOptions{
		Root: root, PackageRoot: packageRoot, RunID: "phase0-admission", Flow: "formal",
		RequirementSource: "requirements.md", VCS: "git", Split: "no",
		AdmissionRegistry: registry, AdmissionRecordID: "candidate",
	}
	if _, err := Start(options); err == nil || !strings.Contains(err.Error(), "UNREGISTERED_INSTALL") {
		t.Fatalf("disabled installation reached the workflow writer: %v", err)
	}
	if _, err := os.Stat(RunDir(root, options.RunID)); !os.IsNotExist(err) {
		t.Fatalf("run directory was created before admission: %v", err)
	}
	if _, err := os.Stat(RunStatePath(root, options.RunID)); !os.IsNotExist(err) {
		t.Fatalf("state file was created before admission: %v", err)
	}

	record.Status = "active"
	phase0CommitRegistry(t, registry, record)
	phase0WriteBootstrapReceipt(t, registry)
	state, err := Start(options)
	if err != nil {
		t.Fatalf("admitted candidate could not start: %v", err)
	}
	if state.RunID != options.RunID || !isFile(RunStatePath(root, options.RunID)) {
		t.Fatalf("admitted start did not create its state: %+v", state)
	}
	if state.AdmissionEpoch == 0 || state.AdmissionGeneration == 0 || state.AdmissionLease == "" || state.AdmissionToken == "" {
		t.Fatalf("admitted run did not bind registry epoch/lease identity: %+v", state)
	}
	record.Token = "replacement-token"
	phase0CommitRegistry(t, registry, record)
	if err := SaveRunState(root, state); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("state write ignored a replaced registry lease/token: %v", err)
	}
}

func TestWhiteboxPhase0WorkflowAdmissionBindsCurrentRoot(t *testing.T) {
	root, packageRoot := phase0StartFixture(t)
	registry := filepath.Join(t.TempDir(), "registry.json")
	record := phase0RegistryRecord("root-bound", packageRoot, filepath.Join(t.TempDir(), "bin", nativeBinaryName()), "project", "codex", root, "active")
	phase0CommitRegistry(t, registry, record)
	phase0WriteBootstrapReceipt(t, registry)
	state, err := Start(StartOptions{
		Root: root, PackageRoot: packageRoot, RunID: "phase0-root-bound", Flow: "formal",
		RequirementSource: "requirements.md", VCS: "git", Split: "no",
		AdmissionRegistry: registry, AdmissionRecordID: record.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveRunState(t.TempDir(), state); err == nil || !strings.Contains(err.Error(), "workflow root does not match") {
		t.Fatalf("state binding was reusable from a different workflow root: %v", err)
	}
}

func TestWhiteboxPhase0GlobalAdmissionAllowsProjectsOutsideHostNamespace(t *testing.T) {
	root, packageRoot := phase0StartFixture(t)
	hostRoot := t.TempDir()
	registry := filepath.Join(t.TempDir(), "registry.json")
	stateRoot := filepath.Join(hostRoot, ".gates")
	resourceRoot := filepath.Join(hostRoot, ".formal-gates-resources")
	if canonicalPath(root) == canonicalPath(hostRoot) {
		t.Fatal("fixture did not create distinct project and host namespaces")
	}
	record := phase0RegistryRecord("global-stable", packageRoot, filepath.Join(hostRoot, "bin", nativeBinaryName()), "global", "codex", hostRoot, "active")
	if record.StateRoot != canonicalRegistryPath(stateRoot) || record.ResourceRoot != canonicalRegistryPath(resourceRoot) {
		t.Fatal("global registry fixture did not bind the documented roots")
	}
	phase0CommitRegistry(t, registry, record)
	phase0WriteBootstrapReceipt(t, registry)
	state, err := Start(StartOptions{
		Root: root, PackageRoot: packageRoot, RunID: "phase0-global-admission", Flow: "formal",
		RequirementSource: "requirements.md", VCS: "git", Split: "no",
		AdmissionRegistry: registry, AdmissionRecordID: record.ID,
	})
	if err != nil {
		t.Fatalf("global stable driver was rejected for a project outside its host namespace: %v", err)
	}
	if state.AdmissionRoot != root || state.AdmissionTarget != packageRoot {
		t.Fatalf("global admission did not persist the invoking root and installed target: %+v", state)
	}
	if err := SaveRunState(root, state); err != nil {
		t.Fatalf("later state write did not re-admit the same global target: %v", err)
	}
}

func TestWhiteboxPhase0SealFencesAdmissionBeforeGitSquash(t *testing.T) {
	root, packageRoot := phase0StartFixture(t)
	registryRoot := t.TempDir()
	registry := filepath.Join(registryRoot, "registry.json")
	record := phase0RegistryRecord("seal-stable", packageRoot, filepath.Join(registryRoot, "bin", nativeBinaryName()), "project", "codex", root, "active")
	phase0CommitRegistry(t, registry, record)
	phase0WriteBootstrapReceipt(t, registry)
	state, err := Start(StartOptions{Root: root, PackageRoot: packageRoot, RunID: "phase0-seal-fence", Flow: "formal", RequirementSource: "requirements.md", VCS: "git", Route: "lightweight", AdmissionRegistry: registry, AdmissionRecordID: record.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareAction(root, packageRoot, state.RunID, "requirements-clarification", "", false, ""); err != nil {
		t.Fatal(err)
	}
	state, err = LoadRunState(root, state.RunID)
	if err != nil {
		t.Fatal(err)
	}
	state, err = RecordAction(root, packageRoot, state.RunID, "requirements-clarification", openDispatchID(state, "action", "requirements-clarification"), "PASS", "", nil, false, "")
	if err != nil {
		t.Fatal(err)
	}
	state, err = UpdateRequirement(root, packageRoot, state.RunID, "", true, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	base := state.BaseSnapshot
	for index := 1; index <= 2; index++ {
		phase0WriteFile(t, filepath.Join(root, fmt.Sprintf("delivery-%d.txt", index)), fmt.Sprintf("delivery %d\n", index), 0o600)
		phase0Run(t, root, "git", "add", "--all")
		phase0Run(t, root, "git", "commit", "-m", fmt.Sprintf("delivery %d", index))
	}
	head := phase0Run(t, root, "git", "rev-parse", "HEAD")
	state, err = LoadRunState(root, state.RunID)
	if err != nil {
		t.Fatal(err)
	}
	state.CurrentSnapshot = head
	if err := SaveRunState(root, state); err != nil {
		t.Fatal(err)
	}
	record.Status = "disabled"
	phase0CommitRegistry(t, registry, record)
	if _, err := Seal(root, packageRoot, state.RunID, nil, false, "must-not-squash"); err == nil || !strings.Contains(err.Error(), "UNREGISTERED_INSTALL") {
		t.Fatalf("disabled admission reached Seal VCS work: %v", err)
	}
	if after := phase0Run(t, root, "git", "rev-parse", "HEAD"); after != head {
		t.Fatalf("Seal rewrote HEAD before admission fencing: before=%s after=%s", head, after)
	}
	if count := phase0Run(t, root, "git", "rev-list", "--count", base+".."+head); count != "2" {
		t.Fatalf("Seal squashed commits before admission fencing: count=%s", count)
	}
}

func TestWhiteboxPhase0LegacyWorkflowRemainsEnvelopeFree(t *testing.T) {
	root, packageRoot := phase0StartFixture(t)
	state, err := Start(StartOptions{
		Root: root, PackageRoot: packageRoot, RunID: "phase0-legacy", Flow: "formal",
		RequirementSource: "requirements.md", VCS: "git", Split: "no",
	})
	if err != nil {
		t.Fatalf("legacy start unexpectedly required registry/version metadata: %v", err)
	}
	data := phase0ReadFile(t, RunStatePath(root, state.RunID))
	for _, engineOnly := range []string{"stateSchemaVersion", "workflowDefinitionVersion", "definitionDigest"} {
		if strings.Contains(data, `"`+engineOnly+`"`) {
			t.Fatalf("legacy state was migrated to the future envelope (%s): %s", engineOnly, data)
		}
	}
	if loaded, err := LoadRunState(root, state.RunID); err != nil || loaded.RunID != state.RunID {
		t.Fatalf("legacy loader no longer reads the unchanged state format: loaded=%+v err=%v", loaded, err)
	}
}

func TestWhiteboxPhase0InstallLockSerializesInstallAndUninstall(t *testing.T) {
	if phase0InstallLockChild(t) {
		return
	}
	target := filepath.Join(t.TempDir(), "skills", "formal-gates")
	unlock, err := acquireInstallLock(target)
	if err != nil {
		t.Fatal(err)
	}
	childOutput := phase0RunInstallLockChild(t, target, "held")
	if !strings.Contains(childOutput, "held-error:") || !strings.Contains(childOutput, "lock is held") {
		unlock()
		t.Fatalf("cross-process install/uninstall acquired the same lock: %s", childOutput)
	}
	lockPath := installLockPath(target)
	if data := phase0ReadFile(t, lockPath); !strings.Contains(data, fmt.Sprintf("pid=%d", os.Getpid())) {
		unlock()
		t.Fatalf("lock does not identify its owner: %q", data)
	}
	unlock()
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("unlock did not remove the lock: %v", err)
	}
	childOutput = phase0RunInstallLockChild(t, target, "free")
	if !strings.Contains(childOutput, "free-acquired") {
		t.Fatalf("lock could not be acquired by a second process after release: %s", childOutput)
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("free child did not release the lock file: %v", err)
	}
}

func TestWhiteboxPhase0InstallFaultMatrixRestoresRuntimeHooksAndRule(t *testing.T) {
	for _, fault := range []string{"intent", "registry", "prepared", "switched", "post-switch-smoke", "pointer", "hook", "managed-rule", "registry-commit"} {
		t.Run(fault, func(t *testing.T) {
			source := phase0InstallSource(t)
			root := t.TempDir()
			project := filepath.Join(root, "project")
			targets, err := resolveInstallTargets("claude", "project", project)
			if err != nil {
				t.Fatal(err)
			}
			target := targets[0]
			launcher := filepath.Join(root, "stable", nativeBinaryName())
			registry := filepath.Join(root, "registry", "registry.json")
			phase0WriteFile(t, filepath.Join(target.targetPath, "old.txt"), "old runtime\n", 0o600)
			phase0WriteFile(t, launcher, "old launcher\n", 0o700)
			hookBefore := `{"hooks":{"Unrelated":[{"command":"keep"}]}}` + "\n"
			ruleBefore := "unrelated rule\n"
			phase0WriteFile(t, target.hookConfig, hookBefore, 0o600)
			phase0WriteFile(t, target.managedRulePath, ruleBefore, 0o600)
			t.Setenv("FORMAL_GATES_INSTALL_FAULT", fault)
			_, err = Install(InstallOptions{Source: source, Host: "claude", Scope: "project", Project: project, BinaryTarget: launcher, RegistryPath: registry, Force: true})
			if err == nil || !strings.Contains(err.Error(), "deterministic install fault") {
				t.Fatalf("fault %q did not interrupt installation: %v", fault, err)
			}
			phase0AssertOldInstallRestored(t, target, hookBefore, ruleBefore)
			if got := phase0ReadFile(t, launcher); got != "old launcher\n" {
				t.Fatalf("stable launcher was not restored: %q", got)
			}
			var receipt installRecoveryReceipt
			failurePath := installOuterJournalPath(registry) + ".failure.json"
			if err := json.Unmarshal([]byte(phase0ReadFile(t, failurePath)), &receipt); err != nil {
				t.Fatal(err)
			}
			if receipt.Operation != "install" || receipt.Target != registry || receipt.Phase == "" || !receipt.Recovered || receipt.Outcome != "ROLLED_BACK" {
				t.Fatalf("fault receipt is incomplete: %+v", receipt)
			}
		})
	}
}

func TestWhiteboxPhase0HookJSONFailureRestoresInstallAndWritesReceipt(t *testing.T) {
	source := phase0InstallSource(t)
	root := t.TempDir()
	project := filepath.Join(root, "project")
	targets, err := resolveInstallTargets("claude", "project", project)
	if err != nil {
		t.Fatal(err)
	}
	target := targets[0]
	launcher := filepath.Join(root, "stable", nativeBinaryName())
	registry := filepath.Join(root, "registry", "registry.json")
	phase0WriteFile(t, filepath.Join(target.targetPath, "old.txt"), "old runtime\n", 0o600)
	phase0WriteFile(t, launcher, "old launcher\n", 0o700)
	hookBefore := "{invalid-json\n"
	ruleBefore := "unrelated rule\n"
	phase0WriteFile(t, target.hookConfig, hookBefore, 0o600)
	phase0WriteFile(t, target.managedRulePath, ruleBefore, 0o600)

	_, err = Install(InstallOptions{Source: source, Host: "claude", Scope: "project", Project: project, BinaryTarget: launcher, RegistryPath: registry, Force: true})
	if err == nil || !strings.Contains(err.Error(), "not valid JSON") {
		t.Fatalf("malformed hook JSON did not stop installation: %v", err)
	}
	phase0AssertOldInstallRestored(t, target, hookBefore, ruleBefore)
	var receipt installRecoveryReceipt
	failurePath := installOuterJournalPath(registry) + ".failure.json"
	if err := json.Unmarshal([]byte(phase0ReadFile(t, failurePath)), &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Operation != "install" || receipt.Target != registry || receipt.Phase != "smoke-passed" || !receipt.Recovered {
		t.Fatalf("hook JSON failure receipt is incomplete: %+v", receipt)
	}
}

func TestWhiteboxPhase0UninstallFaultRestoresRuntimeHooksAndRule(t *testing.T) {
	source := phase0InstallSource(t)
	root := t.TempDir()
	project := filepath.Join(root, "project")
	launcher := filepath.Join(root, "stable", nativeBinaryName())
	registry := filepath.Join(root, "registry", "registry.json")
	report, err := Install(InstallOptions{Source: source, Host: "claude", Scope: "project", Project: project, BinaryTarget: launcher, RegistryPath: registry, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	target := report.Targets[0]
	hookBefore := phase0ReadFile(t, target.HookConfig)
	ruleBefore := phase0ReadFile(t, target.ManagedRulePath)
	t.Setenv("FORMAL_GATES_INSTALL_FAULT", "hook")
	_, err = Uninstall(UninstallOptions{Host: "claude", Scope: "project", Project: project, RegistryPath: registry})
	if err == nil || !strings.Contains(err.Error(), "hook") {
		t.Fatalf("uninstall fault did not interrupt the transaction: %v", err)
	}
	if !isFile(filepath.Join(target.TargetPath, "README.md")) {
		t.Fatal("uninstall did not restore installed runtime")
	}
	if got := phase0ReadFile(t, target.HookConfig); got != hookBefore {
		t.Fatalf("uninstall did not restore hook bytes: %s", got)
	}
	if got := phase0ReadFile(t, target.ManagedRulePath); got != ruleBefore {
		t.Fatalf("uninstall did not restore rule bytes: %s", got)
	}
	var receipt installRecoveryReceipt
	if err := json.Unmarshal([]byte(phase0ReadFile(t, installOuterJournalPath(registry)+".failure.json")), &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Operation != "uninstall" || receipt.Phase != "switched" || !receipt.Recovered {
		t.Fatalf("uninstall failure receipt is incomplete: %+v", receipt)
	}
	doc, err := LoadRegistry(registry)
	if err != nil || len(doc.Records) != 1 || doc.Records[0].Status != "active" {
		t.Fatalf("uninstall rollback did not preserve active registry state: %+v (%v)", doc, err)
	}
}

func TestWhiteboxPhase0CrashJournalReconcileRestoresBackupAndReceipt(t *testing.T) {
	parent := t.TempDir()
	target := filepath.Join(parent, "skills", "formal-gates")
	registry := filepath.Join(parent, "registry", "registry.json")
	transactionRoot := filepath.Join(parent, "transaction")
	staged := filepath.Join(parent, "skills", ".formal-gates-stage-crash")
	phase0WriteFile(t, filepath.Join(target, "stable.txt"), "stable\n", 0o600)
	phase0WriteFile(t, registry, "stable registry\n", 0o600)
	tree, err := snapshotInstallTree(target, filepath.Join(transactionRoot, "target.before"))
	if err != nil {
		t.Fatal(err)
	}
	registryBefore, err := snapshotOuterFile(registry, filepath.Join(transactionRoot, "registry.before"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(target); err != nil {
		t.Fatal(err)
	}
	phase0WriteFile(t, filepath.Join(target, "candidate.txt"), "candidate\n", 0o600)
	phase0WriteFile(t, staged, "partial\n", 0o600)
	phase0WriteFile(t, registry, "candidate registry\n", 0o600)
	journal := outerInstallJournal{Operation: "install", RegistryPath: registry, TransactionRoot: transactionRoot, Phase: "switched", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), Registry: registryBefore, Targets: []outerTargetSnapshot{{TargetPath: target, Tree: outerTreeFromBackup(tree)}}, Staged: []string{staged}}
	journalPath := installOuterJournalPath(registry)
	if err := writeJSONAtomically(journalPath, journal); err != nil {
		t.Fatal(err)
	}
	if err := reconcileOuterInstallJournal(registry); err != nil {
		t.Fatal(err)
	}
	if got := phase0ReadFile(t, filepath.Join(target, "stable.txt")); got != "stable\n" {
		t.Fatalf("reconcile did not restore stable target: %q", got)
	}
	if got := phase0ReadFile(t, registry); got != "stable registry\n" {
		t.Fatalf("reconcile did not restore stable registry: %q", got)
	}
	for _, stale := range []string{filepath.Join(target, "candidate.txt"), staged, transactionRoot, journalPath} {
		if _, err := os.Stat(stale); !os.IsNotExist(err) {
			t.Fatalf("reconcile left stale path %s: %v", stale, err)
		}
	}
	var receipt installRecoveryReceipt
	if err := json.Unmarshal([]byte(phase0ReadFile(t, journalPath+".receipt.json")), &receipt); err != nil {
		t.Fatal(err)
	}
	if !receipt.Recovered || receipt.Operation != "install" || receipt.Phase != "switched" || receipt.Target != registry {
		t.Fatalf("recovery receipt does not describe the reconciled crash: %+v", receipt)
	}
}

func TestWhiteboxPhase0ReleaseRollbackRestoresReleaseAndExecutable(t *testing.T) {
	source := phase0InstallSource(t)
	root := t.TempDir()
	project := filepath.Join(root, "project")
	release := filepath.Join(root, "releases", "candidate")
	binary := filepath.Join(root, "bin", nativeBinaryName())
	registry := filepath.Join(root, "registry", "registry.json")
	phase0WriteFile(t, filepath.Join(release, "old.txt"), "old release\n", 0o600)
	phase0WriteFile(t, binary, "old executable\n", 0o700)
	t.Setenv("FORMAL_GATES_INSTALL_FAULT", "post-switch-smoke")
	_, err := Install(InstallOptions{Source: source, Host: "claude", Scope: "project", Project: project, ReleaseRoot: release, BinaryTarget: binary, RegistryPath: registry, Force: true, SkipHooks: true})
	if err == nil || !strings.Contains(err.Error(), "post-switch-smoke") {
		t.Fatalf("release transaction did not fail at post-switch smoke: %v", err)
	}
	if got := phase0ReadFile(t, filepath.Join(release, "old.txt")); got != "old release\n" {
		t.Fatalf("old release was not restored: %q", got)
	}
	if _, err := os.Stat(filepath.Join(release, "README.md")); !os.IsNotExist(err) {
		t.Fatalf("candidate release remained after rollback: %v", err)
	}
	if got := phase0ReadFile(t, binary); got != "old executable\n" {
		t.Fatalf("old executable was not restored: %q", got)
	}
	var receipt installRecoveryReceipt
	if err := json.Unmarshal([]byte(phase0ReadFile(t, installOuterJournalPath(registry)+".failure.json")), &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Operation != "install" || receipt.Target != registry || receipt.Phase != "switched" || !receipt.Recovered {
		t.Fatalf("release failure receipt is incomplete: %+v", receipt)
	}
}

func TestWhiteboxPhase0InstallValidatesManifestBeforeTargetWrite(t *testing.T) {
	source := phase0InstallSource(t)
	phase0WriteFile(t, filepath.Join(source, "formal-gates.manifest.json"), "{not-json\n", 0o600)
	project := t.TempDir()
	_, err := Install(InstallOptions{Source: source, Host: "claude", Scope: "project", Project: project, RegistryPath: filepath.Join(t.TempDir(), "registry.json"), Force: true, SkipHooks: true})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "manifest") {
		t.Fatalf("installer accepted a package with an invalid manifest: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(project, ".claude", "skills", "formal-gates")); !os.IsNotExist(statErr) {
		t.Fatalf("installer wrote the target before rejecting the manifest: %v", statErr)
	}
}

func TestWhiteboxPhase0RelativeReleaseRootCannotOverlapSource(t *testing.T) {
	source := phase0InstallSource(t)
	releaseInsideSource := filepath.Join(source, "nested-release")
	workingDirectory := t.TempDir()
	useTestWorkingDirectory(t, workingDirectory)
	relativeRelease, err := filepath.Rel(workingDirectory, releaseInsideSource)
	if err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	_, err = Install(InstallOptions{Source: source, Host: "claude", Scope: "project", Project: project, ReleaseRoot: relativeRelease, BinaryTarget: filepath.Join(t.TempDir(), nativeBinaryName()), RegistryPath: filepath.Join(t.TempDir(), "registry.json"), Force: true})
	if err == nil || !strings.Contains(err.Error(), "overlaps release root") {
		t.Fatalf("relative release root inside source was accepted: %v", err)
	}
	if _, statErr := os.Stat(releaseInsideSource); !os.IsNotExist(statErr) {
		t.Fatalf("overlap rejection wrote inside the source: %v", statErr)
	}
}

func TestWhiteboxPhase0InstallReceiptBindsAllRuntimeRoots(t *testing.T) {
	source := phase0InstallSource(t)
	root := t.TempDir()
	project := filepath.Join(root, "project")
	release := filepath.Join(root, "release")
	launcher := filepath.Join(root, "stable", nativeBinaryName())
	registry := filepath.Join(root, "registry", "registry.json")
	report, err := Install(InstallOptions{Source: source, Host: "claude", Scope: "project", Project: project, ReleaseRoot: release, BinaryTarget: launcher, RegistryPath: registry, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Targets) != 1 || report.ReceiptPath != filepath.ToSlash(registry+".install.json") {
		t.Fatalf("install receipt/report shape is incomplete: %+v", report)
	}
	for _, key := range []string{"sourceRoot", "target", "launcher", "projectRoot", "stateRoot", "resourceRoot", "runtimeSibling", "registry", "releaseRoot", "hookConfig", "managedRule"} {
		value := report.Targets[0].CanonicalPaths[key]
		if value == "" || !filepath.IsAbs(value) {
			t.Fatalf("install receipt is missing canonical %s: %+v", key, report.Targets[0].CanonicalPaths)
		}
	}
	var persisted InstallReport
	if err := json.Unmarshal([]byte(phase0ReadFile(t, registry+".install.json")), &persisted); err != nil {
		t.Fatal(err)
	}
	if len(persisted.Targets) != 1 || persisted.Targets[0].CanonicalPaths["launcher"] != canonicalRegistryPath(launcher) || persisted.Targets[0].CanonicalPaths["registry"] != canonicalRegistryPath(registry) {
		t.Fatalf("persisted install receipt lost launcher/registry roots: %+v", persisted)
	}
}

func TestWhiteboxPhase0ReleaseRunsInstalledBinarySmokeBeforeCommit(t *testing.T) {
	source := phase0InstallSource(t)
	binarySource := filepath.Join(source, "bin", nativeBinaryName())
	marker := filepath.Join(t.TempDir(), "candidate-execution.marker")
	root := t.TempDir()
	release := filepath.Join(root, "releases", "candidate")
	binary := filepath.Join(root, "bin", nativeBinaryName())
	registry := filepath.Join(root, "registry", "registry.json")
	journal := installOuterJournalPath(registry)
	// The candidate writes evidence only when the installed executable is
	// actually launched. It also observes the transaction journal while it is
	// running: smoke must happen after the switch, but before the journal moves
	// to committed.
	phase0WriteFile(t, binarySource, fmt.Sprintf("#!/bin/sh\nset -eu\nprintf 'phase0-candidate-smoke-ran\\n' > %q\nprintf 'exec-path=%%s\\n' \"$0\" >> %q\nprintf 'journal-phase=%%s\\n' \"$(awk -F'\\\"' '/\\\"phase\\\"/ {print $4; exit}' %q)\" >> %q\nexit 23\n", marker, marker, journal, marker), 0o700)
	phase0WriteFile(t, filepath.Join(source, "candidate-only.txt"), "candidate\n", 0o600)
	phase0WriteFile(t, filepath.Join(release, "stable.txt"), "stable\n", 0o600)
	phase0WriteFile(t, binary, "stable executable\n", 0o700)

	_, err := Install(InstallOptions{Source: source, Host: "claude", Scope: "project", Project: filepath.Join(root, "project"), ReleaseRoot: release, BinaryTarget: binary, RegistryPath: registry, Force: true, SkipHooks: true})
	if err == nil {
		t.Fatalf("release transaction did not fail because the candidate installed-binary smoke exited non-zero")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "smoke") {
		t.Fatalf("release transaction returned a non-smoke error for the candidate failure: %v", err)
	}
	markerData := phase0ReadFile(t, marker)
	if !strings.Contains(markerData, "phase0-candidate-smoke-ran") {
		t.Fatalf("candidate installed binary did not leave its execution marker: %q", markerData)
	}
	installedBinary := canonicalRegistryPath(filepath.Join(release, "bin", nativeBinaryName()))
	if !strings.Contains(markerData, "exec-path="+installedBinary) {
		t.Fatalf("smoke marker does not prove the installed binary path ran: %q", markerData)
	}
	if !strings.Contains(markerData, "journal-phase=switched") {
		t.Fatalf("candidate smoke did not observe the pre-commit switched phase: %q", markerData)
	}
	if got := phase0ReadFile(t, filepath.Join(release, "stable.txt")); got != "stable\n" {
		t.Fatalf("failed smoke did not restore stable release: %q", got)
	}
	if got := phase0ReadFile(t, binary); got != "stable executable\n" {
		t.Fatalf("failed smoke did not restore stable executable: %q", got)
	}
	if _, err := os.Stat(filepath.Join(release, "candidate-only.txt")); !os.IsNotExist(err) {
		t.Fatalf("candidate-only release content remained after failed smoke: %v", err)
	}
	for _, entry := range []string{".formal-gates-stage-", ".formal-gates-transaction-"} {
		matches, globErr := filepath.Glob(filepath.Join(filepath.Dir(release), entry+"*"))
		if globErr != nil {
			t.Fatal(globErr)
		}
		if len(matches) != 0 {
			t.Fatalf("candidate transaction artifact remained after failed smoke: %v", matches)
		}
	}
	var receipt installRecoveryReceipt
	failurePath := journal + ".failure.json"
	if err := json.Unmarshal([]byte(phase0ReadFile(t, failurePath)), &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Operation != "install" || receipt.Target != registry || receipt.Phase != "switched" || !receipt.Recovered || receipt.ObservedAt == "" {
		t.Fatalf("release smoke failure receipt does not describe the observed boundary and rollback: %+v", receipt)
	}
}

func TestWhiteboxPhase0BootstrapScriptsDelegateToNativeTransactionOwner(t *testing.T) {
	root := phase0RepoRoot(t)
	shell := phase0ReadFile(t, filepath.Join(root, "install.command"))
	powershell := phase0ReadFile(t, filepath.Join(root, "install.ps1"))
	for name, script := range map[string]string{"install.command": shell, "install.ps1": powershell} {
		for _, required := range []string{"--release-root", "--binary-target", "install", "--source"} {
			if !strings.Contains(script, required) {
				t.Fatalf("%s does not delegate %s to the native owner", name, required)
			}
		}
	}
	for _, forbidden := range []string{`rm -rf "$install_root"`, "New-Item -ItemType SymbolicLink", "ln -sfn"} {
		if strings.Contains(shell, forbidden) || strings.Contains(powershell, forbidden) {
			t.Fatalf("bootstrap script still mutates the release pointer itself: %q", forbidden)
		}
	}
	if !strings.Contains(shell, `"$binary_target" install`) || strings.Contains(shell, `"$source_root/bin/formal-gates" install`) {
		t.Fatal("shell bootstrap does not invoke only the fixed stable launcher")
	}
	if !strings.Contains(powershell, `& $formalBinary @args`) || strings.Contains(powershell, `& (Join-Path $sourceDir.FullName "bin\formal-gates.exe")`) {
		t.Fatal("PowerShell bootstrap does not invoke only the fixed stable launcher")
	}
}

func TestWhiteboxPhase0PrecedenceInventoryAndStableDocsStayStageZero(t *testing.T) {
	root := phase0RepoRoot(t)
	inventory := phase0ReadFile(t, filepath.Join(root, "references", "requirements-precedence.md"))
	for _, required := range []string{"current-authority", "orthogonal", "historical", "superseded", "orchestration-pipeline-engine", "stage-0"} {
		if !strings.Contains(inventory, required) {
			t.Fatalf("precedence inventory is missing classification %q", required)
		}
	}
	stableFiles := []string{"SKILL.md", "README.md", "README_EN.md"}
	if matches, err := filepath.Glob(filepath.Join(root, "prompts", "*.md")); err == nil {
		for _, match := range matches {
			stableFiles = append(stableFiles, filepath.ToSlash(strings.TrimPrefix(match, root+string(os.PathSeparator))))
		}
	}
	if matches, err := filepath.Glob(filepath.Join(root, "prompts", "actions", "*.md")); err == nil {
		for _, match := range matches {
			stableFiles = append(stableFiles, filepath.ToSlash(strings.TrimPrefix(match, root+string(os.PathSeparator))))
		}
	}
	for _, rel := range stableFiles {
		content := phase0ReadFile(t, filepath.Join(root, filepath.FromSlash(rel)))
		for _, futurePublicSurface := range []string{"workflow drive", "workflow submit", "drive/submit", "`drive`", "`submit`"} {
			if strings.Contains(strings.ToLower(content), futurePublicSurface) {
				t.Fatalf("stable document %s exposes future public surface %q", rel, futurePublicSurface)
			}
		}
	}
}
