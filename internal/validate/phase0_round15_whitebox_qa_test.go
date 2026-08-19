package validate

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

// These tests independently cover the two gaps from the previous white-box
// review. They use fresh fixtures and do not call another QA test function.

func round15WriteFile(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
}

func round15ReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func round15Run(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	command := exec.Command(name, args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

func round15RepoRoot(t *testing.T) string {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok || !filepath.IsAbs(sourceFile) {
		t.Fatal("could not locate the whitebox test source as an absolute path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "..", ".."))
}

func round15CopyPackage(t *testing.T, source, target string) {
	t.Helper()
	err := filepath.Walk(source, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return os.MkdirAll(target, 0o700)
		}
		if info.IsDir() && (relative == ".git" || relative == ".gates" || relative == "$tmp") {
			return filepath.SkipDir
		}
		destination := filepath.Join(target, relative)
		if info.IsDir() {
			return os.MkdirAll(destination, info.Mode().Perm())
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("package fixture entry is not a regular file: %s", relative)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			return err
		}
		return os.WriteFile(destination, data, info.Mode().Perm())
	})
	if err != nil {
		t.Fatal(err)
	}
}

func round15PathWithin(parent, path string) bool {
	parent, path = canonicalRegistryPath(parent), canonicalRegistryPath(path)
	relative, err := filepath.Rel(parent, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

func round15InstallSource(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	skill := "---\nname: formal-gates\n---\n" + hostInstructionsStartMarker + "\nround fifteen rule\n" + hostInstructionsEndMarker + "\n"
	round15WriteFile(t, filepath.Join(root, "SKILL.md"), []byte(skill), 0o600)
	round15WriteFile(t, filepath.Join(root, "README.md"), []byte("round fifteen runtime\n"), 0o600)
	round15WriteFile(t, filepath.Join(root, "README_EN.md"), []byte("round fifteen runtime\n"), 0o600)
	round15WriteFile(t, filepath.Join(root, "formal-gates.manifest.json"), []byte(`{"name":"formal-gates"}`+"\n"), 0o600)
	round15WriteFile(t, filepath.Join(root, "bin", nativeBinaryName()), []byte("#!/bin/sh\nexit 0\n"), 0o700)
	for _, relative := range []string{"agents/agent.md", "prompts/action.md", "gates/gate.md", "references/reference.md"} {
		round15WriteFile(t, filepath.Join(root, filepath.FromSlash(relative)), []byte(relative+"\n"), 0o600)
	}
	return root
}

func round15AssertNoInstallResidue(t *testing.T, registry, target string) {
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
			t.Fatalf("install residue matched %s: %v", pattern, matches)
		}
	}
	if _, err := os.Stat(installOuterJournalPath(registry)); !os.IsNotExist(err) {
		t.Fatalf("install journal remains: %v", err)
	}
}

func TestWhiteboxPhase0Round15InstallFaultMatrixRecoveryReceiptIdentity(t *testing.T) {
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
	const reconcileAction = "restore all target, release, binary, hook, managed-rule and registry snapshots"

	for _, fault := range faults {
		t.Run(fault.name, func(t *testing.T) {
			source := round15InstallSource(t)
			project := t.TempDir()
			registry := filepath.Join(t.TempDir(), "registry.json")
			launcher := filepath.Join(t.TempDir(), "launcher", nativeBinaryName())
			registryBefore := []byte(`{"schemaVersion":1,"epoch":4,"records":[]}` + "\n")
			round15WriteFile(t, registry, registryBefore, 0o600)
			targets, err := resolveInstallTargets("claude", "project", project)
			if err != nil {
				t.Fatal(err)
			}
			round15WriteFile(t, filepath.Join(targets[0].targetPath, "old.txt"), []byte("old runtime\n"), 0o600)
			round15WriteFile(t, targets[0].hookConfig, []byte(`{"hooks":{"External":[{"command":"keep"}]}}`+"\n"), 0o600)
			round15WriteFile(t, targets[0].managedRulePath, []byte("external rule\n"), 0o600)
			round15WriteFile(t, launcher, []byte("old launcher\n"), 0o700)

			t.Setenv("FORMAL_GATES_INSTALL_FAULT", fault.name)
			if _, err := Install(InstallOptions{Source: source, Host: "claude", Scope: "project", Project: project, RegistryPath: registry, BinaryTarget: launcher, Force: true}); err == nil || !strings.Contains(err.Error(), "deterministic install fault") {
				t.Fatalf("fault %q did not stop installation: %v", fault.name, err)
			}

			if string(round15ReadFile(t, filepath.Join(targets[0].targetPath, "old.txt"))) != "old runtime\n" || string(round15ReadFile(t, targets[0].hookConfig)) != `{"hooks":{"External":[{"command":"keep"}]}}`+"\n" || string(round15ReadFile(t, targets[0].managedRulePath)) != "external rule\n" || string(round15ReadFile(t, launcher)) != "old launcher\n" {
				t.Fatal("install fault did not restore the stable runtime and configuration")
			}
			if got := round15ReadFile(t, registry); string(got) != string(registryBefore) {
				t.Fatalf("install fault changed the authoritative registry: before=%q after=%q", registryBefore, got)
			}

			var receipt installRecoveryReceipt
			failurePath := installOuterJournalPath(registry) + ".failure.json"
			if err := json.Unmarshal(round15ReadFile(t, failurePath), &receipt); err != nil {
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
			round15AssertNoInstallResidue(t, registry, targets[0].targetPath)
		})
	}
}

func round15StableDocs(t *testing.T, root string) []string {
	t.Helper()
	docs := []string{
		filepath.Join(root, "SKILL.md"),
		filepath.Join(root, "README.md"),
		filepath.Join(root, "README_EN.md"),
	}
	for _, pattern := range []string{
		filepath.Join(root, "prompts", "*.md"),
		filepath.Join(root, "prompts", "actions", "*.md"),
	} {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		docs = append(docs, matches...)
	}
	return docs
}

func TestWhiteboxPhase0Round15StableHelpAndDocsRejectFutureCommandTokens(t *testing.T) {
	root := round15RepoRoot(t)
	packageRoot := t.TempDir()
	round15CopyPackage(t, root, packageRoot)
	installedBinary := filepath.Join(packageRoot, "bin", nativeBinaryName())
	round15Run(t, root, "go", "build", "-o", installedBinary, "./cmd/formal-gates")

	if result := Package(packageRoot); !result.OK() {
		t.Fatalf("built package inventory is invalid: %#v", result.Failures)
	}
	var packageManifest manifest
	if err := json.Unmarshal(round15ReadFile(t, filepath.Join(packageRoot, "formal-gates.manifest.json")), &packageManifest); err != nil {
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
		if !round15PathWithin(target, entry.RealPath) {
			t.Fatalf("installed inventory entry escaped the installed target: %+v", entry)
		}
	}
	if got := round15ReadFile(t, filepath.Join(target, "formal-gates.manifest.json")); string(got) != string(round15ReadFile(t, filepath.Join(packageRoot, "formal-gates.manifest.json"))) {
		t.Fatal("installed manifest differs from the built package manifest")
	}

	for _, path := range round15StableDocs(t, target) {
		content := string(round15ReadFile(t, path))
		for _, token := range []string{"drive", "submit"} {
			standalone := regexp.MustCompile(`(?i)(^|[^[:alnum:]_-])` + token + `([^[:alnum:]_-]|$)`)
			backticked := regexp.MustCompile("(?i)`" + token + "`")
			if match := standalone.FindString(content); match != "" {
				t.Fatalf("stable document %s contains standalone future command token %q", path, match)
			}
			if match := backticked.FindString(content); match != "" {
				t.Fatalf("stable document %s contains backticked future command token %q", path, match)
			}
		}
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
		{[]string{"workflow", "future", "--help"}, "workflow future"},
		{[]string{"workflow", "future", "generate", "--help"}, "workflow future generate"},
		{[]string{"workflow", "future", "view", "--help"}, "workflow future view"},
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
	}
	executionDir := t.TempDir()
	for _, helpCase := range helpCases {
		output := round15Run(t, executionDir, filepath.Join(target, "bin", nativeBinaryName()), helpCase.args...)
		if !strings.Contains(strings.ToLower(output), strings.ToLower(helpCase.marker)) {
			t.Fatalf("help %q omitted %q: %s", strings.Join(helpCase.args, " "), helpCase.marker, output)
		}
		for _, token := range []string{"drive", "submit"} {
			standalone := regexp.MustCompile(`(?i)(^|[^[:alnum:]_-])` + token + `([^[:alnum:]_-]|$)`)
			backticked := regexp.MustCompile("(?i)`" + token + "`")
			if match := standalone.FindString(output); match != "" {
				t.Fatalf("help %q contains standalone future command token %q", strings.Join(helpCase.args, " "), match)
			}
			if match := backticked.FindString(output); match != "" {
				t.Fatalf("help %q contains backticked future command token %q", strings.Join(helpCase.args, " "), match)
			}
		}
	}
}
