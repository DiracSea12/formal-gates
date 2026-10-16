package protocol

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
	"formal-gates/internal/requirement"
)

func adjudicationWorkerState(t *testing.T) (*Engine, *State, PendingAction, WorkerResultPayload) {
	t.Helper()
	e, s, _ := guardPipelineFixture(t)
	s.NativeRoot = ""
	s.Validation.Document = requirement.FrozenRequirementProjection{Content: "confirmed requirement"}
	wave := s.currentValidationWave()
	wave.Status = "ADJUDICATING"
	var target *ValidationTarget
	for _, value := range wave.Targets {
		value.Status = "PASS"
		if value.Kind == "GATE" {
			target = value
		}
	}
	target.Status = "FAIL"
	target.ResultAction = "original-gate"
	raw := review.Finding{Severity: "P1", Message: "normal input fails", Requirement: "AC-001", NormalPath: "app run valid", ActualError: "valid input rejected", DeferralImpact: "success path unavailable", SeverityRationale: "core behavior unavailable", Locations: []string{"app.go:1"}}
	s.Results[target.ResultAction] = WorkerResult{ActionID: target.ResultAction, Outcome: OutcomePass, PayloadDigest: "actual-gate-output", Validation: &ValidationResult{Status: "FAIL", Findings: []review.Finding{raw}}}
	s.Validation.Findings["finding"] = &ValidationFinding{ID: "finding", WaveID: wave.ID, Target: target.Task.String(), Raw: &raw}
	input := s.validationObservationInput()
	if input == nil {
		t.Fatal("no original semantic obligation")
	}
	task := observationTask(e.cfg.Definition, input)
	pending := PendingAction{ActionID: "semantic", AttemptID: "original-semantic-attempt", Task: task, Step: string(task.Step)}
	s.PendingActions[pending.ActionID] = pending
	binding := s.validationBinding(task, e.cfg.Definition)
	s.DispatchBindings[pending.ActionID] = binding
	s.DispatchIssueRevisions[pending.ActionID] = 2
	s.Attempts[task] = Attempt{IssuedAction: decision.IssuedAction{ActionID: pending.ActionID, Task: task}, ID: pending.AttemptID, Business: true, MaxAttempts: e.retryMaxAttempts(task.Step), Work: &binding}
	s.SpawnReceipts[pending.ActionID] = SpawnReceipt{ActionID: pending.ActionID, Status: SpawnStatusSpawned, Task: task, LifecycleIdentity: "original-native-semantic-session"}
	s.Tasks[task] = runtime.TaskRunning
	resolution := FindingResolution{FindingID: "finding", Decision: "NEW", Requirement: "AC-001", NormalPath: "app run valid", Module: "input", RootCause: "incorrect valid input condition", Consequence: "success path rejected", Severity: "P1", SeverityRationale: "supported behavior unusable", Verification: review.FindingVerification{Scope: "confirmed AC-001", Severity: "normal core path", Causality: "input guard", Reproduction: "app run valid rejects", Classification: "implementation"}, Reason: "verified actual failure"}
	payload := WorkerResultPayload{ActionID: pending.ActionID, Provider: s.RunProvider, Outcome: OutcomePass, PayloadDigest: "actual-semantic-output", Validation: &ValidationResult{Binding: binding, Status: "PASS", Observations: []OperatorObservationPayload{{Subject: wave.ID, Validation: &ValidationObservation{Kind: "ADJUDICATE", Findings: []FindingResolution{resolution}}}}}}
	return e, s, pending, payload
}

func TestSemanticObservationUsesOriginalAdmissionAndRecovery(t *testing.T) {
	t.Parallel()
	e, s, pending, payload := adjudicationWorkerState(t)
	input := s.WorkerInputs(e.cfg.Definition)[pending.ActionID]
	if input.Validation == nil || input.Validation.Observation == nil || input.Validation.Observation.Findings[0].Evidence.PayloadDigest != "actual-gate-output" {
		t.Fatal("current material lacks original evidence")
	}
	bytes, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var reopened State
	if err := json.Unmarshal(bytes, &reopened); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.WorkerInputs(e.cfg.Definition), reopened.WorkerInputs(e.cfg.Definition)) || !reflect.DeepEqual(s.SpawnReceipts, reopened.SpawnReceipts) {
		t.Fatal("serialized state changed original native/input identity")
	}
	s = &reopened
	accepted, changed, err := e.admitWorkerResultRecord(s, pending.ActionID, &payload, "semantic-result", "", 2, 3, Observation{})
	if err != nil || !changed || accepted.Status != "ACCEPTED" {
		t.Fatalf("admission: %#v %v", accepted, err)
	}
	f := s.Validation.Findings["finding"]
	if len(f.History) != 1 || f.History[0].AcceptedRevision != 3 || f.History[0].Severity != "P1" || f.History[0].ProblemID != FindingProblemID(ValidationFinding{ID: f.ID}) || s.Results[pending.ActionID].PayloadDigest != "actual-semantic-output" {
		t.Fatalf("original adjudication/evidence lost: %#v", f)
	}
	repeated, changed, err := e.admitWorkerResultRecord(s, pending.ActionID, &payload, "semantic-repeat", "", 3, 4, Observation{})
	if err != nil || changed || repeated.Status != "DUPLICATE" || len(f.History) != 1 {
		t.Fatalf("repeat changed original judgment: %#v %v", repeated, err)
	}
}

