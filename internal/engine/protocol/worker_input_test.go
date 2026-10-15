package protocol

import (
	"encoding/json"
	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
	"formal-gates/internal/requirement"
	"reflect"
	"testing"
)

func TestWorkerInputBindsDefinitionCodecs(t *testing.T) {
	e, s, issued := parallelValidationInputState(t)
	var pending PendingAction
	for _, action := range issued {
		if s.PendingActions[action.ActionID].Task.Step == "development.worker" {
			pending = s.PendingActions[action.ActionID]
		}
	}
	input, ok := s.WorkerInput(pending.ActionID, e.cfg.Definition)
	if !ok || input.InputCodec != WorkerInputCodec || input.OutputCodec != "codec.any.out" || input.Handler != "engine.development.batch.v6" {
		t.Fatalf("issued input lost definition codecs or role: %+v", input)
	}
	body, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var recovered State
	if err := json.Unmarshal(body, &recovered); err != nil {
		t.Fatal(err)
	}
	reopened, ok := recovered.WorkerInput(pending.ActionID, e.cfg.Definition)
	if !ok || !reflect.DeepEqual(reopened, input) {
		t.Fatal("recovered issuance changed typed worker input")
	}
	for i := range e.cfg.Definition.Steps {
		step := &e.cfg.Definition.Steps[i]
		if step.Header.ID != pending.Task.Step {
			continue
		}
		if input.InputCodec != step.IO.InputCodec || input.OutputCodec != step.IO.OutputCodec || input.Handler != string(step.Payload.(compiler.CompiledAgentStep).Handler) {
			t.Fatal("worker input does not carry its compiled declaration")
		}
		step.IO.InputCodec = "codec.unsupported.in"
		if _, ok := s.WorkerInput(pending.ActionID, e.cfg.Definition); ok {
			t.Fatal("unsupported codec selected the existing input projection")
		}
		step.IO.InputCodec = WorkerInputCodec
	}
}

func TestWorkerInputKeepsIssuedParallelValidationFrontier(t *testing.T) {
	e, current, issued := parallelValidationInputState(t)
	body, err := json.Marshal(current)
	if err != nil {
		t.Fatal(err)
	}
	var reopened State
	if err := json.Unmarshal(body, &reopened); err != nil {
		t.Fatal(err)
	}
	qa, development := 0, 0
	for _, action := range issued {
		input, ok := current.WorkerInput(action.ActionID, e.cfg.Definition)
		if !ok || input.InputCodec != WorkerInputCodec || input.OutputCodec != "codec.any.out" {
			t.Fatalf("legal issuance lost its declared codecs: %+v", input)
		}
		recovered, ok := reopened.WorkerInput(action.ActionID, e.cfg.Definition)
		if !ok || !reflect.DeepEqual(recovered, input) {
			t.Fatal("recovery changed parallel role input")
		}
		switch input.Handler {
		case "engine.qa.design.v6":
			qa++
		case "engine.development.batch.v6":
			development++
		}
	}
	if qa != 2 || development != 1 {
		t.Fatalf("parallel QA design/development frontier = %d/%d", qa, development)
	}
}

