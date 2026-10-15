//go:build qastructure

package validate

// These tests cover install admission, committed bootstrap receipts, registry
// identities, worker material write fencing and passive Codex hook capture.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/vcs"
	"formal-gates/internal/prompts"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func admissionWrite(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func admissionRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func admissionRun(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	command := exec.Command(name, args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, output)
	}
}

func admissionGitProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	admissionWrite(t, filepath.Join(root, "requirements.md"), testRequirementDocument("Sample requirement."), 0o600)
	admissionRun(t, root, "git", "init")
	admissionRun(t, root, "git", "config", "user.email", "admission@example.invalid")
	admissionRun(t, root, "git", "config", "user.name", "Admission Binding Test")
	admissionRun(t, root, "git", "add", "requirements.md")
	admissionRun(t, root, "git", "commit", "-m", "baseline")
	return root
}

// admissionManifest registers every installable host target so install only
// rejects a host when a scenario explicitly mutates the registration.
const admissionManifest = `{"name":"formal-gates","package_parts":["SKILL.md","README.md","README_EN.md","formal-gates.manifest.json","bin/","agents/","prompts/","gates/","references/","internal/harness/native_dsh.mjs","internal/harness/zcode_credential.mjs"],"hosts":[
  {"name": "Claude Code", "support": "host-target"},
  {"name": "Codex", "support": "host-target"},
  {"name": "Cursor", "support": "host-target"},
  {"name": "DeepSeek Harness", "support": "host-target"}
]}
`

