//go:build qastructure

package validate

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestUninstallRemovesOwnedPayloadsAfterLastSharedInstallation(t *testing.T) {
	source := installFixtureSource(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	project := t.TempDir()
	registry := filepath.Join(home, ".formal-gates", "registry.json")
	launcher := filepath.Join(home, ".local", "bin", nativeBinaryName())
	release := filepath.Join(home, "releases", "shared")
	options := InstallOptions{Source: source, Host: "claude", Scope: "project", Project: project, RegistryPath: registry, BinaryTarget: launcher, ReleaseRoot: release, Force: true}
	first, err := Install(options)
	if err != nil {
		t.Fatal(err)
	}
	options.Host = "codex"
	second, err := Install(options)
	if err != nil {
		t.Fatal(err)
	}
	options.Bootstrap = true
	options.Source = release
	if _, err = Install(options); err != nil {
		t.Fatal(err)
	}
	doc, err := LoadRegistry(registry)
	if err != nil {
		t.Fatal(err)
	}
	var resource string
	for _, record := range doc.Records {
		if record.Host == "codex" {
			resource = record.ResourceRoot
		}
	}
	user := filepath.Join(project, "user.txt")
	if err = os.WriteFile(user, []byte("user file\n"), 0640); err != nil {
		t.Fatal(err)
	}
	if _, err = Uninstall(UninstallOptions{Host: "claude", Scope: "project", Project: project, RegistryPath: registry}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{release, resource, launcher, second.Targets[0].TargetPath} {
		if _, err = os.Stat(path); err != nil {
			t.Fatal("shared or remaining installation removed", path, err)
		}
	}
	if _, err = os.Stat(first.Targets[0].TargetPath); !os.IsNotExist(err) {
		t.Fatal("selected runtime retained", err)
	}
	if err = runInstalledBinarySmoke(launcher); err != nil {
		t.Fatal("remaining installation is unusable", err)
	}
	doc, err = LoadRegistry(registry)
	if err != nil || len(doc.Records) != 1 || doc.Records[0].Host != "codex" {
		t.Fatal("selected record was not physically removed", doc, err)
	}
	if _, err = Uninstall(UninstallOptions{Host: "codex", Scope: "project", Project: project, RegistryPath: registry}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{second.Targets[0].TargetPath, release, resource, launcher, registry, registry + ".install.json", registry + ".bootstrap.json"} {
		if _, err = os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("last owned payload retained", path, err)
		}
	}
	if data, err := os.ReadFile(user); err != nil || !bytes.Equal(data, []byte("user file\n")) {
		t.Fatal("uninstall changed user content", err)
	}
}

func TestUninstallRestoresOwnedPayloadsThroughOriginalJournal(t *testing.T) {
	source := installFixtureSource(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	project := t.TempDir()
	registry := filepath.Join(home, ".formal-gates", "registry.json")
	launcher := filepath.Join(home, ".local", "bin", nativeBinaryName())
	release := filepath.Join(home, "release")
	options := InstallOptions{Source: source, Host: "claude", Scope: "project", Project: project, RegistryPath: registry, BinaryTarget: launcher, ReleaseRoot: release, Force: true}
	installed, err := Install(options)
	if err != nil {
		t.Fatal(err)
	}
	options.Bootstrap = true
	options.Source = release
	if _, err = Install(options); err != nil {
		t.Fatal(err)
	}
	doc, err := LoadRegistry(registry)
	if err != nil {
		t.Fatal(err)
	}
	resource := doc.Records[0].ResourceRoot
	if err = os.Chmod(resource, 0750); err != nil {
		t.Fatal(err)
	}
	paths := []string{registry, registry + ".install.json", registry + ".bootstrap.json", launcher, filepath.Join(release, "bin", nativeBinaryName()), filepath.Join(installed.Targets[0].TargetPath, "README.md")}
	before := map[string][]byte{}
	modes := map[string]os.FileMode{}
	for _, path := range paths {
		before[path] = installReadFile(t, path)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		modes[path] = info.Mode().Perm()
	}
	t.Setenv("FORMAL_GATES_INSTALL_FAULT", "registry-commit")
	if _, err = Uninstall(UninstallOptions{Host: "claude", Scope: "project", Project: project, RegistryPath: registry}); err == nil {
		t.Fatal("original fault did not interrupt commit")
	}
	for _, path := range paths {
		if !bytes.Equal(installReadFile(t, path), before[path]) {
			t.Fatal("journal did not restore original bytes", path)
		}
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != modes[path] {
			t.Fatal("journal changed original mode", path, err)
		}
	}
	info, err := os.Stat(resource)
	if err != nil || info.Mode().Perm() != 0750 {
		t.Fatal("journal did not restore empty resource mode", err)
	}
	var receipt installRecoveryReceipt
	if err := json.Unmarshal(installReadFile(t, installOuterJournalPath(registry)+".failure.json"), &receipt); err != nil || receipt.Operation != "uninstall" || receipt.Outcome != "ROLLED_BACK" || !receipt.Recovered || receipt.StableDigest == "" {
		t.Fatal("original rollback receipt lost its visible facts", receipt, err)
	}
	t.Logf("original recovery receipt: operation=%s outcome=%s recovered=%t", receipt.Operation, receipt.Outcome, receipt.Recovered)

}

func TestUninstallPreservesNonManagedConfigAndRuleBytes(t *testing.T) {
	source := installFixtureSource(t)
	project := t.TempDir()
	registry := filepath.Join(t.TempDir(), "registry.json")
	launcher := filepath.Join(t.TempDir(), "bin", nativeBinaryName())
	targets, err := resolveInstallTargets("claude", "project", project)
	if err != nil {
		t.Fatal(err)
	}
	config := []byte("{\n  \"hooks\": {\n    \"External\": [\n      {\n        \"command\": \"keep-original\"\n      }\n    ]\n  },\n  \"user\": \"original value\"\n}\n")
	rule := []byte("Original user rule.\r\n")
	installWriteFile(t, targets[0].hookConfig, config, 0640)
	installWriteFile(t, targets[0].managedRulePath, rule, 0640)
	if _, err := Install(InstallOptions{Source: source, Host: "claude", Scope: "project", Project: project, RegistryPath: registry, BinaryTarget: launcher, Force: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := Uninstall(UninstallOptions{Host: "claude", Scope: "project", Project: project, RegistryPath: registry}); err != nil {
		t.Fatal(err)
	}
	if got := installReadFile(t, targets[0].hookConfig); !bytes.Equal(got, config) {
		t.Fatalf("uninstall changed non-formal configuration bytes: %q", got)
	}
	if got := installReadFile(t, targets[0].managedRulePath); !bytes.Equal(got, rule) {
		t.Fatalf("uninstall changed original text bytes: %q", got)
	}
}
