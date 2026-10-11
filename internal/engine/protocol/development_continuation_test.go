package protocol

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"

	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
	"formal-gates/internal/requirement"
)

func unfinishedDevelopmentState(t *testing.T, role string) (*Engine, *State, PendingAction) {
	t.Helper()
	cd := compiledCanonicalWorkflow(t)
	s, _, task := developmentCheckState(t)
	if role == "engine.repair.v3" {
		task = pipelineTask(cd, role, "repair-candidate")
		s.Validation.Waves = []ValidationWave{{ID: "wave", Status: "REPAIR", Repair: &ValidationRepair{Task: task}}}
	}
	binding := s.DispatchBindings["edit-action"]
	binding.Task = task
	if role == "engine.repair.v3" {
		binding.WaveID = "wave"
	}
	s.DispatchBindings["edit-action"] = binding
	delete(s.Results, "edit-action")
	pending := PendingAction{ActionID: "edit-action", Task: task, Step: string(task.Step), AttemptID: "same-attempt"}
	s.PendingActions = map[string]PendingAction{pending.ActionID: pending}
	s.Attempts = map[runtime.TaskKey]Attempt{task: {IssuedAction: decision.IssuedAction{ActionID: pending.ActionID, Task: task}, ID: pending.AttemptID, Business: true, Attempts: 1, MaxAttempts: 2, Work: &binding}}
	s.SpawnReceipts = map[string]SpawnReceipt{pending.ActionID: {ActionID: pending.ActionID, Task: task, Status: SpawnStatusSpawned, LifecycleIdentity: "original-native-session"}}
	s.RecoverableResults = map[string][]WorkerResult{}
	s.ObsoleteActions = map[string]ObsoleteAction{}
	s.DispatchIssueRevisions = map[string]uint64{pending.ActionID: 1}
	s.Tasks[task] = runtime.TaskRunning
	s.Validation.Document = requirement.FrozenRequirementProjection{Content: "confirmed requirement"}
	s.RunProvider = "host"
	bindings, err := bindInternalSteps(cd, definition.Registry(), nil)
	if err != nil {
		t.Fatal(err)
	}
	s.Phase = runtime.PhaseIntakeRegistered
	s.DefinitionVersion = definition.Version
	return &Engine{cfg: Config{Definition: cd, Registry: definition.Registry(), InternalSteps: bindings}}, s, pending
}

