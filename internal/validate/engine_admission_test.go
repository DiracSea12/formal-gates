package validate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/testkit"
	"formal-gates/internal/host"
)

func TestResolveEngineAdmissionUsesCanonicalGlobalBinding(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	target, packageDigest := installedAdmissionTarget(t)
	installed, err := PackageReceipt(target)
	if err != nil {
		t.Fatal(err)
	}
	launcher := filepath.Join(home, ".local", "bin", "formal-gates")
	fixtureLauncherInvocation(t, launcher)
	global := admissionRepairRecord(target, launcher, home)
	projectOne := t.TempDir()
	projectTwo := t.TempDir()
	global.PackageDigest = packageDigest
	global.InstalledDigest = installed.Digest
	path := filepath.Join(home, ".formal-gates", "registry.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	doc := RegistryDocument{SchemaVersion: RegistrySchemaVersion, Epoch: 1, Records: []RegistryRecord{global}}
	data, _ := json.Marshal(doc)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	receipt := BootstrapReceipt{Accepted: true, PackageDigest: global.PackageDigest, Records: []RegistryRecord{global}}
	receiptData, _ := json.Marshal(receipt)
	if err := os.WriteFile(path+".bootstrap.json", receiptData, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, project := range []string{projectOne, projectTwo} {
		admission, err := ResolveEngineAdmission(project, target)
		if err != nil {
			t.Fatalf("resolve %s: %v", project, err)
		}
		if admission.RecordID != global.ID || admission.Provider != host.Claude {
			t.Fatalf("resolve %s lost the project or host binding: %+v", project, admission)
		}
	}
	admission, err := ResolveEngineAdmission(t.TempDir(), target)
	if err != nil {
		t.Fatalf("resolve first invocation: %v", err)
	}
	if admission.RecordID != global.ID || admission.Provider != host.Claude {
		t.Fatalf("first invocation selected sibling: %+v", admission)
	}
	byRecord, err := ResolveEngineAdmissionByRecord(global.ID)
	if err != nil || byRecord.RecordID != global.ID || byRecord.Provider != host.Claude {
		t.Fatalf("record-specific resolution = %+v, err=%v", byRecord, err)
	}
}

func TestEngineAdmissionNormalizesRegisteredHostAliases(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	target, packageDigest := installedAdmissionTarget(t)
	installed, err := PackageReceipt(target)
	if err != nil {
		t.Fatal(err)
	}
	record := admissionRepairRecord(target, filepath.Join(home, "bin", "formal-gates"), home)
	fixtureLauncherInvocation(t, record.LauncherPath)
	record.PackageDigest, record.InstalledDigest = packageDigest, installed.Digest
	path := filepath.Join(home, ".formal-gates", "registry.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, descriptor := range host.All() {
		if !descriptor.Installable {
			continue
		}
		for _, alias := range descriptor.Aliases {
			t.Run(descriptor.ID+"/"+alias, func(t *testing.T) {
				record.Host = " " + strings.ToUpper(alias) + " "
				for filename, value := range map[string]any{
					path:                     RegistryDocument{SchemaVersion: RegistrySchemaVersion, Epoch: 1, Records: []RegistryRecord{record}},
					path + ".bootstrap.json": BootstrapReceipt{Accepted: true, PackageDigest: record.PackageDigest, Records: []RegistryRecord{record}},
				} {
					data, err := json.Marshal(value)
					if err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filename, data, 0o600); err != nil {
						t.Fatal(err)
					}
				}
				admission, err := ResolveEngineAdmission(t.TempDir(), target)
				if err != nil || admission.Provider != descriptor.ID || admission.RecordID != record.ID {
					t.Fatalf("host %q: admission=%+v err=%v", record.Host, admission, err)
				}
				byRecord, err := ResolveEngineAdmissionByRecord(record.ID)
				if err != nil || !reflect.DeepEqual(byRecord, admission) {
					t.Fatalf("exact record changed admission: %+v != %+v, err=%v", byRecord, admission, err)
				}
			})
		}
	}
}

func admissionRepairRecord(target, launcher, project string) RegistryRecord {
	stateRoot := filepath.Join(project, ".gates")
	resourceRoot := filepath.Join(project, ".formal-gates-resources")
	return RegistryRecord{
		ID: "claude-global-target", Target: target, LauncherPath: launcher, Scope: "global", Host: "claude",
		ProjectRoot: project, StateRoot: stateRoot, ResourceRoot: resourceRoot, RuntimeSibling: filepath.Dir(target), Status: "active",
		VCSIdentity: "git:test", Generation: 1, Lease: "lease", Token: "token",
		CanonicalPaths: map[string]string{"target": canonicalRegistryPath(target), "launcher": canonicalRegistryPath(launcher), "projectRoot": canonicalRegistryPath(project), "stateRoot": canonicalRegistryPath(stateRoot), "resourceRoot": canonicalRegistryPath(resourceRoot), "runtimeSibling": canonicalRegistryPath(filepath.Dir(target))},
	}
}

func installedAdmissionTarget(t *testing.T) (string, string) {
	t.Helper()
	report, err := Install(InstallOptions{
		Source: copyPackageFixture(t), Host: "claude", Scope: "project", Project: t.TempDir(),
		RegistryPath: filepath.Join(t.TempDir(), "registry.json"),
		BinaryTarget: filepath.Join(t.TempDir(), nativeBinaryName()), SkipHooks: true, Force: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Targets) != 1 || report.PackageDigest == "" {
		t.Fatalf("installed target receipt: %+v", report)
	}
	return report.Targets[0].TargetPath, report.PackageDigest
}

func TestProjectRuntimeFilesCollectsInstalledHosts(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CODEX_HOME", "")
	launcher := filepath.Join(home, ".local", "bin", nativeBinaryName())
	fixtureLauncherInvocation(t, launcher)
	source := copyPackageFixture(t)
	var targets []string
	for _, name := range []string{"claude", "codex"} {
		report, err := Install(InstallOptions{Source: source, Host: name, Scope: "global", BinaryTarget: launcher, Force: true, SkipHooks: true})
		if err != nil {
			t.Fatal(name, err)
		}
		targets = append(targets, report.Targets[0].TargetPath)
	}
	note := filepath.Join(home, ".codex", "user-notes.md")
	const content = "Original user material\n"
	if err := os.WriteFile(note, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	for count := 0; count < 2; count++ {
		files, err := ProjectRuntimeFiles(home)
		if err != nil {
			t.Fatal(err)
		}
		for _, target := range targets {
			installed, err := PackageReceipt(target)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range installed.Entries {
				if !files.ContainsFile(entry.RealPath) {
					t.Fatal("registered leaf was omitted", entry.RealPath)
				}
			}
		}
		if !files.ContainsFile(launcher) || files.ContainsFile(note) {
			t.Fatal("inventory lost launcher or hid adjacent user material", files)
		}
	}
	if data, err := os.ReadFile(note); err != nil || string(data) != content {
		t.Fatal("discovery changed original user material", string(data), err)
	}
	if _, err := ResolveEngineAdmission(home, ""); err == nil || !strings.Contains(err.Error(), "conflicting active registry records") {
		t.Fatal("read-only inventory weakened formal admission conflict", err)
	}
}
