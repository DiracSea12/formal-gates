package facade

import (
	"formal-gates/internal/engine/protocol"
	"os"
	"path/filepath"
	"testing"
)

func TestStreamCoverageRecoveryRetainsOriginalAnswer(t *testing.T) {
	f := facadePreparation(t, []string{"blackbox", "whitebox"})
	r, _ := f.Show()
	dev := r.Regular.Preparation.Resources["development"]
	writePreparationFile(t, dev, "go.mod", "module example.invalid/hello\n\ngo 1.23\n")
	writePreparationFile(t, dev, "main.go", "package main\nfunc main(){}\n")
	facadeResult(t, f, facadeExecution(t, f, "development-worker"), protocol.PreparationResult{Manifest: []string{"go.mod", "main.go"}, Validation: []protocol.BatchValidation{{SubtaskID: "S1", Procedure: "local predecessor fixture", Observation: "source prepared", Result: "PASS"}}, Handoff: "No unresolved decisions"})
	if _, err := f.Drive(); err != nil {
		t.Fatal(err)
	}
	r, _ = f.Show()
	design := facadeDesign(r, "blackbox")
	facadeResult(t, f, facadeExecution(t, f, "blackbox-design"), protocol.PreparationResult{Design: &design})
	r, _ = f.Show()
	facadeResult(t, f, facadeExecution(t, f, "blackbox-review"), protocol.PreparationResult{Review: facadeReview(r, "blackbox")})
	if _, err := f.Drive(); err != nil {
		t.Fatal(err)
	}
	x := facadeExecution(t, f, "whitebox-design")
	preparationSubmit(t, f, protocol.RegularEvent{Operation: "CLAIM", Claim: &x.Claim})
	preparationSubmit(t, f, protocol.RegularEvent{Operation: "SOURCE", Source: &protocol.ExecutionSource{ActionID: x.Claim.ActionID, AttemptID: x.Claim.AttemptID, Provider: x.Claim.Provider, Identity: "native", Status: "RUNNING"}})
	job := f.codexJob(x.Claim.ActionID)
	if err := os.MkdirAll(job, 0700); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONAtomic(filepath.Join(job, "process.json"), map[string]any{"actionId": x.Claim.ActionID, "identity": "native", "pid": os.Getpid(), "stopped": true, "exitCode": 0}); err != nil {
		t.Fatal(err)
	}
	answer := []byte(`{"coverageJudgment":"independent command behavior","behaviorOracles":[{"behavior":"hello command","oracle":"exact output and exit"}]}`)
	if err := os.WriteFile(filepath.Join(job, "coverage-answer.json"), answer, 0600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		r, _ = f.Show()
		x = r.Executions[x.Claim.ActionID]
		run, complete, err := f.reconcileStreamNative(x.Claim.ActionID, x, nil, func(string, string, error) (Run, error) {
			t.Fatal("coverage consumed as final design")
			return Run{}, nil
		})
		if err != nil || complete || !protocol.WhiteboxCoverageReady(run.Executions[x.Claim.ActionID]) {
			t.Fatalf("complete=%v err=%v", complete, err)
		}
		if len(run.Executions[x.Claim.ActionID].Operations) != 1 {
			t.Fatal("retained coverage rerun")
		}
	}
}
