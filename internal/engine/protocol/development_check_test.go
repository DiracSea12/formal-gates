package protocol

import (
	"encoding/json"
	"testing"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
	"slices"
)

func developmentCheckState(t *testing.T) (*State, DevelopmentBatch, runtime.TaskKey) {
	t.Helper()
	cd := compiledCanonicalWorkflow(t)
	batch := DevelopmentBatch{ID: "source", Checks: []DevelopmentCheck{{ID: "quoted", Command: "printf '%s' 'a b' | cat"}, {ID: "exit", Command: "sh -c 'exit 2'; test $? -eq 2"}}}
	dependent := DevelopmentBatch{ID: "consumer", DependsOn: []string{batch.ID}}
	plan := &GranularityReview{Batches: []DevelopmentBatch{batch, dependent}}
	task := planTask(cd, plan, batch.ID)
	binding := WorkBinding{Task: task, PlanRequest: "route", BatchID: batch.ID, RequirementRevision: "req", Generation: 1, WorkspaceID: "edit"}
	source := WorkerResult{ActionID: "edit-action", Outcome: OutcomePass, Validation: &ValidationResult{Binding: binding, Status: "PASS", Development: &DevelopmentEvidence{Paths: []string{"app.txt"}, Completion: &DevelopmentCompletion{Handoff: "ready"}}}}
	s := &State{RunID: "run", NativeRoot: t.TempDir(), Validation: &Validation{RequirementRevision: "req", Generation: 1}, Results: map[string]WorkerResult{"edit-action": source, "readiness": {ActionID: "readiness", Outcome: OutcomePass, Review: &ReviewResultEvidence{Action: review.StartReadiness, GranularityReview: plan}}}, DispatchBindings: map[string]WorkBinding{"edit-action": binding}, NativeResources: map[string]NativeResource{"edit": {ID: "edit", Owner: "run", Status: "READY", Identity: "base"}}, Decisions: map[string]RecordedDecision{"route": {Control: ControlSelectRoute, Review: &ReviewDecisionPayload{Route: &RouteDecision{DevelopmentPlan: &DevelopmentPlanConfirmation{ReviewActionID: "readiness"}}}}}, PendingHostActions: map[string]HostActionIntent{}, HostActionReceipts: map[string]HostActionReceipt{}}
	s.Tasks = map[runtime.TaskKey]runtime.TaskStatus{task: runtime.TaskTerminal}
	s.RequirementRevisions = []ConfirmedRevision{{Revision: "req"}}
	s.ReviewSeries.StartReadiness.Effective = review.EffectiveStatus{Status: "PASS", RequirementRevision: "req", ResultDigest: "readiness-digest"}
	s.ReviewSeries.StartReadiness.RawCandidates = []review.RawCandidate{{DispatchID: "readiness", ResultDigest: "readiness-digest"}}
	return s, batch, task
}

func addDevelopmentSource(t *testing.T, s *State) CandidateEvidence {
	t.Helper()
	p, ready := s.nextEditSource(compiledCanonicalWorkflow(t))
	if !ready {
		t.Fatal("edit source not ready")
	}
	raw, _ := normalizeHostActionParams(p)
	intent := HostActionIntent{ActionID: "freeze", Adapter: &AdapterHostAction{Operation: "op.candidate", Params: raw}, PayloadDigest: "freeze-digest"}
	proof := CandidateEvidence{ParametersDigest: intent.PayloadDigest, WorkspaceID: "edit", Identity: "source", Digest: "source-tree", Paths: p.Paths}
	values, _ := normalizeHostActionParams(proof)
	s.PendingHostActions[intent.ActionID] = intent
	s.HostActionReceipts[intent.ActionID] = HostActionReceipt{ActionID: intent.ActionID, AdapterOperation: "op.candidate", Status: HostActionStatusExecuted, AdapterEvidence: &AdapterEvidence{Values: values}}
	r := s.checkResource("edit-action", proof)
	r.Status, r.Identity = "READY", "source"
	s.NativeResources[r.ID] = r
	return proof
}

