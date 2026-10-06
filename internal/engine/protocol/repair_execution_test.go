package protocol

import (
	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
	"testing"
)

func TestRepairMembersRetainOneObligation(t *testing.T) {
	task := runtime.TaskKey{Node: "development", Step: "repair", Scope: "candidate"}
	w := &ValidationWave{ID: "wave", Status: "REPAIR", Repair: &ValidationRepair{Task: task, Problems: []string{"problem"}}}
	s := &State{Results: map[string]WorkerResult{}}
	if members := s.repairMembers(w); len(members) != 1 || members[0].task != task {
		t.Fatalf("unplanned members: %+v", members)
	}
	p := &GranularityReview{Batches: []DevelopmentBatch{{ID: "parser"}, {ID: "logging"}, {ID: "command", DependsOn: []string{"parser"}}}}
	s.Results["plan"] = WorkerResult{ActionID: "plan", Outcome: OutcomePass, Validation: &ValidationResult{Binding: WorkBinding{Task: task}, GranularityReview: p}}
	members := s.repairMembers(w)
	if len(members) != 3 {
		t.Fatalf("members: %+v", members)
	}
	for _, member := range members {
		if member.wave != w || member.planAction != "plan" || member.task == task {
			t.Fatalf("member lost its original obligation: %+v", member)
		}
	}
	if members[0].task == members[1].task || members[1].task == members[2].task {
		t.Fatal("members share an execution identity")
	}
}