func TestSemanticObservationProjectsExistingProblemOriginalHistoryAndSource(t *testing.T) {
	t.Parallel()
	e, s, _, payload := adjudicationWorkerState(t)
	current := s.Validation.Findings["finding"]
	original := *current
	original.ID, original.WaveID = "original-finding", "original-wave"
	resolution := payload.Validation.Observations[0].Validation.Findings[0]
	resolution.FindingID, resolution.AcceptedRevision = original.ID, 1
	resolution.ProblemID = FindingProblemID(original)
	original.History = []FindingResolution{resolution}
	target := *s.validationFindingTarget(s.currentValidationWave(), current)
	target.ResultAction = "original-source-action"
	s.Validation.Waves = append([]ValidationWave{{ID: original.WaveID, Status: "COMPLETE", Targets: map[string]*ValidationTarget{original.Target: &target}}}, s.Validation.Waves...)
	s.Validation.Findings[original.ID] = &original
	s.Validation.Problems[resolution.ProblemID] = &ValidationProblem{ID: resolution.ProblemID, FindingIDs: []string{original.ID}}
	originalResult := s.Results["original-gate"]
	originalResult.ActionID, originalResult.PayloadDigest, originalResult.SourceResult = target.ResultAction, "original-source-hash", "original-source-path"
	validation := *originalResult.Validation
	validation.Binding = WorkBinding{WaveID: original.WaveID, Task: target.Task}
	originalResult.Validation = &validation
	s.Results[target.ResultAction] = originalResult
	input := s.validationObservationInput()
	if input == nil || len(input.Findings) != 1 || input.Findings[0].Finding.ID != "finding" || len(input.Problems) != 1 || len(input.Problems[0].Findings) != 1 {
		t.Fatal("historical evidence changed the current pending duties")
	}
	evidence := input.Problems[0].Findings[0]
	if input.Problems[0].ID != resolution.ProblemID || !reflect.DeepEqual(evidence.Finding, original) || !reflect.DeepEqual(evidence.Target, target) || !reflect.DeepEqual(evidence.Evidence, originalResult) {
		t.Fatal("existing problem lost its original finding, adjudication, target or source bytes")
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var reopened State
	if err := json.Unmarshal(raw, &reopened); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(input, reopened.validationObservationInput()) || observationTask(e.cfg.Definition, input) != observationTask(e.cfg.Definition, reopened.validationObservationInput()) {
		t.Fatal("reopening changed issued evidence or its task identity")
	}
	if !reflect.DeepEqual(s.Validation.Problems[resolution.ProblemID], reopened.Validation.Problems[resolution.ProblemID]) || !reflect.DeepEqual(s.Results[target.ResultAction], originalResult) {
		t.Fatal("read-only projection mutated a problem or original source")
	}
	s.archiveDispatchResults(target.ResultAction)
	if !reflect.DeepEqual(input, s.validationObservationInput()) || len(s.ObsoleteResults[target.ResultAction]) != 1 || !reflect.DeepEqual(s.ObsoleteResults[target.ResultAction][0], originalResult) {
		t.Fatal("requirement invalidation lost or altered historical problem source")
	}
}

func TestSemanticObservationRejectsStaleAndInvalidOriginalJudgments(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"changed-subject", "missing-reproduction", "illegal-kind", "missing-duty", "later-finding"} {
		t.Run(scenario, func(t *testing.T) {
			e, s, pending, payload := adjudicationWorkerState(t)
			switch scenario {
			case "changed-subject":
				s.currentValidationWave().Candidate.Identity = "next-candidate"
			case "missing-reproduction":
				payload.Validation.Observations[0].Validation.Findings[0].Verification.Reproduction = ""
			case "illegal-kind":
				payload.Validation.Observations[0].Validation.Kind = "SUGGESTION"
			case "missing-duty":
				payload.Validation.Observations = nil
			case "later-finding":
				f := *s.Validation.Findings["finding"]
				f.ID = "later"
				s.Validation.Findings[f.ID] = &f
			}
			original, _ := json.Marshal(s.Validation)
			if scenario == "changed-subject" || scenario == "later-finding" {
				if s.WorkerInputs(e.cfg.Definition)[pending.ActionID].Pending.ActionID != "" {
					t.Fatal("superseded input was offered to a native worker")
				}
				if _, err := e.completeResult(s, workerResultRecord(&payload), 3, Observation{}); err != nil {
					t.Fatal(err)
				}
				if len(s.ObsoleteResults[pending.ActionID]) != 1 || s.ObsoleteResults[pending.ActionID][0].PayloadDigest != payload.PayloadDigest {
					t.Fatal("stale original evidence was not retained")
				}
			} else {
				if _, err := e.prepareValidationResult(s, workerResultRecord(&payload), 3); err == nil {
					t.Fatal("original observation validator accepted false/missing judgment")
				}
			}
			after, _ := json.Marshal(s.Validation)
			if string(original) != string(after) {
				t.Fatal("rejected/stale judgment mutated the validation domain")
			}
		})
	}
}

