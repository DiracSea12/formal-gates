package facade

import (
	"encoding/json"
	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/protocol"
	"os"
	"path/filepath"
	"testing"
	"unicode/utf8"
)

// One small fixture exercises the production assembly; no model calls or active
// run state are used. Logs measure characters, not token or elapsed-time savings.
func TestStageInputAssemblyMeasurement(t *testing.T) {
	f := frozenVerificationFixture(t, false)
	run, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	claim := facadeExecution(t, f, "blackbox-execution").Claim
	p := run.Regular.Preparation
	p.QA["whitebox"] = p.QA["blackbox"]
	for i := range run.Regular.Verification.Waves {
		wave := &run.Regular.Verification.Waves[i]
		if wave.ID == run.Regular.Verification.Current {
			for _, target := range wave.Targets {
				if target.Target == "blackbox" {
					target.Target = "whitebox"
					wave.Targets = append(wave.Targets, target)
					break
				}
			}
		}
	}
	p.Resources["whitebox"] = p.Resources["candidate"]
	repair := protocol.RepairPlan{ID: "repair-fixture", WaveID: run.Regular.Verification.Current}
	run.Regular.Verification.Repairs = []protocol.RepairPlan{repair}
	p.Resources[repair.ID] = p.Resources["candidate"]

	for _, step := range []string{"product-review", "start-readiness", "development-worker", "blackbox-design", "whitebox-design", "blackbox-review", "whitebox-review", "blackbox-execution", "whitebox-execution", "complexity-gate", "implementation-quality-gate", "repair-worker"} {
		t.Run(step, func(t *testing.T) {
			current := claim
			current.Task.Step = authoring.StepID(step)
			job := t.TempDir()
			var input nativeActionInput
			var err error
			if step == "blackbox-review" || step == "whitebox-review" {
				// Measure the same assembly without asking this synthetic shared fixture
				// to claim a second designer identity or create another QA workflow.
				input.material, err = preparePreparationPrompt(run, current)
				input.schema = preparationSchema(step, run, current)
				if err == nil {
					input.toolDescription, err = actionInstructions(run, "actions/development-worker.md#工具说明")
				}
				if err == nil {
					err = input.material.bindFiles(job, run.Regular.Requirement)
				}
			} else {
				input, err = f.prepareNativeActionInput(run, current, protocol.RegularExecution{}, job)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = input.preflight(current, codexRuntimeAdapter{}); err != nil {
				t.Fatal(err)
			}
			if err = input.material.writeFiles(job); err != nil {
				t.Fatal(err)
			}
			for name, want := range input.material.files {
				got, err := os.ReadFile(filepath.Join(job, name))
				if err != nil || string(got) != string(want) {
					t.Fatalf("unreadable bound material %s: %v", name, err)
				}
			}
			schema, err := json.Marshal(input.schema)
			if err != nil {
				t.Fatal(err)
			}
			tool, err := json.Marshal(nativeOperationTool(step, input.toolDescription))
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("assembled step=%s message=%d schema=%d tool=%d files=%d", step, utf8.RuneCountInString(input.prompt), utf8.RuneCount(schema), utf8.RuneCount(tool), len(input.material.files))
			if input.coverage != nil {
				schema, err := json.Marshal(input.coverage.schema)
				if err != nil {
					t.Fatal(err)
				}
				// The coverage transport declares no workflow tools; the
				// Codex protocol endpoint test verifies the actual request.
				t.Logf("assembled step=whitebox-coverage message=%d schema=%d tool=0", utf8.RuneCountInString(input.coverage.prompt), utf8.RuneCount(schema))
			}
		})
	}
}

func TestCapturedUnsplitInstructionsRemainReadable(t *testing.T) {
	f := facadePreparation(t, []string{"blackbox", "whitebox"})
	run, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	claim := facadeExecution(t, f, "blackbox-design").Claim
	run.Regular.Preparation.Resources["whitebox"] = t.TempDir()
	const oldTool = "# 工具说明\nCaptured model and QA operations.\n"
	const oldReview = "# QA 审查\nCaptured review.\n\n## 白盒改动与最终候选\nCaptured test ownership.\n"
	run.Regular.ActionInstructions["actions/development-worker.md"] = "# 通用执行约定\nCaptured common.\n\n" + oldTool
	run.Regular.ActionInstructions["actions/qa-review.md"] = oldReview
	for _, step := range []string{"blackbox-review", "whitebox-review"} {
		claim.Task.Step = authoring.StepID(step)
		material, err := preparePreparationPrompt(run, claim)
		if err != nil {
			t.Fatal(err)
		}
		if material.instructions != "# 通用执行约定\nCaptured common.\n\n\n"+oldReview {
			t.Fatalf("captured instructions changed: %s", material.instructions)
		}
	}
	for _, step := range []string{"development-worker", "blackbox-execution", "repair-worker"} {
		got, err := nativeOperationInstructions(run, step)
		if err != nil || got != oldTool {
			t.Fatalf("captured tool instructions changed: %q %v", got, err)
		}
	}
}
