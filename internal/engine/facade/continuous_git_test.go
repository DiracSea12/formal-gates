package facade

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"formal-gates/internal/engine/protocol"
)

func TestContinuousConsumesConsecutiveGitPreparations(t *testing.T) {
	f, a := continuousPreparationFixtureQA(t, []string{"whitebox"})
	run, _ := f.Show()
	dev := run.Regular.Preparation.Resources["development"]
	writePreparationFile(t, dev, "result.txt", "fixture result\n")
	data, err := os.ReadFile(filepath.Join(dev, "result.txt"))
	if err != nil || string(data) != "fixture result\n" {
		t.Fatal("fixture validation", err)
	}
	x := facadeExecution(t, f, "development-worker")
	preparationSubmit(t, f, protocol.RegularEvent{Operation: "CLAIM", Claim: &x.Claim})
	src := protocol.ExecutionSource{ActionID: x.Claim.ActionID, AttemptID: x.Claim.AttemptID, Provider: x.Claim.Provider, Identity: "fixture-developer", Status: "RUNNING"}
	preparationSubmit(t, f, protocol.RegularEvent{Operation: "SOURCE", Source: &src})
	result := protocol.PreparationResult{ActionID: x.Claim.ActionID, AttemptID: x.Claim.AttemptID, Provider: x.Claim.Provider, Identity: src.Identity, RequirementRevision: x.Claim.RequirementRevision, Candidate: x.Claim.Candidate, Status: "PASS", Manifest: []string{"result.txt"}, Handoff: "none", Validation: []protocol.BatchValidation{{SubtaskID: "S1", Procedure: "read result.txt", Observation: string(data), Result: "PASS"}}}
	preparationSubmit(t, f, protocol.RegularEvent{Operation: "PREPARATION_RESULT", PreparationResult: &result})
	nativeFixtureStop(t, f, src)
	before, _ := f.Show()
	if before.Regular.Preparation.Commits["B1"].After != "" {
		t.Fatal("fixture already committed")
	}
	// Stop at a real adapter boundary only after the engine has consumed both Git
	// operations. No real provider or model is invoked by this test.
	a.beforeCapture = func() error { return fmt.Errorf("fixture capability unavailable") }
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	got, err := f.DriveContinuous(ctx)
	if err != nil || got.Boundary != "BLOCKED" || len(got.Blocked) != 1 {
		t.Fatalf("boundary=%s blocked=%v error=%v", got.Boundary, got.Blocked, err)
	}
	p := got.Run.Regular.Preparation
	receipts := p.Receipts[len(before.Regular.Preparation.Receipts):]
	if len(receipts) != 2 || receipts[0].Operation.Kind != "batch-commit" || receipts[1].Operation.Kind != "whitebox-workspace" || p.Operation != nil || p.Commits["B1"].After == "" {
		t.Fatalf("consecutive Git preparations not consumed: %+v", receipts)
	}
	data, err = os.ReadFile(filepath.Join(p.Resources["whitebox"], "result.txt"))
	if err != nil || string(data) != "fixture result\n" {
		t.Fatalf("whitebox does not contain committed candidate: %q %v", data, err)
	}
	for id := range got.Blocked {
		if got.Run.Executions[id].Claimed || got.Run.Executions[id].Claim.Task.Step != "whitebox-design" {
			t.Fatal("wrong boundary or capability failure consumed claim")
		}
	}
	f, err = Open(f.root, f.runID)
	if err != nil {
		t.Fatal(err)
	}
	again, err := f.DriveContinuous(ctx)
	if err != nil || again.Boundary != "BLOCKED" || again.Run.Revision != got.Run.Revision || len(again.Run.Regular.Preparation.Receipts) != len(p.Receipts) {
		t.Fatalf("restart repeated Git or spun at boundary: %s %v", again.Boundary, err)
	}
}
