package facade

import (
	"fmt"
	"os/exec"
	"reflect"
	"testing"

	"formal-gates/internal/engine/protocol"
)

func TestExternalDeveloperHandoffPublicFlow(t *testing.T) {
	f := facadePreparationBatches(t, []string{"blackbox"}, 2)
	old := facadeExecution(t, f, "development-worker")
	if _, err := f.PrepareExternalDeveloper(old.Claim.ActionID); err == nil {
		t.Fatal("external executor chosen without user authorization")
	}
	q := protocol.RegularRequest{Action: "executor-handoff", Target: old.Claim.ActionID, Reason: "explicit fixture user transfers authoring, not QA", Handoff: &protocol.ExecutorHandoff{To: protocol.ExecutorTarget{Kind: "external", Provider: "chatgpt", Label: "current local test author"}}}
	facadeChoice(t, f, q)
	x := facadeExecution(t, f, "development-worker")
	input, err := f.PrepareExternalDeveloper(x.Claim.ActionID)
	if err != nil {
		t.Fatal(err)
	}
	if input.Binding.Provider != "chatgpt" || input.Binding.Identity == "" || input.Executor.Kind != "external" {
		t.Fatal(input.Binding)
	}
	before, _ := f.Show()
	reopened, err := Open(f.root, f.runID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := reopened.PrepareExternalDeveloper(x.Claim.ActionID)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := f.Show()
	if after.Revision != before.Revision || !reflect.DeepEqual(second.Binding, input.Binding) {
		t.Fatal("reopening external input claimed a second execution")
	}
	writePreparationFile(t, input.Workspace, "go.mod", "module example.invalid/direct-handoff\n\ngo 1.22\n")
	writePreparationFile(t, input.Workspace, "main.go", "package main\nimport \"fmt\"\nfunc main(){fmt.Print(\"hello\")}\n")
	cmd := exec.Command("go", "run", ".")
	cmd.Dir = input.Workspace
	output, err := cmd.CombinedOutput()
	if err != nil || string(output) != "hello" {
		t.Fatalf("%q %v", output, err)
	}
	c := input.Binding
	result := protocol.PreparationResult{ActionID: c.ActionID, AttemptID: c.AttemptID, Provider: c.Provider, Identity: c.Identity, RequirementRevision: c.RequirementRevision, Candidate: c.Candidate, Status: "PASS", Message: "direct external author, not native or independent QA", Manifest: []string{"go.mod", "main.go"}, Validation: []protocol.BatchValidation{{SubtaskID: "S1", Procedure: "go run .", Observation: "stdout exactly hello and exit zero", Result: "PASS"}}, Handoff: "No unresolved fixture decisions"}
	broken := result
	broken.Manifest = []string{"go.mod"}
	current, _ := f.Show()
	ev, _ := protocol.NewRegularEvent(protocol.EventID(fmt.Sprintf("missing-manifest-%d", current.Revision)), protocol.RegularEvent{Operation: "PREPARATION_RESULT", ExpectedRevision: current.Revision, PreparationResult: &broken})
	if _, _, err := f.Submit(ev); err == nil {
		t.Fatal("incomplete external manifest accepted")
	}
	checked, _ := f.Show()
	if checked.Revision != current.Revision {
		t.Fatal("rejected manifest consumed an attempt")
	}
	current, _ = f.Show()
	delivery, _ := protocol.NewRegularEvent("external-delivery-replay", protocol.RegularEvent{Operation: "PREPARATION_RESULT", ExpectedRevision: current.Revision, PreparationResult: &result})
	accepted, run, err := f.Submit(delivery)
	if err != nil || accepted.Status == "REJECTED_RESULT" {
		t.Fatalf("external delivery: %+v %v", accepted, err)
	}
	if run.Regular.Preparation.Batches["B1"].Identity != c.Identity {
		t.Fatalf("external delivery not accepted: %+v", run.Regular.PreparationErrors)
	}
	run, err = f.Drive()
	if err != nil {
		t.Fatal(err)
	}
	if run.Regular.Preparation.Commits["B1"].After == "" {
		t.Fatal("engine did not commit original external delivery")
	}
	if run.Regular.Preparation.QA["blackbox"].Frozen {
		t.Fatal("developer result replaced independent blackbox")
	}
	// The response can be lost after admission and Git commit. Replaying the
	// exact event must consult the existing writer receipt, not re-validate a
	// now-clean workspace as though the original authoring had not happened.
	replayed, same, err := f.Submit(delivery)
	if err != nil || !reflect.DeepEqual(replayed, accepted) || same.Revision != run.Revision || !reflect.DeepEqual(same.Regular.Preparation.Commits, run.Regular.Preparation.Commits) {
		t.Fatalf("accepted delivery was not replayable after commit: %+v %v", replayed, err)
	}
	changed := result
	changed.Message = "conflicting replay must not overwrite accepted history"
	delivery.Regular.PreparationResult = &changed
	if _, _, err := f.Submit(delivery); err == nil {
		t.Fatal("different payload reused an accepted event ID")
	}
	afterConflict, _ := f.Show()
	if afterConflict.Revision != same.Revision {
		t.Fatal("conflicting replay changed state")
	}
}

func TestExecutorHandoffChecksNativeCapabilityBeforeAsk(t *testing.T) {
	f := facadePreparation(t, nil)
	x := facadeExecution(t, f, "development-worker")
	before, _ := f.Show()
	q := protocol.RegularRequest{Action: "executor-handoff", Target: x.Claim.ActionID, Reason: "fixture target is not a supported native runtime", Handoff: &protocol.ExecutorHandoff{To: protocol.ExecutorTarget{Kind: "native", Provider: "unregistered-platform", Label: "test"}}}
	ev, _ := protocol.NewRegularEvent("unavailable-target", protocol.RegularEvent{Operation: "REQUEST", ExpectedRevision: before.Revision, Request: &q})
	if _, _, err := f.Submit(ev); err == nil {
		t.Fatal("unsupported native handoff accepted")
	}
	after, _ := f.Show()
	if before.Revision != after.Revision || after.Regular.Ask != nil {
		t.Fatal("failed capability check modified state")
	}
}

func TestExternalTaskInputDoesNotRequireNativePrompt(t *testing.T) {
	f := facadePreparation(t, nil)
	x := facadeExecution(t, f, "development-worker")
	run, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	copy := *run.Regular
	copy.ActionInstructions = nil
	run.Regular = &copy
	input, workspace, err := preparationInput(run, x.Claim)
	if err != nil || workspace != run.Regular.Preparation.Resources["development"] {
		t.Fatal("external task depends on model prompt configuration", err)
	}
	batch, ok := input["batch"].(protocol.DevelopmentBatch)
	if !ok || batch.ID != "B1" || !reflect.DeepEqual(input["binding"], x.Claim) {
		t.Fatal("structured task lost engine binding")
	}
	doc, ok := input["requirement"].(map[string]any)
	if !ok || doc["text"] != run.Regular.Requirement.Text || doc["revision"] != run.Regular.Requirement.Revision {
		t.Fatal("structured task omitted complete requirement")
	}
	if _, _, err = preparationPrompt(run, x.Claim); err == nil {
		t.Fatal("missing native prompt was silently substituted")
	}
}
