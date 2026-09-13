package protocol

import (
	"reflect"
	"strings"
	"testing"
)

func TestEntryReviewSkipPreservesResultsAndDoesNotInventPASS(t *testing.T) {
	e := preparationEngine(t)
	gitReceipt(t, e, "base")
	before, err := e.Load()
	if err != nil {
		t.Fatal(err)
	}
	pending := currentExecution(t, e, "development-worker")
	for _, name := range reviewActions {
		regularChoice(t, e, RegularRequest{Action: "skip-review", ReviewAction: name, Reason: "user explicitly skips repeated entry review"}, "confirm")
	}
	after, err := e.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range reviewActions {
		old, now := before.State.Regular.Reviews[name], after.State.Regular.Reviews[name]
		if effectiveReview(after.State.Regular, now) != "SKIPPED" || !reviewPrerequisiteSatisfied(after.State.Regular, now) {
			t.Fatalf("%s did not advance as SKIPPED", name)
		}
		if !reflect.DeepEqual(old.RawCandidates, now.RawCandidates) || old.Completed != now.Completed || old.CurrentDigest != now.CurrentDigest {
			t.Fatalf("%s changed original review evidence", name)
		}
		if now.Skip == nil || now.Skip.EventID == "" || now.Skip.Reason == "" {
			t.Fatal("missing explicit user decision binding")
		}
	}
	if after.State.RegularExecutions[pending.Claim.ActionID].Finished {
		t.Fatal("skip retired independent development")
	}
	if !reflect.DeepEqual(before.State.Regular.Plan, after.State.Regular.Plan) || !reflect.DeepEqual(before.State.Regular.Preparation, after.State.Regular.Preparation) {
		t.Fatal("skip changed prepared development")
	}
	if after.State.State.Regular.ProductStatus != "SKIPPED" || after.State.State.Regular.TechnicalStatus != "SKIPPED" {
		t.Fatalf("view hides skip: %+v", after.State.State.Regular)
	}
	for _, item := range requirementImpactItems(after.State.Regular) {
		if (item.ID == "product-review" || item.ID == "start-readiness") && item.CanKeep {
			t.Fatal("skip became reusable PASS evidence")
		}
	}
	after.State.Regular.Requirement.Revision = "another-requirement"
	for _, name := range reviewActions {
		if reviewPrerequisiteSatisfied(after.State.Regular, after.State.Regular.Reviews[name]) {
			t.Fatal("skip leaked into changed requirement")
		}
	}
}
func TestEntryReviewSkipRejectsOtherStagesAndCancelLeavesResults(t *testing.T) {
	e := preparationEngine(t)
	before, _ := e.Load()
	event, err := NewRegularEvent("invalid-review-skip", RegularEvent{Operation: "REQUEST", ExpectedRevision: before.Revision, Request: &RegularRequest{Action: "skip-review", ReviewAction: "blackbox-review", Reason: "not an entry review"}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.Submit(event, fingerprintOf(t, e))
	if err == nil || !strings.Contains(err.Error(), "limited to") {
		t.Fatalf("accepted broader skip: %v", err)
	}
	regularChoice(t, e, RegularRequest{Action: "skip-review", ReviewAction: "product-review", Reason: "cancelled request"}, "cancel")
	after, _ := e.Load()
	if after.State.Regular.Reviews["product-review"].Skip != nil {
		t.Fatal("cancel applied skip")
	}
	if !reflect.DeepEqual(before.State.Regular.Reviews, after.State.Regular.Reviews) {
		t.Fatal("cancel changed reviewer history")
	}
}

func TestEntryReviewSkipStillIssuesGitPreparation(t *testing.T) {
	e := preparationEngine(t)
	gitReceipt(t, e, "base")
	for _, name := range reviewActions {
		regularChoice(t, e, RegularRequest{Action: "skip-review", ReviewAction: name, Reason: "user explicitly skips entry reviews"}, "confirm")
	}
	loaded, err := e.Load()
	if err != nil {
		t.Fatal(err)
	}
	state := loaded.State
	state.Regular.Preparation.Resources = map[string]string{}
	state.Regular.Preparation.Operation = nil
	if !issuePreparationGit(state, loaded.Revision) {
		t.Fatal("skipped entry review blocked Git preparation issuance")
	}
	op := state.Regular.Preparation.Operation
	if op == nil || op.Kind != "workspaces" {
		t.Fatal("missing required workspace preparation")
	}
	if _, ok := state.PendingHostActions[op.ID]; !ok {
		t.Fatal("preparation did not reach executor queue")
	}
}