func TestDevelopmentContinuationUsesOriginalAdmissionAndReceipts(t *testing.T) {
	for _, role := range []string{"engine.development.batch.v3", "engine.repair.v3"} {
		t.Run(role, func(t *testing.T) {
			e, s, pending := unfinishedDevelopmentState(t, role)
			originalAttempt := s.Attempts[pending.Task]
			originalReceipt := s.SpawnReceipts[pending.ActionID]
			partial := WorkerResultPayload{ActionID: pending.ActionID, Provider: "host", Outcome: OutcomePass, PayloadDigest: "actual-partial-output", Validation: &ValidationResult{Binding: s.DispatchBindings[pending.ActionID], Status: "CONTINUE", Development: &DevelopmentEvidence{Paths: []string{}, Summary: "Read the confirmed contract", RemainingWork: "Implement app.txt"}}}
			admit := func(payload *WorkerResultPayload, event string, want string) {
				t.Helper()
				accepted, _, err := e.admitWorkerResultRecord(s, pending.ActionID, payload, event, "", 1, 2, "")
				if err != nil || accepted.Status != want {
					t.Fatalf("%s: %#v %v", event, accepted, err)
				}
			}
			admit(&partial, "first", "ACCEPTED")
			first := s.Results[pending.ActionID]
			if s.PendingActions[pending.ActionID] != pending || !reflect.DeepEqual(s.Attempts[pending.Task], originalAttempt) || !reflect.DeepEqual(s.SpawnReceipts[pending.ActionID], originalReceipt) || len(s.RecoveryRecords) != 0 || !s.developmentContinuation(pending.ActionID, e.cfg.Definition) || s.developmentResultRecord(pending.Task) != nil {
				t.Fatal("partial changed original execution or discharged work")
			}
			if _, ready := s.nextEditSource(e.cfg.Definition); ready {
				t.Fatal("partial froze source")
			}
			if _, ready := s.nextCheckParameters(pending.ActionID, e.cfg.Definition); ready {
				t.Fatal("partial ran approved checks")
			}
			input := s.WorkerInputs(e.cfg.Definition)[pending.ActionID]
			var projected *WorkerResult
			if role == "engine.repair.v3" {
				projected = input.Validation.Repair.Result
			} else {
				projected = input.Validation.Development.Result
			}
			if projected == nil || projected.Digest != first.Digest || projected.Validation.Development.RemainingWork != "Implement app.txt" {
				t.Fatal("original progress was not projected")
			}
			admit(&partial, "repeat-first", "DUPLICATE")
			persisted, err := json.Marshal(s)
			if err != nil {
				t.Fatal(err)
			}
			var reopened State
			if err := json.Unmarshal(persisted, &reopened); err != nil {
				t.Fatal(err)
			}
			s = &reopened
			if !s.developmentContinuation(pending.ActionID, e.cfg.Definition) || !reflect.DeepEqual(s.SpawnReceipts[pending.ActionID], originalReceipt) {
				t.Fatal("recovery lost continuation/native identity")
			}
			second := partial
			body := *partial.Validation
			progress := *body.Development
			progress.Paths = []string{"app.txt"}
			progress.Summary = "Delivered command parsing"
			progress.RemainingWork = "Finish output behavior"
			body.Development = &progress
			second.Validation = &body
			second.PayloadDigest = "actual-second-output"
			second.SourceResult = first.Digest
			admit(&second, "second", "ACCEPTED")
			if len(s.RecoverableResults[pending.ActionID]) != 1 || s.RecoverableResults[pending.ActionID][0].PayloadDigest != partial.PayloadDigest {
				t.Fatal("original partial evidence was not retained")
			}
			stale := second
			stale.PayloadDigest = "actual-stale-output"
			if _, _, err := e.admitWorkerResultRecord(s, pending.ActionID, &stale, "stale", "", 2, 3, ""); err == nil {
				t.Fatal("stale source replaced current progress")
			}
			admit(&partial, "repeat-original", "DUPLICATE")
			if role == "engine.development.batch.v3" {
				final := second
				final.PayloadDigest = "actual-final-output"
				final.SourceResult = s.Results[pending.ActionID].Digest
				completed := *second.Validation
				delivered := *completed.Development
				delivered.RemainingWork = ""
				delivered.Completion = &DevelopmentCompletion{DoD: map[string]string{}, Handoff: "Ready for approved checks"}
				completed.Status = "PASS"
				completed.Development = &delivered
				final.Validation = &completed
				admit(&final, "final", "ACCEPTED")
				if s.developmentContinuation(pending.ActionID, e.cfg.Definition) || s.WorkerInputs(e.cfg.Definition)[pending.ActionID].Pending.ActionID != "" || s.Results[pending.ActionID].Validation.Development.RemainingWork != "" {
					t.Fatal("complete edit remained unfinished")
				}
				addDevelopmentSource(t, s)
				for {
					check, ready := s.nextCheckParameters(pending.ActionID, e.cfg.Definition)
					if !ready {
						break
					}
					addCheckReceipt(t, s, check, 0)
				}
				if s.developmentResultRecord(pending.Task) == nil || len(s.RecoverableResults[pending.ActionID]) != 2 {
					t.Fatal("approved checks did not close exact continued edit/history")
				}
			} else {
				if !s.endedWorkCurrent(originalAttempt, e.cfg.Definition) {
					t.Fatal("repair partial discharged recovery duty")
				}
				final := second
				final.PayloadDigest, final.SourceResult = "actual-final-output", s.Results[pending.ActionID].Digest
				completed, delivered := *second.Validation, *second.Validation.Development
				delivered.RemainingWork = ""
				completed.Status, completed.Development = "PASS", &delivered
				final.Validation = &completed
				admit(&final, "final", "ACCEPTED")
				if s.Results[pending.ActionID].Validation.Status != "PASS" || len(s.RecoverableResults[pending.ActionID]) != 2 {
					t.Fatal("repair completion lost original progress evidence")
				}
				admit(&partial, "repeat-after-complete", "DUPLICATE")
			}
		})
	}
}

func TestDevelopmentContinuationCannotClaimCompletionOrOtherRoles(t *testing.T) {
	e, s, pending := unfinishedDevelopmentState(t, "engine.development.batch.v3")
	partial := WorkerResult{ActionID: pending.ActionID, Outcome: OutcomePass, Validation: &ValidationResult{Binding: s.DispatchBindings[pending.ActionID], Status: "CONTINUE", Development: &DevelopmentEvidence{Paths: []string{}, Summary: "Actual progress", RemainingWork: "Remaining behavior"}}}
	claimed := *partial.Validation.Development
	claimed.Completion = &DevelopmentCompletion{Handoff: "done"}
	partial.Validation.Development = &claimed
	if err := e.acceptValidationResult(s, partial, 2); err == nil {
		t.Fatal("partial DoD completion admitted")
	}
	partial.Validation.Status = "PASS"
	if err := e.acceptValidationResult(s, partial, 2); err == nil {
		t.Fatal("complete result retained unfinished work")
	}
	for _, role := range []string{"engine.qa.design.v2", "engine.qa.execution.v2", "engine.gate.v2", "engine.repair.verify.v2"} {
		task := pipelineTask(e.cfg.Definition, role, "candidate")
		binding := partial.Validation.Binding
		binding.Task = task
		s.DispatchBindings[pending.ActionID] = binding
		partial.Validation.Binding = binding
		partial.Validation.Status = "CONTINUE"
		if err := e.acceptValidationResult(s, partial, 2); err == nil {
			t.Fatalf("continuation admitted for %s", role)
		}
	}
}

