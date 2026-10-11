package validate

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/worker"
	"formal-gates/internal/prompts"
)

func TestPortableEngineWorkflowUsesInstalledFullRoute(t *testing.T) {
	source := portablePackageFixture(t)
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
	restore := fixtureRegisteredInvocation(report.Registry, target.TargetPath, target.RegistryRecordID)
	defer restore()
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
	materials, err := filepath.Glob(filepath.Join(project, facade.EngineNamespace, "canary", "materials", "*.json"))
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
		if material.Input.RunID != "canary" || material.Input.Provider != "codex" || material.Input.Pending.ActionID == "" || !strings.Contains(material.Input.RequirementDocument.Content, "The installed workflow must complete its verified route.") || material.Input.RequirementDocument.ContentDigest == "" || !strings.Contains(material.Prompt, "workspace requirement operation reads the complete original confirmed document") || len(material.ResultSchema) == 0 {
			t.Fatalf("worker material lost complete production context: %s", path)
		}
		if material.Input.Handler == "engine.qa.execution" {
			in := material.Input.Validation
			if len(in.ExecutionFacts) != 2 {
				t.Fatalf("execution omitted real native case facts: %+v", in)
			}
			for _, fact := range in.ExecutionFacts {
				if len(fact.Operations) != 1 || fact.Operations[0].Evidence.Tool == "" || fact.Operations[0].Evidence.Environment == "" || fact.Operations[0].ActionID == "" {
					t.Fatalf("native receipt binding missing: %+v", fact)
				}
				if fact.Operations[0].Parameters.QA.GoTestRef != "" && fact.Operations[0].Evidence.GoResult != "pass" {
					t.Fatalf("whitebox case did not really execute and pass: %+v", fact)
				}
			}
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

func portablePackageFixture(t *testing.T) string {
	t.Helper()
	source := copyPackageFixture(t)
	binary := filepath.Join(t.TempDir(), nativeBinaryName())
	build := exec.Command("go", "build", "-o", binary, "./cmd/formal-gates")
	build.Dir = repoRootValidateTest(t)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build current portable CLI: %v\n%s", err, output)
	}
	body, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "bin", nativeBinaryName()), body, 0755); err != nil {
		t.Fatal(err)
	}
	return source
}

func TestPortableFullTestCommandChecksExactArtifactBytes(t *testing.T) {
	plan := portableDevelopmentPlan()
	command := ""
	for _, check := range plan.Batches[0].Checks {
		if check.Purpose == protocol.CheckFullTests {
			command = check.Command
		}
	}
	for _, contents := range []string{"delivery\n", "delivery", "delivery\n\n"} {
		t.Run(fmt.Sprintf("bytes-%d", len(contents)), func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "built-delivery.txt"), []byte(contents), 0600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("/bin/sh", "-c", command)
			cmd.Dir = root
			output, err := cmd.CombinedOutput()
			if (err == nil) != (contents == canaryDeliveryContents) {
				t.Fatalf("complete tests for %q output=%q err=%v", contents, output, err)
			}
		})
	}
}