func TestSemanticObservationBudgetUsesFrozenDuties(t *testing.T) {
	t.Parallel()
	e, s, pending, _ := adjudicationWorkerState(t)
	basis, err := e.guardBasis(s)
	if err != nil {
		t.Fatal(err)
	}
	if !basisDeclaresTask(basis, pending.Task) {
		t.Fatal("frozen obligation was not in original finite dispatch envelope")
	}
	for _, task := range basis.ExternalTasks {
		if task.TaskKey == pending.Task && *task.MaxAttempts != e.retryMaxAttempts(pending.Task.Step) {
			t.Fatal("semantic role invented a separate retry allowance")
		}
	}
	if err := e.guardTaskAuthorized(s, basis, pending.Task); err != nil {
		t.Fatal(err)
	}
	forged := pending.Task
	forged.Scope += "unissued"
	if err := e.guardTaskAuthorized(s, basis, forged); err == nil {
		t.Fatal("unissued semantic subject expands budget")
	}
	s.currentValidationWave().Status = "COLLECTING"
	before, err := e.deriveGuardBudget(s)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 20 {
		id := string(rune('a' + i))
		s.Validation.Findings[id] = &ValidationFinding{ID: id, Raw: &review.Finding{Severity: "P1", Message: "uncollected text"}}
	}
	after, err := e.deriveGuardBudget(s)
	if err != nil || before.RequestAllowance != after.RequestAllowance || before.RetryAllowance != after.RetryAllowance {
		t.Fatalf("raw findings added automatic semantic calls: %#v %#v %v", before, after, err)
	}
	attempt := s.Attempts[pending.Task]
	attempt.Attempts = attempt.MaxAttempts
	attempt.RetryExhausted = true
	s.EndedAttempts[pending.Task] = attempt
	if err := e.guardTaskAuthorized(s, basis, pending.Task); err == nil {
		t.Fatal("semantic retry exhausted policy was bypassed")
	}
}

func TestSemanticObservationProjectsReadyAndOriginalCandidateWorkspace(t *testing.T) {
	t.Parallel()
	e, s, pending, _ := adjudicationWorkerState(t)
	view := decision.State{RequiredTasks: map[authoring.StepID][]decision.RequiredTask{}}
	s.projectValidationTasks(&view, e.cfg.Definition)
	if !slices.ContainsFunc(view.RequiredTasks[pending.Task.Step], func(v decision.RequiredTask) bool { return v.Task == pending.Task && v.BlockedBy == "" }) {
		t.Fatal("semantic duty stayed only an Operator message")
	}
	s.NativeRoot = t.TempDir()
	existing := NativeResource{ID: "original-candidate", Owner: s.RunID, Role: "candidate", Identity: s.currentValidationWave().Candidate.Identity, Source: s.currentValidationWave().Candidate.Identity, Status: "READY", Path: t.TempDir()}
	s.NativeResources = map[string]NativeResource{existing.ID: existing}
	if actual := s.taskResource(pending.Task, e.cfg.Definition); actual != existing {
		t.Fatalf("semantic judgment failed to reuse candidate resource: %#v", actual)
	}
}

func TestNextWorkflowChoiceRetainsOriginalUserConfirmation(t *testing.T) {
	t.Parallel()
	e, s, _ := guardPipelineFixture(t)
	if s.NextWorkflowChoice(e.cfg.Definition) != "" {
		t.Fatal("user choice was exposed before both reviews passed")
	}
	s.ReviewSeries.ProductReview.Effective = review.EffectiveStatus{Status: "PASS", RequirementRevision: "rev-1"}
	s.ReviewSeries.StartReadiness.Effective = review.EffectiveStatus{Status: "PASS", RequirementRevision: "rev-1"}
	s.Topology = nil
	s.RouteConfirmed = false
	if s.NextWorkflowChoice(e.cfg.Definition) != ControlConfirmTopology {
		t.Fatal("missing original topology confirmation")
	}
	s.PendingAsks["topology"] = PendingAsk{Control: ControlConfirmTopology}
	if s.NextWorkflowChoice(e.cfg.Definition) != "" {
		t.Fatal("unresolved original Ask was duplicated")
	}
	ask := s.PendingAsks["topology"]
	ask.Resolved = true
	s.PendingAsks["topology"] = ask
	s.Topology = &TopologyBinding{Decision: "no-split"}
	if s.NextWorkflowChoice(e.cfg.Definition) != ControlSelectRoute {
		t.Fatal("missing original route confirmation")
	}
}
