package protocol

import (
	"encoding/json"
	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
	"testing"
)

func TestValidationTargetControlsPreserveFrozenExpectedSet(t *testing.T) {
	gate := runtime.TaskKey{Node: "validation", Step: "gate", Scope: "member-source"}
	ordinary := runtime.TaskKey{Node: "validation", Step: "gate", Scope: "candidate"}
	target := &ValidationTarget{Task: gate, Kind: "GATE", Gate: "quality", Status: "PENDING"}
	w := ValidationWave{ID: "wave", Status: "REPAIR", ExpectedDigest: "frozen", Targets: map[string]*ValidationTarget{ordinary.String(): {Task: ordinary, Status: "PASS"}}, Repair: &ValidationRepair{Targets: map[string]*ValidationTarget{gate.String(): target}}}
	s := &State{Validation: &Validation{Waves: []ValidationWave{w}}, PendingActions: map[string]PendingAction{}, Attempts: map[runtime.TaskKey]Attempt{}, EndedAttempts: map[runtime.TaskKey]Attempt{}, ObsoleteActions: map[string]ObsoleteAction{}}
	s.Tasks = map[runtime.TaskKey]runtime.TaskStatus{}
	if s.validationWaveForTask(gate) == nil || !s.sameValidationDuty(WorkBinding{WaveID: w.ID, Task: gate}, &s.Validation.Waves[0]) {
		t.Fatal("repair target lost its frozen duty")
	}
	s.endBusinessAttempt(PendingAction{ActionID: "attempt", Task: gate}, Attempt{ID: "attempt", RetryExhausted: true, Attempts: 2, MaxAttempts: 2}, "transport failed", 2)
	if target.Status != "RETRY_EXHAUSTED" || s.Validation.Waves[0].ExpectedDigest != "frozen" || len(s.Validation.Waves[0].Targets) != 1 {
		t.Fatal("attempt status changed frozen expected set or missed repair target")
	}
	ask := PendingAsk{Validation: &ValidationRequest{Kind: "RETRY", Binding: WorkBinding{WaveID: w.ID, Task: gate}, Obligations: []string{gate.String()}}}
	if err := (&Engine{}).waiveValidation(s, ask, &ValidationDecision{Obligations: []string{gate.String()}, Reason: "skip unavailable source gate"}, &s.Validation.Waves[0]); err != nil {
		t.Fatal(err)
	}
	if target.Status != "AUTHORIZED_SKIP" || target.DispositionReason != "skip unavailable source gate" || len(s.EndedAttempts) != 0 {
		t.Fatal("waiver did not end the exact finite attempt")
	}
}

func TestCandidateChildViewsUseFinalIssuedOwner(t *testing.T) {
	s := &State{Validation: &Validation{Generation: 1, RequirementRevision: "req", Candidate: &coverage.ValidationCandidate{Identity: "candidate", Digest: "tree"}}, PendingHostActions: map[string]HostActionIntent{}, HostActionReceipts: map[string]HostActionReceipt{}}
	for i, stage := range []string{"REVIEW_READY", "DEVELOPMENT"} {
		action := stage
		p := CandidateParameters{Stage: stage, Generation: 1, RequirementRevision: "req"}
		parameters, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		params := map[string]any{}
		if err = json.Unmarshal(parameters, &params); err != nil {
			t.Fatal(err)
		}
		s.PendingHostActions[action] = HostActionIntent{ActionID: action, Revision: uint64(i + 1), Adapter: &AdapterHostAction{Operation: "op.candidate", Params: params}}
		view := ChildValidationView{ContentEqual: stage == "REVIEW_READY"}
		proof := CandidateEvidence{Identity: "candidate", Digest: "tree", ChildViews: map[string]ChildValidationView{"child": view}}
		body, err := json.Marshal(proof)
		if err != nil {
			t.Fatal(err)
		}
		values := map[string]any{}
		if err = json.Unmarshal(body, &values); err != nil {
			t.Fatal(err)
		}
		s.HostActionReceipts[action] = HostActionReceipt{ActionID: action, Status: HostActionStatusExecuted, AdapterEvidence: &AdapterEvidence{Values: values}}
	}
	for i := 0; i < 30; i++ {
		if views := s.candidateChildViews(); !views["child"].ContentEqual {
			t.Fatal("same native identity selected a partial source view")
		}
	}
}

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
