package validate

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCopyDirExcludesScriptRuntimeAndCaches(t *testing.T) {
	source, target := t.TempDir(), filepath.Join(t.TempDir(), "installed")
	files := map[string]string{
		"guide.md":                  "guide\n",
		"nested/schema.go":          "package schema\n",
		"directory.mjs/config.json": "{}\n",
		"__pycache__/cache.txt":     "cache\n",
		"nested/__PYCACHE__/cache":  "cache\n",
	}
	for _, ext := range []string{".ps1", ".psm1", ".psd1", ".py", ".pyc", ".pyo", ".sh", ".bash", ".bat", ".cmd", ".js", ".mjs", ".cjs"} {
		files["nested/helper"+ext] = "script " + ext + "\n"
		files["helper"+strings.ToUpper(ext)] = "script " + ext + "\n"
	}
	for rel, data := range files {
		path := filepath.Join(source, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := copyDir(source, target); err != nil {
		t.Fatal(err)
	}
	for rel, data := range files {
		got, err := os.ReadFile(filepath.Join(target, filepath.FromSlash(rel)))
		switch rel {
		case "guide.md", "nested/schema.go", "directory.mjs/config.json":
			if err != nil || string(got) != data {
				t.Fatalf("ordinary package file was not copied intact: %s: %v", rel, err)
			}
		default:
			if !os.IsNotExist(err) {
				t.Fatalf("script or cache file was copied: %s: %v", rel, err)
			}
		}
	}
}

// Native script bytes participate in the same copy, switch, and rollback
// transaction as every installed runtime file. The package fixture uses a
// smoke stub; the public binary and real script execution are tested by harness.
func TestInstallRestoresNativeHostScriptsAfterInterruptedReplacement(t *testing.T) {
	for _, fault := range []string{
		"copy-component:runtime/langgraph/formal_gates_harness/native_dsh.mjs",
		"copy-component:runtime/langgraph/formal_gates_harness/zcode_credential.mjs",
		"switched",
		"verify-stage:installed-target",
	} {
		t.Run(fault, func(t *testing.T) {
			source := copyPackageFixture(t)
			options := InstallOptions{
				Source: source, Host: "dsh", Scope: "project", Project: t.TempDir(),
				RegistryPath: filepath.Join(t.TempDir(), "registry.json"),
				BinaryTarget: filepath.Join(t.TempDir(), "bin", nativeBinaryName()),
				Force:        true, SkipHooks: true,
			}
			installed, err := Install(options)
			if err != nil {
				t.Fatal(err)
			}
			target := installed.Targets[0].TargetPath
			before, err := PackageReceipt(target)
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"native_dsh.mjs", "zcode_credential.mjs"} {
				path := filepath.Join(source, "runtime", "langgraph", "formal_gates_harness", name)
				if err := os.WriteFile(path, []byte("candidate script "+name+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("FORMAL_GATES_INSTALL_FAULT", fault)
			if _, err := Install(options); err == nil || !strings.Contains(err.Error(), "deterministic install fault") {
				t.Fatalf("replacement was not interrupted at %s: %v", fault, err)
			}
			after, err := PackageReceipt(target)
			if err != nil || after.Digest != before.Digest {
				t.Fatalf("interrupted replacement changed installed bytes: before=%s after=%s err=%v", before.Digest, after.Digest, err)
			}
			registry, err := LoadRegistry(options.RegistryPath)
			if err != nil || len(registry.Records) != 1 || registry.Records[0].InstalledDigest != before.Digest {
				t.Fatalf("rollback changed registered installed identity: %+v err=%v", registry, err)
			}
			t.Setenv("FORMAL_GATES_INSTALL_FAULT", "")
			if _, err := Install(options); err != nil {
				t.Fatalf("replacement after rollback failed: %v", err)
			}
			for _, name := range []string{"native_dsh.mjs", "zcode_credential.mjs"} {
				rel := filepath.Join("runtime", "langgraph", "formal_gates_harness", name)
				want, err := os.ReadFile(filepath.Join(source, rel))
				if err != nil {
					t.Fatal(err)
				}
				got, err := os.ReadFile(filepath.Join(target, rel))
				if err != nil || !bytes.Equal(got, want) {
					t.Fatalf("replacement did not install source script %s: %v", rel, err)
				}
			}
		})
	}
}

func TestInstallAllowsActiveWorkflowRunAndBootstrapKeepsLegacySemantics(t *testing.T) {
	source := copyPackageFixture(t)
	project := t.TempDir()
	registry := filepath.Join(t.TempDir(), "registry.json")
	launcher := filepath.Join(t.TempDir(), "bin", nativeBinaryName())
	activeState := filepath.Join(project, ".gates", "tmp", "active-run", "state.json")
	if err := os.MkdirAll(filepath.Dir(activeState), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(activeState, []byte(`{"runId":"active-run","status":"ACTIVE"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	beforeState, err := os.ReadFile(activeState)
	if err != nil {
		t.Fatal(err)
	}

	report, err := Install(InstallOptions{
		Source: source, Host: "claude", Scope: "project", Project: project,
		RegistryPath: registry, BinaryTarget: launcher, Force: true, SkipHooks: true,
	})
	if err != nil {
		t.Fatalf("install with an active run failed: %v", err)
	}
	if len(report.Targets) != 1 || report.Targets[0].Smoke != "PASS" {
		t.Fatalf("install report=%#v", report)
	}
	afterState, err := os.ReadFile(activeState)
	if err != nil {
		t.Fatal(err)
	}
	if string(afterState) != string(beforeState) {
		t.Fatal("install changed the active run state")
	}

	bootstrapProject := t.TempDir()
	bootstrapRegistry := filepath.Join(t.TempDir(), "registry.json")
	bootstrapLauncher := filepath.Join(t.TempDir(), "bin", nativeBinaryName())
	if _, err := Install(InstallOptions{
		Source: source, Host: "claude", Scope: "project", Project: bootstrapProject,
		RegistryPath: bootstrapRegistry, BinaryTarget: bootstrapLauncher, Force: true, SkipHooks: true,
	}); err != nil {
		t.Fatalf("bootstrap fixture install failed: %v", err)
	}
	bootstrapState := filepath.Join(bootstrapProject, ".gates", "tmp", "active-run", "state.json")
	if err := os.MkdirAll(filepath.Dir(bootstrapState), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bootstrapState, []byte(`{"runId":"active-run","status":"ACTIVE"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := Install(InstallOptions{
		Source: source, Host: "claude", Scope: "project", Project: bootstrapProject,
		RegistryPath: bootstrapRegistry, BinaryTarget: bootstrapLauncher, Bootstrap: true,
		Force: true, SkipHooks: true,
	}); err != nil {
		t.Fatalf("bootstrap was unexpectedly fenced by an active run: %v", err)
	}
}

func TestInstallIgnoresActiveRunFromRelatedRegistryRecord(t *testing.T) {
	source := copyPackageFixture(t)
	hostHome := t.TempDir()
	t.Setenv("HOME", hostHome)
	registry := filepath.Join(t.TempDir(), "registry.json")
	launcher := filepath.Join(t.TempDir(), "bin", nativeBinaryName())
	options := InstallOptions{
		Source: source, Host: "claude", Scope: "global",
		RegistryPath: registry, BinaryTarget: launcher, Force: true, SkipHooks: true,
	}
	if _, err := Install(options); err != nil {
		t.Fatalf("initial install: %v", err)
	}
	document, err := LoadRegistry(registry)
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Records) != 1 {
		t.Fatalf("registry records=%d want=1", len(document.Records))
	}

	project := t.TempDir()
	stateRoot := filepath.Join(project, ".gates")
	related := document.Records[0]
	related.ID += "-project-related"
	related.ProjectRoot = project
	related.StateRoot = stateRoot
	related.ResourceRoot = filepath.Join(project, ".formal-gates-resources")
	related.CanonicalPaths = make(map[string]string, len(document.Records[0].CanonicalPaths))
	for key, value := range document.Records[0].CanonicalPaths {
		related.CanonicalPaths[key] = value
	}
	related.CanonicalPaths["projectRoot"] = canonicalRegistryPath(related.ProjectRoot)
	related.CanonicalPaths["stateRoot"] = canonicalRegistryPath(related.StateRoot)
	related.CanonicalPaths["resourceRoot"] = canonicalRegistryPath(related.ResourceRoot)
	registryUnlock, err := acquireRegistryLock(registry)
	if err != nil {
		t.Fatal(err)
	}
	current, err := loadRegistryForCommit(registry)
	if err == nil {
		_, err = commitRegistryRecordsUnlocked(registry, current, []RegistryRecord{related})
	}
	registryUnlock()
	if err != nil {
		t.Fatal(err)
	}

	activeState := filepath.Join(stateRoot, "tmp", "related-active", "state.json")
	if err := os.MkdirAll(filepath.Dir(activeState), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(activeState, []byte(`{"runId":"related-active","status":"ACTIVE"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	beforeState, err := os.ReadFile(activeState)
	if err != nil {
		t.Fatal(err)
	}

	report, err := Install(options)
	if err != nil {
		t.Fatalf("reinstall with a related active run failed: %v", err)
	}
	if len(report.Targets) != 1 || report.Targets[0].Smoke != "PASS" {
		t.Fatalf("reinstall report=%#v", report)
	}
	afterState, err := os.ReadFile(activeState)
	if err != nil {
		t.Fatal(err)
	}
	if string(afterState) != string(beforeState) {
		t.Fatal("reinstall changed the related active run state")
	}
}

func TestLoadManagedRuleRequiresSingleCurrentRule(t *testing.T) {
	root := t.TempDir()
	skillPath := filepath.Join(root, filepath.FromSlash(managedRuleSourceRelativePath))

	writeManagedRuleTestFile(t, skillPath, "current rule")
	got, err := LoadManagedRule(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != "current rule" {
		t.Fatalf("managed rule=%q want=%q", got, "current rule")
	}

	for _, invalid := range []string{
		"",
		" ",
		"\nleading",
		"trailing\n",
		hostInstructionsStartMarker,
		hostInstructionsEndMarker,
	} {
		t.Run(strings.ReplaceAll(invalid, "\n", "\\n"), func(t *testing.T) {
			writeManagedRuleTestFile(t, skillPath, invalid)
			if _, err := LoadManagedRule(root); err == nil {
				t.Fatalf("invalid managed rule %q was accepted", invalid)
			}
		})
	}
	if err := os.WriteFile(skillPath, []byte("# Skill without markers\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadManagedRule(root); err == nil {
		t.Fatal("SKILL.md without managed rule markers was accepted")
	}
	duplicate := hostInstructionsStartMarker + "\none\n" + hostInstructionsEndMarker + "\n" +
		hostInstructionsStartMarker + "\ntwo\n" + hostInstructionsEndMarker + "\n"
	if err := os.WriteFile(skillPath, []byte(duplicate), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadManagedRule(root); err == nil {
		t.Fatal("SKILL.md with duplicate managed rule blocks was accepted")
	}
}

func TestManagedRuleMarkersCollapseDuplicatesAndPreserveNewlineStyle(t *testing.T) {
	original := "before\r\n" +
		hostInstructionsStartMarker + "\r\nOLD\r\n" + hostInstructionsEndMarker + "\r\n" +
		"keep\r\n" +
		hostInstructionsStartMarker + "\r\nSTALE\r\n" + hostInstructionsEndMarker + "\r\n" +
		"after\r\n"
	updated, err := replaceManagedRuleBlock(original, "CURRENT")
	if err != nil {
		t.Fatal(err)
	}
	want := "before\r\n" +
		hostInstructionsStartMarker + "\r\nCURRENT\r\n" + hostInstructionsEndMarker + "\r\n" +
		"keep\r\nafter\r\n"
	if updated != want {
		t.Fatalf("managed marker migration=%q want=%q", updated, want)
	}
	if strings.Count(updated, hostInstructionsStartMarker) != 1 ||
		strings.Count(updated, hostInstructionsEndMarker) != 1 ||
		strings.Count(updated, "CURRENT") != 1 {
		t.Fatalf("managed block did not converge: %q", updated)
	}
}

func TestManagedRuleMarkersMigrateExactUnmarkedCurrentRule(t *testing.T) {
	original := "before\nCURRENT\nkeep CURRENT embedded\nafter\n"
	updated, err := replaceManagedRuleBlock(original, "CURRENT")
	if err != nil {
		t.Fatal(err)
	}
	want := "before\n" +
		hostInstructionsStartMarker + "\nCURRENT\n" + hostInstructionsEndMarker + "\n" +
		"keep CURRENT embedded\nafter\n"
	if updated != want {
		t.Fatalf("unmarked migration=%q want=%q", updated, want)
	}
}

func TestRemoveManagedRuleMarkersPreservesUnrelatedContent(t *testing.T) {
	original := "unrelated\n" +
		hostInstructionsStartMarker + "\nCURRENT\n" + hostInstructionsEndMarker + "\n" +
		"end\n"
	updated, removed, err := removeManagedRuleBlocks(original)
	if err != nil {
		t.Fatal(err)
	}
	if !removed {
		t.Fatal("expected managed marker block to be removed")
	}
	if want := "unrelated\nend\n"; updated != want {
		t.Fatalf("uninstalled managed block=%q want=%q", updated, want)
	}
}

func TestManagedRuleMarkersRejectMalformedBlocks(t *testing.T) {
	for name, input := range map[string]string{
		"missing-end":   hostInstructionsStartMarker + "\nCURRENT\n",
		"missing-start": hostInstructionsEndMarker + "\n",
		"nested":        hostInstructionsStartMarker + "\n" + hostInstructionsStartMarker + "\n" + hostInstructionsEndMarker + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := replaceManagedRuleBlock(input, "CURRENT"); err == nil {
				t.Fatalf("malformed marker block %q was accepted", input)
			}
		})
	}
}

func TestRemoveManagedRuleFileCanRemoveEmptyCursorRuleFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "formal-gates.mdc")
	text := hostInstructionsStartMarker + "\nCURRENT\n" + hostInstructionsEndMarker + "\n"
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := removeManagedRuleFile(path, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected empty Cursor rule file to be removed, err=%v", err)
	}
}

func TestOuterFileSnapshotRestoresStableLauncherSymlink(t *testing.T) {
	root := t.TempDir()
	oldRelease := filepath.Join(root, "release-v1", "bin", nativeBinaryName())
	launcher := filepath.Join(root, "bin", nativeBinaryName())
	if err := os.MkdirAll(filepath.Dir(oldRelease), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(launcher), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldRelease, []byte("old-release\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(oldRelease, launcher); err != nil {
		t.Fatal(err)
	}
	if got := stableLauncherPath(launcher); got == canonicalRegistryPath(launcher) {
		t.Fatalf("stable launcher path resolved the existing pointer: got=%s", got)
	}

	snapshot, err := snapshotOuterFile(launcher, filepath.Join(root, "transaction", "launcher.before"))
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Kind != "symlink" || snapshot.LinkTarget != oldRelease {
		t.Fatalf("stable launcher snapshot=%+v, want symlink to %s", snapshot, oldRelease)
	}
	if err := os.Remove(launcher); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(launcher, []byte("new-release\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := restoreOuterFile(snapshot); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(launcher)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("restored stable launcher mode=%s, want symlink", info.Mode())
	}
	if got, err := os.Readlink(launcher); err != nil || got != oldRelease {
		t.Fatalf("restored stable launcher target=%q, err=%v, want=%s", got, err, oldRelease)
	}
}

func writeManagedRuleTestFile(t *testing.T, path, current string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	data := "# Skill\n" + hostInstructionsStartMarker + "\n" + current + "\n" + hostInstructionsEndMarker + "\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}
