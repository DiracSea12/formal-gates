package validate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"formal-gates/internal/engine/testkit"
)

func TestWorkBuddyProjectInstallationAndRemovalKeepNativeInstructions(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	source, project, home := copyPackageFixture(t), t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	registry := filepath.Join(home, "registry.json")
	report, err := Install(InstallOptions{Source: source, Host: "workbuddy", Scope: "project", Project: project, RegistryPath: registry, BinaryTarget: filepath.Join(home, "bin", nativeBinaryName()), Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Targets) != 1 || report.Targets[0].TargetPath != filepath.ToSlash(filepath.Join(project, ".codebuddy", "skills", "formal-gates")) || report.Targets[0].HookConfig != "" {
		t.Fatalf("native install target=%+v", report.Targets)
	}
	raw, err := os.ReadFile(filepath.Join(project, "CODEBUDDY.md"))
	if err != nil || !strings.Contains(string(raw), "<formal-gates:host-instructions:start>") {
		t.Fatalf("native instruction content=%q err=%v", raw, err)
	}
	if _, err := os.Stat(filepath.Join(report.Targets[0].TargetPath, "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := Uninstall(UninstallOptions{Host: "workbuddy", Scope: "project", Project: project, RegistryPath: registry}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(report.Targets[0].TargetPath); !os.IsNotExist(err) {
		t.Fatal("removed native skill target retained")
	}
}
