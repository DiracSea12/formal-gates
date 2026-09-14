package protocol

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/coverage"
)

func TestCoverageResultCorrectionRetainsReceipt(t *testing.T) {
	e := preparationEngine(t, "whitebox")
	gitReceipt(t, e, "base")
	completeBatch(t, e, "B1")
	gitReceipt(t, e, "D1")
	completeBatch(t, e, "B2")
	gitReceipt(t, e, "D2")
	gitReceipt(t, e, "D2")
	x := currentExecution(t, e, "whitebox-design")
	claimAndAttach(t, e, x)
	recordCoverage(t, e, x)
	before, _ := e.Load()
	original := before.State.RegularExecutions[x.Claim.ActionID].Operations
	result := preparationResult(x)
	result.Status = "INVALID"
	submitRegular(t, e, RegularEvent{Operation: "PREPARATION_RESULT", PreparationResult: &result})
	sourceUnavailable(t, e, x)
	next := currentExecution(t, e, "whitebox-design")
	if next.Claim.ActionID == x.Claim.ActionID || !WhiteboxCoverageReady(next) || !reflect.DeepEqual(original, next.Operations) {
		t.Fatalf("coverage not retained verbatim: %+v", next)
	}
	regularChoice(t, e, RegularRequest{Action: "qa-revise", Target: "whitebox", Reason: "changed user instructions", Meaning: "stable-execution"}, "confirm")
	revised := currentExecution(t, e, "whitebox-design")
	if WhiteboxCoverageReady(revised) {
		t.Fatal("changed QA inputs reused coverage")
	}
}

func TestIndependentBaselineQAAllowsDevelopmentIncrement(t *testing.T) {
	e := preparationEngine(t, "blackbox")
	gitReceipt(t, e, "base")
	black := currentExecution(t, e, "blackbox-design")
	claimAndAttach(t, e, black)
	completeBatch(t, e, "B1")
	gitReceipt(t, e, "D1")
	before, _ := e.Load()
	original := before.State.RegularExecutions[black.Claim.ActionID]
	regularChoice(t, e, RegularRequest{Action: "development-increment", Target: "B1", Meaning: "preserved", Reason: "independent implementation correction", Impact: map[string]string{"blackbox-review": "unaffected"}, Increment: &DevelopmentIncrement{Kind: "optimize", BatchID: "B1", WorkspaceView: "fixture"}}, "confirm")
	after, _ := e.Load()
	if !reflect.DeepEqual(original, after.State.RegularExecutions[black.Claim.ActionID]) {
		t.Fatal("live baseline QA changed")
	}
	dev := currentExecution(t, e, "development-worker")
	if dev.Claim.Task == before.State.Regular.Preparation.BatchTasks["B1"] {
		t.Fatal("increment not issued")
	}
	claimAndAttach(t, e, dev)
	after, _ = e.Load()
	q := RegularRequest{Target: "B1", Meaning: "preserved", Increment: &DevelopmentIncrement{Kind: "append", BatchID: "B2", WorkspaceView: "fixture"}}
	if err := validateDevelopmentIncrement(after.State, q); err == nil || !strings.Contains(err.Error(), "shares the development workspace") {
		t.Fatalf("expected concrete shared workspace conflict, got %v", err)
	}
	result := preparationResult(black)
	design := testDesign(after.State.Regular, "BLACKBOX")
	result.Design = &design
	submitRegular(t, e, RegularEvent{Operation: "PREPARATION_RESULT", PreparationResult: &result})
	sourceUnavailable(t, e, black)
	after, _ = e.Load()
	if after.State.Regular.Preparation.QA["blackbox"].Design == nil || after.State.RegularExecutions[dev.Claim.ActionID].Finished {
		t.Fatal("independent baseline result was invalidated or interrupted development")
	}
}

func TestIncrementKeepsUnchangedSharedPreparation(t *testing.T) {
	prior := QADesign{Manifest: coverage.AcceptanceManifest{Cases: []coverage.AcceptanceCase{{CaseID: "WHITEBOX:C1", SharedSteps: []string{"setup"}}}, SharedSteps: []coverage.SharedStep{{ID: "setup"}}}}
	before, _ := json.Marshal(prior)
	delta := QADesign{Manifest: coverage.AcceptanceManifest{Cases: []coverage.AcceptanceCase{{CaseID: "new-one", SharedSteps: []string{"setup"}}, {CaseID: "new-two", SharedSteps: []string{"setup"}}}}}
	merged, ids, err := mergeQADesign(&prior, delta, "whitebox", nil)
	if err != nil {
		t.Fatal(err)
	}
	evidence := []TestReuse{{CaseID: ids["new-one"]}, {CaseID: ids["new-two"]}}
	if err = validateQAReuseIncrement(&prior, merged, evidence, nil); err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(prior)
	if string(before) != string(after) || !reflect.DeepEqual(prior.Manifest.SharedSteps, merged.Manifest.SharedSteps) {
		t.Fatal("unchanged setup edited")
	}
	merged.Manifest.SharedSteps[0].Procedure = "actually changed preparation"
	if validateQAReuseIncrement(&prior, merged, evidence, nil) == nil {
		t.Fatal("changed shared preparation inherited old evidence")
	}
}
