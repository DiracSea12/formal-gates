package facade

import (
	"encoding/json"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/runtime"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDevelopmentIncrementMaterialFollowsIssuedTask(t *testing.T) {
	f := facadePreparation(t, []string{"blackbox"})
	run, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	claim := facadeExecution(t, f, "development-worker").Claim
	original := claim
	run.Regular.Plan.Batches = append([]protocol.DevelopmentBatch{{ID: "unrelated"}}, run.Regular.Plan.Batches...)
	first, second := claim.Task, claim.Task
	first.Scope += "-increment-first"
	second.Scope += "-increment-second"
	run.Regular.DevelopmentIncrements = []protocol.DevelopmentIncrementReceipt{
		{ID: "first", DecisionEvent: "confirmed-first", RequirementRevision: "revision-first", Kind: "optimize", Reason: "First confirmed scope; read references/engine-regular.md", Tasks: map[string]runtime.TaskKey{"B1": first}, PreviousPlan: protocol.DevelopmentPlan{Digest: "unrelated old plan"}},
		{ID: "second", DecisionEvent: "confirmed-second", RequirementRevision: "revision-second", Kind: "optimize", Reason: "Second confirmed scope", Tasks: map[string]runtime.TaskKey{"B1": second}},
	}
	for _, task := range []runtime.TaskKey{first, second} {
		for _, operation := range []string{"spawn", "resume", "replace"} {
			claim.Task, claim.Operation, claim.Identity = task, operation, "same-native"
			payload, workspace, err := preparationInput(run, claim)
			if err != nil {
				t.Fatal(err)
			}
			if payload["batch"].(protocol.DevelopmentBatch).ID != "B1" {
				t.Fatal("increment received another unfinished Batch")
			}
			job := t.TempDir()
			material := nativePromptMaterial{instructions: "Read the bound material.", resumeInstructions: "Continue with current material.", input: payload, workspace: workspace}
			if err := material.bindFiles(job, run.Regular.Requirement); err != nil {
				t.Fatal(err)
			}
			if err := material.writeFiles(job); err != nil {
				t.Fatal(err)
			}
			prompt, err := material.render(claim)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(job, "development-increment.json")
			if !strings.Contains(prompt, path) || strings.Contains(prompt, "confirmed scope") || strings.Contains(prompt, "prompt.txt") {
				t.Fatalf("wrong material entry: %s", prompt)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var got map[string]any
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatal(err)
			}
			want := "first"
			if task == second {
				want = "second"
			}
			if got["id"] != want || got["decisionEvent"] != "confirmed-"+want || got["requirementRevision"] != "revision-"+want || got["reason"] == "" || strings.Contains(string(data), "unrelated old plan") {
				t.Fatalf("wrong increment: %s", data)
			}
			// Later decisions do not rewrite a material file already bound to this action.
			run.Regular.DevelopmentIncrements = append(run.Regular.DevelopmentIncrements, protocol.DevelopmentIncrementReceipt{ID: "unrelated"})
			if err := material.writeFiles(job); err != nil {
				t.Fatal(err)
			}
		}
	}
	payload, _, err := preparationInput(run, original)
	if err != nil {
		t.Fatal(err)
	}
	if _, present := payload["developmentIncrement"]; present {
		t.Fatal("ordinary Batch received increment history")
	}
	claim.Task.Scope = "B1-increment-feature-0"
	if _, _, err := preparationInput(run, claim); err != nil {
		t.Fatalf("ordinary Batch name treated as increment: %v", err)
	}
	claim.Task.Scope = "B1-increment-" + strings.Repeat("a", 64)
	if _, _, err := preparationInput(run, claim); err == nil || !strings.Contains(err.Error(), "confirmed receipt missing") {
		t.Fatalf("missing receipt accepted: %v", err)
	}
	job := filepath.Join(t.TempDir(), "not-created")
	if _, err := f.prepareNativeActionInput(run, claim, run.Executions[claim.ActionID], job); err == nil || !strings.Contains(err.Error(), "confirmed receipt missing") {
		t.Fatalf("native preflight did not reject missing receipt: %v", err)
	}
	if _, err := os.Stat(job); !os.IsNotExist(err) {
		t.Fatalf("rejected input allocated job: %v", err)
	}
}
