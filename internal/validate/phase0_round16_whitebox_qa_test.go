//go:build phase0whitebox

package validate

// This file is the independently authored whitebox QA delivery for the
// current incremental round. Each exported TestWhiteboxPhase0Round16*
// function is bound to exactly one QA case and exercises the implementation
// at the lowest responsibility that owns the behavior: the first-start
// bootstrap receipt boundary (Start), the pre-write candidate admission gate
// (mutateRun prepare flows and workflow cleanup --run), the manifest
// host-target registration check (Install), the global project-local sibling
// identity across upgrade and bootstrap (Install/bootstrap), the installed
// digest admission check (AdmitRegistry), and the Codex hook canary
// text-only-session retry. The tests build their own fixtures and do not
// call or name any development-worker test.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func round16Write(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func round16Read(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func round16Run(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	command := exec.Command(name, args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, output)
	}
}

func round16GitProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	round16Write(t, filepath.Join(root, "requirements.md"), "round sixteen requirement\n", 0o600)
	round16Run(t, root, "git", "init")
	round16Run(t, root, "git", "config", "user.email", "round16@example.invalid")
	round16Run(t, root, "git", "config", "user.name", "Round16 Whitebox QA")
	round16Run(t, root, "git", "add", "requirements.md")
	round16Run(t, root, "git", "commit", "-m", "baseline")
	return root
}

// round16Manifest registers every installable host target so install only
// rejects a host when a scenario explicitly mutates the registration.
const round16Manifest = `{"name":"formal-gates","hosts":[
  {"name": "Claude Code", "support": "host-target"},
  {"name": "Codex", "support": "host-target"},
  {"name": "Cursor", "support": "host-target"},
  {"name": "DeepSeek Harness", "support": "host-target"}
]}
`

// round16Source builds a complete runtime install fixture: the managed-rule
// block, the full prompt catalog required by workflow start, and a shebang
// placeholder binary accepted by the installed-binary smoke.
func round16Source(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	skill := "---\nname: formal-gates\n---\n" +
		hostInstructionsStartMarker + "\nround sixteen managed rule\n" + hostInstructionsEndMarker + "\n"
	round16Write(t, filepath.Join(root, "SKILL.md"), skill, 0o600)
	round16Write(t, filepath.Join(root, "README.md"), "round sixteen runtime\n", 0o600)
	round16Write(t, filepath.Join(root, "README_EN.md"), "round sixteen runtime en\n", 0o600)
	round16Write(t, filepath.Join(root, "formal-gates.manifest.json"), round16Manifest, 0o600)
	round16Write(t, filepath.Join(root, "bin", nativeBinaryName()), "#!/bin/sh\nexit 0\n", 0o755)
	round16Write(t, filepath.Join(root, "prompts", "reviewer-base.md"), "round sixteen reviewer base\n", 0o600)
	for _, action := range []string{
		"carry", "development-worker", "product-review", "qa-design",
		"qa-execution", "qa-review", "requirements-clarification", "start-readiness",
	} {
		round16Write(t, filepath.Join(root, "prompts", "actions", action+".md"), action+" prompt\n", 0o600)
	}
	round16Write(t, filepath.Join(root, "gates", "round16-gate.md"), "# round sixteen gate\n", 0o600)
	round16Write(t, filepath.Join(root, "agents", "agent.md"), "agent\n", 0o600)
	round16Write(t, filepath.Join(root, "references", "reference.md"), "reference\n", 0o600)
	return root
}

// round16InstallProject performs the documented native install plus the
// documented install --bootstrap maintenance entry for one project-scope
// claude target, returning the admission identity workflow start needs.
func round16InstallProject(t *testing.T, source, project, registry, launcher string) (string, string) {
	t.Helper()
	report, err := Install(InstallOptions{
		Source: source, Host: "claude", Scope: "project", Project: project,
		RegistryPath: registry, BinaryTarget: launcher, Force: true,
	})
	if err != nil {
		t.Fatalf("round16 native install failed: %v", err)
	}
	if len(report.Targets) != 1 {
		t.Fatalf("round16 install reported %d targets: %+v", len(report.Targets), report)
	}
	if _, statErr := os.Stat(registry + ".bootstrap.json"); !os.IsNotExist(statErr) {
		t.Fatalf("plain install committed a bootstrap receipt: %v", statErr)
	}
	if _, err := Install(InstallOptions{
		Source: source, Host: "claude", Scope: "project", Project: project,
		RegistryPath: registry, BinaryTarget: launcher, Bootstrap: true, Force: true,
	}); err != nil {
		t.Fatalf("round16 documented bootstrap entry failed: %v", err)
	}
	return report.Targets[0].TargetPath, report.Targets[0].RegistryRecordID
}

