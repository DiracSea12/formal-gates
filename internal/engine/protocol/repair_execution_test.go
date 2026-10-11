package protocol

import (
	"encoding/json"
	"fmt"
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

func TestCorrectedSourceProblemsCloseOnlyAfterCompleteValidation(t *testing.T) {
	for _, boundary := range []string{"complete", "missing-member", "missing-self-run", "final-fail", "late-resolution", "same-problem-fail"} {
		t.Run(boundary, func(t *testing.T) {
			cd, err := compiler.Compile(definition.Workflow(), definition.Registry())
			if err != nil {
				t.Fatal(err)
			}
			s := &State{RunID: "run", RouteMode: "full", SelectedGates: []string{"quality"}, Results: map[string]WorkerResult{}, DispatchBindings: map[string]WorkBinding{}, DispatchIssueRevisions: map[string]uint64{}}
			s.Tasks = map[runtime.TaskKey]runtime.TaskStatus{}
			task := pipelineTask(cd, "engine.repair.v5", "candidate")
			s.Validation = &Validation{Generation: 1, RequirementRevision: "req", Findings: map[string]*ValidationFinding{}, Problems: map[string]*ValidationProblem{"original": {ID: "original"}, "source": {ID: "source"}, "later": {ID: "later"}}, Waves: []ValidationWave{{ID: "source-wave", Generation: 1, Status: "REPAIR", Candidate: coverage.ValidationCandidate{Identity: "base"}, Targets: map[string]*ValidationTarget{}, Repair: &ValidationRepair{Task: task, Problems: []string{"original"}}}}}
			w := &s.Validation.Waves[0]
			plan := &GranularityReview{Batches: []DevelopmentBatch{{ID: "parser", Checks: []DevelopmentCheck{{ID: "parse", Command: "printf parsed"}}}, {ID: "logging", Checks: []DevelopmentCheck{{ID: "log", Command: "printf logged"}}}}}
			s.Results["plan"] = WorkerResult{ActionID: "plan", Outcome: OutcomePass, Validation: &ValidationResult{Binding: WorkBinding{Task: task}, GranularityReview: plan}}
			members := s.repairMembers(w)
			complete := func(key runtime.TaskKey, action string) {
				s.Results[action] = WorkerResult{ActionID: action, Outcome: OutcomePass, Validation: &ValidationResult{Binding: WorkBinding{Task: key, Generation: 1, RequirementRevision: "req", WaveID: w.ID, PlanRequest: "plan", BatchID: fixtureRepairBatchID(s, key)}, Status: OutcomePass, Development: &DevelopmentEvidence{Paths: []string{action + ".go"}, Summary: "complete"}}}
				if work := s.repairMember(key); work != nil && work.batch != nil {
					r := s.Results[action]
					r.Validation.Binding.WorkspaceID = action + ":workspace"
					s.Results[action] = r
					s.DispatchBindings[action] = r.Validation.Binding
					recordDevelopmentChecksFixture(t, s, action, *work.batch)
				}
			}
			complete(members[0].task, "parser")
			complete(members[1].task, "logging")
			s.ensureRepairGates(cd)
			key, target := s.memberGateTarget(members[0], "quality")
			target.Status = "FAIL"
			target.ResultAction = "gate-failure"
			_, other := s.memberGateTarget(members[1], "quality")
			other.Status = "PASS"
			f := &ValidationFinding{ID: "finding", WaveID: w.ID, Target: key.String(), History: []FindingResolution{{FindingID: "finding", ProblemID: "source", Decision: "NEW", Severity: "P2", AcceptedRevision: 10}}}
			s.Validation.Findings[f.ID] = f
			correction := s.repairMembers(w)[0]
			complete(correction.task, "correction")
			s.DispatchBindings["correction"] = s.Results["correction"].Validation.Binding
			s.DispatchIssueRevisions["correction"] = 20
			verification := s.repairVerificationWork(w, compiledCanonicalWorkflow(t))
			if verification == nil {
				t.Fatal("missing integrated verification projection")
			}
			s.Results["self-run"] = WorkerResult{ActionID: "self-run", Outcome: OutcomePass, Validation: &ValidationResult{Binding: WorkBinding{Task: verification.task, WaveID: w.ID}, Status: OutcomePass, Development: &DevelopmentEvidence{Summary: "native facts compared"}}}
			final := ValidationWave{ID: "final", Generation: 1, Status: "COLLECTING", Candidate: coverage.ValidationCandidate{Identity: "final"}, SourceTasks: []runtime.TaskKey{correction.task, members[1].task}, Targets: map[string]*ValidationTarget{"gate": {Task: pipelineTask(cd, "engine.gate.v2", "final:quality"), Kind: "GATE", Gate: "quality", Status: "PASS"}}}
			switch boundary {
			case "missing-member":
				final.SourceTasks = final.SourceTasks[:1]
			case "missing-self-run":
				delete(s.Results, "self-run")
			case "final-fail":
				final.Targets["gate"].Status = "FAIL"
			case "late-resolution":
				f.History = append(f.History, FindingResolution{FindingID: f.ID, ProblemID: "source", Decision: "SAME_AS", Severity: "P2", AcceptedRevision: 30})
			case "same-problem-fail":
				final.Targets["other"] = &ValidationTarget{Task: pipelineTask(cd, "engine.gate.v2", "final:security"), Kind: "GATE", Gate: "security", Status: "FAIL"}
				s.Validation.Findings["other-finding"] = &ValidationFinding{ID: "other-finding", WaveID: final.ID, Target: final.Targets["other"].Task.String(), History: []FindingResolution{{FindingID: "other-finding", ProblemID: "source", Decision: "SAME_AS", Severity: "P2", AcceptedRevision: 30}}}
			}
			final.Targets = func() map[string]*ValidationTarget {
				targets := map[string]*ValidationTarget{}
				for _, target := range final.Targets {
					targets[target.Task.String()] = target
				}
				return targets
			}()
			later := &ValidationFinding{ID: "later-finding", WaveID: w.ID, Target: key.String(), History: []FindingResolution{{FindingID: "later-finding", ProblemID: "later", Decision: "NEW", Severity: "P2", AcceptedRevision: 30}}}
			if boundary == "complete" {
				s.Validation.Findings[later.ID] = later
			}
			s.resolveValidationProblems(&final, compiledCanonicalWorkflow(t))
			if (s.Validation.Problems["source"].ResolvedWave != "") != (boundary == "complete") {
				t.Fatalf("source completion boundary %s: %+v", boundary, s.Validation.Problems["source"])
			}
			if s.Validation.Problems["later"].ResolvedWave != "" {
				t.Fatal("old completion swallowed a later problem")
			}
		})
	}
}

func TestInvalidatedRepairDutyUsesConfirmedMemberAuthority(t *testing.T) {
	for _, boundary := range []string{"carry", "member-redo", "all-redo", "retired-task", "thaw", "artifact-disposition", "unconfirmed"} {
		t.Run(boundary, func(t *testing.T) {
			root := runtime.TaskKey{Node: "development", Step: "repair", Scope: "candidate"}
			w := ValidationWave{ID: "wave", Generation: 1, Status: "INVALIDATED", Repair: &ValidationRepair{Task: root}}
			s := &State{Validation: &Validation{Generation: 20, RequirementRevision: "current", Waves: []ValidationWave{w}}, Results: map[string]WorkerResult{}, PendingAsks: map[string]PendingAsk{}, Decisions: map[string]RecordedDecision{}}
			s.Tasks = map[runtime.TaskKey]runtime.TaskStatus{}
			s.Results["plan"] = WorkerResult{ActionID: "plan", Outcome: OutcomePass, Validation: &ValidationResult{Binding: WorkBinding{Task: root}, GranularityReview: &GranularityReview{Batches: []DevelopmentBatch{{ID: "parser"}, {ID: "logging"}}}}}
			members := s.repairMembers(&s.Validation.Waves[0])
			entry := ConfirmedRevision{Revision: "current", StateRevision: 20}
			for i, member := range members {
				disposition := RequirementWorkCarry
				if boundary == "all-redo" || boundary == "member-redo" && i == 1 {
					disposition = RequirementWorkRedo
				}
				entry.WorkDecisions = append(entry.WorkDecisions, RequirementWorkDecision{Work: RequirementWork{Task: member.task}, Disposition: disposition})
			}
			s.RequirementRevisions = []ConfirmedRevision{entry}
			initialResource := s.repairAssemblyResource(&s.Validation.Waves[0])
			if boundary == "retired-task" {
				s.Tasks[members[0].task] = runtime.TaskTerminal
			}
			if boundary == "thaw" || boundary == "artifact-disposition" {
				kind := "THAW"
				decision := RecordedDecision{Choice: "approve", Revision: 30}
				if boundary == "artifact-disposition" {
					kind = "VALIDATION_DETAIL_DISPOSITION"
					decision.Validation = &ValidationDecision{Remedy: "QA_ARTIFACT_REPAIR"}
				}
				s.PendingAsks["retire"] = PendingAsk{Validation: &ValidationRequest{Kind: kind, Binding: WorkBinding{WaveID: w.ID}}}
				s.Decisions["retire"] = decision
			}
			if boundary == "unconfirmed" {
				s.RequirementRevisions = nil
			}
			current := s.repairDutyCurrent(&s.Validation.Waves[0])
			if current != (boundary == "carry" || boundary == "member-redo" || boundary == "all-redo") {
				t.Fatalf("duty authority boundary %s: %t", boundary, current)
			}
			if boundary == "member-redo" || boundary == "all-redo" {
				if initialResource.ID == s.resource("repair-integration", "", w.ID+":"+digestOfCanonical([]runtime.TaskKey{members[0].task, members[1].task})).ID {
					t.Fatal("replacement closure reused the original assembly identity")
				}
			}
		})
	}
}

// A single planned completion discharges the original task through the same
// batch-completion contract as isolated members.
func TestSingleRepairPlanCompletionKeepsOriginalTask(t *testing.T) {
	task := runtime.TaskKey{Node: "validation", Step: "repair", Scope: "candidate"}
	batch := DevelopmentBatch{ID: "command", Subtasks: []DevelopmentSubtask{{ID: "input", Description: "implement input contract"}}, Deliverable: "command", PublicEntry: "app", Artifacts: []string{"app.go"}, Interfaces: "arguments", StateBoundary: "no state", Risk: "normal input", DoD: []DevelopmentCriterion{{ID: "normal", Description: "normal command"}}, Checks: []DevelopmentCheck{{ID: "build", Command: "go build ./..."}}, Rollback: "revert", Recovery: "retry", Handoff: "command", ContextCost: "one file"}
	plan := &GranularityReview{Reason: "one coherent command", VerificationTier: "FINAL", Batches: []DevelopmentBatch{batch}}
	for _, complete := range []bool{false, true} {
		t.Run(fmt.Sprintf("completion=%t", complete), func(t *testing.T) {
			s := &State{Results: map[string]WorkerResult{}, Validation: &Validation{Generation: 1, RequirementRevision: "req", Waves: []ValidationWave{{ID: "wave", Status: "REPAIR", Repair: &ValidationRepair{Task: task}}}}}
			binding := WorkBinding{Task: task, WaveID: "wave", Generation: 1, RequirementRevision: "req"}
			evidence := &DevelopmentEvidence{Paths: []string{"app.go"}, Summary: "command completed"}
			if complete {
				evidence.Completion = &DevelopmentCompletion{DoD: map[string]string{"normal": "verified"}, Handoff: "command ready"}
			}
			r := WorkerResult{ActionID: "completion", Outcome: OutcomePass, Validation: &ValidationResult{Binding: binding, Status: "PASS", GranularityReview: plan, Development: evidence}}
			err := s.acceptRepairResult(r)
			if (err == nil) != complete {
				t.Fatalf("single-batch completion boundary: %v", err)
			}
			if complete {
				s.Results[r.ActionID] = r
				r.Validation.Binding.WorkspaceID = "completion-workspace"
				s.DispatchBindings = map[string]WorkBinding{r.ActionID: r.Validation.Binding}
				recordDevelopmentChecksFixture(t, s, r.ActionID, batch)
				members := s.repairMembers(&s.Validation.Waves[0])
				if len(members) != 1 || members[0].task != task || s.developmentResultRecord(task) == nil {
					t.Fatal("single-batch completion created another task or lost its result")
				}
			}
		})
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
			task := pipelineTask(cd, "engine.repair.v5", "candidate")
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
			if err := e.acceptValidationResult(s, plan, 2); err != nil {
				t.Fatal(err)
			}
			s.Results["plan"] = plan
			complete := func(member repairWork) {
				r := WorkerResult{ActionID: member.task.String() + ":completed", Outcome: OutcomePass, Validation: &ValidationResult{Binding: WorkBinding{Task: member.task, WaveID: w.ID, Generation: 1, RequirementRevision: "req"}, Status: "PASS", Development: &DevelopmentEvidence{Paths: member.batch.Artifacts, Summary: "completed owned behavior", Completion: &DevelopmentCompletion{DoD: map[string]string{"normal": "normal input verified"}, Handoff: "owned boundary delivered"}}}}
				r.Validation.Binding.PlanRequest = member.planAction
				r.Validation.Binding.BatchID = member.batch.ID
				r.Validation.Binding.WorkspaceID = r.ActionID + ":workspace"
				s.Results[r.ActionID] = r
				s.DispatchBindings[r.ActionID] = r.Validation.Binding
				recordDevelopmentChecksFixture(t, s, r.ActionID, *member.batch)
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
				b, _ := s.repairGateBinding(key, s.repairWork())
				action := key.String() + ":review"
				s.DispatchBindings[action] = b
				r := WorkerResult{ActionID: action, Outcome: OutcomePass, Validation: &ValidationResult{Binding: b, Status: "FAIL", Findings: []review.Finding{{Severity: severity, Message: "normal command behavior fails", Requirement: "AC-001", NormalPath: "app normal", ActualError: "wrong normal output", DeferralImpact: "normal command unusable", SeverityRationale: "normal behavior contract", Locations: []string{"parser.go:1"}}}}}
				if err := e.acceptValidationResult(s, r, 2); err != nil {
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
				input := s.repairInput(correction.task)
				if input == nil || !input.Obligation.PureP2 || w.Repair.PureP2 {
					t.Fatal("member P2 correction lost its self-run duty in issued input")
				}
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

// State acceptance and native consumption share the same case evidence owner.
func TestRepairEvidenceUsesSelectedManifests(t *testing.T) {
	manifests := []coverage.AcceptanceManifest{{Binding: coverage.ManifestBinding{ReviewKind: coverage.ReviewMerge}, Cases: []coverage.AcceptanceCase{{CaseID: "changed"}, {CaseID: "unchanged"}}}}
	s := &State{SelectedGates: []string{mergeQAGateID}, Validation: &Validation{Sets: map[coverage.ReviewKind]*CaseSet{coverage.ReviewMerge: {Manifest: manifests[0]}}}}
	row := RepairCaseEvidence{Mode: coverage.ReviewMerge, CaseID: "changed", Result: coverage.ExecutedPass, Procedure: "ran exact changed-case command", Observation: "exit=0 output matched changed-case bytes", Comparison: "matched exact declared changed-case oracle"}
	for _, scenario := range []string{"subset", "all", "extra", "outside", "duplicate-affected", "duplicate-run", "wrong-mode", "bad-status", "empty-reason", "missing-optional", "missing-required", "empty-required", "ambiguous"} {
		t.Run(scenario, func(t *testing.T) {
			d := DevelopmentEvidence{AffectedCases: []string{"changed"}, CaseRuns: []RepairCaseEvidence{row}}
			require := false
			approved := manifests
			valid := true
			switch scenario {
			case "all":
				d.AffectedCases = append(d.AffectedCases, "unchanged")
				other := row
				other.CaseID = "unchanged"
				d.CaseRuns = append(d.CaseRuns, other)
			case "extra":
				other := row
				other.CaseID = "unchanged"
				d.CaseRuns = append(d.CaseRuns, other)
				valid = false
			case "outside":
				d.AffectedCases = []string{"outside"}
				valid = false
			case "duplicate-affected":
				d.AffectedCases = append(d.AffectedCases, "changed")
				valid = false
			case "duplicate-run":
				d.CaseRuns = append(d.CaseRuns, row)
				valid = false
			case "wrong-mode":
				d.CaseRuns[0].Mode = coverage.ReviewBlackbox
				valid = false
			case "bad-status":
				d.CaseRuns[0].Result = coverage.ExecutedFail
				valid = false
			case "empty-reason":
				d.CaseRuns[0].Comparison = ""
				valid = false
			case "missing-optional":
				d.CaseRuns = nil
			case "missing-required":
				d.CaseRuns = nil
				require = true
				valid = false
			case "empty-required":
				d.AffectedCases = nil
				d.CaseRuns = nil
				require = true
				valid = false
			case "ambiguous":
				approved = append(append([]coverage.AcceptanceManifest{}, manifests...), manifests[0])
				valid = false
			}
			shared := ValidateRepairEvidence(approved, d, require)
			if (shared == nil) != valid {
				t.Fatalf("shared evidence %s: %v", scenario, shared)
			}
			if scenario != "ambiguous" {
				actual := s.validateRepairEvidence(d, require)
				if (actual == nil) != (shared == nil) || actual != nil && actual.Error() != shared.Error() {
					t.Fatalf("state evidence differs: %v / %v", actual, shared)
				}
			}
		})
	}
}

func TestFinalRepairEvidenceUsesMemberUnion(t *testing.T) {
	task := runtime.TaskKey{Node: "development", Step: "repair", Scope: "candidate"}
	wave := &ValidationWave{ID: "wave", Generation: 1, RequirementRevision: "req", Status: "REPAIR", Repair: &ValidationRepair{Task: task}}
	plan := &GranularityReview{Batches: []DevelopmentBatch{{ID: "first", Checks: []DevelopmentCheck{{ID: "first-check", Command: "test -n first"}}}, {ID: "second", Checks: []DevelopmentCheck{{ID: "second-check", Command: "test -n second"}}}}}
	manifest := coverage.AcceptanceManifest{Binding: coverage.ManifestBinding{ReviewKind: coverage.ReviewMerge}, Cases: []coverage.AcceptanceCase{{CaseID: "first-case"}, {CaseID: "second-case"}}}
	s := &State{RunID: "run", SelectedGates: []string{mergeQAGateID}, DispatchBindings: map[string]WorkBinding{}, Validation: &Validation{Generation: 1, RequirementRevision: "req", Waves: []ValidationWave{*wave}, Sets: map[coverage.ReviewKind]*CaseSet{coverage.ReviewMerge: {Manifest: manifest}}}, Results: map[string]WorkerResult{"plan": {ActionID: "plan", Outcome: OutcomePass, Validation: &ValidationResult{Binding: WorkBinding{Task: task}, GranularityReview: plan}}}}
	wave = &s.Validation.Waves[0]
	var members []DevelopmentEvidence
	final := DevelopmentEvidence{}
	for _, member := range s.repairMembers(wave) {
		caseID := member.batch.ID + "-case"
		evidence := DevelopmentEvidence{Paths: []string{member.batch.ID + ".go", "shared.go"}, AffectedCases: []string{caseID}, CaseRuns: []RepairCaseEvidence{{Mode: coverage.ReviewMerge, CaseID: caseID, Result: coverage.ExecutedPass, Procedure: "ran declared member command", Observation: "exit=0 exact output bytes", Comparison: "output matched member contract"}}}
		binding := WorkBinding{Task: member.task, WaveID: wave.ID, Generation: 1, RequirementRevision: "req", PlanRequest: "plan", BatchID: member.batch.ID, WorkspaceID: member.batch.ID + ":workspace"}
		s.Results[member.batch.ID] = WorkerResult{ActionID: member.batch.ID, Outcome: OutcomePass, Validation: &ValidationResult{Binding: binding, Status: OutcomePass, Development: &evidence}}
		s.DispatchBindings[member.batch.ID] = binding
		if s.developmentResultRecord(member.task) != nil {
			t.Fatal("accepted edit completed the member before native checks")
		}
		recordDevelopmentChecksFixture(t, s, member.batch.ID, *member.batch)
		members = append(members, evidence)
		final.AffectedCases = append(final.AffectedCases, caseID)
		final.CaseRuns = append(final.CaseRuns, evidence.CaseRuns...)
	}
	final.Paths = []string{"first.go", "second.go", "shared.go"}
	work := repairWork{wave: wave, verification: true}
	before, _ := json.Marshal(final)
	if err := s.validateFinalRepairEvidence(work, final); err != nil {
		t.Fatal(err)
	}
	if err := ValidateFinalRepairEvidence(members, final); err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(final)
	if string(before) != string(after) {
		t.Fatal("final acceptance rewrote submitted union")
	}
	for _, scenario := range []string{"missing-path", "duplicate-path", "missing-affected", "missing-run"} {
		t.Run(scenario, func(t *testing.T) {
			bad := final
			switch scenario {
			case "missing-path":
				bad.Paths = bad.Paths[1:]
			case "duplicate-path":
				bad.Paths = append(append([]string{}, bad.Paths...), bad.Paths[0])
			case "missing-affected":
				bad.AffectedCases = bad.AffectedCases[1:]
			case "missing-run":
				bad.CaseRuns = bad.CaseRuns[1:]
			}
			if err := s.validateFinalRepairEvidence(work, bad); err == nil {
				t.Fatal("final verification accepted incomplete member evidence")
			}
		})
	}
}

func (s *State) validateFinalRepairEvidence(work repairWork, e DevelopmentEvidence) error {
	var members []DevelopmentEvidence
	for _, member := range s.repairMembers(work.wave) {
		r := s.developmentResultRecord(member.task)
		if r == nil {
			return validationError("final verification lacks a completed repair member")
		}
		members = append(members, *r.Validation.Development)
	}
	if err := ValidateFinalRepairEvidence(members, e); err != nil {
		return err
	}
	return s.validateRepairEvidence(e, true)
}

func fixtureRepairBatchID(s *State, key runtime.TaskKey) string {
	if w := s.repairMember(key); w != nil && w.batch != nil {
		return w.batch.ID
	}
	return ""
}
