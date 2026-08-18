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

func phase0StartFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	phase0WriteFile(t, filepath.Join(root, "requirements.md"), "stage zero requirement\n", 0o600)
	phase0Run(t, root, "git", "init")
	phase0Run(t, root, "git", "config", "user.email", "phase0-whitebox@example.invalid")
	phase0Run(t, root, "git", "config", "user.name", "Phase0 Whitebox")
	phase0Run(t, root, "git", "add", "requirements.md")
	phase0Run(t, root, "git", "commit", "-m", "baseline")
	packageRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root, packageRoot
}

func phase0InstallSource(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	phase0WriteFile(t, filepath.Join(root, "SKILL.md"), "---\nname: formal-gates\n---\n"+hostInstructionsStartMarker+"\nstage zero rule\n"+hostInstructionsEndMarker+"\n", 0o600)
	phase0WriteFile(t, filepath.Join(root, "README.md"), "runtime\n", 0o600)
	phase0WriteFile(t, filepath.Join(root, "README_EN.md"), "runtime\n", 0o600)
	phase0WriteFile(t, filepath.Join(root, "formal-gates.manifest.json"), `{"name":"formal-gates"}`+"\n", 0o600)
	phase0WriteFile(t, filepath.Join(root, "bin", nativeBinaryName()), "#!/bin/sh\nexit 0\n", 0o700)
	for _, entry := range []string{"agents/agent.md", "prompts/action.md", "gates/gate.md", "references/reference.md"} {
		phase0WriteFile(t, filepath.Join(root, filepath.FromSlash(entry)), entry+"\n", 0o600)
	}
	return root
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
		DefinitionDigest:          "sha256:definition",
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

func TestWhiteboxPhase0RegistryBootstrapAndIdempotentRegistration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.json")
	initial := RegistryRecord{
		Target: "/install/stable", Scope: "global", Host: "codex",
		ProjectRoot: "/project", StateRoot: "/state", ResourceRoot: "/resources",
		RuntimeSibling: "/runtime/stable", CanonicalPaths: map[string]string{"target": "/install/stable"},
	}
	doc, err := BootstrapRegistry(path, []RegistryRecord{initial})
	if err != nil {
		t.Fatal(err)
	}
	if doc.SchemaVersion != RegistrySchemaVersion || doc.Epoch != 1 || len(doc.Records) != 1 {
		t.Fatalf("bootstrap document is incomplete: %+v", doc)
	}
	if doc.Records[0].ID != "target-1" || doc.Records[0].Status != "active" || len(doc.Records[0].CanonicalPaths) == 0 || doc.Records[0].CanonicalPaths["target"] != "/install/stable" {
		t.Fatalf("bootstrap defaults were not materialized: %+v", doc.Records[0])
	}

	replacement := initial
	replacement.ID = "target-1"
	replacement.Status = "disabled"
	replacement.RuntimeSibling = "/runtime/replacement"
	doc, err = RegisterRegistryRecord(path, replacement)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Epoch != 2 || len(doc.Records) != 1 || doc.Records[0].ID != replacement.ID || doc.Records[0].Status != replacement.Status || doc.Records[0].Target != replacement.Target || doc.Records[0].RuntimeSibling != replacement.RuntimeSibling || len(doc.Records[0].CanonicalPaths) == 0 || doc.Records[0].CanonicalPaths["target"] != replacement.CanonicalPaths["target"] {
		t.Fatalf("same-id registration appended or lost the epoch: %+v", doc)
	}

	second := initial
	second.ID = "project-target"
	doc, err = RegisterRegistryRecord(path, second)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Epoch != 3 || loaded.Epoch != doc.Epoch || len(loaded.Records) != 2 {
		t.Fatalf("new registration was not durably appended: doc=%+v loaded=%+v", doc, loaded)
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

func TestWhiteboxPhase0AdmissionRejectsIncompleteDisabledAndMissingRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.json")
	records := []RegistryRecord{
		{ID: "complete", Target: "/install", Scope: "global", Host: "codex", ProjectRoot: "/project", StateRoot: "/state", ResourceRoot: "/resources", RuntimeSibling: "/runtime", Status: "active"},
		{ID: "incomplete", Status: "active"},
		{ID: "disabled", Target: "/install-disabled", Scope: "project", Host: "claude", ProjectRoot: "/project", StateRoot: "/state", ResourceRoot: "/resources", RuntimeSibling: "/runtime-disabled", Status: "disabled"},
	}
	if _, err := BootstrapRegistry(path, records); err != nil {
		t.Fatal(err)
	}
	accepted, err := AdmitRegistry(path, "complete")
	if err != nil {
		t.Fatal(err)
	}
	if !accepted.Accepted || accepted.Code != "ADMITTED" {
		t.Fatalf("complete active registration was not admitted: %+v", accepted)
	}

	for _, id := range []string{"incomplete", "disabled", "missing"} {
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
	registry := filepath.Join(t.TempDir(), "registry.json")
	if _, err := BootstrapRegistry(registry, []RegistryRecord{
		{ID: "candidate", Target: "/candidate", Scope: "project", Host: "codex", ProjectRoot: root, StateRoot: filepath.Join(root, ".gates"), ResourceRoot: filepath.Join(root, ".resources"), RuntimeSibling: "/runtime/candidate", Status: "disabled"},
	}); err != nil {
		t.Fatal(err)
	}
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

	doc, err := LoadRegistry(registry)
	if err != nil {
		t.Fatal(err)
	}
	record := doc.Records[0]
	record.Status = "active"
	if _, err := RegisterRegistryRecord(registry, record); err != nil {
		t.Fatal(err)
	}
	state, err := Start(options)
	if err != nil {
		t.Fatalf("admitted candidate could not start: %v", err)
	}
	if state.RunID != options.RunID || !isFile(RunStatePath(root, options.RunID)) {
		t.Fatalf("admitted start did not create its state: %+v", state)
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
	for _, fault := range []string{"intent", "prepared", "switched", "hook", "managed-rule", "post-switch-smoke"} {
		t.Run(fault, func(t *testing.T) {
			source := phase0InstallSource(t)
			root := t.TempDir()
			target := installTarget{
				host:            "claude",
				targetPath:      filepath.Join(root, "skills", "formal-gates"),
				hookConfig:      filepath.Join(root, "settings.json"),
				managedRulePath: filepath.Join(root, "CLAUDE.md"),
			}
			phase0WriteFile(t, filepath.Join(target.targetPath, "old.txt"), "old runtime\n", 0o600)
			hookBefore := `{"hooks":{"Unrelated":[{"command":"keep"}]}}` + "\n"
			ruleBefore := "unrelated rule\n"
			phase0WriteFile(t, target.hookConfig, hookBefore, 0o600)
			phase0WriteFile(t, target.managedRulePath, ruleBefore, 0o600)
			t.Setenv("FORMAL_GATES_INSTALL_FAULT", fault)
			err := executeInstallTransaction(source, target, true, false, "stage zero rule")
			if err == nil || !strings.Contains(err.Error(), "deterministic install fault") {
				t.Fatalf("fault %q did not interrupt installation: %v", fault, err)
			}
			phase0AssertOldInstallRestored(t, target, hookBefore, ruleBefore)
			var receipt installRecoveryReceipt
			failurePath := installJournalPath(target.targetPath) + ".failure.json"
			if err := json.Unmarshal([]byte(phase0ReadFile(t, failurePath)), &receipt); err != nil {
				t.Fatal(err)
			}
			if receipt.Operation != "install" || receipt.Target != target.targetPath || receipt.Phase == "" {
				t.Fatalf("fault receipt is incomplete: %+v", receipt)
			}
		})
	}
}

func TestWhiteboxPhase0HookJSONFailureRestoresInstallAndWritesReceipt(t *testing.T) {
	source := phase0InstallSource(t)
	root := t.TempDir()
	target := installTarget{
		host:            "claude",
		targetPath:      filepath.Join(root, "skills", "formal-gates"),
		hookConfig:      filepath.Join(root, "settings.json"),
		managedRulePath: filepath.Join(root, "CLAUDE.md"),
	}
	phase0WriteFile(t, filepath.Join(target.targetPath, "old.txt"), "old runtime\n", 0o600)
	hookBefore := "{invalid-json\n"
	ruleBefore := "unrelated rule\n"
	phase0WriteFile(t, target.hookConfig, hookBefore, 0o600)
	phase0WriteFile(t, target.managedRulePath, ruleBefore, 0o600)

	err := executeInstallTransaction(source, target, true, false, "stage zero rule")
	if err == nil || !strings.Contains(err.Error(), "not valid JSON") {
		t.Fatalf("malformed hook JSON did not stop installation: %v", err)
	}
	phase0AssertOldInstallRestored(t, target, hookBefore, ruleBefore)
	var receipt installRecoveryReceipt
	failurePath := installJournalPath(target.targetPath) + ".failure.json"
	if err := json.Unmarshal([]byte(phase0ReadFile(t, failurePath)), &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Operation != "install" || receipt.Target != target.targetPath || receipt.Phase != "switched" {
		t.Fatalf("hook JSON failure receipt is incomplete: %+v", receipt)
	}
}

func TestWhiteboxPhase0UninstallFaultRestoresRuntimeHooksAndRule(t *testing.T) {
	root := t.TempDir()
	target := installTarget{
		host:            "claude",
		targetPath:      filepath.Join(root, "skills", "formal-gates"),
		hookConfig:      filepath.Join(root, "settings.json"),
		managedRulePath: filepath.Join(root, "CLAUDE.md"),
	}
	phase0WriteFile(t, filepath.Join(target.targetPath, "old.txt"), "old runtime\n", 0o600)
	hookBefore := `{"hooks":{"PreToolUse":[{"matcher":"*","hooks":[{"type":"command","command":"/old/formal-gates/bin/formal-gates hook decide"}]}],"Unrelated":[{"command":"keep"}]}}` + "\n"
	ruleBefore := "unrelated\n" + hostInstructionsStartMarker + "\nstage zero rule\n" + hostInstructionsEndMarker + "\n"
	phase0WriteFile(t, target.hookConfig, hookBefore, 0o600)
	phase0WriteFile(t, target.managedRulePath, ruleBefore, 0o600)
	t.Setenv("FORMAL_GATES_INSTALL_FAULT", "post-switch-smoke")
	err := executeUninstallTransaction(target)
	if err == nil || !strings.Contains(err.Error(), "post-switch-smoke") {
		t.Fatalf("uninstall fault did not interrupt the transaction: %v", err)
	}
	if got := phase0ReadFile(t, filepath.Join(target.targetPath, "old.txt")); got != "old runtime\n" {
		t.Fatalf("uninstall did not restore runtime: %q", got)
	}
	if got := phase0ReadFile(t, target.hookConfig); got != hookBefore {
		t.Fatalf("uninstall did not restore hook bytes: %s", got)
	}
	if got := phase0ReadFile(t, target.managedRulePath); got != ruleBefore {
		t.Fatalf("uninstall did not restore rule bytes: %s", got)
	}
	var receipt installRecoveryReceipt
	if err := json.Unmarshal([]byte(phase0ReadFile(t, installJournalPath(target.targetPath)+".failure.json")), &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Operation != "uninstall" || receipt.Phase != "committed" {
		t.Fatalf("uninstall failure receipt is incomplete: %+v", receipt)
	}
}

func TestWhiteboxPhase0CrashJournalReconcileRestoresBackupAndReceipt(t *testing.T) {
	parent := t.TempDir()
	target := filepath.Join(parent, "skills", "formal-gates")
	backup := filepath.Join(filepath.Dir(target), ".formal-gates.backup-crash")
	temp := filepath.Join(filepath.Dir(target), ".formal-gates.tmp-crash")
	phase0WriteFile(t, filepath.Join(target, "candidate.txt"), "candidate\n", 0o600)
	phase0WriteFile(t, filepath.Join(backup, "stable.txt"), "stable\n", 0o600)
	phase0WriteFile(t, filepath.Join(temp, "partial.txt"), "partial\n", 0o600)
	journal := installJournal{Operation: "install", Target: target, Temp: temp, Backup: backup, Phase: "switched", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	journalPath := installJournalPath(target)
	if err := writeJSONAtomically(journalPath, journal); err != nil {
		t.Fatal(err)
	}
	if err := reconcileInstallJournal(target); err != nil {
		t.Fatal(err)
	}
	if got := phase0ReadFile(t, filepath.Join(target, "stable.txt")); got != "stable\n" {
		t.Fatalf("reconcile did not restore stable target: %q", got)
	}
	for _, stale := range []string{filepath.Join(target, "candidate.txt"), temp, backup, journalPath} {
		if _, err := os.Stat(stale); !os.IsNotExist(err) {
			t.Fatalf("reconcile left stale path %s: %v", stale, err)
		}
	}
	var receipt installRecoveryReceipt
	if err := json.Unmarshal([]byte(phase0ReadFile(t, journalPath+".receipt.json")), &receipt); err != nil {
		t.Fatal(err)
	}
	if !receipt.Recovered || receipt.Operation != "install" || receipt.Phase != "switched" || receipt.Target != target {
		t.Fatalf("recovery receipt does not describe the reconciled crash: %+v", receipt)
	}
}

func TestWhiteboxPhase0ReleaseRollbackRestoresReleaseAndExecutable(t *testing.T) {
	source := phase0InstallSource(t)
	root := t.TempDir()
	release := filepath.Join(root, "releases", "candidate")
	binary := filepath.Join(root, "bin", nativeBinaryName())
	phase0WriteFile(t, filepath.Join(release, "old.txt"), "old release\n", 0o600)
	phase0WriteFile(t, binary, "old executable\n", 0o700)
	t.Setenv("FORMAL_GATES_INSTALL_FAULT", "post-switch-smoke")
	err := installReleaseTransaction(source, release, binary, true)
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
	if err := json.Unmarshal([]byte(phase0ReadFile(t, installJournalPath(release)+".failure.json")), &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Operation != "release-install" || receipt.Phase != "committed" {
		t.Fatalf("release failure receipt is incomplete: %+v", receipt)
	}
}

func TestWhiteboxPhase0InstallValidatesManifestBeforeTargetWrite(t *testing.T) {
	source := phase0InstallSource(t)
	phase0WriteFile(t, filepath.Join(source, "formal-gates.manifest.json"), "{not-json\n", 0o600)
	project := t.TempDir()
	_, err := Install(InstallOptions{Source: source, Host: "claude", Scope: "project", Project: project, Force: true, SkipHooks: true})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "manifest") {
		t.Fatalf("installer accepted a package with an invalid manifest: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(project, ".claude", "skills", "formal-gates")); !os.IsNotExist(statErr) {
		t.Fatalf("installer wrote the target before rejecting the manifest: %v", statErr)
	}
}

func TestWhiteboxPhase0ReleaseRunsInstalledBinarySmokeBeforeCommit(t *testing.T) {
	source := phase0InstallSource(t)
	binarySource := filepath.Join(source, "bin", nativeBinaryName())
	marker := filepath.Join(t.TempDir(), "candidate-execution.marker")
	root := t.TempDir()
	release := filepath.Join(root, "releases", "candidate")
	binary := filepath.Join(root, "bin", nativeBinaryName())
	journal := installJournalPath(release)
	// The candidate writes evidence only when the installed executable is
	// actually launched. It also observes the transaction journal while it is
	// running: smoke must happen after the switch, but before the journal moves
	// to committed.
	phase0WriteFile(t, binarySource, fmt.Sprintf("#!/bin/sh\nset -eu\nprintf 'phase0-candidate-smoke-ran\\n' > %q\nprintf 'exec-path=%%s\\n' \"$0\" >> %q\nprintf 'journal-phase=%%s\\n' \"$(awk -F'\\\"' '/\\\"phase\\\"/ {print $4; exit}' %q)\" >> %q\nexit 23\n", marker, marker, journal, marker), 0o700)
	phase0WriteFile(t, filepath.Join(source, "candidate-only.txt"), "candidate\n", 0o600)
	phase0WriteFile(t, filepath.Join(release, "stable.txt"), "stable\n", 0o600)
	phase0WriteFile(t, binary, "stable executable\n", 0o700)

	err := installReleaseTransaction(source, release, binary, true)
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
	if !strings.Contains(markerData, "exec-path="+binary) {
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
	for _, entry := range []string{".formal-gates-release-", ".formal-gates-release-backup-"} {
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
	if receipt.Operation != "release-install" || receipt.Target != release || receipt.Phase != "switched" || receipt.Recovered || receipt.ObservedAt == "" {
		t.Fatalf("release smoke failure receipt does not describe the observed boundary and rollback: %+v", receipt)
	}
}

func TestWhiteboxPhase0BootstrapScriptsDelegateToNativeTransactionOwner(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
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
	if !strings.Contains(shell, `"$source_root/bin/formal-gates" install`) {
		t.Fatal("shell bootstrap does not invoke the downloaded native owner directly")
	}
	if !strings.Contains(powershell, `Join-Path $sourceDir.FullName "bin\formal-gates.exe"`) {
		t.Fatal("PowerShell bootstrap does not invoke the downloaded native owner directly")
	}
}

func TestWhiteboxPhase0PrecedenceInventoryAndStableDocsStayStageZero(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
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
