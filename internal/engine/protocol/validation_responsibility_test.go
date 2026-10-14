package protocol

import (
	"reflect"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/requirement"
)

func TestPendingGateImpactCannotInheritBeforeItsJudgment(t *testing.T) {
	cd := compiledCanonicalWorkflow(t)
	s := pointResponsibilityState()
	oldTask := pipelineTask(cd, "engine.gate.v3", "original:quality")
	newTask := pipelineTask(cd, "engine.gate.v3", "current:quality")
	original := &ValidationTarget{Task: oldTask, Kind: "GATE", Gate: "quality", Status: "PASS", ResultAction: "original-result"}
	pending := &ValidationTarget{Task: newTask, Kind: "GATE", Gate: "quality", Status: "PENDING", GateImpact: &GateImpact{SourceWave: "original"}}
	candidate := coverage.ValidationCandidate{Identity: "candidate", Digest: "candidate-tree"}
	s.Validation.Waves = []ValidationWave{
		{ID: "original", Candidate: candidate, Targets: map[string]*ValidationTarget{oldTask.String(): original}, Status: "COMPLETE"},
		{ID: "current", Candidate: candidate, Targets: map[string]*ValidationTarget{newTask.String(): pending}, Status: "COLLECTING", Generation: 1},
	}
	s.RequirementRevisions = []ConfirmedRevision{{StateRevision: 1, WorkDecisions: []RequirementWorkDecision{{Work: RequirementWork{ActionID: original.ResultAction, Task: oldTask}, Disposition: RequirementWorkCarry, Reason: "The completed responsibility carries"}}}}
	wave, target := s.targetResponsibility(&s.Validation.Waves[1], pending)
	if wave.ID != "original" || target != original {
		t.Fatal("pending impact lost its original responsibility")
	}
	before := *pending
	if s.carryRequirementTargets(&s.Validation.Waves[1]) || !reflect.DeepEqual(*pending, before) {
		t.Fatal("requirement carry discharged a gate before its impact judgment")
	}
}

func TestRerunGateResponsibilityUsesTheAcceptedResult(t *testing.T) {
	cd := compiledCanonicalWorkflow(t)
	e := &Engine{cfg: Config{Definition: cd}}
	s := pointResponsibilityState()
	s.Validation.Generation, s.Validation.RequirementRevision = 1, "confirmed"
	oldTask := pipelineTask(cd, "engine.gate.v3", "original:quality")
	newTask := pipelineTask(cd, "engine.gate.v3", "repaired:quality")
	old := &ValidationTarget{Task: oldTask, Kind: "GATE", Gate: "quality", Status: "PASS", ResultAction: "original-result"}
	next := &ValidationTarget{Task: newTask, Kind: "GATE", Gate: "quality", Status: "PENDING", GateImpact: &GateImpact{SourceWave: "original"}}
	s.Validation.Waves = []ValidationWave{
		{ID: "original", Candidate: coverage.ValidationCandidate{Identity: "first", Digest: "first-tree"}, Targets: map[string]*ValidationTarget{oldTask.String(): old}, Status: "COMPLETE", Generation: 1, RequirementRevision: "confirmed"},
		{ID: "repaired", Candidate: coverage.ValidationCandidate{Identity: "second", Digest: "second-tree"}, Targets: map[string]*ValidationTarget{newTask.String(): next}, Status: "COLLECTING", Generation: 1, RequirementRevision: "confirmed"},
	}
	if err := s.acceptGateImpact("repaired", &ValidationObservation{Gate: "quality", Decision: "RERUN", Reason: "The changed input boundary requires independent review"}); err != nil {
		t.Fatal(err)
	}
	binding := WorkBinding{Task: newTask, WaveID: "repaired", Candidate: "second", Generation: 1, RequirementRevision: "confirmed"}
	s.DispatchBindings = map[string]WorkBinding{"rerun-result": binding, "original-result": {Task: oldTask, WaveID: "original", Generation: 1, RequirementRevision: "confirmed"}}
	decisions := []RequirementDecision{{PointID: "AC-001", Status: "PASS", Evidence: "Checked current input"}, {PointID: "AC-002", Status: "PASS", Evidence: "Checked current output"}, {PointID: "solution", Status: "PASS", Evidence: "Checked current solution"}}
	record := WorkerResult{ActionID: "rerun-result", Outcome: OutcomePass, Validation: &ValidationResult{Binding: binding, Status: "PASS", RequirementResults: decisions}}
	if err := e.acceptValidationResult(s, record, 2); err != nil {
		t.Fatal(err)
	}
	s.Results = map[string]WorkerResult{"rerun-result": record, "original-result": {Validation: &ValidationResult{RequirementResults: decisions}}}
	wave, target := s.targetResponsibility(&s.Validation.Waves[1], next)
	if target.ResultAction != "rerun-result" || wave.ID != "repaired" {
		t.Fatalf("accepted current review lost its responsibility: wave=%s result=%s", wave.ID, target.ResultAction)
	}
}

