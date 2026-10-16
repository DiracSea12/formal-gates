package validate

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/testkit"
	"formal-gates/internal/engine/worker"
	"formal-gates/internal/harness"
	"formal-gates/internal/prompts"
	"formal-gates/internal/requirement"
)

func TestPortableEngineWorkflowUsesInstalledFullRoute(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
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
	for _, pair := range []struct{ path, content string }{{"delivery.txt", canaryDeliveryContents}, {"whitebox_delivered_test.go", whiteboxDeliveredTestCode}} {
		b, err := os.ReadFile(filepath.Join(project, pair.path))
		if err != nil || string(b) != pair.content {
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
		if material.Input.Handler == "engine.qa.execution.v4" {
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
	t.Parallel()
	if err := runQuickE2ECanaryAt(t.TempDir(), prompts.PromptCatalog{}, "", "", ""); err == nil {
		t.Fatal("uninstalled source was accepted")
	}
}

func TestPortableWriteBlockUsesCurrentEngineEnvelope(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	input := protocol.WorkerInput{Handler: "engine.development.batch.v6", Validation: &protocol.ValidationInput{Handler: "engine.development.batch.v6", Workspace: &protocol.NativeResource{Path: t.TempDir()}, Development: &protocol.DevelopmentInput{Batch: protocol.DevelopmentBatch{ID: "marker", Checks: []protocol.DevelopmentCheck{{Purpose: protocol.CheckFullTests, Goal: "Compare built-delivery.txt with delivery newline"}}}}}}
	var facts []worker.LocationFact
	payload, err := portableWorkerResult(input, map[string]bool{}, &facts)
	if err != nil {
		t.Fatal(err)
	}
	command := payload.(map[string]any)["commands"].([]string)[0]
	argv := coverage.ShellCommand(command)
	for _, contents := range []string{"delivery\n", "delivery", "delivery\n\n", "delivery\r\n", "\xef\xbb\xbfdelivery\n"} {
		t.Run(fmt.Sprintf("bytes-%d", len(contents)), func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "built-delivery.txt"), []byte(contents), 0600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(argv[0], argv[1:]...)
			harness.ApplyNativeCommandLine(cmd)
			cmd.Dir = root
			output, err := cmd.CombinedOutput()
			if (err == nil) != (contents == canaryDeliveryContents) {
				t.Fatalf("complete tests for %q output=%q err=%v", contents, output, err)
			}
		})
	}
}

func TestPortableWorkerReportsFailedDevelopmentCheck(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	argv := coverage.ShellCommand("exit 37")
	command := exec.Command(argv[0], argv[1:]...)
	harness.ApplyNativeCommandLine(command)
	command.Dir = root
	if err := command.Run(); err == nil {
		t.Fatal("failure fixture unexpectedly passed")
	}
	invocation, err := json.Marshal(argv)
	if err != nil {
		t.Fatal(err)
	}
	input := protocol.WorkerInput{Validation: &protocol.ValidationInput{
		Handler: "engine.development.batch.v6", Workspace: &protocol.NativeResource{Path: root},
		Development: &protocol.DevelopmentInput{Checks: []protocol.CheckEvidence{{Invocation: string(invocation), ExitCode: command.ProcessState.ExitCode()}}},
	}}
	var facts []worker.LocationFact
	if _, err := portableWorkerResult(input, map[string]bool{}, &facts); err == nil || !strings.Contains(err.Error(), "exit=37") || !strings.Contains(err.Error(), string(invocation)) {
		t.Fatalf("failed native check lost its original invocation and exit status: %v", err)
	}
}

func TestPortableWorkerResultsCoverIssuedRequirementPoints(t *testing.T) {
	t.Parallel()
	points := []requirement.Point{{ID: "AC-001", Text: "Committed marker contains delivery newline", Oracle: "delivery.txt bytes"}, {ID: "AC-002", Text: "Missing committed marker is rejected", Oracle: "git show exits nonzero"}}
	for _, handler := range []string{"engine.readiness.plan.v6", "engine.development.batch.v6", "engine.gate.v3"} {
		t.Run(handler, func(t *testing.T) {
			input := protocol.WorkerInput{InputCodec: protocol.WorkerInputCodec, OutputCodec: worker.OutputCodec, Handler: handler, Provider: "engine", Pending: protocol.PendingAction{ActionID: "issued", AttemptID: "attempt"}, RequirementPoints: points}
			switch handler {
			case "engine.readiness.plan.v6":
				input.Review = &protocol.WorkerReviewInput{Action: review.StartReadiness}
			case "engine.development.batch.v6":
				input.Validation = &protocol.ValidationInput{Handler: handler, Workspace: &protocol.NativeResource{Path: t.TempDir()}, Development: &protocol.DevelopmentInput{Batch: protocol.DevelopmentBatch{ID: "marker", RequirementPoints: requirement.PointIDs(points), DoD: []protocol.DevelopmentCriterion{{ID: "marker", Description: "Exact delivery marker content"}}}}}
			case "engine.gate.v3":
				input.Validation = &protocol.ValidationInput{Handler: handler, Gate: &protocol.GateInput{Gate: "quality"}}
			}
			var facts []worker.LocationFact
			payload, err := portableWorkerResult(input, map[string]bool{}, &facts)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			paths := []string{}
			if input.Validation != nil && input.Validation.Workspace != nil {
				entries, err := os.ReadDir(input.Validation.Workspace.Path)
				if err != nil {
					t.Fatal(err)
				}
				for _, entry := range entries {
					if !entry.IsDir() {
						paths = append(paths, entry.Name())
					}
				}
			}
			event, err := worker.DecodeResultWithFacts(input, "result", raw, "fixture-output", paths, facts)
			if err != nil {
				t.Fatalf("portable semantic fixture rejected for issued points: %v; payload=%s", err, raw)
			}
			switch handler {
			case "engine.readiness.plan.v6":
				if got := event.Result.Review.GranularityReview.Batches[0].RequirementPoints; len(got) != 2 || got[0] != points[0].ID || got[1] != points[1].ID {
					t.Fatal("portable plan lost issued points", got)
				}
			case "engine.development.batch.v6":
				got := event.Result.Validation.Development.Completion.RequirementResults
				if len(got) != 2 || got[0].PointID != points[0].ID || got[1].PointID != points[1].ID || got[0].Evidence == got[1].Evidence {
					t.Fatal("portable completion lost distinct original evidence", got)
				}
			case "engine.gate.v3":
				got := event.Result.Validation.RequirementResults
				if len(got) != 2 || got[0].PointID != points[0].ID || got[1].PointID != points[1].ID || got[0].Evidence == got[1].Evidence {
					t.Fatal("portable gate lost distinct independent judgments", got)
				}
			}
		})
	}
}
