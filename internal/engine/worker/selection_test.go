package worker

import (
	"encoding/json"
	"reflect"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
)

func TestPlanAllocatesIdentitiesAndMapsSemanticChoices(t *testing.T) {
	owner := 1
	value := &Granularity{Reason: "Independent responsibilities", VerificationTier: "CHECKPOINT", CheckpointGroups: [][]int{{1}, {2}}, Batches: []Batch{{Owner: &owner, Subtasks: []Subtask{{Description: "Create public behavior"}}, DoD: []Criterion{{Description: "Public oracle holds"}}, Checks: []Check{{Purpose: protocol.CheckFullTests, Command: "project-tests"}}, Route: &RouteProposal{Mode: "custom", Selected: []int{2}}}, {DependsOn: []int{1}, Subtasks: []Subtask{{Description: "Integrate output"}}}}}
	input := protocol.WorkerInput{AvailableRouteCandidates: []string{"quality", "diff"}}
	before, _ := json.Marshal(value)
	plan, err := DecodeGranularity(input, value)
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
	if _, err := DecodeGranularity(input, value); err == nil {
		t.Fatal("unknown batch selector accepted")
	}
	value.Batches[1].DependsOn = []int{1}
	value.Batches[0].Route.Selected = []int{3}
	if _, err := DecodeGranularity(input, value); err == nil {
		t.Fatal("unknown gate selector accepted")
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

func TestDevelopmentPathsComeOnlyFromTerminalFacts(t *testing.T) {
	input := protocol.WorkerInput{Handler: "engine.development.batch", Provider: "host", Pending: protocol.PendingAction{ActionID: "edit"}, Validation: &protocol.ValidationInput{Development: &protocol.DevelopmentInput{Batch: protocol.DevelopmentBatch{ID: "issued"}}}}
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