func TestCompletedRepairMemberRetainsTheWholeGateResponsibility(t *testing.T) {
	cd := compiledCanonicalWorkflow(t)
	s := pointResponsibilityState()
	s.Validation.Generation, s.Validation.RequirementRevision = 1, "confirmed"
	s.RouteMode, s.SelectedGates = "full", []string{"quality"}
	root := pipelineTask(cd, "engine.repair.v7", "original")
	gateTask := pipelineTask(cd, "engine.gate.v3", "original:quality")
	original := &ValidationTarget{Task: gateTask, Kind: "GATE", Gate: "quality", Status: "FAIL", ResultAction: "gate-result"}
	s.Validation.Waves = []ValidationWave{{ID: "original", Generation: 1, RequirementRevision: "confirmed", Status: "REPAIR", Candidate: coverage.ValidationCandidate{Identity: "original"}, Targets: map[string]*ValidationTarget{gateTask.String(): original}, Repair: &ValidationRepair{Task: root, PureP2: true}}}
	plan := plannedReviewEvidence(review.StartReadiness, "readiness", "", "PASS", "Independent input and output work", nil).GranularityReview
	plan.Batches[0].RequirementPoints = []string{"AC-001"}
	second := plan.Batches[0]
	second.ID, second.RequirementPoints = "output", []string{"AC-002", "solution"}
	second.Subtasks = []DevelopmentSubtask{{ID: "output-body", Description: "Complete the output behavior"}}
	plan.Batches = append(plan.Batches, second)
	if err := ValidateRepairPlan(plan, false); err != nil {
		t.Fatal(err)
	}
	planBinding := WorkBinding{Task: root, WaveID: "original", Generation: 1, RequirementRevision: "confirmed"}
	proposal := WorkerResult{ActionID: "plan", Outcome: OutcomePass, Validation: &ValidationResult{Binding: planBinding, Status: "PASS", GranularityReview: plan}}
	s.DispatchBindings = map[string]WorkBinding{"plan": planBinding, "gate-result": {Task: gateTask, WaveID: "original", Generation: 1, RequirementRevision: "confirmed"}}
	s.Results = map[string]WorkerResult{"gate-result": {ActionID: "gate-result", Validation: &ValidationResult{RequirementResults: []RequirementDecision{{PointID: "AC-001", Status: "FAIL", Evidence: "Original input mismatch"}, {PointID: "AC-002", Status: "PASS", Evidence: "Output unchanged"}, {PointID: "solution", Status: "PASS", Evidence: "Solution unchanged"}}}}}
	if err := s.acceptRepairResult(proposal); err != nil {
		t.Fatal(err)
	}
	s.Results[proposal.ActionID] = proposal
	members := s.repairExecutions(&s.Validation.Waves[0])
	for _, member := range members {
		batch := *member.batch
		completion := &DevelopmentCompletion{Handoff: "The completed contribution is ready for integration", DoD: map[string]string{}}
		for _, criterion := range batch.DoD {
			completion.DoD[criterion.ID] = "Compared the delivered command against " + criterion.Description
		}
		for _, point := range batch.RequirementPoints {
			completion.RequirementResults = append(completion.RequirementResults, requirement.PointResult{PointID: point, Status: "PASS", Evidence: "Implemented the assigned contribution"})
		}
		binding := WorkBinding{Task: member.task, WaveID: "original", Generation: 1, RequirementRevision: "confirmed", PlanRequest: "plan", BatchID: batch.ID, WorkspaceID: batch.ID + ":workspace"}
		record := WorkerResult{ActionID: batch.ID, Outcome: OutcomePass, Validation: &ValidationResult{Binding: binding, Status: "PASS", Development: &DevelopmentEvidence{Paths: []string{batch.ID + ".go"}, Summary: "Completed the original repair contribution", Completion: completion}}}
		s.DispatchBindings[record.ActionID] = binding
		if err := s.acceptRepairResult(record); err != nil {
			t.Fatal(err)
		}
		s.Results[record.ActionID] = record
		recordDevelopmentChecksFixture(t, s, record.ActionID, batch)
		if s.developmentResultRecord(member.task) == nil {
			t.Fatal("member lacks its original completed contract and native check evidence")
		}
	}
	resultAction := members[0].batch.ID
	previousTask := pipelineTask(cd, "engine.gate.v3", "repaired:quality")
	nextTask := pipelineTask(cd, "engine.gate.v3", "route-added:quality")
	previous := &ValidationTarget{Task: previousTask, Kind: "GATE", Gate: "quality", Status: "PASS", ResultAction: resultAction}
	target := &ValidationTarget{Task: nextTask, Kind: "GATE", Gate: "quality", Status: "PENDING"}
	candidate := coverage.ValidationCandidate{Identity: "repaired", Digest: "repaired-tree"}
	s.Validation.Waves = append(s.Validation.Waves, ValidationWave{ID: "repaired", Candidate: candidate, Targets: map[string]*ValidationTarget{previousTask.String(): previous}}, ValidationWave{ID: "route-added", Candidate: candidate, Targets: map[string]*ValidationTarget{nextTask.String(): target}})
	prior, current := &s.Validation.Waves[1], &s.Validation.Waves[2]
	wave, source := s.targetResponsibility(prior, previous)
	if wave.ID != "original" || source != original {
		t.Fatalf("generated completed repair member lost the original whole responsibility: wave=%s action=%s", wave.ID, source.ResultAction)
	}
	if !s.carryValidationTarget(target, previous, prior, current) || target.ResultAction != resultAction {
		t.Fatal("same whole gate responsibility rejected its actual P2 completion")
	}
	// An actual member gate is a different duty from the whole gate repaired by its member.
	s.Validation.Waves[0].Repair.PureP2 = false
	s.ensureRepairGates(cd)
	work := s.repairMembers(&s.Validation.Waves[0])[0]
	memberTask, memberTarget := s.memberGateTarget(work, "quality")
	memberBinding, known := s.repairGateBinding(memberTask, s.repairWork())
	if !known {
		t.Fatal("original member gate has no generated binding")
	}
	s.DispatchBindings["member-gate"] = memberBinding
	memberTarget.Status, memberTarget.ResultAction = "PASS", "member-gate"
	wave, source = s.targetResponsibility(&s.Validation.Waves[0], memberTarget)
	if wave.ID != "original" || source != memberTarget {
		t.Fatal("a signed member gate was reinterpreted as the whole gate")
	}
}