func parallelValidationInputState(t *testing.T) (*Engine, *State, decision.IssuedSet) {
	t.Helper()
	e, _ := canonicalReviewEngine(t)
	product := issueReady(t, e)[0]
	submitVerifiedReviewResult(t, e, "parallel-product", product, plannedReviewEvidence(review.ProductReview, product, "", "PASS", "complete", nil))
	readiness := pendingActionID(t, e)
	submitVerifiedReviewResult(t, e, "parallel-readiness", readiness, plannedReviewEvidence(review.StartReadiness, readiness, "", "PASS", "complete", nil))
	askAndDecide(t, e, ControlConfirmTopology, "input-topology", topologyOptions(), "no-split", &ReviewDecisionPayload{Topology: &TopologyDecision{Decision: "no-split", Note: "one workspace"}})
	confirmPlannedRoute(t, e, "input-route", RouteDecision{Selected: []string{"blackbox", "whitebox"}, Gates: []string{"blackbox", "whitebox"}})
	snap := e.LoadMustSucceed(t)
	s := snap.State
	s.Validation = &Validation{RequirementRevision: s.RequirementRevision(), Generation: s.requirementGeneration(), Document: requirement.FrozenRequirementProjection{Content: "confirmed requirement"}, Sets: map[coverage.ReviewKind]*CaseSet{}, AuthoringWorkspace: "authoring"}
	s.NativeResources = map[string]NativeResource{}
	view := s.decisionView(e.cfg.Definition)
	for _, tasks := range view.RequiredTasks {
		for _, task := range tasks {
			resource := s.taskResource(task.Task, e.cfg.Definition)
			resource.Status, resource.Identity = "READY", "native-candidate"
			s.NativeResources[resource.ID] = resource
		}
	}
	if _, err := e.commit(s, snap.Revision, Observation{}); err != nil {
		t.Fatal(err)
	}
	plan, _, err := e.Plan(Observation{})
	if err != nil || plan.Next.Kind != decision.KindReady {
		t.Fatalf("parallel public plan did not become ready: %+v %v", plan, err)
	}
	issued, _, err := e.IssueFromPlan(plan, decision.Admission{Capacity: 4}, Observation{})
	if err != nil {
		t.Fatal(err)
	}
	return e, e.LoadMustSucceed(t).State, issued
}

func TestWorkerInputPreservesIssuedExecutionContext(t *testing.T) {
	for _, role := range []string{"engine.development.batch.v6", "engine.repair.v7", "engine.validation.observation.v2"} {
		t.Run(role, func(t *testing.T) {
			e, state, pending := unfinishedDevelopmentState(t, role)
			if role == "engine.validation.observation.v2" {
				e, state, pending, _ = adjudicationWorkerState(t)
			}
			want := state.WorkerInputs(e.cfg.Definition)[pending.ActionID]
			input, current := state.WorkerInput(pending.ActionID, e.cfg.Definition)
			if !current || !reflect.DeepEqual(input, want) {
				t.Fatal("selected input differs from complete issuance projection")
			}
			admitted, identity, err := state.WorkerExecutionIdentity(pending.ActionID, pending.AttemptID, "", e.cfg.Definition)
			if err != nil || identity != "" || !reflect.DeepEqual(admitted, want) {
				t.Fatal("execution admission lost original context", err)
			}
			body, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			var reopened State
			if err := json.Unmarshal(body, &reopened); err != nil {
				t.Fatal(err)
			}
			admitted, _, err = reopened.WorkerExecutionIdentity(pending.ActionID, pending.AttemptID, "", e.cfg.Definition)
			if err != nil || !reflect.DeepEqual(admitted, want) {
				t.Fatal("recovery changed admitted input", err)
			}
			if _, _, err := state.WorkerExecutionIdentity(pending.ActionID, "different-attempt", "", e.cfg.Definition); err == nil {
				t.Fatal("different attempt passed original issuance")
			}
			if _, _, err := state.WorkerExecutionIdentity("different-action", pending.AttemptID, "", e.cfg.Definition); err == nil {
				t.Fatal("different action passed original issuance")
			}
			original := state.SpawnReceipts[pending.ActionID]
			unknown := original
			unknown.Status = SpawnStatusUnknown
			state.SpawnReceipts[pending.ActionID] = unknown
			if _, _, err := state.WorkerExecutionIdentity(pending.ActionID, pending.AttemptID, "", e.cfg.Definition); err == nil {
				t.Fatal("UNKNOWN execution was admitted")
			}
			state.SpawnReceipts[pending.ActionID] = original
			state.PendingAsks = map[string]PendingAsk{"change": {Control: ControlRequestRequirementChange}}
			if _, current := state.WorkerInput(pending.ActionID, e.cfg.Definition); current {
				t.Fatal("requirement barrier admitted selected work")
			}
			if len(state.WorkerInputs(e.cfg.Definition)) != 0 {
				t.Fatal("requirement barrier admitted enumerated work")
			}
		})
	}
}

