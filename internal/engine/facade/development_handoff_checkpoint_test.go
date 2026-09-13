package facade

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"formal-gates/internal/engine/protocol"
)

func TestExternalHandoffPreservesLastDevelopmentCheckpoint(t *testing.T) {
	f := facadePreparation(t, nil)
	transfer := func(provider string) ExternalDevelopment {
		t.Helper()
		x := facadeExecution(t, f, "development-worker")
		facadeChoice(t, f, protocol.RegularRequest{
			Action: "executor-handoff", Target: x.Claim.ActionID,
			Reason: "local fixture user transfers only this unfinished Batch",
			Handoff: &protocol.ExecutorHandoff{To: protocol.ExecutorTarget{
				Kind: "external", Provider: provider, Label: "explicit local fixture author",
			}},
		})
		next := facadeExecution(t, f, "development-worker")
		input, err := f.PrepareExternalDeveloper(next.Claim.ActionID)
		if err != nil {
			t.Fatal(err)
		}
		return input
	}
	first := transfer("chatgpt")
	writePreparationFile(t, first.Workspace, "checkpoint.txt", "unfinished implementation checkpoint\n")
	c := first.Binding
	checkpoint := protocol.PreparationResult{
		ActionID: c.ActionID, AttemptID: c.AttemptID, Provider: c.Provider,
		Identity: c.Identity, RequirementRevision: c.RequirementRevision, Candidate: c.Candidate,
		Status: "CONTINUE", Message: "checkpoint.txt exists; implementation and final validation remain",
		Manifest: []string{"checkpoint.txt"}, Handoff: "preserve the observed checkpoint before completing this Batch",
		Validation: []protocol.BatchValidation{{SubtaskID: "S1", Procedure: "write and read checkpoint.txt",
			Observation: "local file checkpoint only; no native model or independent QA", Result: "PASS"}},
	}
	before := preparationSubmit(t, f, protocol.RegularEvent{Operation: "PREPARATION_RESULT", PreparationResult: &checkpoint})
	pending := facadeExecution(t, f, "development-worker")
	if pending.Claimed || pending.PreviousAction != c.ActionID {
		t.Fatal("fixture did not reach the normal unclaimed continuation boundary")
	}
	second := transfer("other-fixture-executor")
	if second.Binding.Task != c.Task || second.Binding.Provider == c.Provider || second.Binding.Identity == c.Identity || second.Workspace != first.Workspace {
		t.Fatal("handoff changed workspace/TaskKey or reused the old executor's identity")
	}
	reference, ok := second.Input["previousDevelopmentResult"].(map[string]string)
	if !ok {
		t.Fatalf("handoff must reference the last checkpoint: %#v", second.Input["previousDevelopmentResult"])
	}
	captured, err := os.ReadFile(reference["path"])
	if err != nil {
		t.Fatal(err)
	}
	var previous protocol.PreparationResult
	if err := json.Unmarshal(captured, &previous); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(previous, checkpoint) {
		t.Fatal("handoff changed the last actual development checkpoint")
	}
	after, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before.Executions[c.ActionID], after.Executions[c.ActionID]) {
		t.Fatal("handoff rewrote the old result, source or identity")
	}
	data, err := os.ReadFile(filepath.Join(second.Workspace, "checkpoint.txt"))
	if err != nil || string(data) != "unfinished implementation checkpoint\n" {
		t.Fatal("handoff lost existing working files", err)
	}
	reopened, err := Open(f.root, f.runID)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := reopened.PrepareExternalDeveloper(second.Binding.ActionID)
	if err != nil || !reflect.DeepEqual(replayed.Input["previousDevelopmentResult"], reference) {
		t.Fatal("restart did not recover the same previous checkpoint", err)
	}
	replayedData, err := os.ReadFile(reference["path"])
	if err != nil || string(replayedData) != string(captured) {
		t.Fatal("restart changed captured checkpoint bytes", err)
	}
	t.Log("public external CONTINUE -> unclaimed continuation -> cross-executor handoff preserves original checkpoint, TaskKey, workspace, result and source; no native model calls")
}