func addCheckReceipt(t *testing.T, s *State, p CheckParameters, exit int) HostActionIntent {
	t.Helper()
	values, _ := normalizeHostActionParams(p)
	intent := HostActionIntent{ActionID: p.Check.ID, Adapter: &AdapterHostAction{Operation: "op.check", Params: values}, PayloadDigest: p.Check.ID + "-digest"}
	proof := CheckEvidence{ParametersDigest: intent.PayloadDigest, SourceActionID: p.SourceActionID, SourceIdentity: p.SourceIdentity, SourceDigest: p.SourceDigest, WorkspaceID: p.WorkspaceID, RequestDigest: "request-digest", Directory: "evidence", ExitCode: exit, Stdout: CheckStreamProof{SHA256: "out"}, Stderr: CheckStreamProof{SHA256: "err"}}
	encoded, _ := normalizeHostActionParams(proof)
	s.PendingHostActions[intent.ActionID] = intent
	s.HostActionReceipts[intent.ActionID] = HostActionReceipt{ActionID: intent.ActionID, AdapterOperation: "op.check", Status: HostActionStatusExecuted, AdapterEvidence: &AdapterEvidence{Values: encoded}}
	return intent
}

func TestDevelopmentCheckCompletionOwnsDependency(t *testing.T) {
	s, batch, task := developmentCheckState(t)
	cd := compiledCanonicalWorkflow(t)
	if s.developmentResult(task) != nil {
		t.Fatal("worker PASS completed approved commands")
	}
	addDevelopmentSource(t, s)
	for i, command := range batch.Checks {
		p, ready := s.nextCheckParameters("edit-action", cd)
		if !ready || p.Check != command {
			t.Fatalf("approved command order/value: %#v", p)
		}
		if s.developmentResult(task) != nil {
			t.Fatal("partial checks completed batch")
		}
		view := decision.State{RequiredTasks: map[authoring.StepID][]decision.RequiredTask{}}
		s.projectDevelopmentTasks(&view, cd)
		for _, tasks := range view.RequiredTasks {
			for _, required := range tasks {
				if required.Task == task {
					t.Fatal("pending checks reissued editing task")
				}
			}
		}
		addCheckReceipt(t, s, p, 0)
		if i == len(batch.Checks)-1 && s.developmentResult(task) == nil {
			t.Fatal("all actual checks did not complete batch")
		}
	}
	// Persistence keeps the raw edit outcome and the same execution receipts.
	body, _ := json.Marshal(s)
	var reopened State
	if err := json.Unmarshal(body, &reopened); err != nil {
		t.Fatal(err)
	}
	if reopened.developmentResult(task) == nil || reopened.Results["edit-action"].Outcome != OutcomePass {
		t.Fatal("reopen lost completion/raw result")
	}
}

func TestDevelopmentCheckFailureCannotBeTextPass(t *testing.T) {
	s, _, task := developmentCheckState(t)
	cd := compiledCanonicalWorkflow(t)
	addDevelopmentSource(t, s)
	p, ready := s.nextCheckParameters("edit-action", cd)
	if !ready {
		t.Fatal("not ready")
	}
	intent := addCheckReceipt(t, s, p, 1)
	if s.developmentResult(task) != nil || s.developmentCheckFailure() == nil {
		t.Fatal("actual failure did not block completion")
	}
	if _, ready := s.nextCheckParameters("edit-action", cd); ready {
		t.Fatal("business failure became automatic retry")
	}
	original := s.HostActionReceipts[intent.ActionID]
	for _, field := range []string{"sourceActionId", "sourceIdentity", "sourceDigest", "workspaceId", "parametersDigest"} {
		altered := original
		values := map[string]any{}
		for key, value := range original.AdapterEvidence.Values {
			values[key] = value
		}
		values[field] = "other"
		altered.AdapterEvidence = &AdapterEvidence{Values: values}
		if validateCheckReceipt(intent, altered) == nil {
			t.Fatalf("receipt admitted changed %s", field)
		}
	}
}

