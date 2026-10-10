package validate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLauncherChecksApplyToTheActualTestExecutable(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	fixtureLauncherInvocation(t, executable)
	options := InstallOptions{Host: "codex", Scope: "global"}
	if err := RequireInstallLauncher(options); err == nil || !strings.Contains(err.Error(), "UNREGISTERED_INSTALL") {
		t.Fatalf("unregistered test executable passed launcher admission: %v", err)
	}
	if err := RequireUninstallLauncher(UninstallOptions{Host: "codex", Scope: "global"}); err == nil || !strings.Contains(err.Error(), "UNREGISTERED_INSTALL") {
		t.Fatalf("unregistered test executable passed uninstall admission: %v", err)
	}
	record := admissionRepairRecord(filepath.Join(home, "target"), filepath.Join(home, ".local", "bin", nativeBinaryName()), home)
	record.Scope = "project"
	if err := verifyRegistryBindingRecord(record, home, record.Target); err == nil {
		t.Fatal("test executable passed a different registered launcher binding")
	}
	fixtureLauncherInvocation(t, record.LauncherPath)
	if err := RequireInstallLauncher(options); err != nil {
		t.Fatal("the original invocation owner rejected its documented launcher", err)
	}
	if err := verifyRegistryBindingRecord(record, home, record.Target); err != nil {
		t.Fatal("correct launcher binding rejected", err)
	}
}

func TestAdmissionRequiresCompleteIdentityInEveryBuild(t *testing.T) {
	root := t.TempDir()
	record := admissionRepairRecord(filepath.Join(root, "target"), filepath.Join(root, "launcher"), root)
	record.Scope = "project"
	record.PackageDigest, record.InstalledDigest = "sha256:fixture-source", "sha256:fixture-target"
	if !validAdmissionRegistryRecord(record) {
		t.Fatal("complete fixture identity rejected")
	}
	for _, field := range []string{"native", "package", "installed", "generation", "lease", "token"} {
		copy := record
		switch field {
		case "native":
			copy.VCSIdentity = ""
		case "package":
			copy.PackageDigest = ""
		case "installed":
			copy.InstalledDigest = ""
		case "generation":
			copy.Generation = 0
		case "lease":
			copy.Lease = ""
		case "token":
			copy.Token = ""
		}
		if validAdmissionRegistryRecord(copy) {
			t.Fatalf("missing %s was accepted because of executable/build identity", field)
		}
	}
}
