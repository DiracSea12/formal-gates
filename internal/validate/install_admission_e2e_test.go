//go:build qastructure

package validate

// End-to-end tests for the versioned envelope, the install/uninstall
// transaction, and registry admission. Each test exercises the behavior at the
// owning responsibility through the public entry points and on-disk evidence.

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

func e2eWriteFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func e2eReadFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func e2eRun(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	command := exec.Command(name, args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

func e2eRepoRoot(t *testing.T) string {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok || !filepath.IsAbs(sourceFile) {
		t.Fatal("could not locate the whitebox test source as an absolute path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "..", ".."))
}

func e2eStartFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	e2eWriteFile(t, filepath.Join(root, "requirements.md"), "sample requirement\n", 0o600)
	e2eRun(t, root, "git", "init")
	e2eRun(t, root, "git", "config", "user.email", "formal-gates@example.invalid")
	e2eRun(t, root, "git", "config", "user.name", "Formal Gates Test")
	e2eRun(t, root, "git", "add", "requirements.md")
	e2eRun(t, root, "git", "commit", "-m", "baseline")
	return root, e2eRepoRoot(t)
}

// hostTargetTestManifest 是测试 fixture 的最小 manifest：登记四个可安装 host
// target。install 会拒绝 manifest 未登记为 host-target 的安装目标（unknown
// target 在写 target/state 前非零拒绝），fixture 必须携带该登记。
const hostTargetTestManifest = `{"name":"formal-gates","package_parts":["SKILL.md","README.md","README_EN.md","formal-gates.manifest.json","bin/","agents/","prompts/","gates/","references/","internal/harness/native_dsh.mjs","internal/harness/zcode_credential.mjs"],"hosts":[` +
	`{"name":"Claude Code","support":"host-target"},` +
	`{"name":"Codex","support":"host-target"},` +
	`{"name":"Cursor","support":"host-target"},` +
	`{"name":"DeepSeek Harness","support":"host-target"}]}` + "\n"

func e2eInstallSource(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	e2eWriteFile(t, filepath.Join(root, "SKILL.md"), "---\nname: formal-gates\n---\n"+hostInstructionsStartMarker+"\nsample rule\n"+hostInstructionsEndMarker+"\n", 0o600)
	e2eWriteFile(t, filepath.Join(root, "README.md"), "runtime\n", 0o600)
	e2eWriteFile(t, filepath.Join(root, "README_EN.md"), "runtime\n", 0o600)
	e2eWriteFile(t, filepath.Join(root, "formal-gates.manifest.json"), hostTargetTestManifest, 0o600)
	e2eWriteFile(t, filepath.Join(root, "bin", nativeBinaryName()), string(stubBinaryPayload(t)), 0o700)
	for _, entry := range []string{"agents/agent.md", "prompts/action.md", "gates/gate.md", "references/reference.md"} {
		e2eWriteFile(t, filepath.Join(root, filepath.FromSlash(entry)), entry+"\n", 0o600)
	}
	for _, name := range []string{"native_dsh.mjs", "zcode_credential.mjs"} {
		copyValidateTestFile(t, repoRootValidateTest(t), root, "internal/harness/"+name)
	}
	return root
}

func e2eRegistryRecord(id, target, launcher, scope, host, project, status string) RegistryRecord {
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
		VCSIdentity: "fixture-native", PackageDigest: "sha256:fixture-source", InstalledDigest: "sha256:fixture-target",
		CanonicalPaths: map[string]string{
			"target": target, "launcher": launcher, "projectRoot": project,
			"stateRoot": state, "resourceRoot": resources, "runtimeSibling": runtime,
		},
	}
}

// e2eCommitRegistry exercises the one production semantic registry owner
// while supplying the lock that Install/Uninstall normally hold around it.
func e2eCommitRegistry(t *testing.T, path string, records ...RegistryRecord) RegistryDocument {
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

// writeTestBootstrapReceipt materializes the committed bootstrap receipt
// workflow start requires beside a test registry. Test fixtures commit
// registry records directly instead of running the install --bootstrap
// maintenance entry, so they must provide the same first-start boundary the
// documented flow produces.
func writeTestBootstrapReceipt(t *testing.T, registryPath string) {
	t.Helper()
	if err := writeJSONAtomically(registryPath+".bootstrap.json", BootstrapReceipt{Operation: "bootstrap", Accepted: true, Registry: registryPath, Epoch: 1, ObservedAt: nowReceiptTime()}); err != nil {
		t.Fatal(err)
	}
}

func e2eAssertOldInstallRestored(t *testing.T, target installTarget, hookBefore, ruleBefore string) {
	t.Helper()
	if got := e2eReadFile(t, filepath.Join(target.targetPath, "old.txt")); got != "old runtime\n" {
		t.Fatalf("old runtime was not restored: %q", got)
	}
	if _, err := os.Stat(filepath.Join(target.targetPath, "README.md")); !os.IsNotExist(err) {
		t.Fatalf("candidate runtime remained after rollback: %v", err)
	}
	if got := e2eReadFile(t, target.hookConfig); got != hookBefore {
		t.Fatalf("hook config was not restored byte-for-byte:\n%s", got)
	}
	if got := e2eReadFile(t, target.managedRulePath); got != ruleBefore {
		t.Fatalf("managed rule was not restored byte-for-byte:\n%s", got)
	}
}

func e2eInstallLockChild(t *testing.T) bool {
	t.Helper()
	mode := os.Getenv("TEST_INSTALL_LOCK_CHILD")
	if mode == "" {
		return false
	}
	target := os.Getenv("TEST_INSTALL_LOCK_TARGET")
	unlock, err := acquireInstallLock(target)
	switch mode {
	case "held":
		if err == nil {
			unlock()
			fmt.Fprint(os.Stdout, "unexpected-acquire")
			return true
		}
		var held *LockHeldError
		if !errors.As(err, &held) || held.Operation != "install/uninstall" || held.Path != installLockPath(target) {
			t.Fatalf("held install lock identity: %v", err)
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

func e2eRunInstallLockChild(t *testing.T, target, mode string) string {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run", "^TestInstallLockSerializesInstallAndUninstall$", "-test.v")
	command.Env = append(os.Environ(), "TEST_INSTALL_LOCK_CHILD="+mode, "TEST_INSTALL_LOCK_TARGET="+target)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("lock child %s failed: %v\n%s", mode, err, output)
	}
	return string(output)
}

func TestPackageReceiptDeterminismAndIsolation(t *testing.T) {
	// 夹具必须是真实 git 仓库：PackageReceipt 用 git ls-files --ignored 识
	// 别被忽略的本地数据，只放一个文件的假 .git 目录会让 git 以退出码
	// 128 失败。.gitignore 忽略 .gates/ 运行数据并忽略自身，收据清单因此
	// 只剩两个被提交跟踪的包输入。
	root := t.TempDir()
	e2eWriteFile(t, filepath.Join(root, "z.txt"), "z\n", 0o600)
	e2eWriteFile(t, filepath.Join(root, "nested", "a.txt"), "a\n", 0o640)
	e2eWriteFile(t, filepath.Join(root, ".gitignore"), ".gates/\n.gitignore\n", 0o600)
	e2eWriteFile(t, filepath.Join(root, ".gates", "ignored"), "runtime\n", 0o600)
	e2eRun(t, root, "git", "init")
	e2eRun(t, root, "git", "config", "user.email", "formal-gates@example.invalid")
	e2eRun(t, root, "git", "config", "user.name", "Formal Gates Test")
	e2eRun(t, root, "git", "add", "z.txt", filepath.Join("nested", "a.txt"))
	e2eRun(t, root, "git", "commit", "-m", "baseline")

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
	e2eWriteFile(t, filepath.Join(root, "nested", "a.txt"), "changed\n", 0o640)
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
	e2eWriteFile(t, filepath.Join(outside, "payload"), "outside\n", 0o600)
	if err := os.Symlink(filepath.Join(outside, "payload"), filepath.Join(root, "live-link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := PackageReceipt(root); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("live package symlink was accepted: %v", err)
	}
}

func TestBaselineReceiptBindsAllIdentities(t *testing.T) {
	source := t.TempDir()
	installed := t.TempDir()
	e2eWriteFile(t, filepath.Join(source, "payload"), "same bytes\n", 0o600)
	e2eWriteFile(t, filepath.Join(installed, "payload"), "same bytes\n", 0o600)
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
	if err := json.Unmarshal([]byte(e2eReadFile(t, output)), &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.VCSIdentity != receipt.VCSIdentity || persisted.SourceRoot != receipt.SourceRoot || persisted.PackageDigest != receipt.PackageDigest || persisted.InstalledTarget != receipt.InstalledTarget || persisted.InstalledTargetDigest != receipt.InstalledTargetDigest || len(persisted.CanonicalPaths) != len(receipt.CanonicalPaths) || persisted.CanonicalPaths["hostConfig"] != receipt.CanonicalPaths["hostConfig"] || persisted.GeneratedAt != receipt.GeneratedAt {
		t.Fatalf("persisted baseline lost identities: %+v", persisted)
	}
	data := e2eReadFile(t, output)
	if !strings.HasPrefix(data, "{") || !strings.HasSuffix(data, "}\n") {
		t.Fatalf("baseline receipt was not persisted as one complete JSON document: %q", data)
	}
	entries, err := os.ReadDir(filepath.Dir(output))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".state-") {
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

func TestRegistryTransactionOwnerPreservesRecords(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "registry.json")
	initial := e2eRegistryRecord("target-1", filepath.Join(root, "install", "stable"), filepath.Join(root, "bin", nativeBinaryName()), "global", "codex", filepath.Join(root, "home"), "active")
	doc := e2eCommitRegistry(t, path, initial)
	if doc.SchemaVersion != RegistrySchemaVersion || doc.Epoch != 1 || len(doc.Records) != 1 {
		t.Fatalf("initial transaction document is incomplete: %+v", doc)
	}
	if doc.Records[0].ID != "target-1" || doc.Records[0].Status != "active" || doc.Records[0].Generation == 0 || doc.Records[0].Lease == "" || doc.Records[0].Token == "" {
		t.Fatalf("transaction identity was not materialized: %+v", doc.Records[0])
	}

	replacement := initial
	replacement.Status = "disabled"
	doc = e2eCommitRegistry(t, path, replacement)
	if doc.Epoch != 2 || len(doc.Records) != 1 || doc.Records[0].ID != replacement.ID || doc.Records[0].Status != replacement.Status || doc.Records[0].Target != replacement.Target || doc.Records[0].RuntimeSibling != replacement.RuntimeSibling || len(doc.Records[0].CanonicalPaths) == 0 || doc.Records[0].CanonicalPaths["target"] != replacement.CanonicalPaths["target"] {
		t.Fatalf("same-id transaction appended or lost the epoch: %+v", doc)
	}

	second := e2eRegistryRecord("project-target", filepath.Join(root, "install", "project"), filepath.Join(root, "bin", nativeBinaryName()), "project", "claude", filepath.Join(root, "project"), "active")
	doc = e2eCommitRegistry(t, path, second)
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

func TestInstallBootstrapReceiptBindsRecordAndCreatesNoState(t *testing.T) {
	source := e2eInstallSource(t)
	project := t.TempDir()
	registry := filepath.Join(t.TempDir(), "registry.json")
	launcher := filepath.Join(t.TempDir(), "stable", nativeBinaryName())
	e2eWriteFile(t, launcher, e2eReadFile(t, filepath.Join(source, "bin", nativeBinaryName())), 0o700)
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
	if report.BootstrapReceiptPath != filepath.ToSlash(registry+".bootstrap.json") || report.ReceiptPath != report.BootstrapReceiptPath {
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
	if err := json.Unmarshal([]byte(e2eReadFile(t, report.BootstrapReceiptPath)), &receipt); err != nil {
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

func TestAdmissionRejectsIncompleteDisabledAndMissingRecords(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "registry.json")
	home := filepath.Join(root, "home")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	source := e2eInstallSource(t)
	report, err := Install(InstallOptions{Source: source, Host: "codex", Scope: "global", RegistryPath: path, BinaryTarget: filepath.Join(root, "bin", nativeBinaryName()), Force: true, SkipHooks: true})
	if err != nil {
		t.Fatal(err)
	}
	document, err := LoadRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	complete := installRecordByID(t, document, report.Targets[0].RegistryRecordID)
	disabled := e2eRegistryRecord("disabled", filepath.Join(root, "install-disabled"), filepath.Join(root, "bin", nativeBinaryName()), "project", "claude", filepath.Join(root, "project"), "disabled")
	e2eCommitRegistry(t, path, complete, disabled)
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
	accepted, err := AdmitRegistry(path, complete.ID)
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
		if err := json.Unmarshal([]byte(e2eReadFile(t, path+".admission.json")), &persisted); err != nil {
			t.Fatal(err)
		}
		if persisted.RecordID != id || persisted.Code != "UNREGISTERED_INSTALL" || persisted.Accepted {
			t.Fatalf("denial receipt does not match attempted registration %q: %+v", id, persisted)
		}
	}
}

func TestInstallLockSerializesInstallAndUninstall(t *testing.T) {
	if e2eInstallLockChild(t) {
		return
	}
	target := filepath.Join(t.TempDir(), "skills", "formal-gates")
	unlock, err := acquireInstallLock(target)
	if err != nil {
		t.Fatal(err)
	}
	unlock()
	lockPath := installLockPath(target)
	contents := e2eReadFile(t, lockPath)
	before, err := os.Stat(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	assertLockUnchanged := func(locked bool) {
		t.Helper()
		after, err := os.Stat(lockPath)
		if err != nil || !os.SameFile(before, after) {
			t.Fatalf("install lock identity changed: %v", err)
		}
		// Windows denies reads of the region while its lock is held.
		if locked && runtime.GOOS == "windows" {
			return
		}
		if data := e2eReadFile(t, lockPath); data != contents {
			t.Fatalf("install lock contents changed: before=%q after=%q", contents, data)
		}
	}
	unlock, err = acquireInstallLock(target)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(unlock)
	childOutput := e2eRunInstallLockChild(t, target, "held")
	if !strings.Contains(childOutput, "held-error:") || !strings.Contains(childOutput, "lock is held") {
		unlock()
		t.Fatalf("cross-process install/uninstall acquired the same lock: %s", childOutput)
	}
	assertLockUnchanged(true)
	unlock()
	assertLockUnchanged(false)
	// 释放即关闭描述符：锁文件持久保留（内核文件锁永不删除），能否被
	// 第二个进程获取才是释放语义。
	childOutput = e2eRunInstallLockChild(t, target, "free")
	if !strings.Contains(childOutput, "free-acquired") {
		t.Fatalf("lock could not be acquired by a second process after release: %s", childOutput)
	}
	assertLockUnchanged(false)
}

func TestInstallFaultMatrixRestoresRuntimeHooksAndRule(t *testing.T) {
	for _, fault := range []string{"intent", "registry", "prepared", "switched", "post-switch-smoke", "pointer", "hook", "managed-rule", "registry-commit"} {
		t.Run(fault, func(t *testing.T) {
			source := e2eInstallSource(t)
			root := t.TempDir()
			project := filepath.Join(root, "project")
			targets, err := resolveInstallTargets("claude", "project", project)
			if err != nil {
				t.Fatal(err)
			}
			target := targets[0]
			launcher := filepath.Join(root, "stable", nativeBinaryName())
			registry := filepath.Join(root, "registry", "registry.json")
			e2eWriteFile(t, filepath.Join(target.targetPath, "old.txt"), "old runtime\n", 0o600)
			e2eWriteFile(t, launcher, "old launcher\n", 0o700)
			hookBefore := `{"hooks":{"Unrelated":[{"command":"keep"}]}}` + "\n"
			ruleBefore := "unrelated rule\n"
			e2eWriteFile(t, target.hookConfig, hookBefore, 0o600)
			e2eWriteFile(t, target.managedRulePath, ruleBefore, 0o600)
			t.Setenv("FORMAL_GATES_INSTALL_FAULT", fault)
			_, err = Install(InstallOptions{Source: source, Host: "claude", Scope: "project", Project: project, BinaryTarget: launcher, RegistryPath: registry, Force: true})
			if err == nil || !strings.Contains(err.Error(), "deterministic install fault") {
				t.Fatalf("fault %q did not interrupt installation: %v", fault, err)
			}
			e2eAssertOldInstallRestored(t, target, hookBefore, ruleBefore)
			if got := e2eReadFile(t, launcher); got != "old launcher\n" {
				t.Fatalf("stable launcher was not restored: %q", got)
			}
			var receipt installRecoveryReceipt
			failurePath := installOuterJournalPath(registry) + ".failure.json"
			if err := json.Unmarshal([]byte(e2eReadFile(t, failurePath)), &receipt); err != nil {
				t.Fatal(err)
			}
			if receipt.Operation != "install" || receipt.Target != registry || receipt.Phase == "" || !receipt.Recovered || receipt.Outcome != "ROLLED_BACK" {
				t.Fatalf("fault receipt is incomplete: %+v", receipt)
			}
		})
	}
}

func TestHookJSONFailureRestoresInstallAndWritesReceipt(t *testing.T) {
	source := e2eInstallSource(t)
	root := t.TempDir()
	project := filepath.Join(root, "project")
	targets, err := resolveInstallTargets("claude", "project", project)
	if err != nil {
		t.Fatal(err)
	}
	target := targets[0]
	launcher := filepath.Join(root, "stable", nativeBinaryName())
	registry := filepath.Join(root, "registry", "registry.json")
	e2eWriteFile(t, filepath.Join(target.targetPath, "old.txt"), "old runtime\n", 0o600)
	e2eWriteFile(t, launcher, "old launcher\n", 0o700)
	hookBefore := "{invalid-json\n"
	ruleBefore := "unrelated rule\n"
	e2eWriteFile(t, target.hookConfig, hookBefore, 0o600)
	e2eWriteFile(t, target.managedRulePath, ruleBefore, 0o600)

	_, err = Install(InstallOptions{Source: source, Host: "claude", Scope: "project", Project: project, BinaryTarget: launcher, RegistryPath: registry, Force: true})
	if err == nil || !strings.Contains(err.Error(), "not valid JSON") {
		t.Fatalf("malformed hook JSON did not stop installation: %v", err)
	}
	e2eAssertOldInstallRestored(t, target, hookBefore, ruleBefore)
	var receipt installRecoveryReceipt
	failurePath := installOuterJournalPath(registry) + ".failure.json"
	if err := json.Unmarshal([]byte(e2eReadFile(t, failurePath)), &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Operation != "install" || receipt.Target != registry || receipt.Phase != "smoke-passed" || !receipt.Recovered {
		t.Fatalf("hook JSON failure receipt is incomplete: %+v", receipt)
	}
}

func TestUninstallFaultRestoresRuntimeHooksAndRule(t *testing.T) {
	source := e2eInstallSource(t)
	root := t.TempDir()
	project := filepath.Join(root, "project")
	launcher := filepath.Join(root, "stable", nativeBinaryName())
	registry := filepath.Join(root, "registry", "registry.json")
	report, err := Install(InstallOptions{Source: source, Host: "claude", Scope: "project", Project: project, BinaryTarget: launcher, RegistryPath: registry, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	target := report.Targets[0]
	hookBefore := e2eReadFile(t, target.HookConfig)
	ruleBefore := e2eReadFile(t, target.ManagedRulePath)
	t.Setenv("FORMAL_GATES_INSTALL_FAULT", "hook")
	_, err = Uninstall(UninstallOptions{Host: "claude", Scope: "project", Project: project, RegistryPath: registry})
	if err == nil || !strings.Contains(err.Error(), "hook") {
		t.Fatalf("uninstall fault did not interrupt the transaction: %v", err)
	}
	if !isFile(filepath.Join(target.TargetPath, "README.md")) {
		t.Fatal("uninstall did not restore installed runtime")
	}
	if got := e2eReadFile(t, target.HookConfig); got != hookBefore {
		t.Fatalf("uninstall did not restore hook bytes: %s", got)
	}
	if got := e2eReadFile(t, target.ManagedRulePath); got != ruleBefore {
		t.Fatalf("uninstall did not restore rule bytes: %s", got)
	}
	var receipt installRecoveryReceipt
	if err := json.Unmarshal([]byte(e2eReadFile(t, installOuterJournalPath(registry)+".failure.json")), &receipt); err != nil {
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

func TestCrashJournalReconcileRestoresBackupAndReceipt(t *testing.T) {
	parent := t.TempDir()
	target := filepath.Join(parent, "skills", "formal-gates")
	registry := filepath.Join(parent, "registry", "registry.json")
	transactionRoot := filepath.Join(parent, "transaction")
	staged := filepath.Join(parent, "skills", ".formal-gates-stage-crash")
	e2eWriteFile(t, filepath.Join(target, "stable.txt"), "stable\n", 0o600)
	e2eWriteFile(t, registry, "stable registry\n", 0o600)
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
	e2eWriteFile(t, filepath.Join(target, "candidate.txt"), "candidate\n", 0o600)
	e2eWriteFile(t, staged, "partial\n", 0o600)
	e2eWriteFile(t, registry, "candidate registry\n", 0o600)
	journal := outerInstallJournal{Operation: "install", RegistryPath: registry, TransactionRoot: transactionRoot, Phase: "switched", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), Registry: registryBefore, Targets: []outerTargetSnapshot{{TargetPath: target, Tree: outerTreeFromBackup(tree)}}, Staged: []string{staged}}
	journalPath := installOuterJournalPath(registry)
	if err := writeJSONAtomically(journalPath, journal); err != nil {
		t.Fatal(err)
	}
	if err := reconcileOuterInstallJournal(registry); err != nil {
		t.Fatal(err)
	}
	if got := e2eReadFile(t, filepath.Join(target, "stable.txt")); got != "stable\n" {
		t.Fatalf("reconcile did not restore stable target: %q", got)
	}
	if got := e2eReadFile(t, registry); got != "stable registry\n" {
		t.Fatalf("reconcile did not restore stable registry: %q", got)
	}
	for _, stale := range []string{filepath.Join(target, "candidate.txt"), staged, transactionRoot, journalPath} {
		if _, err := os.Stat(stale); !os.IsNotExist(err) {
			t.Fatalf("reconcile left stale path %s: %v", stale, err)
		}
	}
	var receipt installRecoveryReceipt
	if err := json.Unmarshal([]byte(e2eReadFile(t, journalPath+".receipt.json")), &receipt); err != nil {
		t.Fatal(err)
	}
	if !receipt.Recovered || receipt.Operation != "install" || receipt.Phase != "switched" || receipt.Target != registry {
		t.Fatalf("recovery receipt does not describe the reconciled crash: %+v", receipt)
	}
}

func TestReleaseRollbackRestoresReleaseAndExecutable(t *testing.T) {
	source := e2eInstallSource(t)
	root := t.TempDir()
	project := filepath.Join(root, "project")
	release := filepath.Join(root, "releases", "candidate")
	binary := filepath.Join(root, "bin", nativeBinaryName())
	registry := filepath.Join(root, "registry", "registry.json")
	e2eWriteFile(t, filepath.Join(release, "old.txt"), "old release\n", 0o600)
	e2eWriteFile(t, binary, "old executable\n", 0o700)
	t.Setenv("FORMAL_GATES_INSTALL_FAULT", "post-switch-smoke")
	_, err := Install(InstallOptions{Source: source, Host: "claude", Scope: "project", Project: project, ReleaseRoot: release, BinaryTarget: binary, RegistryPath: registry, Force: true, SkipHooks: true})
	if err == nil || !strings.Contains(err.Error(), "post-switch-smoke") {
		t.Fatalf("release transaction did not fail at post-switch smoke: %v", err)
	}
	if got := e2eReadFile(t, filepath.Join(release, "old.txt")); got != "old release\n" {
		t.Fatalf("old release was not restored: %q", got)
	}
	if _, err := os.Stat(filepath.Join(release, "README.md")); !os.IsNotExist(err) {
		t.Fatalf("candidate release remained after rollback: %v", err)
	}
	if got := e2eReadFile(t, binary); got != "old executable\n" {
		t.Fatalf("old executable was not restored: %q", got)
	}
	var receipt installRecoveryReceipt
	if err := json.Unmarshal([]byte(e2eReadFile(t, installOuterJournalPath(registry)+".failure.json")), &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Operation != "install" || receipt.Target != registry || receipt.Phase != "switched" || !receipt.Recovered {
		t.Fatalf("release failure receipt is incomplete: %+v", receipt)
	}
}

func TestInstallValidatesManifestBeforeTargetWrite(t *testing.T) {
	source := e2eInstallSource(t)
	e2eWriteFile(t, filepath.Join(source, "formal-gates.manifest.json"), "{not-json\n", 0o600)
	project := t.TempDir()
	_, err := Install(InstallOptions{Source: source, Host: "claude", Scope: "project", Project: project, RegistryPath: filepath.Join(t.TempDir(), "registry.json"), Force: true, SkipHooks: true})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "manifest") {
		t.Fatalf("installer accepted a package with an invalid manifest: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(project, ".claude", "skills", "formal-gates")); !os.IsNotExist(statErr) {
		t.Fatalf("installer wrote the target before rejecting the manifest: %v", statErr)
	}
}

func TestRelativeReleaseRootCannotOverlapSource(t *testing.T) {
	source := e2eInstallSource(t)
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

func TestInstallReceiptBindsAllRuntimeRoots(t *testing.T) {
	source := e2eInstallSource(t)
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
	if err := json.Unmarshal([]byte(e2eReadFile(t, registry+".install.json")), &persisted); err != nil {
		t.Fatal(err)
	}
	if len(persisted.Targets) != 1 || persisted.Targets[0].CanonicalPaths["launcher"] != canonicalRegistryPath(launcher) || persisted.Targets[0].CanonicalPaths["registry"] != canonicalRegistryPath(registry) {
		t.Fatalf("persisted install receipt lost launcher/registry roots: %+v", persisted)
	}
}

func TestReleaseRunsInstalledBinarySmokeBeforeCommit(t *testing.T) {
	source := e2eInstallSource(t)
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
	t.Setenv("FAKE_SMOKE_MARKER", marker)
	t.Setenv("FAKE_SMOKE_JOURNAL", journal)
	writeExecutableFixture(t, binarySource, `package main

import (
	"encoding/json"
	"fmt"
	"os"
)

func main() {
	if len(os.Args) == 5 && os.Args[1] == "package" && os.Args[2] == "validate" && os.Args[3] == "--root" {
		root, err := os.Getwd()
		if err != nil || root != os.Args[4] {
			panic("incorrect package validation root")
		}
		return
	}
	if len(os.Args) != 2 || os.Args[1] != "--version" {
		panic("unexpected executable protocol")
	}

	marker := os.Getenv("FAKE_SMOKE_MARKER")
	journal := os.Getenv("FAKE_SMOKE_JOURNAL")
	data, err := os.ReadFile(journal)
	if err != nil {
		panic(err)
	}
	var state struct {
		Phase string
	}
	if err := json.Unmarshal(data, &state); err != nil {
		panic(err)
	}
	evidence := fmt.Sprintf("candidate-smoke-ran\nexec-path=%s\njournal-phase=%s\n", os.Args[0], state.Phase)
	if err := os.WriteFile(marker, []byte(evidence), 0o600); err != nil {
		panic(err)
	}
	os.Exit(23)
}
`)
	e2eWriteFile(t, filepath.Join(source, "candidate-only.txt"), "candidate\n", 0o600)
	e2eWriteFile(t, filepath.Join(release, "stable.txt"), "stable\n", 0o600)
	e2eWriteFile(t, binary, "stable executable\n", 0o700)

	_, err := Install(InstallOptions{Source: source, Host: "claude", Scope: "project", Project: filepath.Join(root, "project"), ReleaseRoot: release, BinaryTarget: binary, RegistryPath: registry, Force: true, SkipHooks: true})
	if err == nil {
		t.Fatalf("release transaction did not fail because the candidate installed-binary smoke exited non-zero")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "smoke") {
		t.Fatalf("release transaction returned a non-smoke error for the candidate failure: %v", err)
	}
	markerData := e2eReadFile(t, marker)
	if !strings.Contains(markerData, "candidate-smoke-ran") {
		t.Fatalf("candidate installed binary did not leave its execution marker: %q", markerData)
	}
	installedBinary := canonicalRegistryPath(filepath.Join(release, "bin", nativeBinaryName()))
	if !strings.Contains(markerData, "exec-path="+installedBinary) {
		t.Fatalf("smoke marker does not prove the installed binary path ran: %q", markerData)
	}
	if !strings.Contains(markerData, "journal-phase=switched") {
		t.Fatalf("candidate smoke did not observe the pre-commit switched phase: %q", markerData)
	}
	if got := e2eReadFile(t, filepath.Join(release, "stable.txt")); got != "stable\n" {
		t.Fatalf("failed smoke did not restore stable release: %q", got)
	}
	if got := e2eReadFile(t, binary); got != "stable executable\n" {
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
	if err := json.Unmarshal([]byte(e2eReadFile(t, failurePath)), &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Operation != "install" || receipt.Target != registry || receipt.Phase != "switched" || !receipt.Recovered || receipt.ObservedAt == "" {
		t.Fatalf("release smoke failure receipt does not describe the observed boundary and rollback: %+v", receipt)
	}
}

func TestBootstrapScriptsDelegateToNativeTransactionOwner(t *testing.T) {
	root := e2eRepoRoot(t)
	shell := e2eReadFile(t, filepath.Join(root, "install.command"))
	powershell := e2eReadFile(t, filepath.Join(root, "install.ps1"))
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
	if !strings.Contains(shell, `"$binary_target" install`) || !strings.Contains(shell, `"$source_root/bin/formal-gates" "${cmd[@]:1}"`) {
		t.Fatal("shell bootstrap omits stable maintenance or native release upgrade")
	}
	if !strings.Contains(powershell, `& $formalBinary @args`) || !strings.Contains(powershell, `& (Join-Path $sourceDir.FullName "bin\formal-gates.exe") @args`) {
		t.Fatal("PowerShell bootstrap omits stable maintenance or native release upgrade")
	}
	for _, forbidden := range []string{"launcher.before", "launcherBackup", "launcherWasLink"} {
		if strings.Contains(shell, forbidden) || strings.Contains(powershell, forbidden) {
			t.Fatalf("bootstrap retains launcher backup/replacement ownership: %q", forbidden)
		}
	}
}

func TestCurrentAuthorityInventoryAndPublicDocs(t *testing.T) {
	root := e2eRepoRoot(t)
	inventory := e2eReadFile(t, filepath.Join(root, "references", "requirements-precedence.md"))
	for _, required := range []string{"current-authority", "orchestration-pipeline-engine/master-requirements.md", "final-implementation-draft.md", "adr-001-typed-authoring-compiled-canonical-definitions.md", "adr-002-development-batching-and-verification-tiers.md", "references/formal-flow.md", "references/install-and-hooks.md", "references/qa-operations.md"} {
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
		content := e2eReadFile(t, filepath.Join(root, filepath.FromSlash(rel)))
		for _, retiredPublicSurface := range []string{"workflow prepare-action", "workflow prepare-gate", "workflow record-action", "workflow record-gate", "workflow qa-worktree", "workflow cleanup", "workflow resume --adopt-external"} {
			if strings.Contains(strings.ToLower(content), retiredPublicSurface) {
				t.Fatalf("current document %s exposes retired public surface %q", rel, retiredPublicSurface)
			}
		}
	}
}