func TestFailedDeveloperKeepsSharedEditingFrontierOpen(t *testing.T) {
	s, _, task := developmentCheckState(t)
	cd := compiledCanonicalWorkflow(t)
	addDevelopmentSource(t, s)
	p, ready := s.nextCheckParameters("edit-action", cd)
	if !ready {
		t.Fatal("original check not ready")
	}
	addCheckReceipt(t, s, p, 1)
	s.PendingActions = map[string]PendingAction{"edit-action": {ActionID: "edit-action", Task: task}}
	if s.editWorkspaceQuiescent("edit", cd) {
		t.Fatal("a developer eligible to change its failed source was treated as a settled writer")
	}
	// The next accepted edit has a distinct result identity. Its source cannot
	// borrow the preceding failed source; the original receipt remains in place.
	current := s.Results["edit-action"]
	current.Digest = "new-edit-digest"
	s.Results["edit-action"] = current
	if !s.editWorkspaceQuiescent("edit", cd) {
		t.Fatal("returned new edit did not settle its writer")
	}
	if _, frozen := s.editSourceProof("edit-action"); frozen {
		t.Fatal("new edit borrowed failed source")
	}
	if source, ready := s.nextEditSource(cd); !ready || source.SourceResults["edit-action"] != current.Digest {
		t.Fatal("new freeze lacks exact delivered result digest")
	}
}

func TestDevelopmentCheckSharedFrontierFreezesAfterWriters(t *testing.T) {
	s, batch, firstTask := developmentCheckState(t)
	cd := compiledCanonicalWorkflow(t)
	plan := s.Results["readiness"].Review.GranularityReview
	independent := DevelopmentBatch{ID: "independent", Checks: []DevelopmentCheck{{ID: "actual", Command: "false"}}}
	plan.Batches = append(plan.Batches, independent, DevelopmentBatch{ID: "blocked", DependsOn: []string{independent.ID}})
	firstTask = planTask(cd, plan, batch.ID)
	firstBinding := s.DispatchBindings["edit-action"]
	firstBinding.Task = firstTask
	s.DispatchBindings["edit-action"] = firstBinding
	firstResult := s.Results["edit-action"]
	firstResult.Validation.Binding = firstBinding
	s.Results["edit-action"] = firstResult
	s.Tasks[firstTask] = runtime.TaskTerminal
	otherTask := planTask(cd, plan, independent.ID)
	binding := s.DispatchBindings["edit-action"]
	binding.Task, binding.BatchID = otherTask, independent.ID
	other := s.Results["edit-action"]
	other.ActionID = "other-edit"
	copy := *other.Validation
	copy.Binding = binding
	dev := *copy.Development
	dev.Paths = []string{"other.txt"}
	copy.Development = &dev
	other.Validation = &copy
	s.DispatchBindings[other.ActionID] = binding
	s.PendingActions = map[string]PendingAction{other.ActionID: {ActionID: other.ActionID, Task: otherTask}}
	if _, ready := s.nextEditSource(cd); ready {
		t.Fatal("froze while another shared writer was active")
	}
	delete(s.PendingActions, other.ActionID)
	s.EndedAttempts = map[runtime.TaskKey]Attempt{otherTask: {AwaitingResult: true, Work: &binding}}
	if _, ready := s.nextEditSource(cd); ready {
		t.Fatal("froze while another shared writer owed its result")
	}
	delete(s.EndedAttempts, otherTask)
	s.Results[other.ActionID] = other
	s.Tasks[otherTask] = runtime.TaskTerminal
	p, ready := s.nextEditSource(cd)
	if !ready || !sameTaskSet(p.SourceTasks, []runtime.TaskKey{firstTask, otherTask}) || !slices.Equal(p.Paths, []string{"app.txt", "other.txt"}) {
		t.Fatalf("shared complete frontier %#v ready %v", p, ready)
	}
	proof := addDevelopmentSource(t, s)
	if otherProof, ok := s.editSourceProof(other.ActionID); !ok || otherProof.Identity != proof.Identity || otherProof.Digest != proof.Digest {
		t.Fatal("parallel edits did not share one frozen source")
	}
	resource := s.checkResource(other.ActionID, proof)
	resource.Status, resource.Identity = "READY", proof.Identity
	s.NativeResources[resource.ID] = resource
	for range batch.Checks {
		check, ready := s.nextCheckParameters("edit-action", cd)
		if !ready {
			t.Fatal("missing first check")
		}
		addCheckReceipt(t, s, check, 0)
	}
	otherCheck, ready := s.nextCheckParameters(other.ActionID, cd)
	if !ready {
		t.Fatal("missing independent check")
	}
	addCheckReceipt(t, s, otherCheck, 1)
	if s.developmentResult(firstTask) == nil || s.developmentResult(otherTask) != nil {
		t.Fatal("parallel actual verdicts were merged")
	}
	dependent := s.currentPlanTask(cd, plan, "consumer")
	blocked := s.currentPlanTask(cd, plan, "blocked")
	// Native workspaces are orthogonal to the dependency verdict in this projection.
	s.NativeRoot = ""
	view := decision.State{RequiredTasks: map[authoring.StepID][]decision.RequiredTask{}}
	s.projectDevelopmentTasks(&view, cd)
	found := map[runtime.TaskKey]decision.WaitReason{}
	for _, tasks := range view.RequiredTasks {
		for _, required := range tasks {
			found[required.Task] = required.BlockedBy
		}
	}
	if found[dependent] != "" || found[blocked] != decision.WaitDependencies {
		t.Fatalf("dependency checks did not unlock separately %#v", found)
	}
}

