package validate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkBuddyInstallNativeEntry(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	source := copyPackageFixture(t)
	project := t.TempDir()
	registry := filepath.Join(home, "registry.json")
	launcher := filepath.Join(home, "bin", nativeBinaryName())
	report, err := Install(InstallOptions{Source: source, Host: "workbuddy", Scope: "project", Project: project, RegistryPath: registry, BinaryTarget: launcher, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Targets) != 1 {
		t.Fatalf("targets=%+v", report.Targets)
	}
	if _, err := os.Stat(filepath.Join(project, ".codebuddy", "skills", "formal-gates", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	rule, err := os.ReadFile(filepath.Join(project, "CODEBUDDY.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rule), "formal-gates:host-instructions:start") {
		t.Fatal("native host entry missing")
	}
	if _, err := os.Stat(filepath.Join(project, ".codebuddy", "settings.json")); !os.IsNotExist(err) {
		t.Fatalf("unexpected global hook configuration: %v", err)
	}
}