func TestDevelopmentContinuationRetainsConfirmedRedoAndResetDuty(t *testing.T) {
	_, s, pending := unfinishedDevelopmentState(t, "engine.repair.v3")
	original := RequirementWork{Task: pending.Task}
	entry := ConfirmedRevision{Revision: "req", StateRevision: 1, WorkDecisions: []RequirementWorkDecision{{Work: original, Disposition: RequirementWorkRedo}}}
	task := replacementWorkTask(entry, 0)
	s.RequirementRevisions = []ConfirmedRevision{entry}
	partial := WorkerResult{ActionID: pending.ActionID, Outcome: OutcomePass, Validation: &ValidationResult{Binding: WorkBinding{Task: task}, Status: "CONTINUE", Development: &DevelopmentEvidence{RemainingWork: "Still owed"}}}
	s.Results[pending.ActionID] = partial
	receipt := s.SpawnReceipts[pending.ActionID]
	receipt.Task = task
	s.SpawnReceipts[pending.ActionID] = receipt
	if !slices.Contains(s.requiredWorkTasks(), task) {
		t.Fatal("transport PASS discharged confirmed replacement")
	}
	s.Decisions["reset"] = RecordedDecision{RequestID: "reset", Control: ControlReset, Choice: "proceed", Revision: 3, Reset: &ResetDecision{Applied: true, Confirmed: true}}
	if !s.beforeResetBoundary(pending.ActionID) {
		t.Fatal("unfinished edit escaped the reset boundary")
	}
}

func TestDevelopmentContinuationPreservesFrozenRepairPlan(t *testing.T) {
	e, s, pending := unfinishedDevelopmentState(t, "engine.repair.v3")
	plan := plannedReviewEvidence(review.StartReadiness, "plan", "", "PASS", "approved member", nil).GranularityReview
	origin := pending.Task
	s.Results["plan"] = WorkerResult{ActionID: "plan", Outcome: OutcomePass, Validation: &ValidationResult{Binding: WorkBinding{Task: origin}, Status: "PASS", GranularityReview: plan}}
	member := s.repairMembers(&s.Validation.Waves[0])[0]
	delete(s.Attempts, origin)
	attempt := Attempt{IssuedAction: decision.IssuedAction{ActionID: pending.ActionID, Task: member.task}, ID: pending.AttemptID, Business: true, MaxAttempts: 2}
	pending.Task = member.task
	s.PendingActions[pending.ActionID] = pending
	s.Attempts[member.task] = attempt
	s.Tasks[member.task] = runtime.TaskRunning
	binding := s.DispatchBindings[pending.ActionID]
	binding.Task, binding.PlanRequest, binding.BatchID = member.task, "plan", member.batch.ID
	s.DispatchBindings[pending.ActionID] = binding
	partial := WorkerResult{ActionID: pending.ActionID, Outcome: OutcomePass, Digest: "actual-partial", PayloadDigest: "native-output", Validation: &ValidationResult{Binding: binding, Status: "CONTINUE", Development: &DevelopmentEvidence{Paths: []string{"app.go"}, Summary: "Implemented command parsing", RemainingWork: "Implement output contract"}}}
	if _, err := e.completeResult(s, partial, 2, ""); err != nil {
		t.Fatal(err)
	}
	input := s.WorkerInputs(e.cfg.Definition)[pending.ActionID]
	if input.Validation == nil || input.Validation.Development == nil || input.Validation.Development.Batch.ID != member.batch.ID || input.Validation.Repair.Result == nil || !reflect.DeepEqual(input.Validation.Repair.Plan, plan) {
		t.Fatal("continuation lost original frozen plan/member")
	}
	dod := map[string]string{}
	for _, criterion := range member.batch.DoD {
		dod[criterion.ID] = "Implemented the approved behavior"
	}
	final := partial
	body, delivered := *partial.Validation, *partial.Validation.Development
	body.Status = "PASS"
	delivered.RemainingWork = ""
	delivered.Completion = &DevelopmentCompletion{DoD: dod, Handoff: "Ready for approved checks"}
	body.Development = &delivered
	final.Validation = &body
	if err := e.acceptValidationResult(s, final, 2); err != nil {
		t.Fatalf("completion required re-authoring the approved plan: %v", err)
	}
	if !reflect.DeepEqual(s.Results["plan"].Validation.GranularityReview, plan) || s.developmentResultRecord(member.task) != nil {
		t.Fatal("semantic completion modified plan or bypassed approved checks")
	}
}
