package validate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallFencesActiveWorkflowRunAndBootstrapKeepsLegacySemantics(t *testing.T) {
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

	_, err := Install(InstallOptions{
		Source: source, Host: "claude", Scope: "project", Project: project,
		RegistryPath: registry, BinaryTarget: launcher, Force: true, SkipHooks: true,
	})
	if err == nil || !strings.Contains(err.Error(), `active workflow run "active-run"`) || !strings.Contains(err.Error(), "fences install") {
		t.Fatalf("install did not fence active run: %v", err)
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
