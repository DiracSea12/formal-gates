package protocol

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
	"formal-gates/internal/requirement"
)

func pointResponsibilityState() *State {
	return &State{Validation: &Validation{Document: requirement.FrozenRequirementProjection{Requirements: []requirement.FrozenRequirement{{ID: "REQ-001", Requirement: "Complete product behavior", Source: "user confirmation", AcceptanceConditions: []requirement.FrozenAcceptanceCondition{{ID: "AC-001", Text: "First behavior"}, {ID: "AC-002", Text: "Second behavior"}}}}}}, IntakeConfirmationReceipt: &IntakeConfirmationReceipt{Coverage: &CoverageInventory{Points: []coverage.AcceptancePoint{{PointID: "solution", ObservableBehavior: "Confirmed solution obligation", Oracle: "Observable solution effect"}}}}}
}

func TestDevelopmentPlanCoversTheConfirmedPointUniverse(t *testing.T) {
	s := pointResponsibilityState()
	plan := &GranularityReview{Batches: []DevelopmentBatch{{RequirementPoints: []string{"AC-001", "solution"}}, {RequirementPoints: []string{"AC-001", "AC-002"}}}}
	if err := s.validatePlanRequirementCoverage(plan, true); err != nil {
		t.Fatal(err)
	}
	plan.Batches[1].RequirementPoints = []string{"AC-002", "AC-001"}
	if s.validatePlanRequirementCoverage(plan, true) == nil {
		t.Fatal("raw plan changed the issued positional binding")
	}
	plan.Batches[1].RequirementPoints = []string{"AC-001"}
	if s.validatePlanRequirementCoverage(plan, true) == nil {
		t.Fatal("accepted a plan that omitted a confirmed product point")
	}
	if err := s.validatePlanRequirementCoverage(plan, false); err != nil {
		t.Fatalf("a bounded repair need not reimplement the full project: %v", err)
	}
	plan.Batches[0].RequirementPoints = []string{"unknown"}
	if s.validatePlanRequirementCoverage(plan, false) == nil {
		t.Fatal("accepted an invented repair duty")
	}
}

func TestWorkerPointResponsibilitiesSurviveRecovery(t *testing.T) {
	s := pointResponsibilityState()
	all := s.workerRequirementPoints(WorkerInput{})
	if len(all) != 3 {
		t.Fatalf("ordinary gate lost the full confirmed point set: %+v", all)
	}
	s.SliceChild = &SliceChildBinding{SliceID: "child"}
	s.Topology = &TopologyBinding{ACOwners: map[string]string{"AC-001": "child", "AC-002": "other"}}
	scoped := s.workerRequirementPoints(WorkerInput{})
	if len(scoped) != 1 || scoped[0].ID != "AC-001" || scoped[0].Requirement != "Complete product behavior" {
		t.Fatalf("child lost its original responsibility or parent meaning: %+v", scoped)
	}
	issued := WorkerInput{Validation: &ValidationInput{Development: &DevelopmentInput{Batch: DevelopmentBatch{RequirementPoints: []string{"solution", "AC-001"}}}}}
	want := s.workerRequirementPoints(issued)
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var recovered State
	if err := json.Unmarshal(data, &recovered); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(want, recovered.workerRequirementPoints(issued)) {
		t.Fatal("recovery changed the issued point identities or evidence context")
	}
	if len((&State{}).workerRequirementPoints(WorkerInput{})) != 0 {
		t.Fatal("an unstructured workflow invented requirement points")
	}
}

func TestGatePointDecisionsRequireExistingFindingsWithoutSeverityPolicy(t *testing.T) {
	points := []requirement.Point{{ID: "point"}}
	decision := []RequirementDecision{{PointID: "point", Status: "FAIL", Evidence: "Observed mismatch", FindingIndexes: []int{0}}}
	if err := ValidateRequirementDecisions(points, decision, 1); err != nil {
		t.Fatal(err)
	}
	if ValidateRequirementDecisions(points, decision, 0) == nil {
		t.Fatal("accepted an invented gate finding")
	}
	decision[0].FindingIndexes = nil
	if ValidateRequirementDecisions(points, decision, 1) == nil {
		t.Fatal("accepted a failed point without its actual gate finding")
	}
	decision[0].Status = "PASS"
	if err := ValidateRequirementDecisions(points, decision, 1); err != nil {
		t.Fatalf("nonblocking gate findings must retain the ordinary PASS policy: %v", err)
	}
}

