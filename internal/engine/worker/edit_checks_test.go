package worker

import (
	"encoding/json"
	"reflect"
	"testing"

	"formal-gates/internal/engine/protocol"
)

func TestCompletedRepairCarriesApprovedEditChecksWithoutCopyingCommands(t *testing.T) {
	checks := []protocol.DevelopmentCheck{{ID: "low-cost", Purpose: protocol.CheckEdit, Command: "project-check 'exact value'"}, {ID: "tests", Purpose: protocol.CheckFullTests, Command: "project-tests"}}
	input := protocol.WorkerInput{Handler: "engine.repair.v4", Provider: "host", Pending: protocol.PendingAction{ActionID: "repair"}, ApprovedChecks: checks, Validation: &protocol.ValidationInput{Repair: &protocol.RepairInput{}}}
	value := Repair{Summary: "Restored behavior", Completion: &Completion{DoD: []string{"Behavior restored"}, Handoff: "Ready"}, GranularityReview: &Granularity{Reason: "One repair", VerificationTier: "FINAL", Batches: []Batch{{DoD: []Criterion{{Description: "Behavior restored"}}}}}}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	original, err := DecodeResultWithPaths(input, "result", raw, "original-source", []string{"source.ext"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(original.Result.Validation.GranularityReview.Batches[0].Checks, checks) {
		t.Fatal("approved EDIT or FULL check was not inherited")
	}
	stored, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var recovered protocol.WorkerInput
	if err := json.Unmarshal(stored, &recovered); err != nil {
		t.Fatal(err)
	}
	replay, err := DecodeResultWithPaths(recovered, "result", raw, "original-source", []string{"source.ext"})
	if err != nil || !reflect.DeepEqual(original, replay) {
		t.Fatalf("restored check contract changed: %v", err)
	}
}