// round16SetRecordStatus flips one record's status through the one semantic
// registry transaction owner while holding the shared registry lock.
func round16SetRecordStatus(t *testing.T, registry, recordID, status string) {
	t.Helper()
	unlock, err := acquireRegistryLock(registry)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	document, err := loadRegistryForCommit(registry)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for index := range document.Records {
		if document.Records[index].ID == recordID {
			document.Records[index].Status = status
			found = true
		}
	}
	if !found {
		t.Fatalf("registry record %q was not found for status flip", recordID)
	}
	if _, err := commitRegistryRecordsUnlocked(registry, document, document.Records); err != nil {
		t.Fatal(err)
	}
}

func round16RecordByID(t *testing.T, document RegistryDocument, id string) RegistryRecord {
	t.Helper()
	for _, record := range document.Records {
		if record.ID == id {
			return record
		}
	}
	t.Fatalf("registry record %q was not found", id)
	return RegistryRecord{}
}

// TestWhiteboxPhase0Round16FirstStartRequiresCommittedBootstrapReceipt pins
// the documented first-start boundary: a normal install commits registry
// records without a bootstrap receipt, and workflow start must hard-reject
// (UNREGISTERED_INSTALL, machine-readable rejection receipt, no state) until
// the documented install --bootstrap entry commits an accepted receipt.
func TestWhiteboxPhase0Round16FirstStartRequiresCommittedBootstrapReceipt(t *testing.T) {
	source := round16Source(t)
	project := round16GitProject(t)
	isolated := t.TempDir()
	registry := filepath.Join(isolated, "registry.json")
	launcher := filepath.Join(isolated, "bin", nativeBinaryName())
	report, err := Install(InstallOptions{
		Source: source, Host: "claude", Scope: "project", Project: project,
		RegistryPath: registry, BinaryTarget: launcher, Force: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	targetPath, recordID := report.Targets[0].TargetPath, report.Targets[0].RegistryRecordID
	if _, statErr := os.Stat(registry + ".bootstrap.json"); !os.IsNotExist(statErr) {
		t.Fatalf("plain install committed a bootstrap receipt before the documented maintenance entry: %v", statErr)
	}
	options := StartOptions{
		Root: project, PackageRoot: targetPath, RunID: "round16-first-start",
		Flow: "formal", RequirementSource: "requirements.md", VCS: "git", Split: "no",
		AdmissionRegistry: registry, AdmissionRecordID: recordID,
	}
	rejectStart := func(wantReason string) {
		t.Helper()
		_, startErr := Start(options)
		if startErr == nil || !strings.Contains(startErr.Error(), "UNREGISTERED_INSTALL") || !strings.Contains(startErr.Error(), wantReason) {
			t.Fatalf("start was not refused with %q: %v", wantReason, startErr)
		}
		if _, statErr := os.Stat(RunDir(project, options.RunID)); !os.IsNotExist(statErr) {
			t.Fatalf("refused start created a run directory: %v", statErr)
		}
		if _, statErr := os.Stat(RunStatePath(project, options.RunID)); !os.IsNotExist(statErr) {
			t.Fatalf("refused start created workflow state: %v", statErr)
		}
		var receipt AdmissionReceipt
		if err := json.Unmarshal(round16Read(t, registry+".admission.json"), &receipt); err != nil {
			t.Fatal(err)
		}
		if receipt.Accepted || receipt.Code != "UNREGISTERED_INSTALL" || receipt.RecordID != recordID {
			t.Fatalf("rejection receipt is not the machine-readable first-start refusal: %+v", receipt)
		}
		if !strings.Contains(receipt.Reason, "bootstrap receipt") {
			t.Fatalf("rejection receipt lost the bootstrap boundary reason: %+v", receipt)
		}
	}
	rejectStart("bootstrap receipt")

	round16Write(t, registry+".bootstrap.json", "{not a receipt", 0o600)
	rejectStart("not a valid receipt")

	if err := writeJSONAtomically(registry+".bootstrap.json", BootstrapReceipt{
		Operation: "bootstrap", Accepted: false, Registry: registry,
		Epoch: 1, ObservedAt: nowReceiptTime(),
	}); err != nil {
		t.Fatal(err)
	}
	rejectStart("was not accepted")

	if _, err := Install(InstallOptions{
		Source: source, Host: "claude", Scope: "project", Project: project,
		RegistryPath: registry, BinaryTarget: launcher, Bootstrap: true, Force: true,
	}); err != nil {
		t.Fatalf("documented install --bootstrap maintenance entry failed: %v", err)
	}
	var receipt BootstrapReceipt
	if err := json.Unmarshal(round16Read(t, registry+".bootstrap.json"), &receipt); err != nil {
		t.Fatal(err)
	}
	if !receipt.Accepted || receipt.Operation != "bootstrap" {
		t.Fatalf("bootstrap entry did not commit an accepted receipt: %+v", receipt)
	}
	state, err := Start(options)
	if err != nil {
		t.Fatalf("start still refused after the documented bootstrap: %v", err)
	}
	if state.AdmissionRecordID != recordID || !isFile(RunStatePath(project, options.RunID)) {
		t.Fatalf("bootstrapped start did not persist its admission identity: %+v", state)
	}
}

// TestWhiteboxPhase0Round16MutateRunAdmissionPrecedesPromptArtifact pins the
// pre-write candidate admission gate inside mutateRun: when a run's registry
// binding stops admitting, a prepare flow must fail before writing the
// canonical dispatch prompt file, leaving the state ledger byte-identical
// with no orphan artifact; once the binding admits again the same prepare
// succeeds and records the dispatch with its canonical prompt file.
func TestWhiteboxPhase0Round16MutateRunAdmissionPrecedesPromptArtifact(t *testing.T) {
	source := round16Source(t)
	project := round16GitProject(t)
	isolated := t.TempDir()
	registry := filepath.Join(isolated, "registry.json")
	launcher := filepath.Join(isolated, "bin", nativeBinaryName())
	targetPath, recordID := round16InstallProject(t, source, project, registry, launcher)
	runID := "round16-mutate-fence"
	state, err := Start(StartOptions{
		Root: project, PackageRoot: targetPath, RunID: runID,
		Flow: "formal", RequirementSource: "requirements.md", VCS: "git", Route: "lightweight",
		AdmissionRegistry: registry, AdmissionRecordID: recordID,
	})
	if err != nil {
		t.Fatal(err)
	}
	before := round16Read(t, RunStatePath(project, runID))

	round16SetRecordStatus(t, registry, state.AdmissionRecordID, "disabled")
	if _, err := PrepareAction(project, targetPath, runID, "requirements-clarification", "", false, ""); err == nil ||
		!strings.Contains(err.Error(), "UNREGISTERED_INSTALL") {
		t.Fatalf("prepare did not hard-reject the disabled admission before writing: %v", err)
	}
	if after := round16Read(t, RunStatePath(project, runID)); string(after) != string(before) {
		t.Fatal("rejected prepare rewrote the state ledger")
	}
	promptsDir := filepath.Join(RunDir(project, runID), "prompts")
	if entries, readErr := os.ReadDir(promptsDir); readErr == nil && len(entries) != 0 {
		t.Fatalf("rejected prepare left orphan prompt artifacts: %v", entries)
	}
	unchanged, err := LoadRunState(project, runID)
	if err != nil {
		t.Fatal(err)
	}
	if len(unchanged.Dispatches) != 0 {
		t.Fatalf("rejected prepare recorded a dispatch: %+v", unchanged.Dispatches)
	}

	round16SetRecordStatus(t, registry, state.AdmissionRecordID, "active")
	prompt, err := PrepareAction(project, targetPath, runID, "requirements-clarification", "", false, "")
	if err != nil {
		t.Fatalf("admitted prepare was still refused: %v", err)
	}
	if strings.TrimSpace(prompt) == "" {
		t.Fatal("admitted prepare returned an empty prompt")
	}
	admitted, err := LoadRunState(project, runID)
	if err != nil {
		t.Fatal(err)
	}
	if len(admitted.Dispatches) != 1 {
		t.Fatalf("admitted prepare did not record exactly one dispatch: %+v", admitted.Dispatches)
	}
	for _, dispatch := range admitted.Dispatches {
		if dispatch.Status != "OPEN" || strings.TrimSpace(dispatch.PromptFile) == "" {
			t.Fatalf("dispatch was not recorded as open with its canonical file: %+v", dispatch)
		}
		data := round16Read(t, dispatch.PromptFile)
		if string(data) != prompt {
			t.Fatalf("canonical prompt file does not carry the prepared prompt: %q", string(data))
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != dispatch.PromptHash {
			t.Fatalf("canonical prompt file hash does not match the dispatch record: %+v", dispatch)
		}
	}
}

// TestWhiteboxPhase0Round16CleanupRunAdmitsRunBinding pins the admission
// fence on the workflow cleanup --run escape hatch: deleting a run directory
// destroys workflow state, so a run with a recorded registry binding may only
// be deleted by an invocation that still admits that binding, while residue
// without a state ledger keeps the legacy stable-launcher check, and unsafe
// run ids stay rejected.
func TestWhiteboxPhase0Round16CleanupRunAdmitsRunBinding(t *testing.T) {
	source := round16Source(t)
	project := round16GitProject(t)
	isolated := t.TempDir()
	registry := filepath.Join(isolated, "registry.json")
	launcher := filepath.Join(isolated, "bin", nativeBinaryName())
	targetPath, recordID := round16InstallProject(t, source, project, registry, launcher)
	runID := "round16-cleanup-fence"
	state, err := Start(StartOptions{
		Root: project, PackageRoot: targetPath, RunID: runID,
		Flow: "formal", RequirementSource: "requirements.md", VCS: "git", Route: "lightweight",
		AdmissionRegistry: registry, AdmissionRecordID: recordID,
	})
	if err != nil {
		t.Fatal(err)
	}
	statePath := RunStatePath(project, runID)
	runDir := RunDir(project, runID)
	before := round16Read(t, statePath)

	round16SetRecordStatus(t, registry, state.AdmissionRecordID, "disabled")
	if _, err := CleanupTempRun(project, runID); err == nil || !strings.Contains(err.Error(), "UNREGISTERED_INSTALL") {
		t.Fatalf("cleanup --run deleted a bound run without admission: %v", err)
	}
	if _, statErr := os.Stat(runDir); statErr != nil {
		t.Fatalf("unadmitted cleanup removed the run directory: %v", statErr)
	}
	if after := round16Read(t, statePath); string(after) != string(before) {
		t.Fatal("unadmitted cleanup rewrote the run state ledger")
	}

	round16SetRecordStatus(t, registry, state.AdmissionRecordID, "active")
	deleted, err := CleanupTempRun(project, runID)
	if err != nil || !deleted {
		t.Fatalf("admitted cleanup --run failed: deleted=%v err=%v", deleted, err)
	}
	if _, statErr := os.Stat(runDir); !os.IsNotExist(statErr) {
		t.Fatalf("admitted cleanup left the run directory: %v", statErr)
	}

	residueID := "round16-stateless-residue"
	residueDir := RunDir(project, residueID)
	round16Write(t, filepath.Join(residueDir, "stray.txt"), "abandoned residue\n", 0o600)
	deleted, err = CleanupTempRun(project, residueID)
	if err != nil || !deleted {
		t.Fatalf("stateless residue cleanup failed: deleted=%v err=%v", deleted, err)
	}
	if _, statErr := os.Stat(residueDir); !os.IsNotExist(statErr) {
		t.Fatalf("stateless residue was not removed: %v", statErr)
	}

	if _, err := CleanupTempRun(project, "../"+runID); err == nil || !strings.Contains(err.Error(), "unsafe run id") {
		t.Fatalf("unsafe run id was not rejected: %v", err)
	}
}

// TestWhiteboxPhase0Round16InstallRejectsManifestUnregisteredHostTarget pins
// the payload-manifest host-target registration check: an install into a
// host the payload manifest does not register as an installable host-target
// (downgraded support or a missing entry) must be rejected before any
// target, launcher, or registry is created, while the registered manifest
// installs normally.
func TestWhiteboxPhase0Round16InstallRejectsManifestUnregisteredHostTarget(t *testing.T) {
	const claudeEntry = `{"name": "Claude Code", "support": "host-target"}`
	scenarios := []struct {
		name     string
		manifest string
		rejects  bool
	}{
		{name: "registered-control", manifest: round16Manifest, rejects: false},
		{
			name: "downgraded-support",
			manifest: strings.Replace(round16Manifest, claudeEntry,
				`{"name": "Claude Code", "support": "explanation-level"}`, 1),
			rejects: true,
		},
		{
			name: "missing-entry",
			manifest: strings.Replace(round16Manifest, claudeEntry+",\n  ", "", 1),
			rejects:  true,
		},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			source := round16Source(t)
			round16Write(t, filepath.Join(source, "formal-gates.manifest.json"), scenario.manifest, 0o600)
			project := t.TempDir()
			registry := filepath.Join(t.TempDir(), "registry.json")
			launcher := filepath.Join(t.TempDir(), "bin", nativeBinaryName())
			report, err := Install(InstallOptions{
				Source: source, Host: "claude", Scope: "project", Project: project,
				RegistryPath: registry, BinaryTarget: launcher, Force: true,
			})
			if scenario.rejects {
				if err == nil || !strings.Contains(err.Error(), "does not register") || !strings.Contains(err.Error(), "Claude Code") {
					t.Fatalf("install accepted a manifest-unregistered host target: %v", err)
				}
				if _, statErr := os.Stat(filepath.Join(project, ".claude", "skills", "formal-gates")); !os.IsNotExist(statErr) {
					t.Fatalf("rejected install left a target behind: %v", statErr)
				}
				if _, statErr := os.Stat(launcher); !os.IsNotExist(statErr) {
					t.Fatalf("rejected install left a stable launcher behind: %v", statErr)
				}
				if _, statErr := os.Stat(registry); !os.IsNotExist(statErr) {
					t.Fatalf("rejected install created a registry: %v", statErr)
				}
				if _, statErr := os.Stat(filepath.Join(project, ".gates")); !os.IsNotExist(statErr) {
					t.Fatalf("rejected install created workflow state roots: %v", statErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("registered manifest install failed: %v", err)
			}
			if len(report.Targets) != 1 || !isFile(registry) {
				t.Fatalf("registered manifest install did not commit its target and registry: %+v", report)
			}
		})
	}
}

// TestWhiteboxPhase0Round16GlobalSiblingIdentitySurvivesUpgradeAndBootstrap
// pins the global project-local sibling identity: a global install followed
// by a workflow start from an external project derives a project-local
// sibling record; a later upgrade of the global target refreshes the
// sibling's package and installed digests in place (same identity,
// generation, lease, and token), the documented bootstrap entry accepts the
// refreshed sibling instead of rejecting an unaccounted identity, and the
// next start of the same project reuses exactly that sibling binding.
func TestWhiteboxPhase0Round16GlobalSiblingIdentitySurvivesUpgradeAndBootstrap(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	source := round16Source(t)
	project := round16GitProject(t)
	registry := filepath.Join(home, ".formal-gates", "registry.json")
	launcher := filepath.Join(home, ".local", "bin", nativeBinaryName())
	first, err := Install(InstallOptions{
		Source: source, Host: "claude", Scope: "global",
		RegistryPath: registry, BinaryTarget: launcher, Force: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Install(InstallOptions{
		Source: source, Host: "claude", Scope: "global",
		RegistryPath: registry, BinaryTarget: launcher, Bootstrap: true, Force: true,
	}); err != nil {
		t.Fatalf("initial global bootstrap failed: %v", err)
	}
	canonicalID := first.Targets[0].RegistryRecordID
	state, err := Start(StartOptions{
		Root: project, PackageRoot: first.Targets[0].TargetPath, RunID: "round16-global-sibling",
		Flow: "formal", RequirementSource: "requirements.md", VCS: "git", Route: "lightweight",
		AdmissionRegistry: registry, AdmissionRecordID: canonicalID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if state.AdmissionRecordID == canonicalID || !strings.HasPrefix(state.AdmissionRecordID, canonicalID+"-project-") {
		t.Fatalf("global start did not bind the project-local sibling record: %+v", state)
	}
	if err := SaveRunState(project, state); err != nil {
		t.Fatalf("sibling-bound state write failed: %v", err)
	}
	document, err := LoadRegistry(registry)
	if err != nil {
		t.Fatal(err)
	}
	siblingBefore := round16RecordByID(t, document, state.AdmissionRecordID)
	if canonicalRegistryPath(siblingBefore.ProjectRoot) != canonicalRegistryPath(project) ||
		canonicalRegistryPath(siblingBefore.StateRoot) != canonicalRegistryPath(filepath.Join(project, ".gates")) {
		t.Fatalf("sibling record did not derive project-local roots: %+v", siblingBefore)
	}
	if err := DeleteRun(project, state.RunID); err != nil {
		t.Fatal(err)
	}

	round16Write(t, filepath.Join(source, "README.md"), "round sixteen upgraded runtime\n", 0o600)
	second, err := Install(InstallOptions{
		Source: source, Host: "claude", Scope: "global",
		RegistryPath: registry, BinaryTarget: launcher, Force: true,
	})
	if err != nil {
		t.Fatalf("global upgrade install failed: %v", err)
	}
	if second.PackageDigest == first.PackageDigest {
		t.Fatal("upgrade fixture did not change the package digest")
	}
	document, err = LoadRegistry(registry)
	if err != nil {
		t.Fatal(err)
	}
	siblingAfter := round16RecordByID(t, document, state.AdmissionRecordID)
	if siblingAfter.PackageDigest != second.PackageDigest ||
		siblingAfter.InstalledDigest != second.Targets[0].InstalledDigest {
		t.Fatalf("upgrade did not refresh the project-local sibling identity: before=%+v after=%+v", siblingBefore, siblingAfter)
	}
	if siblingAfter.Generation != siblingBefore.Generation ||
		siblingAfter.Lease != siblingBefore.Lease || siblingAfter.Token != siblingBefore.Token {
		t.Fatalf("upgrade rotated the sibling generation/lease/token: before=%+v after=%+v", siblingBefore, siblingAfter)
	}
	if _, err := Install(InstallOptions{
		Source: source, Host: "claude", Scope: "global",
		RegistryPath: registry, BinaryTarget: launcher, Bootstrap: true, Force: true,
	}); err != nil {
		t.Fatalf("bootstrap rejected the refreshed global sibling as unaccounted identity: %v", err)
	}

	secondState, err := Start(StartOptions{
		Root: project, PackageRoot: second.Targets[0].TargetPath, RunID: "round16-global-sibling-2",
		Flow: "formal", RequirementSource: "requirements.md", VCS: "git", Route: "lightweight",
		AdmissionRegistry: registry, AdmissionRecordID: canonicalID,
	})
	if err != nil {
		t.Fatalf("post-upgrade start of the same project failed: %v", err)
	}
	if secondState.AdmissionRecordID != state.AdmissionRecordID {
		t.Fatalf("post-upgrade start did not reuse the refreshed sibling binding: first=%q second=%q",
			state.AdmissionRecordID, secondState.AdmissionRecordID)
	}
	if secondState.AdmissionGeneration != siblingAfter.Generation {
		t.Fatalf("reused sibling binding lost its preserved generation: %+v", secondState)
	}
}

// TestWhiteboxPhase0Round16AdmissionRejectsStaleInstalledDigest pins the
// installed-target identity check inside admission: after a normal install
// the record admits, but once the installed tree stops matching the recorded
// digest (the installed runtime was edited in place), AdmitRegistry returns
// an unaccepted UNREGISTERED_INSTALL receipt with the stale-digest reason,
// the machine-readable receipt persists next to the registry, and workflow
// start through that record is refused before any state is created. The
// start branch is pinned at the same project root as the installed record
// and is triangulated: restored tree + documented bootstrap makes the same
// start succeed, so the re-edited refusal is specifically the stale digest.
func TestWhiteboxPhase0Round16AdmissionRejectsStaleInstalledDigest(t *testing.T) {
	source := round16Source(t)
	project := round16GitProject(t)
	isolated := t.TempDir()
	registry := filepath.Join(isolated, "registry.json")
	launcher := filepath.Join(isolated, "bin", nativeBinaryName())
	report, err := Install(InstallOptions{
		Source: source, Host: "claude", Scope: "project", Project: project,
		RegistryPath: registry, BinaryTarget: launcher, Force: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	recordID := report.Targets[0].RegistryRecordID
	receipt, err := AdmitRegistry(registry, recordID)
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.Accepted || receipt.Code != "ADMITTED" {
		t.Fatalf("freshly installed record was not admitted: %+v", receipt)
	}

	// 留存原始字节，供“恢复→可启动→再篡改→拒绝”三角验证使用。
	installedReadme := filepath.Join(report.Targets[0].TargetPath, "README.md")
	originalReadme := round16Read(t, installedReadme)
	round16Write(t, installedReadme, "edited installed runtime\n", 0o600)
	receipt, err = AdmitRegistry(registry, recordID)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Accepted || receipt.Code != "UNREGISTERED_INSTALL" || !strings.Contains(receipt.Reason, "stale") {
		t.Fatalf("edited installed target was still admitted: %+v", receipt)
	}
	var persisted AdmissionReceipt
	if err := json.Unmarshal(round16Read(t, registry+".admission.json"), &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.Accepted || persisted.Code != "UNREGISTERED_INSTALL" || persisted.RecordID != recordID {
		t.Fatalf("persisted admission receipt does not bind the stale-digest refusal: %+v", persisted)
	}

	// 篡改状态下，同一 project root 的 start 被拒且不创建 run 目录。
	if _, err := Start(StartOptions{
		Root: project, PackageRoot: report.Targets[0].TargetPath, RunID: "round16-stale-digest",
		Flow: "formal", RequirementSource: "requirements.md", VCS: "git", Split: "no",
		AdmissionRegistry: registry, AdmissionRecordID: recordID,
	}); err == nil || !strings.Contains(err.Error(), "UNREGISTERED_INSTALL") {
		t.Fatalf("workflow start admitted a stale installed digest: %v", err)
	}
	if _, statErr := os.Stat(RunDir(project, "round16-stale-digest")); !os.IsNotExist(statErr) {
		t.Fatalf("stale-digest start created a run directory: %v", statErr)
	}
	// 恢复原始字节并经文档化 bootstrap 提交 receipt：同一 root 的 start 必须
	// 成功，证明 root 与 receipt 路径本身畅通。
	round16Write(t, installedReadme, string(originalReadme), 0o600)
	if _, err := Install(InstallOptions{
		Source: source, Host: "claude", Scope: "project", Project: project,
		RegistryPath: registry, BinaryTarget: launcher, Bootstrap: true, Force: true,
	}); err != nil {
		t.Fatalf("documented bootstrap after restoring the installed tree failed: %v", err)
	}
	if _, err := Start(StartOptions{
		Root: project, PackageRoot: report.Targets[0].TargetPath, RunID: "round16-stale-digest-ok",
		Flow: "formal", RequirementSource: "requirements.md", VCS: "git", Split: "no",
		AdmissionRegistry: registry, AdmissionRecordID: recordID,
	}); err != nil {
		t.Fatalf("workflow start still refused after restore+bootstrap at the same root: %v", err)
	}
	// 再次篡改：同一 root（且 receipt 已在）的 start 再次被拒，拒绝因此特异
	// 地来自 stale installed digest。
	round16Write(t, installedReadme, "edited installed runtime\n", 0o600)
	if _, err := Start(StartOptions{
		Root: project, PackageRoot: report.Targets[0].TargetPath, RunID: "round16-stale-digest",
		Flow: "formal", RequirementSource: "requirements.md", VCS: "git", Split: "no",
		AdmissionRegistry: registry, AdmissionRecordID: recordID,
	}); err == nil || !strings.Contains(err.Error(), "UNREGISTERED_INSTALL") {
		t.Fatalf("workflow start admitted a re-edited installed digest: %v", err)
	}
	if _, statErr := os.Stat(RunDir(project, "round16-stale-digest")); !os.IsNotExist(statErr) {
		t.Fatalf("re-edited stale-digest start created a run directory: %v", statErr)
	}
}

// round16FakeCodex writes a stub codex command used through the documented
// CodexCommand option. It answers --version and "exec --help" (exposing
// --profile) and drives the canary session according to FAKE_CODEX_MODE:
// "retry-proof" emits no PreToolUse payload on the first exec run and one on
// the second, "never" never emits a payload, and "marker-proof" emits a
// payload together with the forbidden marker file on the first run.
func round16FakeCodex(t *testing.T, stateFile string) string {
	t.Helper()
	script := fmt.Sprintf(`#!/bin/sh
if [ "${1:-}" = "--version" ]; then
  echo "fake-codex 0.0.0"
  exit 0
fi
if [ "${2:-}" = "--help" ]; then
  echo "usage: codex exec --profile NAME"
  exit 0
fi
runs=0
if [ -f %q ]; then
  runs=$(cat %q)
fi
runs=$((runs+1))
printf '%%s\n' "$runs" > %q
mode="${FAKE_CODEX_MODE:-never}"
if [ "$mode" = "marker-proof" ]; then
  for dir in "$PWD"/.gates/tmp/codex-hook-canary/*/payloads; do
    [ -d "$dir" ] || continue
    printf '{"hook_event_name":"PreToolUse","tool_name":"shell"}\n' > "$dir/hook-PreToolUse-shell-fake-codex.json"
    printf 'HIT\n' > "$dir/../marker.txt"
  done
  exit 0
fi
if [ "$mode" = "retry-proof" ] && [ "$runs" -ge 2 ]; then
  for dir in "$PWD"/.gates/tmp/codex-hook-canary/*/payloads; do
    [ -d "$dir" ] || continue
    printf '{"hook_event_name":"PreToolUse","tool_name":"shell"}\n' > "$dir/hook-PreToolUse-shell-fake-codex.json"
  done
fi
exit 0
`, stateFile, stateFile, stateFile)
	path := filepath.Join(t.TempDir(), "fake-codex")
	round16Write(t, path, script, 0o755)
	return path
}

// TestWhiteboxPhase0Round16CodexHookCanaryRetriesTextOnlySession pins the
// documented live-proof retry behavior of the Codex hook canary: a driven
// session that answers the prompt in text (no PreToolUse payload, no marker)
// is retried exactly once and passes when the retry produces the payload
// proof; a session that never emits a payload stays FAIL after the single
// retry, and a session whose proof includes the forbidden marker fails
// without consuming a retry.
func TestWhiteboxPhase0Round16CodexHookCanaryRetriesTextOnlySession(t *testing.T) {
	cases := []struct {
		name        string
		mode        string
		wantStatus  string
		wantAttempt int
		wantReason  string
	}{
		{name: "text-only-then-retry-proof", mode: "retry-proof", wantStatus: "PASS", wantAttempt: 2},
		{name: "never-emits-payload", mode: "never", wantStatus: "FAIL", wantAttempt: 2, wantReason: "no PreToolUse"},
		{name: "marker-proof-no-retry", mode: "marker-proof", wantStatus: "FAIL", wantAttempt: 1, wantReason: "marker file"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			worktree := t.TempDir()
			t.Setenv("CODEX_HOME", filepath.Join(t.TempDir(), "codex-home"))
			t.Setenv("FAKE_CODEX_MODE", tc.mode)
			stateFile := filepath.Join(t.TempDir(), "runs")
			stub := round16FakeCodex(t, stateFile)
			summary, result := CodexHookCanary(CodexHookCanaryOptions{
				Worktree: worktree, CodexCommand: stub, TimeoutSeconds: 60, KeepTemp: true,
			})
			if summary.Status != tc.wantStatus {
				t.Fatalf("canary status=%q want %q (summary=%+v)", summary.Status, tc.wantStatus, summary)
			}
			if summary.Attempts != tc.wantAttempt {
				t.Fatalf("canary attempts=%d want %d (summary=%+v)", summary.Attempts, tc.wantAttempt, summary)
			}
			if tc.wantStatus == "PASS" {
				if !result.OK() {
					t.Fatalf("passing canary reported failures: %#v", result.Failures)
				}
				if summary.PreToolUsePayloadCount == 0 || summary.MarkerExists {
					t.Fatalf("passing canary lost its proof conditions: %+v", summary)
				}
				if !strings.Contains(summary.Diagnostic, "retry") {
					t.Fatalf("retry-produced proof was not diagnosed: %+v", summary)
				}
				if !isFile(filepath.Join(summary.ArtifactDir, "payloads", "hook-PreToolUse-shell-fake-codex.json")) {
					t.Fatalf("retry proof payload was not kept as evidence: %+v", summary)
				}
				return
			}
			if result.OK() {
				t.Fatalf("failing canary reported no failures: %#v", result.Failures)
			}
			if !strings.Contains(summary.FailureReason, tc.wantReason) {
				t.Fatalf("failure reason %q does not contain %q: %+v", summary.FailureReason, tc.wantReason, summary)
			}
			if tc.mode == "marker-proof" && !summary.MarkerExists {
				t.Fatalf("marker-proof session did not record the forbidden marker: %+v", summary)
			}
		})
	}
}