// admissionSource builds the actual fixture package: managed instructions,
// admitted runtime inventory, prompt catalog and executable package owner.
func admissionSource(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	skill := "---\nname: formal-gates\n---\n" +
		hostInstructionsStartMarker + "\nsample managed rule\n" + hostInstructionsEndMarker + "\n"
	admissionWrite(t, filepath.Join(root, "SKILL.md"), skill, 0o600)
	admissionWrite(t, filepath.Join(root, "README.md"), "sample runtime\n", 0o600)
	admissionWrite(t, filepath.Join(root, "README_EN.md"), "sample runtime en\n", 0o600)
	admissionWrite(t, filepath.Join(root, "formal-gates.manifest.json"), admissionManifest, 0o600)
	admissionWrite(t, filepath.Join(root, "bin", nativeBinaryName()), string(stubBinaryPayload(t)), 0o755)
	admissionWrite(t, filepath.Join(root, "prompts", "reviewer-base.md"), "sample reviewer base\n", 0o600)
	catalog, err := prompts.LoadPromptCatalog(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range catalog.Actions {
		admissionWrite(t, filepath.Join(root, "prompts", "actions", action.ID+".md"), action.Content, 0o600)
	}
	for _, rules := range catalog.RoleRules {
		admissionWrite(t, filepath.Join(root, "prompts", "roles", rules.ID+".md"), rules.Content, 0o600)
	}
	admissionWrite(t, filepath.Join(root, "gates", "admission-gate.md"), "# sample gate\n", 0o600)
	admissionWrite(t, filepath.Join(root, "agents", "agent.md"), "agent\n", 0o600)
	admissionWrite(t, filepath.Join(root, "references", "reference.md"), "reference\n", 0o600)
	for _, name := range []string{"native_dsh.mjs", "zcode_credential.mjs"} {
		copyValidateTestFile(t, repoRootValidateTest(t), root, "internal/harness/"+name)
	}
	return root
}

// admissionInstallProject performs the documented native install plus the
// documented install --bootstrap maintenance entry for one project-scope
// claude target, returning the admission identity workflow start needs.
func admissionInstallProject(t *testing.T, source, project, registry, launcher string) (string, string) {
	t.Helper()
	report, err := Install(InstallOptions{
		Source: source, Host: "claude", Scope: "project", Project: project,
		RegistryPath: registry, BinaryTarget: launcher, Force: true,
	})
	if err != nil {
		t.Fatalf("admission native install failed: %v", err)
	}
	if len(report.Targets) != 1 {
		t.Fatalf("admission install reported %d targets: %+v", len(report.Targets), report)
	}
	if _, statErr := os.Stat(registry + ".bootstrap.json"); !os.IsNotExist(statErr) {
		t.Fatalf("plain install committed a bootstrap receipt: %v", statErr)
	}
	if _, err := Install(InstallOptions{
		Source: source, Host: "claude", Scope: "project", Project: project,
		RegistryPath: registry, BinaryTarget: launcher, Bootstrap: true, Force: true,
	}); err != nil {
		t.Fatalf("admission documented bootstrap entry failed: %v", err)
	}
	return report.Targets[0].TargetPath, report.Targets[0].RegistryRecordID
}

// admissionSetRecordStatus flips one record's status through the one semantic
// registry transaction owner while holding the shared registry lock.
func admissionSetRecordStatus(t *testing.T, registry, recordID, status string) {
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

func admissionRecordByID(t *testing.T, document RegistryDocument, id string) RegistryRecord {
	t.Helper()
	for _, record := range document.Records {
		if record.ID == id {
			return record
		}
	}
	t.Fatalf("registry record %q was not found", id)
	return RegistryRecord{}
}

// TestFirstStartRequiresCommittedBootstrapReceipt pins
// the documented first-start boundary: a normal install commits registry
// records without a bootstrap receipt, and workflow start must hard-reject
// (UNREGISTERED_INSTALL and no state) until
// the documented install --bootstrap entry commits an accepted receipt.
func TestFirstStartRequiresCommittedBootstrapReceipt(t *testing.T) {
	source := admissionSource(t)
	project := admissionGitProject(t)
	isolated := t.TempDir()
	registry := filepath.Join(isolated, "registry.json")
	launcher := filepath.Join(isolated, "bin", nativeBinaryName())
	report, err := Install(InstallOptions{Source: source, Host: "claude", Scope: "project", Project: project, RegistryPath: registry, BinaryTarget: launcher, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	targetPath, recordID := report.Targets[0].TargetPath, report.Targets[0].RegistryRecordID
	if _, err := os.Stat(registry + ".bootstrap.json"); !os.IsNotExist(err) {
		t.Fatalf("plain install wrote bootstrap receipt: %v", err)
	}
	runID := "admission-first-start"
	rejectStart := func(reason string) {
		t.Helper()
		if _, err := admissionStartCurrent(project, targetPath, registry, recordID, runID, "regular"); err == nil || !strings.Contains(err.Error(), "UNREGISTERED_INSTALL") || !strings.Contains(err.Error(), reason) {
			t.Fatalf("start did not reject %q: %v", reason, err)
		}
		if _, err := os.Stat(admissionEngineDirectory(project, runID)); !os.IsNotExist(err) {
			t.Fatalf("rejected start created authority: %v", err)
		}
	}
	rejectStart("bootstrap receipt")
	admissionWrite(t, registry+".bootstrap.json", "{not a receipt", 0600)
	rejectStart("bootstrap receipt is invalid")
	if err := writeJSONAtomically(registry+".bootstrap.json", BootstrapReceipt{Operation: "bootstrap", Accepted: false, Registry: registry, Epoch: 1, ObservedAt: nowReceiptTime()}); err != nil {
		t.Fatal(err)
	}
	rejectStart("not an accepted pre-state receipt")
	if _, err = Install(InstallOptions{Source: source, Host: "claude", Scope: "project", Project: project, RegistryPath: registry, BinaryTarget: launcher, Bootstrap: true, Force: true}); err != nil {
		t.Fatal(err)
	}
	var receipt BootstrapReceipt
	if err = json.Unmarshal(admissionRead(t, registry+".bootstrap.json"), &receipt); err != nil {
		t.Fatal(err)
	}
	if !receipt.Accepted || receipt.Operation != "bootstrap" {
		t.Fatalf("bootstrap receipt not committed: %+v", receipt)
	}
	run, err := admissionStartCurrent(project, targetPath, registry, recordID, runID, "regular")
	if err != nil {
		t.Fatal(err)
	}
	request := admissionCurrentRequest(t, project, runID)
	if request.InstalledTargetIdentity != recordID || !isFile(filepath.Join(admissionEngineDirectory(project, runID), "state.json")) {
		t.Fatalf("start lost admission identity: %+v %+v", run, request)
	}
}

func TestEngineAdmissionPrecedesWorkerMaterial(t *testing.T) {
	source := admissionSource(t)
	project := admissionGitProject(t)
	isolated := t.TempDir()
	registry := filepath.Join(isolated, "registry.json")
	launcher := filepath.Join(isolated, "bin", nativeBinaryName())
	target, recordID := admissionInstallProject(t, source, project, registry, launcher)
	runID := "admission-write-fence"
	_, err := admissionStartCurrent(project, target, registry, recordID, runID, "regular")
	if err != nil {
		t.Fatal(err)
	}
	f, err := facade.Open(project, runID)
	if err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(admissionEngineDirectory(project, runID), "state.json")
	before := admissionRead(t, statePath)
	admissionSetRecordStatus(t, registry, recordID, "disabled")
	if _, err = f.Drive(); err == nil || !strings.HasPrefix(err.Error(), "STALE_FENCING_TOKEN:") {
		t.Fatalf("drive accepted disabled admission: %v", err)
	}
	if after := admissionRead(t, statePath); string(after) != string(before) {
		t.Fatal("rejected drive changed authority")
	}
	if _, err = os.Stat(filepath.Join(admissionEngineDirectory(project, runID), "materials")); !os.IsNotExist(err) {
		t.Fatalf("rejected drive left materials: %v", err)
	}
	admissionSetRecordStatus(t, registry, recordID, "active")
	run, err := f.Drive()
	if err != nil {
		t.Fatal(err)
	}
	if len(run.WorkerMaterials) != 1 || len(run.PendingActions) != 1 {
		t.Fatalf("restored drive lost issued material: %+v", run.WorkerMaterials)
	}
	for id, ref := range run.WorkerMaterials {
		raw := admissionRead(t, ref.Path)
		if ref.ActionID != id || !strings.Contains(string(raw), "Sample requirement.") {
			t.Fatalf("issued material lost context: %s", raw)
		}
		sum := sha256.Sum256(raw)
		if "sha256:"+hex.EncodeToString(sum[:]) != ref.Digest {
			t.Fatalf("issued material digest mismatch: %+v", ref)
		}
	}
}

func TestInstallRejectsManifestUnregisteredHostTarget(t *testing.T) {
	const claudeEntry = `{"name": "Claude Code", "support": "host-target"}`
	scenarios := []struct {
		name     string
		manifest string
		rejects  bool
	}{
		{name: "registered-control", manifest: admissionManifest, rejects: false},
		{
			name: "downgraded-support",
			manifest: strings.Replace(admissionManifest, claudeEntry,
				`{"name": "Claude Code", "support": "explanation-level"}`, 1),
			rejects: true,
		},
		{
			name:     "missing-entry",
			manifest: strings.Replace(admissionManifest, claudeEntry+",\n  ", "", 1),
			rejects:  true,
		},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			source := admissionSource(t)
			admissionWrite(t, filepath.Join(source, "formal-gates.manifest.json"), scenario.manifest, 0o600)
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

func TestWorkflowStartRefusesEditedInstalledTarget(t *testing.T) {
	source := admissionSource(t)
	project := admissionGitProject(t)
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
	originalReadme := admissionRead(t, installedReadme)
	admissionWrite(t, installedReadme, "edited installed runtime\n", 0o600)
	receipt, err = AdmitRegistry(registry, recordID)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Accepted || receipt.Code != "UNREGISTERED_INSTALL" || !strings.Contains(receipt.Reason, "stale") {
		t.Fatalf("edited installed target was still admitted: %+v", receipt)
	}
	var persisted AdmissionReceipt
	if err := json.Unmarshal(admissionRead(t, registry+".admission.json"), &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.Accepted || persisted.Code != "UNREGISTERED_INSTALL" || persisted.RecordID != recordID {
		t.Fatalf("persisted admission receipt does not bind the stale-digest refusal: %+v", persisted)
	}

	// 篡改状态下，同一 project root 的 start 被拒且不创建 run 目录。
	if _, err := admissionStartCurrent(project, report.Targets[0].TargetPath, registry, recordID, "admission-stale-digest", "regular"); err == nil || !strings.Contains(err.Error(), "UNREGISTERED_INSTALL") {
		t.Fatalf("workflow start admitted a stale installed digest: %v", err)
	}
	if _, statErr := os.Stat(admissionEngineDirectory(project, "admission-stale-digest")); !os.IsNotExist(statErr) {
		t.Fatalf("stale-digest start created a run directory: %v", statErr)
	}
	// 恢复原始字节并经文档化 bootstrap 提交 receipt：同一 root 的 start 必须
	// 成功，证明 root 与 receipt 路径本身畅通。
	admissionWrite(t, installedReadme, string(originalReadme), 0o600)
	if _, err := Install(InstallOptions{
		Source: source, Host: "claude", Scope: "project", Project: project,
		RegistryPath: registry, BinaryTarget: launcher, Bootstrap: true, Force: true,
	}); err != nil {
		t.Fatalf("documented bootstrap after restoring the installed tree failed: %v", err)
	}
	if _, err := admissionStartCurrent(project, report.Targets[0].TargetPath, registry, recordID, "admission-stale-digest-ok", "regular"); err != nil {
		t.Fatalf("workflow start still refused after restore+bootstrap at the same root: %v", err)
	}
	// 再次篡改：同一 root（且 receipt 已在）的 start 再次被拒，拒绝因此特异
	// 地来自 stale installed digest。
	admissionWrite(t, installedReadme, "edited installed runtime\n", 0o600)
	if _, err := admissionStartCurrent(project, report.Targets[0].TargetPath, registry, recordID, "admission-stale-digest", "regular"); err == nil || !strings.Contains(err.Error(), "UNREGISTERED_INSTALL") {
		t.Fatalf("workflow start admitted a re-edited installed digest: %v", err)
	}
	if _, statErr := os.Stat(admissionEngineDirectory(project, "admission-stale-digest")); !os.IsNotExist(statErr) {
		t.Fatalf("re-edited stale-digest start created a run directory: %v", statErr)
	}
}

// Current test setup uses the same admission and typed bootstrap owners as the CLI.
func admissionStartCurrent(project, target, registry, recordID, runID, route string) (facade.Run, error) {
	restore := fixtureRegisteredInvocation(registry, target, recordID)
	defer restore()
	admission, err := ResolveEngineAdmissionByRecord(recordID, registry)
	if err != nil {
		return facade.Run{}, err
	}
	bound, err := ResolveEngineAdmission(project, target, registry)
	if err != nil {
		return facade.Run{}, err
	}
	if admission.RecordID != bound.RecordID {
		return facade.Run{}, fmt.Errorf("UNREGISTERED_INSTALL: record does not match workflow root")
	}
	raw, err := os.ReadFile(filepath.Join(project, "requirements.md"))
	if err != nil {
		return facade.Run{}, err
	}
	revision := fmt.Sprintf("%x", sha256.Sum256(raw))
	receipt := protocol.IntakeConfirmationReceipt{Source: facade.DefaultIntakeSource, Authority: facade.DefaultIntakeAuthority, Transport: facade.DefaultIntakeTransport, RequirementSource: "requirements.md", RequirementRevision: revision, Artifacts: []protocol.IntakeArtifact{{Path: "requirements.md", Revision: revision}}, SolutionRevision: revision, SolutionDigest: "sha256:" + revision}
	base := ""
	if route == "regular" {
		base, err = vcs.New(project, nil).Resolve()
		if err != nil {
			return facade.Run{}, err
		}
	}
	_, run, err := facade.Start(facade.StartOptions{Root: project, ArtifactRoot: project, Capacity: 1, Request: facade.StartRequest{RunID: runID, Route: route, VCS: "git", BaseSnapshot: base, Provider: admission.Provider, IntakeConfirmationReceipt: receipt}, Admission: &facade.Admission{RegistryPath: admission.RegistryPath, Target: admission.Target, Provider: admission.Provider, PackageDigest: admission.PackageDigest, InstalledTargetIdentity: admission.RecordID, Generation: admission.Generation, Lease: admission.Lease, Token: admission.Token}})
	return run, err
}
func admissionEngineDirectory(root, runID string) string {
	return filepath.Join(root, facade.EngineNamespace, runID)
}
func admissionCurrentRequest(t *testing.T, root, runID string) facade.StartRequest {
	t.Helper()
	var request facade.StartRequest
	if err := json.Unmarshal(admissionRead(t, filepath.Join(admissionEngineDirectory(root, runID), "request.json")), &request); err != nil {
		t.Fatal(err)
	}
	return request
}
