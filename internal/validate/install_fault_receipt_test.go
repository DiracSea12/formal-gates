//go:build qastructure

package validate

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// These tests cover install-fault recovery receipts and the installed
// package's stable surface. They use fresh fixtures and do not call another QA
// test function.

func faultWriteFile(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
}

func faultReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func faultRun(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	command := exec.Command(name, args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

func faultRepoRoot(t *testing.T) string {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok || !filepath.IsAbs(sourceFile) {
		t.Fatal("could not locate the whitebox test source as an absolute path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "..", ".."))
}

// faultCopyPackage 按生产的包输入口径把源仓库复制成无 .git 的包夹具：
// Git 是输入集的唯一权威——tracked 文件（即使命中 ignore 规则）与未跟踪
// 且未被 ignore 的文件都是包输入；生产 PackageReceipt 无条件排除的本地
// 运行根 .git/.gates/.harness 同样排除。逐条目复制并要求常规文件，夹具
// 因此不会携带符号链接等执行器工件。
func faultCopyPackage(t *testing.T, source, target string) {
	t.Helper()
	command := exec.Command("git", "-C", source, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	out, err := command.Output()
	if err != nil {
		t.Fatalf("%s: %v", strings.Join(command.Args, " "), err)
	}
	for _, relative := range strings.Split(string(out), "\x00") {
		if relative == "" {
			continue
		}
		if root, _, _ := strings.Cut(relative, "/"); root == ".git" || root == ".gates" || root == ".harness" {
			continue
		}
		sourcePath := filepath.Join(source, filepath.FromSlash(relative))
		info, err := os.Lstat(sourcePath)
		if err != nil {
			t.Fatal(err)
		}
		if !info.Mode().IsRegular() {
			t.Fatalf("package fixture entry is not a regular file: %s", relative)
		}
		data, err := os.ReadFile(sourcePath)
		if err != nil {
			t.Fatal(err)
		}
		destination := filepath.Join(target, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(destination, data, info.Mode().Perm()); err != nil {
			t.Fatal(err)
		}
	}
}

func faultPathWithin(parent, path string) bool {
	parent, path = canonicalRegistryPath(parent), canonicalRegistryPath(path)
	relative, err := filepath.Rel(parent, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

func faultInstallSource(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	skill := "---\nname: formal-gates\n---\n" + hostInstructionsStartMarker + "\nfault fixture rule\n" + hostInstructionsEndMarker + "\n"
	faultWriteFile(t, filepath.Join(root, "SKILL.md"), []byte(skill), 0o600)
	faultWriteFile(t, filepath.Join(root, "README.md"), []byte("fault fixture runtime\n"), 0o600)
	faultWriteFile(t, filepath.Join(root, "README_EN.md"), []byte("fault fixture runtime\n"), 0o600)
	faultWriteFile(t, filepath.Join(root, "formal-gates.manifest.json"), []byte(hostTargetTestManifest), 0o600)
	faultWriteFile(t, filepath.Join(root, "bin", nativeBinaryName()), []byte("#!/bin/sh\nexit 0\n"), 0o700)
	for _, relative := range []string{"agents/agent.md", "prompts/action.md", "gates/gate.md", "references/reference.md"} {
		faultWriteFile(t, filepath.Join(root, filepath.FromSlash(relative)), []byte(relative+"\n"), 0o600)
	}
	for _, name := range []string{"native_dsh.mjs", "zcode_credential.mjs"} {
		copyValidateTestFile(t, repoRootValidateTest(t), root, "runtime/langgraph/formal_gates_harness/"+name)
	}
	return root
}

// faultAssertNoInstallResidue 验证故障回滚后没有安装残留。
// .formal-gates-install.lock 例外：它是内核文件锁的持久工件（释放＝关
// 闭描述符，锁文件永不删除），属于运行期输出而非事务状态，因此显式豁免
// 该文件，改验“留下的确为普通锁文件，且事务已释放它”。
func faultAssertNoInstallResidue(t *testing.T, registry, target string) {
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
			t.Fatalf("install residue matched %s: %v", pattern, matches)
		}
	}
	if _, err := os.Stat(installOuterJournalPath(registry)); !os.IsNotExist(err) {
		t.Fatalf("install journal remains: %v", err)
	}
	if info, err := os.Lstat(installLockPath(registry)); err == nil && !info.Mode().IsRegular() {
		t.Fatalf("install lock artifact is not a regular file: %s", installLockPath(registry))
	}
	if release, err := acquireInstallLock(registry); err != nil {
		t.Fatalf("interrupted install did not release the install lock: %v", err)
	} else {
		release()
	}
}

func TestInstallFaultMatrixRecoveryReceiptIdentity(t *testing.T) {
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
	const reconcileAction = "restore all target, release, binary, hook, hook-state, managed-rule and registry snapshots"

	for _, fault := range faults {
		t.Run(fault.name, func(t *testing.T) {
			source := faultInstallSource(t)
			project := t.TempDir()
			registry := filepath.Join(t.TempDir(), "registry.json")
			launcher := filepath.Join(t.TempDir(), "launcher", nativeBinaryName())
			registryBefore := []byte(`{"schemaVersion":1,"epoch":4,"records":[]}` + "\n")
			faultWriteFile(t, registry, registryBefore, 0o600)
			targets, err := resolveInstallTargets("claude", "project", project)
			if err != nil {
				t.Fatal(err)
			}
			faultWriteFile(t, filepath.Join(targets[0].targetPath, "old.txt"), []byte("old runtime\n"), 0o600)
			faultWriteFile(t, targets[0].hookConfig, []byte(`{"hooks":{"External":[{"command":"keep"}]}}`+"\n"), 0o600)
			faultWriteFile(t, targets[0].managedRulePath, []byte("external rule\n"), 0o600)
			faultWriteFile(t, launcher, []byte("old launcher\n"), 0o700)

			t.Setenv("FORMAL_GATES_INSTALL_FAULT", fault.name)
			if _, err := Install(InstallOptions{Source: source, Host: "claude", Scope: "project", Project: project, RegistryPath: registry, BinaryTarget: launcher, Force: true}); err == nil || !strings.Contains(err.Error(), "deterministic install fault") {
				t.Fatalf("fault %q did not stop installation: %v", fault.name, err)
			}

			if string(faultReadFile(t, filepath.Join(targets[0].targetPath, "old.txt"))) != "old runtime\n" || string(faultReadFile(t, targets[0].hookConfig)) != `{"hooks":{"External":[{"command":"keep"}]}}`+"\n" || string(faultReadFile(t, targets[0].managedRulePath)) != "external rule\n" || string(faultReadFile(t, launcher)) != "old launcher\n" {
				t.Fatal("install fault did not restore the stable runtime and configuration")
			}
			if got := faultReadFile(t, registry); string(got) != string(registryBefore) {
				t.Fatalf("install fault changed the authoritative registry: before=%q after=%q", registryBefore, got)
			}

			var receipt installRecoveryReceipt
			failurePath := installOuterJournalPath(registry) + ".failure.json"
			if err := json.Unmarshal(faultReadFile(t, failurePath), &receipt); err != nil {
				t.Fatal(err)
			}
			if receipt.Operation != "install" {
				t.Fatalf("recovery receipt has wrong operation: %q", receipt.Operation)
			}
			if receipt.Target != registry {
				t.Fatalf("recovery receipt has wrong target: got=%q want=%q", receipt.Target, registry)
			}
			if receipt.Phase != fault.phase {
				t.Fatalf("recovery receipt has wrong interrupted phase for %s: got=%q want=%q", fault.name, receipt.Phase, fault.phase)
			}
			if receipt.ObservedFact != "deterministic install fault injected at "+fault.name {
				t.Fatalf("recovery receipt has wrong observed fact: %q", receipt.ObservedFact)
			}
			if receipt.Reconcile != reconcileAction {
				t.Fatalf("recovery receipt has wrong reconciliation action: %q", receipt.Reconcile)
			}
			if observedAt, err := time.Parse(time.RFC3339Nano, receipt.ObservedAt); err != nil || observedAt.IsZero() {
				t.Fatalf("recovery receipt has invalid observation timestamp %q: %v", receipt.ObservedAt, err)
			}
			if !receipt.Recovered || receipt.Outcome != "ROLLED_BACK" || receipt.StableDigest == "" || receipt.VCSIdentity == "" || receipt.PackageDigest == "" || receipt.InstalledTarget == "" || receipt.Generation == 0 || receipt.Lease == "" || receipt.Token == "" || receipt.RecoveryReceipt != failurePath {
				t.Fatalf("recovery receipt has incomplete stable identity: %+v", receipt)
			}
			faultAssertNoInstallResidue(t, registry, targets[0].targetPath)
		})
	}
}

func TestInstalledPackageInventoryAndHelp(t *testing.T) {
	root := faultRepoRoot(t)
	packageRoot := t.TempDir()
	faultCopyPackage(t, root, packageRoot)
	installedBinary := filepath.Join(packageRoot, "bin", nativeBinaryName())
	faultRun(t, root, "go", "build", "-o", installedBinary, "./cmd/formal-gates")

	if result := Package(packageRoot); !result.OK() {
		t.Fatalf("built package inventory is invalid: %#v", result.Failures)
	}
	var packageManifest manifest
	if err := json.Unmarshal(faultReadFile(t, filepath.Join(packageRoot, "formal-gates.manifest.json")), &packageManifest); err != nil {
		t.Fatal(err)
	}
	for _, part := range packageManifest.Parts {
		path := filepath.Join(packageRoot, filepath.FromSlash(strings.TrimSuffix(part, "/")))
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("manifest package part is absent from built package: %s: %v", part, err)
		}
	}

	project := t.TempDir()
	registry := filepath.Join(t.TempDir(), "registry.json")
	launcher := filepath.Join(t.TempDir(), "launcher", nativeBinaryName())
	report, err := Install(InstallOptions{
		Source:       packageRoot,
		Host:         "claude",
		Scope:        "project",
		Project:      project,
		RegistryPath: registry,
		BinaryTarget: launcher,
		Force:        true,
		SkipHooks:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Targets) != 1 {
		t.Fatalf("install report has unexpected target count: %+v", report)
	}
	target := filepath.FromSlash(report.Targets[0].TargetPath)
	installedReceipt, err := PackageReceipt(target, packageRoot)
	if err != nil {
		t.Fatal(err)
	}
	if installedReceipt.Digest != report.Targets[0].InstalledDigest || report.Targets[0].InstalledLstat != pathLstat(target) {
		t.Fatalf("installed target identity is not bound to the installed tree: report=%+v receipt=%+v", report.Targets[0], installedReceipt)
	}
	for _, entry := range installRuntimeEntries {
		path := filepath.Join(target, filepath.FromSlash(entry))
		if _, err := os.Lstat(path); err != nil {
			t.Fatalf("installed runtime inventory omits %s: %v", entry, err)
		}
	}
	for _, entry := range installedReceipt.Entries {
		if !faultPathWithin(target, entry.RealPath) {
			t.Fatalf("installed inventory entry escaped the installed target: %+v", entry)
		}
	}
	if got := faultReadFile(t, filepath.Join(target, "formal-gates.manifest.json")); string(got) != string(faultReadFile(t, filepath.Join(packageRoot, "formal-gates.manifest.json"))) {
		t.Fatal("installed manifest differs from the built package manifest")
	}

	helpCases := []struct {
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
		{[]string{"workflow", "drive", "--help"}, "workflow drive"},
		{[]string{"workflow", "submit", "--help"}, "workflow submit"},
		{[]string{"workflow", "status", "--help"}, "workflow status"},
		{[]string{"workflow", "next", "--help"}, "workflow next"},
		{[]string{"workflow", "guard-preview", "--help"}, "workflow guard-preview"},
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
	}
	executionDir := t.TempDir()
	for _, helpCase := range helpCases {
		output := faultRun(t, executionDir, filepath.Join(target, "bin", nativeBinaryName()), helpCase.args...)
		if !strings.Contains(strings.ToLower(output), strings.ToLower(helpCase.marker)) {
			t.Fatalf("help %q omitted %q: %s", strings.Join(helpCase.args, " "), helpCase.marker, output)
		}
	}
}
