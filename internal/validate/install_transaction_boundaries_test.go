//go:build qastructure

package validate

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

func installWriteFile(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
}

func installReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func installRun(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	command := exec.Command(name, args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

func installRepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok || !filepath.IsAbs(file) {
		t.Fatal("could not locate the whitebox test source as an absolute path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func installRawRecordByID(t *testing.T, data []byte, id string) []byte {
	t.Helper()
	var envelope struct {
		Records []json.RawMessage `json:"records"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	for _, raw := range envelope.Records {
		var record RegistryRecord
		if err := json.Unmarshal(raw, &record); err != nil {
			t.Fatal(err)
		}
		if record.ID == id {
			return append([]byte(nil), raw...)
		}
	}
	t.Fatalf("registry record %q was not found in raw document", id)
	return nil
}

func installGitProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	return installGitProjectAt(t, root)
}

func installGitProjectAt(t *testing.T, root string) string {
	t.Helper()
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	installWriteFile(t, filepath.Join(root, "requirements.md"), []byte("sample requirement\n"), 0o600)
	installRun(t, root, "git", "init")
	installRun(t, root, "git", "config", "user.email", "install-fixture@example.invalid")
	installRun(t, root, "git", "config", "user.name", "Install Boundaries Test")
	installRun(t, root, "git", "add", "requirements.md")
	installRun(t, root, "git", "commit", "-m", "baseline")
	return root
}

func installFixtureSource(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	skill := "---\nname: formal-gates\n---\n" + hostInstructionsStartMarker + "\ninstall fixture rule\n" + hostInstructionsEndMarker + "\n"
	installWriteFile(t, filepath.Join(root, "SKILL.md"), []byte(skill), 0o600)
	installWriteFile(t, filepath.Join(root, "README.md"), []byte("install fixture runtime\n"), 0o600)
	installWriteFile(t, filepath.Join(root, "README_EN.md"), []byte("install fixture runtime\n"), 0o600)
	installWriteFile(t, filepath.Join(root, "formal-gates.manifest.json"), []byte(hostTargetTestManifest), 0o600)
	installWriteFile(t, filepath.Join(root, "bin", nativeBinaryName()), []byte("#!/bin/sh\nexit 0\n"), 0o700)
	for _, relative := range []string{"agents/agent.md", "prompts/action.md", "gates/gate.md", "references/reference.md"} {
		installWriteFile(t, filepath.Join(root, filepath.FromSlash(relative)), []byte(relative+"\n"), 0o600)
	}
	for _, name := range []string{"native_dsh.mjs", "zcode_credential.mjs"} {
		copyValidateTestFile(t, repoRootValidateTest(t), root, "runtime/langgraph/formal_gates_harness/"+name)
	}
	return root
}

func installRecord(id, target, launcher, scope, host, projectRoot, status string) RegistryRecord {
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

func installWriteRegistry(t *testing.T, path string, epoch uint64, records ...RegistryRecord) []byte {
	t.Helper()
	document, err := json.MarshalIndent(RegistryDocument{SchemaVersion: RegistrySchemaVersion, Epoch: epoch, Records: records}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data := append(document, '\n')
	installWriteFile(t, path, data, 0o600)
	return data
}

func installRecordByID(t *testing.T, document RegistryDocument, id string) RegistryRecord {
	t.Helper()
	for _, record := range document.Records {
		if record.ID == id {
			return record
		}
	}
	t.Fatalf("registry record %q was not found", id)
	return RegistryRecord{}
}

func installPathWithin(parent, path string) bool {
	parent = canonicalRegistryPath(parent)
	path = canonicalRegistryPath(path)
	relative, err := filepath.Rel(parent, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

// installAssertNoTransactionArtifacts 验证事务结束（提交或回滚）后没有
// 残留。.formal-gates-install.lock 例外：它是内核文件锁的持久工件（释放
// ＝关闭描述符，锁文件永不删除），属于运行期输出而非事务状态，因此显式
// 豁免该文件，改验“留下的确为普通锁文件，且事务已释放它”。
func installAssertNoTransactionArtifacts(t *testing.T, registry, target string) {
	t.Helper()
	for _, pattern := range []string{
		filepath.Join(filepath.Dir(registry), ".formal-gates-transaction-*"),
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
	if info, err := os.Lstat(installLockPath(registry)); err == nil && !info.Mode().IsRegular() {
		t.Fatalf("install lock artifact is not a regular file: %s", installLockPath(registry))
	}
	if release, err := acquireInstallLock(registry); err != nil {
		t.Fatalf("finished transaction did not release the install lock: %v", err)
	} else {
		release()
	}
}

func TestPackageReceiptBindsStableAndInstalledIdentities(t *testing.T) {
	source := installFixtureSource(t)
	installed := installFixtureSource(t)
	installWriteFile(t, filepath.Join(installed, "README.md"), []byte("installed runtime only\n"), 0o600)
	installRun(t, source, "git", "init")
	installRun(t, source, "git", "config", "user.email", "install-package@example.invalid")
	installRun(t, source, "git", "config", "user.name", "Install Boundaries Test")
	installRun(t, source, "git", "add", ".")
	installRun(t, source, "git", "commit", "-m", "package identity")
	vcsIdentity := installRun(t, source, "git", "rev-parse", "HEAD")
	hostConfig := filepath.Join(t.TempDir(), "host", "config.json")
	installWriteFile(t, hostConfig, []byte("{}\n"), 0o600)
	unauthorized := filepath.Join(t.TempDir(), "outside", "sentinel")
	installWriteFile(t, unauthorized, []byte("must remain\n"), 0o600)

	first, err := PackageReceipt(source, installed)
	if err != nil {
		t.Fatal(err)
	}
	second, err := PackageReceipt(source, installed)
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest == "" || !reflect.DeepEqual(first, second) {
		t.Fatalf("package receipt was not deterministic: first=%+v second=%+v", first, second)
	}
	installedReceipt, err := PackageReceipt(installed, source)
	if err != nil {
		t.Fatal(err)
	}
	if installedReceipt.Digest == first.Digest || reflect.DeepEqual(installedReceipt.Entries, first.Entries) {
		t.Fatalf("source and installed fixtures did not establish distinct package identity: source=%+v installed=%+v", first, installedReceipt)
	}
	for _, entry := range first.Entries {
		expected := filepath.Join(source, filepath.FromSlash(entry.Path))
		if !installPathWithin(source, entry.RealPath) || entry.RealPath != canonicalRegistryPath(expected) {
			t.Fatalf("package entry did not resolve within source identity: entry=%+v expected=%s", entry, canonicalRegistryPath(expected))
		}
	}
	for _, entry := range installedReceipt.Entries {
		expected := filepath.Join(installed, filepath.FromSlash(entry.Path))
		if !installPathWithin(installed, entry.RealPath) || entry.RealPath != canonicalRegistryPath(expected) {
			t.Fatalf("installed package entry did not resolve within installed identity: entry=%+v expected=%s", entry, canonicalRegistryPath(expected))
		}
	}
	baseline, err := BuildBaselineReceipt("git:"+vcsIdentity, source, installed, map[string]string{"hostConfig": hostConfig})
	if err != nil {
		t.Fatal(err)
	}
	if baseline.VCSIdentity != "git:"+vcsIdentity || baseline.SourceRoot != canonicalRegistryPath(source) || baseline.InstalledTarget != canonicalRegistryPath(installed) || baseline.PackageDigest != first.Digest || baseline.InstalledTargetDigest != installedReceipt.Digest {
		t.Fatalf("baseline did not bind package identities: %+v", baseline)
	}
	if baseline.CanonicalPaths["hostConfig"] != canonicalRegistryPath(hostConfig) || len(baseline.PackageManifest) != len(first.Entries) {
		t.Fatalf("baseline lost canonical or manifest identity: %+v", baseline)
	}
	if got := installReadFile(t, unauthorized); string(got) != "must remain\n" {
		t.Fatalf("package receipt touched an unauthorized namespace: %q", got)
	}
	if _, err := os.Stat(filepath.Join(source, ".gates")); !os.IsNotExist(err) {
		t.Fatalf("package receipt created a workflow namespace: %v", err)
	}
}

func TestBaselineReceiptValidatesManifestPathsAndIdentityBoundaries(t *testing.T) {
	source := installFixtureSource(t)
	installed := installFixtureSource(t)
	stateRoot := t.TempDir()
	resourceRoot := t.TempDir()
	registry := filepath.Join(t.TempDir(), "registry.json")
	installWriteFile(t, registry, []byte("registry\n"), 0o600)
	hostConfig := filepath.Join(t.TempDir(), "host.json")
	installWriteFile(t, hostConfig, []byte("{}\n"), 0o600)

	receipt, err := BuildBaselineReceipt("git:install", source, installed, map[string]string{
		"hostConfig": hostConfig, "stateRoot": stateRoot, "resourceRoot": resourceRoot, "registry": registry,
	})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.InstalledTarget != canonicalRegistryPath(installed) || receipt.InstalledTargetDigest == "" || receipt.PackageDigest == "" {
		t.Fatalf("installed identity is incomplete: %+v", receipt)
	}
	for _, entry := range receipt.PackageManifest {
		if !filepath.IsAbs(entry.RealPath) || !installPathWithin(source, entry.RealPath) || entry.Size < 0 || entry.SHA256 == "" {
			t.Fatalf("manifest entry lacks realpath/Lstat-like identity: %+v", entry)
		}
		path := filepath.Join(source, filepath.FromSlash(entry.Path))
		info, statErr := os.Lstat(path)
		if statErr != nil {
			t.Fatal(statErr)
		}
		actualDigest, digestErr := fileDigest(path)
		if digestErr != nil {
			t.Fatal(digestErr)
		}
		if entry.Size != info.Size() || entry.SHA256 != actualDigest || entry.RealPath != canonicalRegistryPath(path) || filepath.Clean(entry.RealPath) != canonicalRegistryPath(path) {
			t.Fatalf("manifest entry is not faithful to its source file: entry=%+v size=%d digest=%s realpath=%s", entry, info.Size(), actualDigest, canonicalRegistryPath(path))
		}
	}
	for name, want := range map[string]string{"sourceRoot": source, "installedTarget": installed, "hostConfig": hostConfig, "stateRoot": stateRoot, "resourceRoot": resourceRoot, "registry": registry} {
		if got := receipt.CanonicalPaths[name]; got != canonicalRegistryPath(want) {
			t.Fatalf("canonical path %s is not equal to its realpath: got=%q want=%q", name, got, canonicalRegistryPath(want))
		}
	}
	if len(receipt.Disjoint) != 1 || receipt.Disjoint[canonicalRegistryPath(source)] != canonicalRegistryPath(source) {
		t.Fatalf("installed identity did not retain the complete source disjoint proof: %+v", receipt.Disjoint)
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
			if _, err := BuildBaselineReceipt("git:install", test.root, test.path, nil); err == nil || !strings.Contains(err.Error(), "overlaps") {
				t.Fatalf("overlapping identity was accepted: %v", err)
			}
		})
	}
	for name, invalid := range map[string]BaselineReceipt{
		"missing VCS":     func() BaselineReceipt { value := receipt; value.VCSIdentity = ""; return value }(),
		"missing package": func() BaselineReceipt { value := receipt; value.PackageDigest = ""; return value }(),
	} {
		t.Run(name, func(t *testing.T) {
			target := filepath.Join(t.TempDir(), "nested", "invalid.json")
			if err := WriteBaselineReceipt(target, invalid); err == nil {
				t.Fatal("incomplete identity was persisted")
			}
			if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
				t.Fatalf("incomplete identity created a receipt before rejection: %v", statErr)
			}
		})
	}
	for name, path := range map[string]string{"auxiliary root mixed with installed target": installed, "auxiliary root mixed with source root": source} {
		t.Run(name, func(t *testing.T) {
			if _, err := BuildBaselineReceipt("git:install", source, installed, map[string]string{"stateRoot": path}); err == nil || !strings.Contains(err.Error(), "overlaps") {
				t.Fatalf("mixed source/installed identity was accepted: %v", err)
			}
		})
	}
}

func TestRegistryOwnerPreservesRecordsAcrossInstallAndUninstall(t *testing.T) {
	source := installFixtureSource(t)
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
	claudeBefore := installRecordByID(t, firstDocument, firstDocument.Records[0].ID)
	claudeTarget, err := resolveInstallTargets("claude", "project", project)
	if err != nil {
		t.Fatal(err)
	}
	claudeDigest, err := PackageDigest(claudeTarget[0].targetPath)
	if err != nil {
		t.Fatal(err)
	}
	claudeHook := installReadFile(t, claudeTarget[0].hookConfig)
	claudeRule := installReadFile(t, claudeTarget[0].managedRulePath)
	claudeRawBeforeSecond := installRawRecordByID(t, installReadFile(t, registry), claudeBefore.ID)

	if _, err := Install(InstallOptions{Source: source, Host: "codex", Scope: "project", Project: project, RegistryPath: registry, BinaryTarget: launcher, Force: true}); err != nil {
		t.Fatal(err)
	}
	secondDocument, err := LoadRegistry(registry)
	if err != nil {
		t.Fatal(err)
	}
	if len(secondDocument.Records) != 2 {
		t.Fatalf("second host install changed the registry to an unexpected record set: %+v", secondDocument.Records)
	}
	claudeAfter := installRecordByID(t, secondDocument, claudeBefore.ID)
	if !reflect.DeepEqual(claudeBefore, claudeAfter) {
		t.Fatalf("install of a second host changed the existing record: before=%+v after=%+v", claudeBefore, claudeAfter)
	}
	if secondDocument.Epoch <= firstDocument.Epoch {
		t.Fatalf("registry epoch did not advance monotonically: before=%d after=%d", firstDocument.Epoch, secondDocument.Epoch)
	}
	if got, err := PackageDigest(claudeTarget[0].targetPath); err != nil || got != claudeDigest {
		t.Fatalf("second install changed the existing runtime: before=%s after=%s err=%v", claudeDigest, got, err)
	}
	if got := installRawRecordByID(t, installReadFile(t, registry), claudeBefore.ID); !bytes.Equal(got, claudeRawBeforeSecond) {
		t.Fatalf("second host install changed the original record bytes: before=%q after=%q", claudeRawBeforeSecond, got)
	}
	if !bytes.Equal(installReadFile(t, claudeTarget[0].hookConfig), claudeHook) || !bytes.Equal(installReadFile(t, claudeTarget[0].managedRulePath), claudeRule) {
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
	if len(thirdDocument.Records) != 2 {
		t.Fatalf("uninstall changed registry record cardinality: got=%d records=%+v", len(thirdDocument.Records), thirdDocument.Records)
	}
	if thirdDocument.Epoch <= secondDocument.Epoch {
		t.Fatalf("uninstall registry epoch regressed: before=%d after=%d", secondDocument.Epoch, thirdDocument.Epoch)
	}
	if got := installRawRecordByID(t, installReadFile(t, registry), claudeBefore.ID); !bytes.Equal(got, claudeRawBeforeSecond) {
		t.Fatalf("uninstall changed the unrelated record bytes: before=%q after=%q", claudeRawBeforeSecond, got)
	}
	if got := installRecordByID(t, thirdDocument, claudeBefore.ID); !reflect.DeepEqual(got, claudeBefore) {
		t.Fatalf("uninstall changed the unrelated record: before=%+v after=%+v", claudeBefore, got)
	}
	codexAfter := installRecordByID(t, thirdDocument, codexID)
	codexWant := installRecordByID(t, secondDocument, codexID)
	codexWant.Status = "disabled"
	if !reflect.DeepEqual(codexAfter, codexWant) {
		t.Fatalf("uninstall changed non-status fields of the installing record: before=%+v after=%+v", codexWant, codexAfter)
	}
}

func TestBootstrapReceiptBindsExactRegistryIdentity(t *testing.T) {
	source := installFixtureSource(t)
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
	installWriteFile(t, launcher, installReadFile(t, filepath.Join(source, "bin", nativeBinaryName())), 0o700)
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
	if err := json.Unmarshal(installReadFile(t, report.BootstrapReceiptPath), &receipt); err != nil {
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

func TestAdmissionRejectsInvalidRecordsBeforeNamespaceWrites(t *testing.T) {
	packageRoot := installRepoRoot(t)
	cases := []struct {
		name   string
		record func(root string) RegistryRecord
		id     string
	}{
		{"incomplete", func(root string) RegistryRecord { return RegistryRecord{ID: "incomplete", Status: "active"} }, "incomplete"},
		{"conflicting canonical target", func(root string) RegistryRecord {
			record := installRecord("conflict", packageRoot, filepath.Join(root, "launcher", nativeBinaryName()), "project", "codex", root, "active")
			record.CanonicalPaths["target"] = canonicalRegistryPath(filepath.Join(root, "other-package"))
			return record
		}, "conflict"},
		{"missing", func(root string) RegistryRecord {
			return installRecord("present", packageRoot, filepath.Join(root, "launcher", nativeBinaryName()), "project", "codex", root, "active")
		}, "missing"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			root := installGitProject(t)
			registry := filepath.Join(t.TempDir(), "registry.json")
			record := test.record(root)
			before := installWriteRegistry(t, registry, 7, record)
			if test.name == "missing" {
				test.id = "absent"
			}
			runID := "install-" + test.id
			_, err := Start(StartOptions{Root: root, PackageRoot: packageRoot, RunID: runID, Flow: "formal", RequirementSource: "requirements.md", VCS: "git", Split: "no", AdmissionRegistry: registry, AdmissionRecordID: test.id})
			if err == nil || !strings.Contains(err.Error(), "UNREGISTERED_INSTALL") {
				t.Fatalf("invalid admission was accepted: %v", err)
			}
			if got := installReadFile(t, registry); !bytes.Equal(got, before) {
				t.Fatalf("authoritative registry bytes changed on rejected admission: before=%q after=%q", before, got)
			}
			if _, err := os.Stat(RunDir(root, runID)); !os.IsNotExist(err) {
				t.Fatalf("rejected admission created a run directory: %v", err)
			}
			// 判据是“无业务命名空间残留”，不是“.gates 不存在”：Start 在
			// 准入校验前就持有内核 run 锁，.gates/run-locks/<run>.lock 是
			// 运行期工件（内核文件锁永不删除），不是命名空间状态。逐项枚
			// 举 .gates 下的残留，除该锁工件外的任何条目都说明被拒准入
			// 写入了业务命名空间。
			residue := installWorkflowNamespaceResidue(t, root)
			for _, entry := range residue {
				if entry != ".gates/run-locks" && entry != ".gates/run-locks/"+runID+".lock" {
					t.Fatalf("rejected admission created a workflow namespace entry %q (residue: %v)", entry, residue)
				}
			}
		})
	}
}

// installWorkflowNamespaceResidue 枚举 root/.gates 下的全部条目（相对
// root 的 slash 路径，含目录与文件，不含 .gates 自身）；.gates 不存在时
// 返回空。
func installWorkflowNamespaceResidue(t *testing.T, root string) []string {
	t.Helper()
	var residue []string
	namespaceRoot := filepath.Join(root, ".gates")
	err := filepath.WalkDir(namespaceRoot, func(path string, dirEntry fs.DirEntry, walkErr error) error {
		if os.IsNotExist(walkErr) {
			return nil
		}
		if walkErr != nil {
			return walkErr
		}
		if path == namespaceRoot {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		residue = append(residue, filepath.ToSlash(relative))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return residue
}

func TestWorkflowStateUsesNestedProjectBinding(t *testing.T) {
	packageRoot := installRepoRoot(t)
	parent := t.TempDir()
	nested := filepath.Join(parent, "nested")
	installGitProjectAt(t, nested)
	registry := filepath.Join(t.TempDir(), "registry.json")
	launcher := filepath.Join(t.TempDir(), "launcher", nativeBinaryName())
	parentRecord := installRecord("parent", packageRoot, launcher, "project", "codex", parent, "active")
	nestedRecord := installRecord("nested", packageRoot, launcher, "project", "codex", nested, "active")
	installWriteRegistry(t, registry, 12, parentRecord, nestedRecord)
	writeTestBootstrapReceipt(t, registry)
	if _, err := Start(StartOptions{Root: nested, PackageRoot: packageRoot, RunID: "install-parent", Flow: "formal", RequirementSource: "requirements.md", VCS: "git", Split: "no", AdmissionRegistry: registry, AdmissionRecordID: parentRecord.ID}); err == nil || !strings.Contains(err.Error(), "workflow root") {
		t.Fatalf("less-specific project binding was accepted for nested root: %v", err)
	}
	state, err := Start(StartOptions{Root: nested, PackageRoot: packageRoot, RunID: "install-nested", Flow: "formal", RequirementSource: "requirements.md", VCS: "git", Split: "no", AdmissionRegistry: registry, AdmissionRecordID: nestedRecord.ID})
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

func TestGlobalAdmissionDerivesProjectLocalRoots(t *testing.T) {
	packageRoot := installRepoRoot(t)
	hostRoot := t.TempDir()
	project := installGitProject(t)
	otherProject := t.TempDir()
	registry := filepath.Join(t.TempDir(), "registry.json")
	launcher := filepath.Join(hostRoot, "bin", nativeBinaryName())
	global := installRecord("global", packageRoot, launcher, "global", "codex", hostRoot, "active")
	installWriteRegistry(t, registry, 20, global)
	writeTestBootstrapReceipt(t, registry)
	state, err := Start(StartOptions{Root: project, PackageRoot: packageRoot, RunID: "install-global", Flow: "formal", RequirementSource: "requirements.md", VCS: "git", Split: "no", AdmissionRegistry: registry, AdmissionRecordID: global.ID})
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
	derived := installRecordByID(t, document, loaded.AdmissionRecordID)
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

func TestLegacyLifecyclePreservesExistingStateAndNamespaces(t *testing.T) {
	root := installGitProject(t)
	packageRoot := installRepoRoot(t)
	resourceSentinel := filepath.Join(t.TempDir(), "resources", "sentinel")
	registrySentinel := filepath.Join(t.TempDir(), "registry.json")
	installWriteFile(t, resourceSentinel, []byte("resource\n"), 0o600)
	installWriteFile(t, registrySentinel, []byte("registry\n"), 0o600)
	state, err := Start(StartOptions{Root: root, PackageRoot: packageRoot, RunID: "install-legacy", Flow: "formal", RequirementSource: "requirements.md", VCS: "git", Split: "no"})
	if err != nil {
		t.Fatal(err)
	}
	statePath := RunStatePath(root, state.RunID)
	beforeState := installReadFile(t, statePath)
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
	if got := installReadFile(t, statePath); !bytes.Equal(got, beforeState) || !bytes.Equal(installReadFile(t, resourceSentinel), []byte("resource\n")) || !bytes.Equal(installReadFile(t, registrySentinel), []byte("registry\n")) {
		t.Fatal("legacy show/resume/diagnose changed existing state or unrelated namespaces")
	}
}

func TestInstallAndUninstallUseTheSameLockOwner(t *testing.T) {
	source := installFixtureSource(t)
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
	// 内核文件锁永不删除：释放判据是锁可被再次获取，而非锁文件消失。
	if info, statErr := os.Lstat(lockPath); statErr == nil && !info.Mode().IsRegular() {
		t.Fatalf("install lock artifact is not a regular file: %s", lockPath)
	}
	if relock, relockErr := acquireInstallLock(registry); relockErr != nil {
		t.Fatalf("released install lock is still held: %v", relockErr)
	} else {
		relock()
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
	registryBefore := installReadFile(t, registry)
	unlock, err = acquireInstallLock(registry)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Uninstall(UninstallOptions{Host: "claude", Scope: "project", Project: project, RegistryPath: registry}); err == nil || !strings.Contains(err.Error(), "lock is held") {
		t.Fatalf("installed uninstall did not honor the shared lock: %v", err)
	}
	unlock()
	if got, err := PackageDigest(targets[0].targetPath); err != nil || got != beforeDigest || !bytes.Equal(installReadFile(t, registry), registryBefore) {
		t.Fatalf("busy uninstall changed transaction identity: digest=%s err=%v", got, err)
	}
	if _, err := Uninstall(UninstallOptions{Host: "claude", Scope: "project", Project: project, RegistryPath: registry}); err != nil {
		t.Fatal(err)
	}
	// 同上：完成的卸载事务释放共享锁，锁文件按内核锁语义持久保留。
	if relock, relockErr := acquireInstallLock(registry); relockErr != nil {
		t.Fatalf("completed uninstall left the shared lock held: %v", relockErr)
	} else {
		relock()
	}
}

func TestInstallFaultsRestoreRegistryIdentityAndCleanup(t *testing.T) {
	faults := []struct {
		name  string
		phase string
	}{
		{"journal-boundary", "intent"},
		{"intent", "intent"},
		{"registry", "intent"},
		{"prepared", "prepared"},
		{"switched", "switched"},
		{"post-switch-smoke", "switched"},
		{"pointer", "smoke-passed"},
		{"hook", "smoke-passed"},
		{"managed-rule", "smoke-passed"},
		{"registry-commit", "smoke-passed"},
		{"copy-component:prompts", "intent"},
		{"verify-stage:installed-target", "switched"},
	}
	for _, fault := range faults {
		t.Run(fault.name, func(t *testing.T) {
			source := installFixtureSource(t)
			project := t.TempDir()
			registry := filepath.Join(t.TempDir(), "registry.json")
			launcher := filepath.Join(t.TempDir(), "launcher", nativeBinaryName())
			registryBefore := installWriteRegistry(t, registry, 4)
			targets, err := resolveInstallTargets("claude", "project", project)
			if err != nil {
				t.Fatal(err)
			}
			installWriteFile(t, filepath.Join(targets[0].targetPath, "old.txt"), []byte("old runtime\n"), 0o600)
			installWriteFile(t, targets[0].hookConfig, []byte(`{"hooks":{"External":[{"command":"keep"}]}}`+"\n"), 0o600)
			installWriteFile(t, targets[0].managedRulePath, []byte("external rule\n"), 0o600)
			installWriteFile(t, launcher, []byte("old launcher\n"), 0o700)
			t.Setenv("FORMAL_GATES_INSTALL_FAULT", fault.name)
			_, err = Install(InstallOptions{Source: source, Host: "claude", Scope: "project", Project: project, RegistryPath: registry, BinaryTarget: launcher, Force: true})
			if err == nil || !strings.Contains(err.Error(), "deterministic install fault") {
				t.Fatalf("fault %q did not stop installation: %v", fault.name, err)
			}
			if got := installReadFile(t, filepath.Join(targets[0].targetPath, "old.txt")); string(got) != "old runtime\n" || string(installReadFile(t, targets[0].hookConfig)) != `{"hooks":{"External":[{"command":"keep"}]}}`+"\n" || string(installReadFile(t, targets[0].managedRulePath)) != "external rule\n" || string(installReadFile(t, launcher)) != "old launcher\n" {
				t.Fatal("install fault did not restore old runtime/configuration identity")
			}
			if got := installReadFile(t, registry); !bytes.Equal(got, registryBefore) {
				t.Fatalf("install fault changed authoritative registry: before=%q after=%q", registryBefore, got)
			}
			var receipt installRecoveryReceipt
			if err := json.Unmarshal(installReadFile(t, installOuterJournalPath(registry)+".failure.json"), &receipt); err != nil {
				t.Fatal(err)
			}
			if receipt.Operation != "install" || receipt.Target != registry || receipt.Phase != fault.phase {
				t.Fatalf("recovery receipt does not identify the interrupted operation: fault=%s receipt=%+v", fault.name, receipt)
			}
			expectedFact := "deterministic install fault injected at " + fault.name
			if receipt.ObservedFact != expectedFact || receipt.Reconcile != "restore all target, release, binary, hook, hook-state, managed-rule and registry snapshots" {
				t.Fatalf("recovery receipt does not bind observation and reconciliation: fault=%s receipt=%+v", fault.name, receipt)
			}
			if observedAt, err := time.Parse(time.RFC3339Nano, receipt.ObservedAt); err != nil || observedAt.IsZero() {
				t.Fatalf("recovery receipt has invalid observation timestamp %q: %v", receipt.ObservedAt, err)
			}
			if !receipt.Recovered || receipt.Outcome != "ROLLED_BACK" || receipt.VCSIdentity == "" || receipt.PackageDigest == "" || receipt.InstalledTarget == "" || receipt.Generation == 0 || receipt.Lease == "" || receipt.Token == "" || receipt.RecoveryReceipt == "" || receipt.StableDigest == "" {
				t.Fatalf("recovery identity is incomplete: %+v", receipt)
			}
			installAssertNoTransactionArtifacts(t, registry, targets[0].targetPath)
		})
	}
}

func TestMalformedHookRestoresRegistryAndRecoveryEvidence(t *testing.T) {
	source := installFixtureSource(t)
	project := t.TempDir()
	registry := filepath.Join(t.TempDir(), "registry.json")
	launcher := filepath.Join(t.TempDir(), "launcher", nativeBinaryName())
	registryBefore := installWriteRegistry(t, registry, 5)
	targets, err := resolveInstallTargets("claude", "project", project)
	if err != nil {
		t.Fatal(err)
	}
	hookBefore := []byte("{malformed hook\n")
	ruleBefore := []byte("external rule\n")
	installWriteFile(t, filepath.Join(targets[0].targetPath, "old.txt"), []byte("old runtime\n"), 0o600)
	installWriteFile(t, targets[0].hookConfig, hookBefore, 0o600)
	installWriteFile(t, targets[0].managedRulePath, ruleBefore, 0o600)
	installWriteFile(t, launcher, []byte("old launcher\n"), 0o700)
	if _, err := Install(InstallOptions{Source: source, Host: "claude", Scope: "project", Project: project, RegistryPath: registry, BinaryTarget: launcher, Force: true}); err == nil || !strings.Contains(strings.ToLower(err.Error()), "json") {
		t.Fatalf("malformed hook did not stop install: %v", err)
	}
	if string(installReadFile(t, filepath.Join(targets[0].targetPath, "old.txt"))) != "old runtime\n" || !bytes.Equal(installReadFile(t, targets[0].hookConfig), hookBefore) || !bytes.Equal(installReadFile(t, targets[0].managedRulePath), ruleBefore) || !bytes.Equal(installReadFile(t, registry), registryBefore) || string(installReadFile(t, launcher)) != "old launcher\n" {
		t.Fatal("malformed hook rollback did not preserve all authoritative bytes")
	}
	var receipt installRecoveryReceipt
	if err := json.Unmarshal(installReadFile(t, installOuterJournalPath(registry)+".failure.json"), &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Phase != "smoke-passed" || !receipt.Recovered || receipt.Outcome != "ROLLED_BACK" || receipt.VCSIdentity == "" || receipt.PackageDigest == "" || receipt.Generation == 0 || receipt.Lease == "" || receipt.Token == "" || receipt.StableDigest == "" || receipt.RecoveryReceipt == "" {
		t.Fatalf("malformed-hook recovery evidence is incomplete: %+v", receipt)
	}
	installAssertNoTransactionArtifacts(t, registry, targets[0].targetPath)
}

func TestUninstallFaultsReconcileAndPreserveExternalContent(t *testing.T) {
	t.Run("install post-switch smoke recovery", func(t *testing.T) {
		source := installFixtureSource(t)
		project := t.TempDir()
		registry := filepath.Join(t.TempDir(), "registry.json")
		launcher := filepath.Join(t.TempDir(), "launcher", nativeBinaryName())
		registryBefore := installWriteRegistry(t, registry, 2)
		targets, err := resolveInstallTargets("claude", "project", project)
		if err != nil {
			t.Fatal(err)
		}
		installWriteFile(t, filepath.Join(targets[0].targetPath, "stable.txt"), []byte("stable\n"), 0o600)
		installWriteFile(t, launcher, []byte("stable launcher\n"), 0o700)
		t.Setenv("FORMAL_GATES_INSTALL_FAULT", "post-switch-smoke")
		if _, err := Install(InstallOptions{Source: source, Host: "claude", Scope: "project", Project: project, RegistryPath: registry, BinaryTarget: launcher, Force: true}); err == nil {
			t.Fatal("post-switch smoke fault unexpectedly succeeded")
		}
		if string(installReadFile(t, filepath.Join(targets[0].targetPath, "stable.txt"))) != "stable\n" || string(installReadFile(t, launcher)) != "stable launcher\n" || !bytes.Equal(installReadFile(t, registry), registryBefore) {
			t.Fatal("post-switch smoke recovery did not preserve the stable identity")
		}
		var receipt installRecoveryReceipt
		if err := json.Unmarshal(installReadFile(t, installOuterJournalPath(registry)+".failure.json"), &receipt); err != nil {
			t.Fatal(err)
		}
		if receipt.Operation != "install" || receipt.Phase != "switched" || !receipt.Recovered || receipt.Outcome != "ROLLED_BACK" || receipt.RecoveryReceipt == "" || receipt.StableDigest == "" {
			t.Fatalf("post-switch install recovery evidence is incomplete: %+v", receipt)
		}
		installAssertNoTransactionArtifacts(t, registry, targets[0].targetPath)
	})

	for _, fault := range []string{"intent", "switched", "post-switch-smoke", "managed-rule", "hook"} {
		t.Run("uninstall "+fault, func(t *testing.T) {
			source := installFixtureSource(t)
			project := t.TempDir()
			registry := filepath.Join(t.TempDir(), "registry.json")
			launcher := filepath.Join(t.TempDir(), "launcher", nativeBinaryName())
			targets, err := resolveInstallTargets("claude", "project", project)
			if err != nil {
				t.Fatal(err)
			}
			externalHook := []byte(`{"hooks":{"External":[{"command":"keep"}]}}` + "\n")
			externalRule := []byte("external rule\n")
			installWriteFile(t, targets[0].hookConfig, externalHook, 0o600)
			installWriteFile(t, targets[0].managedRulePath, externalRule, 0o600)
			if _, err := Install(InstallOptions{Source: source, Host: "claude", Scope: "project", Project: project, RegistryPath: registry, BinaryTarget: launcher, Force: true}); err != nil {
				t.Fatal(err)
			}
			registryBefore := installReadFile(t, registry)
			hookBefore := installReadFile(t, targets[0].hookConfig)
			ruleBefore := installReadFile(t, targets[0].managedRulePath)
			t.Setenv("FORMAL_GATES_INSTALL_FAULT", fault)
			if _, err := Uninstall(UninstallOptions{Host: "claude", Scope: "project", Project: project, RegistryPath: registry}); err == nil || !strings.Contains(err.Error(), "deterministic install fault") {
				t.Fatalf("uninstall fault %q did not stop the transaction: %v", fault, err)
			}
			if _, err := os.Stat(filepath.Join(targets[0].targetPath, "README.md")); err != nil {
				t.Fatalf("uninstall rollback did not restore runtime: %v", err)
			}
			if !bytes.Equal(installReadFile(t, targets[0].hookConfig), hookBefore) || !bytes.Equal(installReadFile(t, targets[0].managedRulePath), ruleBefore) || !bytes.Equal(installReadFile(t, registry), registryBefore) {
				t.Fatal("uninstall rollback did not preserve external/configuration bytes")
			}
			var receipt installRecoveryReceipt
			if err := json.Unmarshal(installReadFile(t, installOuterJournalPath(registry)+".failure.json"), &receipt); err != nil {
				t.Fatal(err)
			}
			if receipt.Operation != "uninstall" || !receipt.Recovered || receipt.Outcome != "ROLLED_BACK" || receipt.Generation == 0 || receipt.Lease == "" || receipt.Token == "" || receipt.RecoveryReceipt == "" || receipt.StableDigest == "" {
				t.Fatalf("uninstall recovery evidence is incomplete: %+v", receipt)
			}
			installAssertNoTransactionArtifacts(t, registry, targets[0].targetPath)
		})
	}
}

func TestSupportedRestartReconcilesCrashJournalAutomatically(t *testing.T) {
	source := installFixtureSource(t)
	project := t.TempDir()
	root := t.TempDir()
	target := filepath.Join(project, ".claude", "skills", "formal-gates")
	registry := filepath.Join(root, "registry.json")
	launcher := filepath.Join(root, "launcher", nativeBinaryName())
	registryBefore := installWriteRegistry(t, registry, 3)
	installWriteFile(t, filepath.Join(target, "stable.txt"), []byte("stable\n"), 0o600)
	installWriteFile(t, registry, registryBefore, 0o600)
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
	installWriteFile(t, filepath.Join(target, "candidate.txt"), []byte("candidate\n"), 0o600)
	staged := filepath.Join(project, ".formal-gates-stage-crashed")
	installWriteFile(t, staged, []byte("staged\n"), 0o600)
	installWriteFile(t, registry, []byte(`{"schemaVersion":1,"epoch":4,"records":[]}`+"\n"), 0o600)
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
	installWriteFile(t, installLockPath(registry), []byte("pid=999999999\n"), 0o600)
	t.Setenv("FORMAL_GATES_INSTALL_FAULT", "intent")
	if _, err := Install(InstallOptions{Source: source, Host: "claude", Scope: "project", Project: project, RegistryPath: registry, BinaryTarget: launcher, Force: true}); err == nil || !strings.Contains(err.Error(), "deterministic install fault") {
		t.Fatalf("supported restart did not reach the new transaction after reconcile: %v", err)
	}
	if string(installReadFile(t, filepath.Join(target, "stable.txt"))) != "stable\n" || !bytes.Equal(installReadFile(t, registry), registryBefore) {
		t.Fatal("automatic restart reconcile did not restore stable bytes")
	}
	var recovered installRecoveryReceipt
	if err := json.Unmarshal(installReadFile(t, journalPath+".receipt.json"), &recovered); err != nil {
		t.Fatal(err)
	}
	if !recovered.Recovered || recovered.Outcome != "RECOVERED" || recovered.Phase != "switched" || recovered.Generation != journal.Generation || recovered.Lease != journal.Lease || recovered.Token != journal.Token || recovered.StableDigest == "" {
		t.Fatalf("automatic reconcile receipt is incomplete: %+v", recovered)
	}
	if _, err := os.Stat(staged); !os.IsNotExist(err) {
		t.Fatalf("crashed staged path remains after supported restart: %v", err)
	}
	installAssertNoTransactionArtifacts(t, registry, target)
}

func TestReleaseRollbackRestoresAllOuterIdentity(t *testing.T) {
	source := installFixtureSource(t)
	root := t.TempDir()
	project := filepath.Join(root, "project")
	release := filepath.Join(root, "release")
	launcher := filepath.Join(root, "stable", nativeBinaryName())
	registry := filepath.Join(root, "registry", "registry.json")
	targets, err := resolveInstallTargets("claude", "project", project)
	if err != nil {
		t.Fatal(err)
	}
	registryBefore := installWriteRegistry(t, registry, 6)
	installWriteFile(t, filepath.Join(release, "old-release.txt"), []byte("old release\n"), 0o600)
	installWriteFile(t, launcher, []byte("old launcher\n"), 0o700)
	installWriteFile(t, filepath.Join(targets[0].targetPath, "old.txt"), []byte("old runtime\n"), 0o600)
	hookBefore := []byte(`{"hooks":{"External":[{"command":"keep"}]}}` + "\n")
	ruleBefore := []byte("external rule\n")
	installWriteFile(t, targets[0].hookConfig, hookBefore, 0o600)
	installWriteFile(t, targets[0].managedRulePath, ruleBefore, 0o600)
	t.Setenv("FORMAL_GATES_INSTALL_FAULT", "registry-commit")
	if _, err := Install(InstallOptions{Source: source, Host: "claude", Scope: "project", Project: project, ReleaseRoot: release, BinaryTarget: launcher, RegistryPath: registry, Force: true}); err == nil || !strings.Contains(err.Error(), "deterministic install fault") {
		t.Fatalf("release transaction did not fail at registry commit: %v", err)
	}
	if string(installReadFile(t, filepath.Join(release, "old-release.txt"))) != "old release\n" || string(installReadFile(t, launcher)) != "old launcher\n" || string(installReadFile(t, filepath.Join(targets[0].targetPath, "old.txt"))) != "old runtime\n" || !bytes.Equal(installReadFile(t, targets[0].hookConfig), hookBefore) || !bytes.Equal(installReadFile(t, targets[0].managedRulePath), ruleBefore) || !bytes.Equal(installReadFile(t, registry), registryBefore) {
		t.Fatal("release rollback did not restore the complete outer identity")
	}
	if _, err := os.Stat(filepath.Join(release, "old-release.txt")); err != nil {
		t.Fatalf("pre-existing release was not restored: %v", err)
	}
	for _, candidatePath := range []string{"SKILL.md", "README.md", "README_EN.md", "formal-gates.manifest.json", "bin", "agents", "prompts", "gates", "references", "definitions"} {
		if _, err := os.Stat(filepath.Join(release, candidatePath)); !os.IsNotExist(err) {
			t.Fatalf("candidate release content remained at %s: %v", candidatePath, err)
		}
	}
	var receipt installRecoveryReceipt
	if err := json.Unmarshal(installReadFile(t, installOuterJournalPath(registry)+".failure.json"), &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Phase != "smoke-passed" || !receipt.Recovered || receipt.Outcome != "ROLLED_BACK" || receipt.VCSIdentity == "" || receipt.PackageDigest == "" || receipt.Generation == 0 || receipt.Lease == "" || receipt.Token == "" || receipt.StableDigest == "" || receipt.RecoveryReceipt == "" {
		t.Fatalf("release rollback receipt is incomplete: %+v", receipt)
	}
	installAssertNoTransactionArtifacts(t, registry, targets[0].targetPath)
}

func TestManifestRejectsBeforeAnyTargetOrReceiptWrite(t *testing.T) {
	source := installFixtureSource(t)
	installWriteFile(t, filepath.Join(source, "formal-gates.manifest.json"), []byte("{invalid manifest\n"), 0o600)
	project := t.TempDir()
	registry := filepath.Join(t.TempDir(), "registry.json")
	launcher := filepath.Join(t.TempDir(), "launcher", nativeBinaryName())
	registryBefore := installWriteRegistry(t, registry, 8)
	targets, err := resolveInstallTargets("claude", "project", project)
	if err != nil {
		t.Fatal(err)
	}
	installWriteFile(t, filepath.Join(targets[0].targetPath, "old.txt"), []byte("old\n"), 0o600)
	installWriteFile(t, launcher, []byte("old launcher\n"), 0o700)
	hookBefore := []byte(`{"hooks":{"External":[{"command":"keep"}]}}` + "\n")
	ruleBefore := []byte("external rule\n")
	installWriteFile(t, targets[0].hookConfig, hookBefore, 0o600)
	installWriteFile(t, targets[0].managedRulePath, ruleBefore, 0o600)
	receiptPath := registry + ".install.json"
	receiptBefore := []byte("old receipt\n")
	installWriteFile(t, receiptPath, receiptBefore, 0o600)
	if _, err := Install(InstallOptions{Source: source, Host: "claude", Scope: "project", Project: project, RegistryPath: registry, BinaryTarget: launcher, Force: true}); err == nil || !strings.Contains(strings.ToLower(err.Error()), "manifest") {
		t.Fatalf("invalid manifest was accepted: %v", err)
	}
	if string(installReadFile(t, filepath.Join(targets[0].targetPath, "old.txt"))) != "old\n" || string(installReadFile(t, launcher)) != "old launcher\n" || !bytes.Equal(installReadFile(t, targets[0].hookConfig), hookBefore) || !bytes.Equal(installReadFile(t, targets[0].managedRulePath), ruleBefore) || !bytes.Equal(installReadFile(t, registry), registryBefore) || !bytes.Equal(installReadFile(t, receiptPath), receiptBefore) {
		t.Fatal("manifest rejection changed an existing install namespace")
	}
	installAssertNoTransactionArtifacts(t, registry, targets[0].targetPath)
}

func TestRelativeReleaseOverlapRejectsWithoutNamespaceWrites(t *testing.T) {
	source := installFixtureSource(t)
	releaseInsideSource := filepath.Join(source, "nested-release")
	workingDirectory := t.TempDir()
	useTestWorkingDirectory(t, workingDirectory)
	relativeRelease, err := filepath.Rel(workingDirectory, releaseInsideSource)
	if err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	registry := filepath.Join(t.TempDir(), "registry.json")
	launcher := filepath.Join(t.TempDir(), "launcher", nativeBinaryName())
	registryBefore := installWriteRegistry(t, registry, 9)
	targets, err := resolveInstallTargets("claude", "project", project)
	if err != nil {
		t.Fatal(err)
	}
	installWriteFile(t, filepath.Join(targets[0].targetPath, "old.txt"), []byte("old\n"), 0o600)
	installWriteFile(t, launcher, []byte("old launcher\n"), 0o700)
	if _, err := Install(InstallOptions{Source: source, Host: "claude", Scope: "project", Project: project, ReleaseRoot: relativeRelease, BinaryTarget: launcher, RegistryPath: registry, Force: true}); err == nil || !strings.Contains(err.Error(), "overlaps release root") {
		t.Fatalf("relative source/release overlap was accepted: %v", err)
	}
	if _, err := os.Stat(releaseInsideSource); !os.IsNotExist(err) {
		t.Fatalf("overlap rejection wrote inside source: %v", err)
	}
	if string(installReadFile(t, filepath.Join(targets[0].targetPath, "old.txt"))) != "old\n" || string(installReadFile(t, launcher)) != "old launcher\n" || !bytes.Equal(installReadFile(t, registry), registryBefore) {
		t.Fatal("overlap rejection changed target or registry namespace")
	}
	installAssertNoTransactionArtifacts(t, registry, targets[0].targetPath)
}

func installCreateSourceArchive(t *testing.T, path string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	entries := map[string]string{
		"formal-gates-install/SKILL.md":                   "skill\n",
		"formal-gates-install/README.md":                  "readme\n",
		"formal-gates-install/README_EN.md":               "readme\n",
		"formal-gates-install/formal-gates.manifest.json": hostTargetTestManifest,
		"formal-gates-install/agents/agent.md":            "agent\n",
		"formal-gates-install/prompts/action.md":          "prompt\n",
		"formal-gates-install/gates/gate.md":              "gate\n",
		"formal-gates-install/references/reference.md":    "reference\n",
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

func TestBootstrapWrappersForwardOnlyToStableOwner(t *testing.T) {
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
	root := installRepoRoot(t)
	script := filepath.Join(root, "install.command")
	help := installRun(t, root, "bash", script, "--help")
	for _, flag := range []string{"--version", "--host", "--scope", "--project", "--force", "--skip-hooks"} {
		if !strings.Contains(help, flag) {
			t.Fatalf("wrapper help omits %s: %s", flag, help)
		}
	}
	fixture := t.TempDir()
	sourceZip := filepath.Join(fixture, "source.zip")
	installCreateSourceArchive(t, sourceZip)
	binaryAsset := filepath.Join(fixture, "formal-gates-"+suffix)
	logPath := filepath.Join(fixture, "owner.log")
	installWriteFile(t, binaryAsset, []byte("#!/bin/sh\nset -eu\nprintf '%s %s\\n' \"$0\" \"$*\" >> \"$INSTALL_OWNER_LOG\"\nif [ \"$1\" = install ] && [ \"${2:-}\" != --bootstrap ]; then\n  previous=\"\"\n  target=\"\"\n  for argument in \"$@\"; do\n    if [ \"$previous\" = --binary-target ]; then target=\"$argument\"; fi\n    previous=\"$argument\"\n  done\n  mkdir -p \"$(dirname \"$target\")\"\n  if [ \"$0\" != \"$target\" ]; then\n    cp \"$0\" \"$target\"\n    chmod +x \"$target\"\n  fi\nfi\nexit 0\n"), 0o700)
	canary := filepath.Join(fixture, "portable-canary-"+suffix+".json")
	checksums := filepath.Join(fixture, "SHA256SUMS-"+suffix+".txt")
	installWriteFile(t, canary, []byte("{}\n"), 0o600)
	installWriteFile(t, checksums, []byte("fixture\n"), 0o600)
	binDir := filepath.Join(fixture, "bin")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	curl := filepath.Join(binDir, "curl")
	sha256sum := filepath.Join(binDir, "sha256sum")
	installWriteFile(t, curl, []byte(`#!/bin/sh
set -eu
output=""
url=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = "-o" ]; then output="$2"; shift 2; else url="$1"; shift; fi
done
case "$url" in
  *zipball*) cp "$INSTALL_SOURCE_ZIP" "$output" ;;
  *portable-canary*) cp "$INSTALL_CANARY" "$output" ;;
  *SHA256SUMS*) cp "$INSTALL_CHECKSUMS" "$output" ;;
  *formal-gates-*) cp "$INSTALL_BINARY" "$output" ;;
  *) exit 1 ;;