func TestCompletionRecordsOnlyItsAssignedPointContributions(t *testing.T) {
	batch := DevelopmentBatch{RequirementPoints: []string{"shared"}, DoD: []DevelopmentCriterion{{ID: "delivery"}}}
	evidence := &DevelopmentEvidence{Completion: &DevelopmentCompletion{Handoff: "the next batch consumes this interface", DoD: map[string]string{"delivery": "the interface is implemented"}, RequirementResults: []requirement.PointResult{{PointID: "shared", Status: "PASS", Evidence: "this batch implements the input half of the shared behavior"}}}}
	if err := validateDevelopmentCompletion(batch, evidence); err != nil {
		t.Fatalf("a completed contribution does not need to claim another batch's work: %v", err)
	}
	evidence.RequirementResults = evidence.Completion.RequirementResults
	if validateDevelopmentCompletion(batch, evidence) == nil {
		t.Fatal("a planned batch mixed standalone point claims with its completion contract")
	}
	evidence.RequirementResults = nil
	for _, result := range []requirement.PointResult{{PointID: "shared", Status: "FAIL", Evidence: "unfinished"}, {PointID: "shared", Status: "PASS", Evidence: " "}, {PointID: "other", Status: "PASS", Evidence: "different duty"}} {
		evidence.Completion.RequirementResults = []requirement.PointResult{result}
		if validateDevelopmentCompletion(batch, evidence) == nil {
			t.Fatalf("accepted incomplete or unassigned contribution: %+v", result)
		}
	}
	evidence.Completion.RequirementResults = nil
	if validateDevelopmentCompletion(batch, evidence) == nil {
		t.Fatal("DoD and handoff alone replaced the required point evidence")
	}
}

func TestCheckpointPointContextUsesTheFrozenCompletedPrefix(t *testing.T) {
	s := pointResponsibilityState()
	plan := &GranularityReview{Batches: []DevelopmentBatch{{ID: "input", RequirementPoints: []string{"AC-001"}, Subtasks: []DevelopmentSubtask{{Description: "Implement accepted input"}}, DoD: []DevelopmentCriterion{{Description: "Accepted input responds"}}}, {ID: "rejection", RequirementPoints: []string{"AC-002", "solution"}}}}
	s.Results = map[string]WorkerResult{"readiness": {Review: &ReviewResultEvidence{GranularityReview: plan}}}
	s.Decisions = map[string]RecordedDecision{"route": {Control: ControlSelectRoute, Review: &ReviewDecisionPayload{Route: &RouteDecision{DevelopmentPlan: &DevelopmentPlanConfirmation{ReviewActionID: "readiness"}}}}}
	completed := runtime.TaskKey{Scope: planTaskScope(plan, "input")}
	gate := runtime.TaskKey{Scope: "candidate:quality"}
	s.Validation.Waves = []ValidationWave{{ID: "checkpoint", CheckpointTasks: []runtime.TaskKey{completed}, Targets: map[string]*ValidationTarget{gate.String(): {Task: gate, Kind: "GATE"}}}}
	binding := WorkBinding{WaveID: "checkpoint", Task: gate}
	points, batches := s.gateRequirementContext(binding)
	if !reflect.DeepEqual(requirement.PointIDs(points), []string{"AC-001"}) || !reflect.DeepEqual(batches, plan.Batches[:1]) {
		t.Fatalf("checkpoint included unfinished work or lost the original contribution: %+v %+v", points, batches)
	}
	s.Validation.Waves[0].CheckpointTasks = nil
	points, batches = s.gateRequirementContext(binding)
	if len(points) != 3 || len(batches) != 0 {
		t.Fatal("final review narrowed the complete confirmed responsibility")
	}
}

