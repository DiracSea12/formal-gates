package protocol

import (
	"encoding/json"
	"reflect"
	"testing"

	"formal-gates/internal/engine/review"
)

func TestChildWorkerScopePreservesConfirmedResponsibilitiesOnRecovery(t *testing.T) {
	e, state, pending := unfinishedDevelopmentState(t, "engine.development.batch.v3")
	state.SliceChild = &SliceChildBinding{SliceID: "slice-2"}
	state.Topology = &TopologyBinding{Decision: "split", ACOwners: map[string]string{"AC-001": "slice-1", "AC-002": "slice-2", "AC-003": "master-merge", "AC-004": "master-merge"}}
	want := &WorkerScope{Owner: "slice-2", AcceptanceOwners: map[string]string{"AC-001": "slice-1", "AC-002": "slice-2", "AC-003": "master-merge", "AC-004": "master-merge"}}
	input := state.WorkerInputs(e.cfg.Definition)[pending.ActionID]
	if !reflect.DeepEqual(input.Scope, want) || input.RequirementDocument.Content != state.Validation.Document.Content {
		t.Fatalf("child lacks its original responsibility or complete requirement: %+v", input)
	}
	stored, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	var recovered State
	if err := json.Unmarshal(stored, &recovered); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(input, recovered.WorkerInputs(e.cfg.Definition)[pending.ActionID]) {
		t.Fatal("recovery changed the issued responsibility projection")
	}
	input.Scope.AcceptanceOwners["AC-001"] = "slice-2"
	if state.Topology.ACOwners["AC-001"] != "slice-1" || !reflect.DeepEqual(state.WorkerInputs(e.cfg.Definition)[pending.ActionID].Scope, want) {
		t.Fatal("worker scope changed the authoritative topology")
	}
	state.SliceChild = nil
	if state.WorkerInputs(e.cfg.Definition)[pending.ActionID].Scope != nil {
		t.Fatal("merge worker narrowed full-project acceptance to master-owned conditions")
	}
}

func TestRepairWorkerChecksUseTheConfirmedProjectOrSlicePlan(t *testing.T) {
	for _, child := range []bool{false, true} {
		e, state, pending := unfinishedDevelopmentState(t, "engine.repair.v3")
		plan := plannedReviewEvidence(review.StartReadiness, "readiness", "", "PASS", "Approved project plan", nil).GranularityReview
		plan.Batches[0].ID = "owned"
		plan.Batches[0].Checks = []DevelopmentCheck{{ID: "build", Purpose: CheckFullBuild, Command: "complete-build"}, {ID: "tests", Purpose: CheckFullTests, Command: "complete-tests"}, {ID: "local", Command: "local-oracle"}}
		second := plan.Batches[0]
		second.ID = "independent"
		second.Subtasks = []DevelopmentSubtask{{ID: "independent-body", Description: "Independent local behavior"}}
		second.Checks = []DevelopmentCheck{{ID: "local", Command: "independent-local-oracle"}}
		plan.Batches = append(plan.Batches, second)
		if err := ValidateGranularity(plan); err != nil {
			t.Fatal("original per-batch check identities must be legitimate", err)
		}
		if child {
			state.SliceChild = &SliceChildBinding{SliceID: "slice-2", RequirementRevision: "req"}
			state.Topology = &TopologyBinding{Decision: "split", ACOwners: map[string]string{"AC-002": "slice-2"}}
			decision := state.Decisions["route"]
			decision.Review.Route.DevelopmentPlan = &DevelopmentPlanConfirmation{Proposal: plan}
			state.Decisions["route"] = decision
		} else {
			source := state.Results["readiness"]
			source.Review.GranularityReview = plan
			state.Results["readiness"] = source
		}
		input := state.WorkerInputs(e.cfg.Definition)[pending.ActionID]
		if len(input.ApprovedChecks) != 4 {
			t.Fatalf("repair lost confirmed project/slice checks (child=%v): %+v", child, input)
		}
		if input.ApprovedChecks[2].ID == input.ApprovedChecks[3].ID || input.ApprovedChecks[3].Command != "independent-local-oracle" {
			t.Fatal("independent approved local checks lost their distinct batch identity")
		}
		for index, original := range plan.Batches[0].Checks {
			if input.ApprovedChecks[index].Command != original.Command || input.ApprovedChecks[index].Purpose != original.Purpose {
				t.Fatal("repair changed original approved command bytes or purpose")
			}
		}
		stored, err := json.Marshal(state)
		if err != nil {
			t.Fatal(err)
		}
		var recovered State
		if err := json.Unmarshal(stored, &recovered); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(input, recovered.WorkerInputs(e.cfg.Definition)[pending.ActionID]) {
			t.Fatal("recovery changed approved check projection")
		}
		input.ApprovedChecks[0].Command = "not-approved"
		if plan.Batches[0].Checks[0].Command != "complete-build" {
			t.Fatal("worker projection modified authoritative plan")
		}
	}
}