esac
`), 0o700)
	installWriteFile(t, sha256sum, []byte("#!/bin/sh\n[ \"$1\" = \"-c\" ]\n"), 0o700)
	home := filepath.Join(fixture, "home")
	project := filepath.Join(fixture, "project")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("bash", script, "--host", "codex", "--scope", "project", "--project", project, "--force", "--skip-hooks")
	command.Dir = root
	command.Env = append(os.Environ(),
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"HOME="+home, "USERPROFILE="+home, "FORMAL_GATES_VERSION=vinstall",
		"INSTALL_SOURCE_ZIP="+sourceZip, "INSTALL_BINARY="+binaryAsset, "INSTALL_CANARY="+canary,
		"INSTALL_CHECKSUMS="+checksums, "INSTALL_OWNER_LOG="+logPath,
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("bootstrap wrapper failed: %v\n%s", err, output)
	}
	lines := strings.Split(strings.TrimSpace(string(installReadFile(t, logPath))), "\n")
	if len(lines) != 2 {
		t.Fatalf("wrapper did not make exactly install and bootstrap owner calls: %q", lines)
	}
	launcher := filepath.Join(home, ".local", "bin", "formal-gates")
	release := filepath.Join(home, ".formal-gates", "releases", "install-"+suffix)
	if info, err := os.Lstat(launcher); err != nil || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("wrapper did not leave a regular stable launcher: info=%+v err=%v", info, err)
	}
	if !strings.Contains(lines[0], "install --source") || !strings.Contains(lines[0], "--release-root "+release) || !strings.Contains(lines[0], "--binary-target "+launcher) || !strings.Contains(lines[0], "--host codex --scope project --project "+project+" --force --skip-hooks") {
		t.Fatalf("install arguments were not forwarded to the stable owner: %q", lines[0])
	}
	if !strings.HasPrefix(lines[0], launcher+" ") || strings.Contains(lines[0], "--candidate-binary") {
		t.Fatalf("first install was not executed by the stable launcher: %q", lines[0])
	}
	if !strings.Contains(lines[1], "install --bootstrap --source "+release) || !strings.Contains(lines[1], "--binary-target "+launcher) || !strings.Contains(lines[1], "--host codex --scope project --project "+project+" --skip-hooks") {
		t.Fatalf("bootstrap arguments were not forwarded to the stable owner: %q", lines[1])
	}
	if !strings.HasPrefix(lines[1], launcher+" ") || strings.Contains(lines[1], "--candidate-binary") {
		t.Fatalf("bootstrap was not executed by the stable launcher: %q", lines[1])
	}
	powershell := string(installReadFile(t, filepath.Join(root, "install.ps1")))
	unixWrapper := string(installReadFile(t, script))
	batch := string(installReadFile(t, filepath.Join(root, "install.bat")))
	for _, required := range []string{"--release-root", "--binary-target", "install", "--bootstrap", "& $formalBinary @args", "& $formalBinary @bootstrapArgs"} {
		if !strings.Contains(powershell, required) {
			t.Fatalf("PowerShell wrapper omits native-owner delegation %q", required)
		}
	}
	if strings.Contains(unixWrapper, "--candidate-binary") || strings.Contains(powershell, "--candidate-binary") {
		t.Fatal("wrappers retain the removed candidate writer flag")
	}
	oldStable := []byte("#!/bin/sh\nset -eu\nprintf 'old %s %s\\n' \"$0\" \"$*\" >> \"$INSTALL_OWNER_LOG\"\n")
	installWriteFile(t, launcher, oldStable, 0o700)
	upgrade := exec.Command("bash", script, "--host", "codex", "--scope", "project", "--project", project, "--force", "--skip-hooks")
	upgrade.Dir = root
	upgrade.Env = append(os.Environ(),
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"HOME="+home, "USERPROFILE="+home, "FORMAL_GATES_VERSION=vinstall-upgrade",
		"INSTALL_SOURCE_ZIP="+sourceZip, "INSTALL_BINARY="+binaryAsset, "INSTALL_CANARY="+canary,
		"INSTALL_CHECKSUMS="+checksums, "INSTALL_OWNER_LOG="+logPath,
	)
	if output, err := upgrade.CombinedOutput(); err != nil {
		t.Fatalf("stable upgrade wrapper failed: %v\n%s", err, output)
	}
	lines = strings.Split(strings.TrimSpace(string(installReadFile(t, logPath))), "\n")
	if len(lines) != 4 || !strings.HasPrefix(lines[2], "old ") || !strings.HasPrefix(lines[3], "old ") {
		t.Fatalf("existing stable launcher was overwritten before native ownership: %q", lines)
	}
	if got := string(installReadFile(t, launcher)); got != string(oldStable) {
		t.Fatalf("existing stable launcher changed during wrapper preflight: %q", got)
	}
	if !strings.Contains(batch, "install.ps1") || strings.Contains(powershell, "SymbolicLink") || strings.Contains(powershell, "Remove-Item $installRoot") {
		t.Fatal("Windows wrappers retain a second pointer-mutating transaction")
	}
}

func TestInstalledInventoryAndStableHelpStayWithinStageZero(t *testing.T) {
	root := installRepoRoot(t)
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
	inventory := string(installReadFile(t, filepath.Join(root, "references", "requirements-precedence.md")))
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
		"P2-REPAIR-AND-COMMENT-CLEANUP-REQUIREMENT.md", "QA-WORKTREE-ISOLATION-AND-INCREMENTAL-REQUIREMENT.md", "TRIGGER-MODEL-EXPLICIT-REQUEST-REQUIREMENT.md", "TRIGGER-MODEL-DEFAULT-REMINDER-REQUIREMENT.md",
	} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(path))); err != nil {
			t.Fatalf("inventory path is not present in installed tree: %s: %v", path, err)
		}
	}
	stableDocFiles := []string{filepath.Join(root, "SKILL.md"), filepath.Join(root, "README.md"), filepath.Join(root, "README_EN.md")}
	for _, pattern := range []string{filepath.Join(root, "prompts", "*.md"), filepath.Join(root, "prompts", "actions", "*.md")} {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		stableDocFiles = append(stableDocFiles, matches...)
	}
	futureCommandToken := regexp.MustCompile(`(?i)(^|[^[:alnum:]_-])(drive|submit)([^[:alnum:]_-]|$)`)
	// Stable documents must not mention the candidate-only drive/submit
	// commands; the candidate CLI intentionally exposes them in its help.
	for _, path := range stableDocFiles {
		content := string(installReadFile(t, path))
		if token := futureCommandToken.FindString(content); token != "" {
			t.Fatalf("stable document %s leaks future command token %q", path, token)
		}
	}
	docFiles := append([]string{}, stableDocFiles...)
	referenceFiles, err := filepath.Glob(filepath.Join(root, "references", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	docFiles = append(docFiles, filepath.Join(root, "references", "requirements-precedence.md"))
	docFiles = append(docFiles, referenceFiles...)
	for _, path := range docFiles {
		content := strings.ToLower(string(installReadFile(t, path)))
		for _, forbidden := range []string{"workflow drive", "workflow submit", "drive/submit", "shadow"} {
			if strings.Contains(content, forbidden) {
				t.Fatalf("stable document %s leaks future public surface %q", path, forbidden)
			}
		}
	}
	var help strings.Builder
	for _, surface := range []struct {
		args   []string
		marker string
	}{
		{[]string{"--help"}, "commands:"},
		{[]string{"package", "--help"}, "package validate"},
		{[]string{"package", "validate", "--help"}, "package validate"},
		{[]string{"package", "route-candidates", "--help"}, "package route-candidates"},
		{[]string{"package", "baseline", "--help"}, "package baseline"},
		{[]string{"workflow", "--help"}, "subcommands:"},
		{[]string{"workflow", "start", "--help"}, "workflow start"},
		{[]string{"workflow", "show", "--help"}, "workflow show"},
		{[]string{"workflow", "diagnose", "--help"}, "workflow diagnose"},
		{[]string{"workflow", "resume", "--help"}, "workflow resume"},
		{[]string{"workflow", "abort", "--help"}, "workflow abort"},
		{[]string{"workflow", "reset", "--help"}, "workflow reset"},
		{[]string{"workflow", "requirement", "--help"}, "workflow requirement"},
		{[]string{"workflow", "route-candidates", "--help"}, "workflow route-candidates"},
		{[]string{"workflow", "route", "--help"}, "workflow route"},
		{[]string{"workflow", "route-add", "--help"}, "workflow route-add"},
		{[]string{"workflow", "slicing", "--help"}, "workflow slicing"},
		{[]string{"workflow", "settle-findings", "--help"}, "workflow settle-findings"},
		{[]string{"workflow", "qa-worktree", "--help"}, "workflow qa-worktree"},
		{[]string{"workflow", "prepare-gate", "--help"}, "workflow prepare-gate"},
		{[]string{"workflow", "prepare-action", "--help"}, "workflow prepare-action"},
		{[]string{"workflow", "claim-dispatch", "--help"}, "workflow claim-dispatch"},
		{[]string{"workflow", "record-action", "--help"}, "workflow record-action"},
		{[]string{"workflow", "record-gate", "--help"}, "workflow record-gate"},
		{[]string{"workflow", "qa-design", "--help"}, "workflow qa-design"},
		{[]string{"workflow", "qa-review", "--help"}, "workflow qa-review"},
		{[]string{"workflow", "qa-execution", "--help"}, "workflow qa-execution"},
		{[]string{"workflow", "qa-execution-scope", "--help"}, "workflow qa-execution-scope"},
		{[]string{"workflow", "snapshot", "--help"}, "workflow snapshot"},
		{[]string{"workflow", "cleanup", "--help"}, "workflow cleanup"},
		{[]string{"workflow", "candidate", "--help"}, "workflow candidate"},
		{[]string{"workflow", "candidate", "generate", "--help"}, "workflow candidate generate"},
		{[]string{"workflow", "candidate", "view", "--help"}, "workflow candidate view"},
		{[]string{"workflow", "carry", "--help"}, "workflow carry"},
		{[]string{"workflow", "authorize-repair", "--help"}, "workflow authorize-repair"},
		{[]string{"workflow", "seal", "--help"}, "workflow seal"},
		{[]string{"registry", "admit", "--help"}, "registry admit"},
		{[]string{"registry", "show", "--help"}, "registry show"},
		{[]string{"registry", "--help"}, "subcommands:"},
		{[]string{"install", "--help"}, "usage of install"},
		{[]string{"uninstall", "--help"}, "usage of uninstall"},
		{[]string{"gate", "--help"}, "subcommands:"},
		{[]string{"gate", "run", "--help"}, "gate run"},
		{[]string{"gate", "report", "--help"}, "gate report"},
		{[]string{"hook", "--help"}, "hook decide"},
		{[]string{"hook", "decide", "--help"}, "hook decide"},
		{[]string{"lifecycle", "--help"}, "capture"},
		{[]string{"lifecycle", "capture", "--help"}, "lifecycle capture"},
		{[]string{"lifecycle", "verify", "--help"}, "lifecycle verify"},
		{[]string{"canary", "--help"}, "subcommands:"},
		{[]string{"canary", "portable", "--help"}, "canary portable"},
		{[]string{"canary", "fault-matrix", "--help"}, "canary fault-matrix"},
		{[]string{"canary", "codex-hook", "--help"}, "canary codex-hook"},
		{[]string{"canary", "codex-hook-probe", "--help"}, "canary codex-hook-probe"},
	} {
		output := installRun(t, root, "go", append([]string{"run", "./cmd/formal-gates"}, surface.args...)...)
		if !strings.Contains(strings.ToLower(output), strings.ToLower(surface.marker)) {
			t.Fatalf("help surface %q omitted %q: %s", strings.Join(surface.args, " "), surface.marker, output)
		}
		help.WriteString(output)
		help.WriteByte('\n')
	}
	helpText := strings.ToLower(help.String())
	// The installed candidate exposes workflow drive/submit as public
	// commands, so they are not forbidden tokens in CLI help.
	for _, forbidden := range []string{"shadow"} {
		if strings.Contains(helpText, forbidden) {
			t.Fatalf("public help leaks future surface %q: %s", forbidden, helpText)
		}
	}
}

var _ = fmt.Sprintf