func TestRepairRequiredPointsUseTheOriginalFindingAssociations(t *testing.T) {
	s := pointResponsibilityState()
	raw := []review.Finding{{Message: "Actual input mismatch", Severity: "P1"}, {Message: "Independent readability issue", Severity: "P2"}}
	gate := runtime.TaskKey{Scope: "gate"}
	s.Validation.Waves = []ValidationWave{{ID: "origin", Targets: map[string]*ValidationTarget{gate.String(): {Task: gate, Kind: "GATE", ResultAction: "gate-result"}}}}
	s.Results = map[string]WorkerResult{"gate-result": {Validation: &ValidationResult{Findings: raw, RequirementResults: []RequirementDecision{{PointID: "AC-001", FindingIndexes: []int{0}}}}}}
	input := &RepairInput{Obligation: ValidationRepair{Problems: []string{"behavior", "quality", "execution"}}}
	for index, finding := range raw {
		problem := "behavior"
		if index == 1 {
			problem = "quality"
		}
		input.Findings = append(input.Findings, ValidationFinding{ID: validationFindingID("origin", gate.String(), index, finding), WaveID: "origin", Target: gate.String(), Raw: &raw[index], History: []FindingResolution{{Decision: "CONFIRMED", ProblemID: problem}}})
	}
	input.Findings = append(input.Findings, ValidationFinding{ID: "execution-finding", ExecutionFailure: &ExecutionFailure{CaseID: "invalid"}, History: []FindingResolution{{Decision: "CONFIRMED", ProblemID: "execution"}}})
	input.ExecutionFailures = []RepairExecutionFailure{{FindingID: "execution-finding", Records: []coverage.ExecutionRecord{{PointID: "AC-002", CaseID: "invalid", Result: coverage.ExecutedFail}}}}
	points := s.repairRequirementPoints(input)
	if !reflect.DeepEqual(requirement.PointIDs(points), []string{"AC-001", "AC-002"}) {
		t.Fatalf("repair invented project obligations or lost actual original evidence: %+v", points)
	}
	input.Findings[0].History[0].Decision = "DISMISSED"
	if got := requirement.PointIDs(s.repairRequirementPoints(input)); !reflect.DeepEqual(got, []string{"AC-002"}) {
		t.Fatalf("dismissed or unassociated quality findings invented requirement work: %v", got)
	}
	if validateImplementedPoints([]string{"AC-002"}, nil) == nil {
		t.Fatal("standalone summary replaced the actual required point evidence")
	}
}

func TestGateInheritanceRequiresTheSameCompletedContribution(t *testing.T) {
	s := pointResponsibilityState()
	plan := &GranularityReview{Batches: []DevelopmentBatch{{ID: "input", RequirementPoints: []string{"AC-001"}, Subtasks: []DevelopmentSubtask{{Description: "Implement accepted input"}}}, {ID: "verification", RequirementPoints: []string{"AC-001"}, Subtasks: []DevelopmentSubtask{{Description: "Verify the complete input contract"}}}}}
	s.Results = map[string]WorkerResult{"readiness": {Review: &ReviewResultEvidence{GranularityReview: plan}}, "gate-result": {Validation: &ValidationResult{RequirementResults: []RequirementDecision{{PointID: "AC-001", Status: "PASS", Evidence: "The implementation contribution is complete"}}}}}
	s.Decisions = map[string]RecordedDecision{"route": {Control: ControlSelectRoute, Review: &ReviewDecisionPayload{Route: &RouteDecision{DevelopmentPlan: &DevelopmentPlanConfirmation{ReviewActionID: "readiness"}}}}}
	oldTask, newTask := runtime.TaskKey{Scope: "prefix:quality"}, runtime.TaskKey{Scope: "current:quality"}
	prefix := []runtime.TaskKey{{Scope: planTaskScope(plan, "input")}}
	previous := &ValidationTarget{Task: oldTask, Kind: "GATE", Gate: "quality", Status: "PASS", ResultAction: "gate-result"}
	prior := ValidationWave{ID: "prefix", CheckpointTasks: prefix, Targets: map[string]*ValidationTarget{oldTask.String(): previous}}
	s.Validation.Waves = []ValidationWave{prior}
	s.DispatchBindings = map[string]WorkBinding{"gate-result": {WaveID: prior.ID, Task: oldTask}}
	for _, sample := range []struct {
		name  string
		tasks []runtime.TaskKey
		pass  bool
	}{{"same contribution", prefix, true}, {"additional contribution to the same point", append(slices.Clone(prefix), runtime.TaskKey{Scope: planTaskScope(plan, "verification")}), false}, {"final responsibility", nil, false}} {
		t.Run(sample.name, func(t *testing.T) {
			target := &ValidationTarget{Task: newTask, Kind: "GATE", Gate: "quality", Status: "PENDING"}
			current := ValidationWave{ID: "current", CheckpointTasks: sample.tasks, Targets: map[string]*ValidationTarget{newTask.String(): target}}
			if carried := s.carryValidationTarget(target, previous, &prior, &current); carried != sample.pass {
				t.Fatalf("inheritance equated different contribution contracts: carried=%v target=%+v", carried, target)
			}
			if !sample.pass && (target.Status != "PENDING" || target.ResultAction != "") {
				t.Fatal("rejected inheritance changed the target")
			}
		})
	}
}
