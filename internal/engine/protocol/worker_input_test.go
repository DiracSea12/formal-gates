package protocol

import (
	"encoding/json"
	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
	"reflect"
	"testing"
)

func TestWorkerInputPreservesIssuedExecutionContext(t *testing.T) {
	for _, role := range []string{"engine.development.batch.v4", "engine.repair.v5", "engine.validation.observation.v2"} {
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
	engine, s, _ := unfinishedDevelopmentState(t, "engine.development.batch.v4")
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
		if payload, ok := step.Payload.(compiler.CompiledAgentStep); ok && payload.Handler == "engine.readiness.plan.v4" {
			reviewTask.Node, reviewTask.Step = step.Header.NodeID, step.Header.ID
		}
	}
	pending := PendingAction{ActionID: "readiness-replan", Task: reviewTask, Step: string(reviewTask.Step), AttemptID: "replan-attempt", ReviewSeries: review.StartReadiness}
	s.PendingActions[pending.ActionID] = pending
	s.Attempts[reviewTask] = Attempt{IssuedAction: decision.IssuedAction{ActionID: pending.ActionID, Task: reviewTask}, ID: pending.AttemptID}
	s.Tasks[reviewTask] = runtime.TaskRunning
	input, ok := s.WorkerInput(pending.ActionID, cd)
	if !ok || input.Handler != "engine.readiness.plan.v4" || input.ApprovedPlan != plan {
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
