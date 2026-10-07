package validate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/worker"
	"formal-gates/internal/prompts"
)

func TestPortableEngineWorkflowUsesInstalledFullRoute(t *testing.T) {
	source := copyPackageFixture(t)
	project := t.TempDir()
	options := InstallOptions{Source: source, Host: "codex", Scope: "project", Project: project, RegistryPath: filepath.Join(t.TempDir(), "registry.json"), BinaryTarget: filepath.Join(t.TempDir(), "bin", nativeBinaryName()), Force: true, SkipHooks: true}
	report, err := Install(options)
	if err != nil {
		t.Fatal(err)
	}
	options.Bootstrap = true
	if _, err = Install(options); err != nil {
		t.Fatal(err)
	}
	target := report.Targets[0]
	catalog, err := prompts.LoadPromptCatalog(target.TargetPath)
	if err != nil {
		t.Fatal(err)
	}
	if err = runQuickE2ECanaryAt(source, catalog, project, report.Registry, target.RegistryRecordID); err == nil {
		t.Fatal("source package forged installed admission")
	}
	if _, err = os.Stat(filepath.Join(project, ".gates", "engine", "canary", "state.json")); !os.IsNotExist(err) {
		t.Fatalf("rejected source created engine authority: %v", err)
	}
	if err = runQuickE2ECanaryAt(target.TargetPath, catalog, project, report.Registry, target.RegistryRecordID); err != nil {
		t.Fatal(err)
	}
	f, err := facade.Open(project, "canary")
	if err != nil {
		t.Fatal(err)
	}
	run, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "COMPLETE" || run.Unverified || run.PromotedSnapshot == "" {
		t.Fatalf("missing verified engine terminal: %+v", run)
	}
	for _, pair := range []struct{ path, content string }{{"delivery.txt", canaryDeliveryContents}, {"whitebox_delivered_test.go", canaryWhiteboxMarker}} {
		b, err := os.ReadFile(filepath.Join(project, pair.path))
		if err != nil || !strings.Contains(string(b), pair.content) {
			t.Fatalf("promoted artifact %s missing content: %s %v", pair.path, b, err)
		}
	}
	materials, err := filepath.Glob(filepath.Join(project, facade.EngineNamespace, "canary", "materials", "*", "*.json"))
	if err != nil || len(materials) == 0 {
		t.Fatalf("immutable worker evidence missing: %v", err)
	}
	for _, path := range materials {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var material worker.Material
		if err = json.Unmarshal(body, &material); err != nil {
			t.Fatal(err)
		}
		if material.Input.RunID != "canary" || material.Input.Provider != "codex" || material.Input.Pending.ActionID == "" || !strings.Contains(material.Prompt, "The installed workflow must complete its verified route.") || len(material.ResultSchema) == 0 {
			t.Fatalf("worker material lost complete production context: %s", path)
		}
	}
	if _, err = os.Stat(filepath.Join(project, ".gates", "tmp", "canary", "state.json")); !os.IsNotExist(err) {
		t.Fatalf("legacy authority remains: %v", err)
	}
}

func TestPortableWorkflowRequiresInstalledAdmission(t *testing.T) {
	if err := runQuickE2ECanaryAt(t.TempDir(), prompts.PromptCatalog{}, "", "", ""); err == nil {
		t.Fatal("uninstalled source was accepted")
	}
}

func TestPortableWriteBlockUsesCurrentEngineEnvelope(t *testing.T) {
	root := t.TempDir()
	if err := runWriteBlockCanary(root); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(root, ".gates", "engine", "write-block-canary", "state.json")
	state, err := readHookEngineState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !state.DevelopmentStarted() {
		t.Fatal("current issued development fact missing")
	}
	if _, err = os.Stat(filepath.Join(root, ".gates", "tmp", "write-block-canary", "state.json")); !os.IsNotExist(err) {
		t.Fatalf("legacy write-wall state remains: %v", err)
	}
}