func TestRepairGateFailureKeepsObligationAndFiniteAuthorization(t *testing.T) {
	for _, severity := range []string{"P1", "P2"} {
		t.Run(severity, func(t *testing.T) {
			cd, err := compiler.Compile(definition.Workflow(), definition.Registry())
			if err != nil {
				t.Fatal(err)
			}
			s := &State{RunID: "run", Route: "regular", RouteMode: "full", SelectedGates: []string{"quality"}, Results: map[string]WorkerResult{}, DispatchBindings: map[string]WorkBinding{}, DispatchIssueRevisions: map[string]uint64{}, PendingAsks: map[string]PendingAsk{}, Decisions: map[string]RecordedDecision{}}
			s.Tasks = map[runtime.TaskKey]runtime.TaskStatus{}
			task := pipelineTask(cd, "engine.repair", "candidate")
			s.Validation = &Validation{Generation: 1, RequirementRevision: "req", Findings: map[string]*ValidationFinding{}, Problems: map[string]*ValidationProblem{}, Waves: []ValidationWave{{ID: "wave", Generation: 1, RequirementRevision: "req", Status: "REPAIR", Counted: true, Repair: &ValidationRepair{Task: task, Problems: []string{"original"}}}}}
			w := &s.Validation.Waves[0]
			base := DevelopmentBatch{ID: "parser", Subtasks: []DevelopmentSubtask{{ID: "parse", Description: "parse"}}, Artifacts: []string{"parser.go"}, Deliverable: "parser", PublicEntry: "app", Interfaces: "input", StateBoundary: "none", Risk: "normal input", DoD: []DevelopmentCriterion{{ID: "normal", Description: "normal input"}}, Checks: []DevelopmentCheck{{ID: "build", Command: "go build ./..."}}, Rollback: "revert", Recovery: "retry", Handoff: "parser output", ContextCost: "one file"}
			other := base
			other.ID = "logging"
			other.Subtasks = []DevelopmentSubtask{{ID: "log", Description: "log"}}
			other.Artifacts = []string{"logging.go"}
			p := &GranularityReview{Reason: "two independent owned boundaries", VerificationTier: "FINAL", Batches: []DevelopmentBatch{base, other}}
			binding := WorkBinding{Task: task, WaveID: w.ID, Generation: 1, RequirementRevision: "req", RouteScope: s.validationRouteDigest()}
			s.DispatchBindings["plan"] = binding
			e := &Engine{cfg: Config{Definition: cd}}
			plan := WorkerResult{ActionID: "plan", Outcome: OutcomePass, Validation: &ValidationResult{Binding: binding, Status: "PASS", GranularityReview: p}}
			if err := e.acceptValidationResult(s, plan); err != nil {
				t.Fatal(err)
			}
			s.Results["plan"] = plan
			complete := func(member repairWork) {
				r := WorkerResult{ActionID: member.task.String() + ":completed", Outcome: OutcomePass, Validation: &ValidationResult{Binding: WorkBinding{Task: member.task, WaveID: w.ID, Generation: 1, RequirementRevision: "req"}, Status: "PASS", Development: &DevelopmentEvidence{Paths: member.batch.Artifacts, Summary: "completed owned behavior", Build: []string{"complete build"}, Tests: []string{"complete test suite"}, Completion: &DevelopmentCompletion{DoD: map[string]string{"normal": "normal input verified"}, Checks: map[string]string{"build": "build passed"}, Handoff: "owned boundary delivered"}}}}
				s.Results[r.ActionID] = r
			}
			for _, member := range s.repairMembers(w) {
				complete(member)
			}
			if s.completedValidationWaves() != 1 {
				t.Fatal("member count increased business repairs")
			}
			fail := func(member repairWork) {
				s.ensureRepairGates(cd)
				key, target := s.memberGateTarget(member, "quality")
				if target == nil {
					t.Fatal("missing member gate")
				}
				b, _ := s.repairGateBinding(key)
				action := key.String() + ":review"
				s.DispatchBindings[action] = b
				r := WorkerResult{ActionID: action, Outcome: OutcomePass, Validation: &ValidationResult{Binding: b, Status: "FAIL", Findings: []review.Finding{{Severity: severity, Message: "normal command behavior fails", Requirement: "AC-001", NormalPath: "app normal", ActualError: "wrong normal output", DeferralImpact: "normal command unusable", SeverityRationale: "normal behavior contract", Locations: []string{"parser.go:1"}}}}}
				if err := e.acceptValidationResult(s, r); err != nil {
					t.Fatal(err)
				}
				s.Results[action] = r
				var finding string
				for id, f := range s.Validation.Findings {
					if f.Target == key.String() {
						finding = id
					}
				}
				resolution := FindingResolution{FindingID: finding, Decision: "NEW", Requirement: "AC-001", NormalPath: "app normal", Module: "parser", RootCause: "normal output", Consequence: "normal result fails", Severity: severity, SeverityRationale: "normal behavior contract", Verification: review.FindingVerification{Scope: "owned command", Severity: "normal behavior", Causality: "parser output", Reproduction: "app normal", Classification: "implementation"}, Reason: "observed source-specific failure"}
				if err := e.adjudicateValidation(s, w.ID, []FindingResolution{resolution}, 10); err != nil {
					t.Fatal(err)
				}
			}
			members := s.repairMembers(w)
			s.ensureRepairGates(cd)
			_, otherTarget := s.memberGateTarget(members[1], "quality")
			otherTarget.Status = "PASS"
			fail(members[0])
			correction := s.repairMembers(w)[0]
			if correction.task == members[0].task || correction.previous == nil || correction.wave.Repair.Task != task {
				t.Fatal("gate correction lost original obligation or frozen source")
			}
			if severity == "P2" {
				if len(s.repairGates(correction)) != 0 || s.completedValidationWaves() != 1 {
					t.Fatal("P2 correction repeated gate or spent a business round")
				}
				return
			}
			if s.completedValidationWaves() != 2 {
				t.Fatal("new P1 source did not count one business repair")
			}
			complete(correction)
			fail(correction)
			next := s.repairMembers(w)[0]
			if s.completedValidationWaves() != automaticValidationWaveLimit || s.repairExtraAuthorized(&next) {
				t.Fatal("exhausted source acquired stale authority")
			}
			s.PendingAsks["grant"] = PendingAsk{Validation: &ValidationRequest{Kind: "REPAIR", Binding: WorkBinding{Task: next.task}}}
			s.Decisions["grant"] = RecordedDecision{Choice: "approve"}
			if !s.repairExtraAuthorized(&next) {
				t.Fatal("exact source grant missing")
			}
			complete(next)
			fail(next)
			later := s.repairMembers(w)[0]
			w.ExtraAuthorized = true
			if s.repairExtraAuthorized(&later) {
				t.Fatal("an earlier source grant authorized the next failure")
			}
		})
	}
}