func TestMissingVerificationCallsRequireBoundActualReceipts(t *testing.T) {
	s, batch, _ := developmentCheckState(t)
	cd := compiledCanonicalWorkflow(t)
	for i := range batch.Checks {
		batch.Checks[i].Goal, batch.Checks[i].Command = "Verify the confirmed project boundary", ""
	}
	plan := s.Results["readiness"].Review.GranularityReview
	plan.Batches[0] = batch
	task := planTask(cd, plan, batch.ID)
	source := s.Results["edit-action"]
	source.Validation.Binding.Task = task
	s.DispatchBindings[source.ActionID] = source.Validation.Binding
	s.Results[source.ActionID] = source
	if s.developmentResult(task) != nil {
		t.Fatal("missing calls completed development")
	}
	if _, err := bindDevelopmentChecks(batch, nil); err == nil {
		t.Fatal("missing calls counted as skipped checks")
	}
	if _, err := bindDevelopmentChecks(batch, []string{"one"}); err == nil {
		t.Fatal("partial binding accepted")
	}
	source.Validation.Development.Commands = []string{"build-project", "tests-project"}
	s.Results[source.ActionID] = source
	addDevelopmentSource(t, s)
	first, ready := s.nextCheckParameters(source.ActionID, cd)
	if !ready || first.Check.ID != batch.Checks[0].ID || first.Check.Command != "build-project" || first.Check.Goal != "" {
		t.Fatalf("program did not bind the confirmed obligation: %+v", first)
	}
	addCheckReceipt(t, s, first, 0)
	second, ready := s.nextCheckParameters(source.ActionID, cd)
	if !ready || second.Check.Command != "tests-project" {
		t.Fatal("second missing call was lost")
	}
	addCheckReceipt(t, s, second, 127)
	if s.developmentResult(task) != nil || !s.developmentChecksFailed(source) {
		t.Fatal("actual missing-command failure completed development")
	}
	data, _ := json.Marshal(s)
	var reopened State
	if err := json.Unmarshal(data, &reopened); err != nil {
		t.Fatal(err)
	}
	if !reopened.developmentChecksFailed(source) || reopened.developmentResult(task) != nil {
		t.Fatal("recovery changed failed command qualification")
	}
}