func TestReadinessInputReusesPreparedCallsFromConfirmedBatch(t *testing.T) {
	engine, s, _ := unfinishedDevelopmentState(t, "engine.development.batch.v6")
	cd := engine.cfg.Definition
	plan := s.Results["readiness"].Review.GranularityReview
	batch := &plan.Batches[0]
	batch.Checks = []DevelopmentCheck{{ID: "build", Purpose: CheckFullBuild, Goal: "Build every production input"}, {ID: "tests", Purpose: CheckFullTests, Goal: "Test all project behavior"}, {ID: "regression", Goal: "Verify the changed public entry"}, {ID: "edit", Purpose: CheckEdit, Command: "original-edit-call"}}
	task := planTask(cd, plan, batch.ID)
	binding := s.DispatchBindings["edit-action"]
	binding.Task = task
	s.DispatchBindings["edit-action"] = binding
	s.Results["edit-action"] = WorkerResult{ActionID: "edit-action", Outcome: OutcomePass, Validation: &ValidationResult{Binding: binding, Status: "PASS", Development: &DevelopmentEvidence{Commands: []string{"prepared-build", "prepared-tests", "prepared-regression"}}}}
	reviewTask := runtime.TaskKey{Scope: "readiness-replan"}
	for _, step := range cd.Steps {
		if payload, ok := step.Payload.(compiler.CompiledAgentStep); ok && payload.Handler == "engine.readiness.plan.v6" {
			reviewTask.Node, reviewTask.Step = step.Header.NodeID, step.Header.ID
		}
	}
	pending := PendingAction{ActionID: "readiness-replan", Task: reviewTask, Step: string(reviewTask.Step), AttemptID: "replan-attempt", ReviewSeries: review.StartReadiness}
	s.PendingActions[pending.ActionID] = pending
	s.Attempts[reviewTask] = Attempt{IssuedAction: decision.IssuedAction{ActionID: pending.ActionID, Task: reviewTask}, ID: pending.AttemptID}
	s.Tasks[reviewTask] = runtime.TaskRunning
	input, ok := s.WorkerInput(pending.ActionID, cd)
	if !ok || input.Handler != "engine.readiness.plan.v6" || input.ApprovedPlan != plan {
		t.Fatal("readiness did not retain its confirmed plan")
	}
	commands := []string{}
	for _, check := range input.ApprovedChecks {
		if check.Command == "" || check.Goal != "" {
			t.Fatal("unbound goal was offered as an existing call")
		}
		commands = append(commands, check.Command)
	}
	expected := []string{"prepared-build", "prepared-tests", "prepared-regression", "original-edit-call"}
	if !reflect.DeepEqual(commands, expected) {
		t.Fatalf("readiness missed prepared calls: %+v", commands)
	}
	body, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var reopened State
	if err := json.Unmarshal(body, &reopened); err != nil {
		t.Fatal(err)
	}
	recovered, ok := reopened.WorkerInput(pending.ActionID, cd)
	if !ok || !reflect.DeepEqual(recovered, input) {
		t.Fatal("replan recovery changed original command choices")
	}
}

