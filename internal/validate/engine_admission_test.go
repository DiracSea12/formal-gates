package validate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveEngineAdmissionChoosesProjectBinding(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	workingDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	target, err := filepath.Abs(filepath.Join(workingDir, "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	installed, err := PackageReceipt(target)
	if err != nil {
		t.Fatal(err)
	}
	launcher := filepath.Join(home, ".local", "bin", "formal-gates")
	global := admissionRepairRecord(target, launcher, home)
	projectOne := t.TempDir()
	projectTwo := t.TempDir()
	siblingOne := admissionRepairRecord(target, launcher, projectOne)
	siblingOne.ID = global.ID + "-project-one"
	siblingTwo := admissionRepairRecord(target, launcher, projectTwo)
	siblingTwo.ID = global.ID + "-project-two"
	global.PackageDigest = "sha256:package"
	global.InstalledDigest = installed.Digest
	siblingOne.PackageDigest, siblingOne.InstalledDigest = global.PackageDigest, global.InstalledDigest
	siblingTwo.PackageDigest, siblingTwo.InstalledDigest = global.PackageDigest, global.InstalledDigest
	path := filepath.Join(home, ".formal-gates", "registry.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	doc := RegistryDocument{SchemaVersion: RegistrySchemaVersion, Epoch: 1, Records: []RegistryRecord{global, siblingOne, siblingTwo}}
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
		if admission.RecordID == global.ID {
			t.Fatalf("resolve %s selected canonical global record: %+v", project, admission)
		}
	}
	admission, err := ResolveEngineAdmission(t.TempDir(), target)
	if err != nil {
		t.Fatalf("resolve first invocation: %v", err)
	}
	if admission.RecordID != global.ID {
		t.Fatalf("first invocation selected sibling: %+v", admission)
	}
	byRecord, err := ResolveEngineAdmissionByRecord(siblingTwo.ID)
	if err != nil || byRecord.RecordID != siblingTwo.ID {
		t.Fatalf("record-specific resolution = %+v, err=%v", byRecord, err)
	}
	conflicting := siblingOne
	conflicting.ID = global.ID + "-project-conflict"
	conflicting.PackageDigest = "sha256:other-package"
	doc.Records = append(doc.Records, conflicting)
	data, _ = json.Marshal(doc)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveEngineAdmission(projectOne, target); err == nil || !strings.Contains(err.Error(), "conflicting active registry records") {
		t.Fatalf("identity conflict was not rejected: %v", err)
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
