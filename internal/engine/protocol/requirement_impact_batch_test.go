package protocol

import (
	"reflect"
	"testing"
)

func TestRequirementImpactRetainsCompletedBatchAndContinuesNext(t *testing.T) {
	e := preparationEngine(t)
	gitReceipt(t, e, "base")
	completeBatch(t, e, "B1")
	gitReceipt(t, e, "commit-B1")
	before, _ := e.Load()
	original := before.State.Regular.Preparation.Batches["B1"]
	originalTask := before.State.RegularExecutions[original.ActionID].Claim.Task
	originalAttempts := before.State.Attempts[originalTask]
	pending := currentExecution(t, e, "development-worker")
	submitRegular(t, e, RegularEvent{Operation: "REQUEST", Request: &RegularRequest{Action: "requirement-change", Origin: "USER_INITIATED_CHANGE", Reason: "clarify later fixture work, preserving B1"}})
	snap, _ := e.Load()
	a := snap.State.Regular.Ask
	choices := map[string]string{}
	for _, item := range a.ImpactItems {
		if item.CanKeep {
			choices[item.ID] = "keep"
		} else {
			choices[item.ID] = "delegate"
		}
	}
	submitRegular(t, e, RegularEvent{Operation: "DECIDE", Decision: &RegularDecision{RequestID: a.ID, Token: RegularFreshness(snap.Revision, a.ID), Digest: a.Digest, Choice: "confirm", Dispositions: choices}})
	doc := Document("requirements.md", []byte(regularDocument+"\nClarify later fixture delivery.\n"))
	regularChoice(t, e, RegularRequest{Action: "apply-requirement", Document: &doc, Origin: "USER_INITIATED_CHANGE", Meaning: "preserved", Reason: "retain user-selected completed evidence"}, "confirm")
	changed, _ := e.Load()
	r := changed.State.Regular
	for _, name := range reviewActions {
		if effectiveReview(r, r.Reviews[name]) != "PASS" || r.Reviews[name].Completed != before.State.Regular.Reviews[name].Completed {
			t.Fatalf("retained %s was rerun", name)
		}
	}
	regularChoice(t, e, RegularRequest{Action: "topology", PlanDigest: r.Plan.Digest, Reason: "same no-split topology after candidate confirmation"}, "confirm")
	after, _ := e.Load()
	p := after.State.Regular.Preparation
	if !reflect.DeepEqual(p.Batches["B1"], original) || p.Commits["B1"].After != "commit-B1" || batchTask(p, "B1") != originalTask || !reflect.DeepEqual(after.State.Attempts[originalTask], originalAttempts) {
		t.Fatal("completed batch history, lineage, commit or attempts changed")
	}
	if len(after.State.Regular.Changes) == 0 || after.State.Regular.Changes[0].UserDisposition.Choices["batch:B1"] != "keep" {
		t.Fatal("user decision receipt missing")
	}
	next := currentExecution(t, e, "development-worker")
	if next.Claim.Task != pending.Claim.Task || next.Claim.RequirementRevision != doc.Revision || next.Claim.ActionID == pending.Claim.ActionID {
		t.Fatal("unstarted obsolete action was not replaced on its original lineage")
	}
	old := after.State.RegularExecutions[pending.Claim.ActionID]
	if !old.Finished || old.SupersededBy == "" || old.PreparationResult != nil {
		t.Fatal("obsolete action history was lost or fabricated as a failure")
	}
}