func TestWorkerResumeUsesOriginalObservedDispatchIdentity(t *testing.T) {
	for _, status := range []string{SpawnStatusSpawned, SpawnStatusAttached} {
		t.Run(status, func(t *testing.T) {
			cd := compiledCanonicalWorkflow(t)
			engine, err := New(mustStore(t, t.TempDir()), Config{Definition: cd, Registry: definition.Registry(), Capacity: 4})
			if err != nil {
				t.Fatal(err)
			}
			view, err := decision.NewState(definition.Version, runtime.PhaseIntakeRegistered)
			if err != nil {
				t.Fatal(err)
			}
			if err := view.CompleteStep("entry.registered", cd); err != nil {
				t.Fatal(err)
			}
			document := "# Requirement\n\n## 需求点\n\n### REQ-001：运行\n\n#### 要求\n保持原执行身份。\n\n#### 验收条件\n- AC-001：恢复使用原执行身份。\n\n#### 来源\n用户要求。\n"
			projection, err := requirement.ParseRequirementProjection("requirement.md", []byte(document))
			if err != nil {
				t.Fatal(err)
			}
			confirmation := reviewConfirmation()
			confirmation.RequirementRevision = "sha256:" + projection.ContentDigest
			confirmation.Artifacts = []IntakeArtifact{{Path: "requirement.md", Revision: confirmation.RequirementRevision}}
			observedInput := Observation{}
			if err := engine.InitWithMetadata(view, testProvider, observedInput, "native-resume", "regular", confirmation, "", "", nil); err != nil {
				t.Fatal(err)
			}
			if _, err := engine.RecordIntakeReceipt(IntakeReceipt{Confirmation: *confirmation, IntakeDigest: digestOfCanonical(confirmation)}, observedInput); err != nil {
				t.Fatal(err)
			}
			observation := Event{ID: "confirmed-document", Kind: KindOperatorObservation, Observation: &OperatorObservationPayload{Subject: confirmation.RequirementRevision, Validation: &ValidationObservation{Kind: "REQUIREMENT", RequirementRevision: confirmation.RequirementRevision, Document: document}}}
			if _, err := engine.Submit(observation, observedInput); err != nil {
				t.Fatal(err)
			}
			action := issueReady(t, engine)[0]
			if input, current := engine.LoadMustSucceed(t).State.WorkerInput(action, cd); !current || input.Pending.ActionID != action || input.RequirementDocument.ContentDigest != projection.ContentDigest {
				t.Fatalf("canonical producer omitted its confirmed original input: %+v current=%t", input, current)
			}

			if status == SpawnStatusAttached {
				for _, edge := range []string{LifecycleStart, LifecycleStop} {
					if _, err := engine.Submit(mustLifecycleEvent(t, "native-"+edge, "transport-key", "actual-worker", edge), observedInput); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := engine.Submit(unknownSpawnReceipt(t, "native-receipt", action, "transport-key"), observedInput); err != nil {
					t.Fatal(err)
				}
			} else if _, err := engine.Submit(spawnReceipt(t, "native-receipt", action, "actual-worker"), observedInput); err != nil {
				t.Fatal(err)
			}
			recovery, _, err := engine.RecoverAttempt(action, Interruption{InterruptionReason: "native outcome interrupted before completion"}, observedInput)
			if err != nil || recovery.Action != RecoveryAsk {
				t.Fatalf("original recovery = %+v err=%v", recovery, err)
			}
			token, err := engine.Freshness(RequestID(recovery.RequestID))
			if err != nil {
				t.Fatal(err)
			}
			decision, err := NewDecideEvent("resume-authorized", RequestID(recovery.RequestID), token, "resume")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := engine.Submit(decision, observedInput); err != nil {
				t.Fatal(err)
			}
			state := engine.LoadMustSucceed(t).State
			pending := state.PendingActions[action]
			var intent HostActionIntent
			for _, original := range state.PendingHostActions {
				if original.Resume != nil && original.Resume.WorkerActionID == action {
					intent = original
				}
			}
			if intent.Resume == nil || intent.Resume.AttemptID != pending.AttemptID || intent.Resume.Identity != "actual-worker" || state.SpawnReceipts[action].Status != status {
				t.Fatalf("resume lost original execution binding: %+v", intent)
			}
			want, current := state.WorkerInput(action, engine.cfg.Definition)
			input, identity, err := state.WorkerExecutionIdentity(action, pending.AttemptID, intent.ActionID, engine.cfg.Definition)
			if !current || err != nil || identity != "actual-worker" || !reflect.DeepEqual(input, want) {
				t.Fatalf("resume changed original identity/input: %+v identity=%q err=%v", input, identity, err)
			}
			if _, _, err := state.WorkerExecutionIdentity(action, "different-attempt", intent.ActionID, engine.cfg.Definition); err == nil {
				t.Fatal("resume admitted a different attempt")
			}
			original := state.SpawnReceipts[action]
			mismatch := original
			mismatch.LifecycleIdentity = "different-native-worker"
			state.SpawnReceipts[action] = mismatch
			if _, _, err := state.WorkerExecutionIdentity(action, pending.AttemptID, intent.ActionID, engine.cfg.Definition); err == nil {
				t.Fatal("resume admitted a different native identity")
			}
			unknown := original
			unknown.Status = SpawnStatusUnknown
			state.SpawnReceipts[action] = unknown
			if _, _, err := state.WorkerExecutionIdentity(action, pending.AttemptID, intent.ActionID, engine.cfg.Definition); err == nil {
				t.Fatal("resume admitted an unresolved dispatch")
			}
		})
	}
}
