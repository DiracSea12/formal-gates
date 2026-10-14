package worker

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
)

func TestPlanAllocatesIdentitiesAndMapsSemanticChoices(t *testing.T) {
	owner := 1
	value := &Granularity{Reason: "Independent responsibilities", VerificationTier: "CHECKPOINT", CheckpointGroups: [][]int{{1}, {2}}, Batches: []Batch{{Owner: &owner, Subtasks: []Subtask{{Description: "Create public behavior"}}, DoD: []Criterion{{Description: "Public oracle holds"}}, Checks: []Check{{Purpose: protocol.CheckFullTests, Command: "project-tests"}}, Route: &RouteProposal{Mode: "custom", Selected: []int{2}}}, {DependsOn: []int{1}, Subtasks: []Subtask{{Description: "Integrate output"}}}}}
	input := protocol.WorkerInput{AvailableRouteCandidates: []string{"quality", "diff"}}
	before, _ := json.Marshal(value)
	plan, err := DecodeGranularity(input, value, nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Batches[0].ID == "" || plan.Batches[0].Subtasks[0].ID == "" || plan.Batches[0].DoD[0].ID == "" || plan.Batches[0].Checks[0].ID == "" || plan.Batches[0].Owner != "slice-1" || !reflect.DeepEqual(plan.Batches[1].DependsOn, []string{plan.Batches[0].ID}) || !reflect.DeepEqual(plan.CheckpointGroups, [][]string{{plan.Batches[0].ID}, {plan.Batches[1].ID}}) || !reflect.DeepEqual(plan.Batches[0].Route.Gates, input.AvailableRouteCandidates) || !reflect.DeepEqual(plan.Batches[0].Route.Selected, []string{"diff"}) {
		t.Fatal("program identity or semantic choice mapping lost")
	}
	after, _ := json.Marshal(value)
	if string(before) != string(after) {
		t.Fatal("identity assembly mutated semantic output")
	}
	value.Batches[1].DependsOn = []int{3}
	if _, err := DecodeGranularity(input, value, nil, nil, false); err == nil {
		t.Fatal("unknown batch selector accepted")
	}
	value.Batches[1].DependsOn = []int{1}
	value.Batches[0].Route.Selected = []int{3}
	if _, err := DecodeGranularity(input, value, nil, nil, false); err == nil {
		t.Fatal("unknown gate selector accepted")
	}
}

func TestRepairRoutesPreserveCompleteAndSubsetMeaning(t *testing.T) {
	input := protocol.WorkerInput{AvailableRouteCandidates: []string{"quality", "complexity"}}
	batch := Batch{Subtasks: []Subtask{{Description: "Restore public behavior"}}, NewArtifacts: []string{"command.ext"}, Deliverable: "Restored command", PublicEntry: "command", Interfaces: "Public command contract", StateBoundary: "Command files", Risk: "Local behavior", DoD: []Criterion{{Description: "Public oracle holds"}}, Checks: []Check{{Command: "check-command"}}, Rollback: "Restore original file", Recovery: "Continue the issued repair", Handoff: "Ready for native verification", ContextCost: "One command"}
	for _, sample := range []struct {
		name     string
		route    RouteProposal
		selected []string
		invalid  bool
	}{
		{name: "full", route: RouteProposal{Mode: "full"}},
		{name: "custom", route: RouteProposal{Mode: "custom", Selected: []int{2}}, selected: []string{"complexity"}},
		{name: "full-subset", route: RouteProposal{Mode: "full", Selected: []int{1, 2}}, invalid: true},
		{name: "custom-empty", route: RouteProposal{Mode: "custom"}},
	} {
		t.Run(sample.name, func(t *testing.T) {
			batch.Route = &sample.route
			value := &Granularity{Reason: "One cohesive repair", VerificationTier: "FINAL", Batches: []Batch{batch}}
			before, _ := json.Marshal(value)
			plan, err := DecodeGranularity(input, value, nil, nil, false)
			if err != nil {
				t.Fatal(err)
			}
			err = protocol.ValidateRepairPlan(plan, false)
			if (err != nil) != sample.invalid {
				t.Fatalf("route meaning changed: %v", err)
			}
			if sample.name == "full-subset" && !strings.Contains(err.Error(), "full route selects the complete gate list without a subset") {
				t.Fatalf("full subset lost its original rejection: %v", err)
			}
			if !sample.invalid && (!slices.Equal(plan.Batches[0].Route.Gates, input.AvailableRouteCandidates) || !slices.Equal(plan.Batches[0].Route.Selected, sample.selected)) {
				t.Fatal("program changed the issued complete gate list or semantic subset")
			}
			after, _ := json.Marshal(value)
			if string(before) != string(after) {
				t.Fatal("route assembly rewrote authored semantic output")
			}
			stored, _ := json.Marshal(input)
			var recovered protocol.WorkerInput
			if err := json.Unmarshal(stored, &recovered); err != nil {
				t.Fatal(err)
			}
			replay, err := DecodeGranularity(recovered, value, nil, nil, false)
			if err != nil || !reflect.DeepEqual(plan, replay) {
				t.Fatalf("recovery changed route assembly: %v", err)
			}
		})
	}
}

func TestScopeIncludesMandatoryCasesWithoutModelRepetition(t *testing.T) {
	input := &protocol.ObservationInput{Scopes: []protocol.ObservationScope{{Mode: coverage.ReviewBlackbox, ApprovedCases: []string{"required", "selected", "stable"}, MustExecute: []string{"required"}}}}
	judgments := &ObservationJudgments{Findings: []FindingJudgment{}, Scopes: []ScopeJudgment{{Cases: []int{2}, Reason: "Related behavior changed"}}, GateImpacts: []ImpactJudgment{}}
	rows, err := assembleObservations(input, judgments)
	if err != nil || !reflect.DeepEqual(rows[0].Validation.AffectedCases, []string{"selected", "required"}) {
		t.Fatal("mandatory execution did not come from its original owner", err)
	}
	if !reflect.DeepEqual(judgments.Scopes[0].Cases, []int{2}) {
		t.Fatal("program changed authored selection")
	}
}

func TestProposalArtifactsReuseOriginalLocationsAndAuthorOnlyNewNames(t *testing.T) {
	input := protocol.WorkerInput{}
	facts := []LocationFact{{Kind: "listing", Paths: []string{"existing/source.ext", "other/source.ext"}}}
	value := &Granularity{Reason: "One boundary", VerificationTier: "FINAL", Batches: []Batch{{Artifacts: []Location{{Listing: 1, Entry: 2}}, NewArtifacts: []string{"new/source.ext"}, Checks: []Check{{Purpose: protocol.CheckFullTests, Command: "original-project-test"}}}}}
	plan, err := DecodeGranularity(input, value, nil, facts, false)
	if err != nil || !reflect.DeepEqual(plan.Batches[0].Artifacts, []string{"other/source.ext", "new/source.ext"}) || plan.Batches[0].Checks[0].Command != "original-project-test" || plan.Batches[0].Checks[0].Purpose != protocol.CheckFullTests {
		t.Fatalf("proposal lost source selection or original authorship: %v %+v", err, plan)
	}
	value.Batches[0].NewArtifacts = []string{"existing/source.ext"}
	if _, err := DecodeGranularity(input, value, nil, facts, false); err == nil {
		t.Fatal("known source path was accepted as a new artifact")
	}
	value.Batches[0].NewArtifacts = nil
	value.Batches[0].Artifacts = []Location{{Listing: 1, Entry: 3}}
	if _, err := DecodeGranularity(input, value, nil, facts, false); err == nil {
		t.Fatal("unpublished source selection was accepted")
	}
}

func TestDevelopmentPathsComeOnlyFromTerminalFacts(t *testing.T) {
	input := protocol.WorkerInput{InputCodec: protocol.WorkerInputCodec, OutputCodec: OutputCodec, Handler: "engine.development.batch.v6", Provider: "host", Pending: protocol.PendingAction{ActionID: "edit"}, Validation: &protocol.ValidationInput{Development: &protocol.DevelopmentInput{Batch: protocol.DevelopmentBatch{ID: "issued"}}}}
	raw := []byte(`{"summary":"Created the authorized behavior","remainingWork":"Finish the oracle"}`)
	event, err := DecodeResultWithPaths(input, "result", raw, "native-source", []string{"arbitrary-language/source.ext"})
	if err != nil || !reflect.DeepEqual(event.Result.Validation.Development.Paths, []string{"arbitrary-language/source.ext"}) || event.Result.Validation.Status != "CONTINUE" {
		t.Fatal("terminal path facts not assembled", err)
	}
	for _, body := range []string{`{"paths":["guessed"],"summary":"progress","remainingWork":"pending"}`, `{"status":"CONTINUE","summary":"progress","remainingWork":"pending"}`} {
		if _, err := DecodeResultWithPaths(input, "result", []byte(body), "native-source", []string{"real"}); err == nil {
			t.Fatal("manual mechanical field accepted")
		}
	}
}

func TestVerificationGoalsRetainDistinctObligations(t *testing.T) {
	input := protocol.WorkerInput{Handler: "engine.readiness.plan.v6", Review: &protocol.WorkerReviewInput{Action: review.StartReadiness}}
	value := &Granularity{Batches: []Batch{{Checks: []Check{{Purpose: protocol.CheckFullTests, Goal: "Verify malformed options"}, {Purpose: protocol.CheckFullTests, Goal: "Verify stored state"}}}}}
	plan, err := DecodeGranularity(input, value, nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	checks := plan.Batches[0].Checks
	if len(checks) != 2 || checks[0].Goal != value.Batches[0].Checks[0].Goal || checks[1].Goal != value.Batches[0].Checks[1].Goal || checks[0].ID != "batch-1-check-1" || checks[1].ID != "batch-1-check-2" {
		t.Fatalf("distinct goals lost their original duties or allocated identities: %+v", checks)
	}
	input = protocol.WorkerInput{Handler: "engine.repair.v7"}
	value.Batches[0].Checks = []Check{{Command: "original-call"}, {Command: "original-call"}}
	plan, err = DecodeGranularity(input, value, nil, nil, false)
	if err != nil || len(plan.Batches[0].Checks) != 1 {
		t.Fatalf("equal existing calls changed their deduplication: %v", err)
	}
}
