package protocol

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestDevelopmentIncrementRejectsCallerTaskContentAndPreservesRecordedEvidence(t *testing.T) {
	e := preparationEngine(t)
	gitReceipt(t, e, "base")
	completeBatch(t, e, "B1")
	gitReceipt(t, e, "B1-original")
	for _, extra := range []string{
		`"batch":{"id":"B1","task":"replacement","subtasks":[],"definitionOfDone":"different acceptance"}`,
		`"prompt":"extra dispatch instructions"`,
		`"task":"replacement"`,
		`"definitionOfDone":"different acceptance"`,
	} {
		raw := []byte(`{"kind":"optimize","batchId":"B1","workspaceView":"current-view",` + extra + `}`)
		var increment DevelopmentIncrement
		if err := json.Unmarshal(raw, &increment); err != nil {
			t.Fatal(err)
		}
		// Decoding stored evidence must not discard the rejected/obsolete text.
		recorded, err := json.Marshal(increment)
		if err != nil || !bytes.Equal(recorded, raw) {
			t.Fatalf("historical request changed: %s, %v", recorded, err)
		}
		before, _ := e.Load()
		q := RegularRequest{Action: "development-increment", Target: "B1", Meaning: "preserved", Reason: "authorized retry", Increment: &increment}
		event, err := NewRegularEvent(EventID("reject-caller-content-"+digestOfCanonical(extra)), RegularEvent{Operation: "REQUEST", ExpectedRevision: before.Revision, Request: &q})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = e.Submit(event, fingerprintOf(t, e)); err == nil || !strings.Contains(err.Error(), "task definitions must come from the confirmed plan") {
			t.Fatalf("caller text accepted: %v", err)
		}
		after, _ := e.Load()
		if after.Revision != before.Revision || !reflect.DeepEqual(after.State, before.State) {
			t.Fatal("rejected authoring changed state, attempts or history")
		}
	}
}

func TestDevelopmentIncrementRequiresExistingConfirmedBatch(t *testing.T) {
	e := preparationEngine(t)
	gitReceipt(t, e, "base")
	completeBatch(t, e, "B1")
	gitReceipt(t, e, "B1-original")
	s, _ := e.Load()
	q := RegularRequest{Action: "development-increment", Target: "B1", Meaning: "preserved", Increment: &DevelopmentIncrement{Kind: "optimize", BatchID: "missing", WorkspaceView: "view"}}
	if _, err := incrementalPlan(s.State.Regular, q); err == nil || !strings.Contains(err.Error(), "reference the confirmed plan") {
		t.Fatalf("unregistered Batch accepted: %v", err)
	}
	q.Increment.BatchID = "B1"
	s.State.Regular.Plan.ConfirmedRevision = ""
	if _, err := incrementalPlan(s.State.Regular, q); err == nil || !strings.Contains(err.Error(), "confirm the current development plan") {
		t.Fatalf("unconfirmed plan accepted: %v", err)
	}
}

func TestDevelopmentIncrementOptimizeStartsNewTaskAndPreservesHistory(t *testing.T) {
	e := preparationEngine(t)
	gitReceipt(t, e, "base")
	completeBatch(t, e, "B1")
	gitReceipt(t, e, "B1-original")
	before, _ := e.Load()
	oldResult := before.State.Regular.Preparation.Batches["B1"]
	old := before.State.RegularExecutions[oldResult.ActionID]
	pending := currentExecution(t, e, "development-worker")
	batch := before.State.Regular.Plan.Batches[0]
	regularChoice(t, e, RegularRequest{Action: "development-increment", Target: "B1", Meaning: "preserved", Reason: "fixture user requests in-place optimization", Increment: &DevelopmentIncrement{Kind: "optimize", BatchID: batch.ID, WorkspaceView: "fixture-working-view"}}, "confirm")
	opened, _ := e.Load()
	next := currentExecution(t, e, "development-worker")
	if next.Claim.Task == old.Claim.Task || next.Claim.ActionID == old.Claim.ActionID || next.Claim.Identity != "" || next.Claim.Operation != "spawn" || next.Number != 1 {
		t.Fatalf("optimization must issue new work, not resume an accepted PASS: %+v", next.Claim)
	}
	if !reflect.DeepEqual(opened.State.RegularExecutions[old.Claim.ActionID], old) || opened.State.Regular.Preparation.DevelopmentSnapshot != "B1-original" {
		t.Fatal("old PASS, native history or checkpoint changed")
	}
	if !reflect.DeepEqual(opened.State.Attempts[old.Claim.Task], before.State.Attempts[old.Claim.Task]) || !reflect.DeepEqual(opened.State.Cost, before.State.Cost) {
		t.Fatal("optimization reset previous attempts or cost")
	}
	lineage := opened.State.Regular.DevelopmentIncrements[0]
	if lineage.PreviousTasks["B1"] != old.Claim.Task || lineage.Tasks["B1"] != next.Claim.Task {
		t.Fatal("increment receipt does not link the new task to its predecessor")
	}
	history := opened.State.Regular.PreparationHistory
	if len(history) == 0 || history[len(history)-1].Commits["B1"].After != "B1-original" || !reflect.DeepEqual(history[len(history)-1].Batches["B1"], oldResult) {
		t.Fatal("original batch commit/result missing from history")
	}
	submitRegular(t, e, RegularEvent{Operation: "CLAIM", Claim: &next.Claim})
	source := ExecutionSource{ActionID: next.Claim.ActionID, AttemptID: next.Claim.AttemptID, Provider: next.Claim.Provider, Identity: "developer-B1-increment", Status: "RUNNING"}
	submitRegular(t, e, RegularEvent{Operation: "SOURCE", Source: &source})
	result := preparationResult(next)
	result.Identity = source.Identity
	result.Manifest = []string{"B1.go"}
	result.Handoff = "No unresolved fixture decisions"
	result.Validation = []BatchValidation{{SubtaskID: "B1-S1", Procedure: "deterministic protocol fixture", Observation: "optimization returns on its new TaskKey with original history retained", Result: "PASS"}}
	submitRegular(t, e, RegularEvent{Operation: "PREPARATION_RESULT", PreparationResult: &result})
	source.Status = "UNAVAILABLE"
	source.Reason = "local fixture has no billable native model"
	submitRegular(t, e, RegularEvent{Operation: "SOURCE", Source: &source})
	gitReceipt(t, e, "B1-optimized")
	after, _ := e.Load()
	b2 := currentExecution(t, e, "development-worker")
	if b2.Claim.Task != pending.Claim.Task || b2.Claim.Candidate != "B1-optimized" {
		t.Fatal("next batch did not continue its original lineage from the optimized snapshot")
	}
	if len(after.State.Regular.DevelopmentIncrements) != 1 || !reflect.DeepEqual(after.State.Regular.Plan.Batches[0], batch) {
		t.Fatal("increment decision/plan missing")
	}
	for _, name := range reviewActions {
		if after.State.Regular.Reviews[name].Completed != before.State.Regular.Reviews[name].Completed {
			t.Fatal("increment re-ran prerequisite review", name)
		}
	}
	regularChoice(t, e, RegularRequest{Action: "development-increment", Target: "B1", Meaning: "preserved", Reason: "fixture user requests another optimization with identical plan text", Increment: &DevelopmentIncrement{Kind: "optimize", BatchID: batch.ID, WorkspaceView: "fixture-second-working-view"}}, "confirm")
	again := currentExecution(t, e, "development-worker")
	if again.Claim.Task == next.Claim.Task || again.Claim.Task == old.Claim.Task {
		t.Fatal("repeated optimization reused an already completed TaskKey")
	}
}

func TestDevelopmentIncrementAppendKeepsPreviousBatchEvidence(t *testing.T) {
	e := preparationEngineWithPlan(t, func(plan *DevelopmentPlan) {
		batch := plan.Batches[1]
		batch.ID = "B3"
		batch.Dependencies = []string{"B2"}
		batch.Subtasks = []DevelopmentSubtask{{ID: "B3-S1", Task: "incremental output validation"}}
		plan.Batches = append(plan.Batches, batch)
	})
	gitReceipt(t, e, "base")
	completeBatch(t, e, "B1")
	gitReceipt(t, e, "B1-original")
	before, _ := e.Load()
	b1 := before.State.Regular.Preparation.Batches["B1"]
	pending := currentExecution(t, e, "development-worker")
	batch := before.State.Regular.Plan.Batches[2]
	regularChoice(t, e, RegularRequest{Action: "development-increment", Target: "B2", Meaning: "preserved", Reason: "fixture user appends B3 after existing B2", Impact: map[string]string{"batch:B1": "unaffected"}, Increment: &DevelopmentIncrement{Kind: "append", BatchID: batch.ID, WorkspaceView: "fixture-working-view"}}, "confirm")
	current, _ := e.Load()
	next := currentExecution(t, e, "development-worker")
	if next.Claim.Task != pending.Claim.Task || len(current.State.Regular.Plan.Batches) != 3 || !reflect.DeepEqual(current.State.Regular.Preparation.Batches["B1"], b1) || current.State.Regular.Preparation.Commits["B1"].After != "B1-original" {
		t.Fatal("append re-created old batches or lost their evidence")
	}
	completeBatch(t, e, "B2")
	gitReceipt(t, e, "B2-original")
	next = currentExecution(t, e, "development-worker")
	if next.Claim.Task == pending.Claim.Task || next.Number != 1 {
		t.Fatal("appended batch did not get its own engine-issued task")
	}
	completeBatch(t, e, "B3")
	gitReceipt(t, e, "B3-increment")
	after, _ := e.Load()
	if after.State.Regular.Preparation.Commits["B1"].After != "B1-original" || len(after.State.Regular.DevelopmentIncrements[0].PreviousPlan.Batches) != 3 {
		t.Fatal("append history was rewritten")
	}
}

func TestDevelopmentIncrementClassifiesOnlyUserDelegatedItems(t *testing.T) {
	e := preparationEngineWithPlan(t, func(plan *DevelopmentPlan) {
		batch := plan.Batches[1]
		batch.ID = "B3"
		batch.Dependencies = []string{"B2"}
		batch.Subtasks = []DevelopmentSubtask{{ID: "B3-S1", Task: "incremental output validation"}}
		plan.Batches = append(plan.Batches, batch)
	})
	gitReceipt(t, e, "base")
	completeBatch(t, e, "B1")
	gitReceipt(t, e, "B1-original")
	before, _ := e.Load()
	batch := before.State.Regular.Plan.Batches[2]
	q := RegularRequest{Action: "development-increment", Target: "B2", Meaning: "preserved", Reason: "fixture user appends work", Increment: &DevelopmentIncrement{Kind: "append", BatchID: batch.ID, WorkspaceView: "fixture-working-view"}}
	submitRegular(t, e, RegularEvent{Operation: "REQUEST", Request: &q})
	preview, _ := e.Load()
	a := preview.State.Regular.Ask
	d := RegularDecision{RequestID: a.ID, Token: RegularFreshness(preview.Revision, a.ID), Digest: a.Digest, Choice: "confirm", Dispositions: map[string]string{"batch:B1": "keep"}, Impact: map[string]string{"batch:B1": "affected"}}
	ev, _ := NewRegularEvent("increment-cannot-override-user", RegularEvent{Operation: "DECIDE", ExpectedRevision: preview.Revision, Decision: &d})
	if _, err := e.Submit(ev, fingerprintOf(t, e)); err == nil || !strings.Contains(err.Error(), "USER_IMPACT_CONFLICT") {
		t.Fatalf("operator overrode fixed keep: %v", err)
	}
	unchanged, _ := e.Load()
	if unchanged.Revision != preview.Revision || unchanged.State.Regular.Preparation.Commits["B1"].After != "B1-original" {
		t.Fatal("rejected override changed the Ask or accepted Batch")
	}
	d.Dispositions["batch:B1"] = "delegate"
	d.Impact["batch:B1"] = "unaffected"
	submitRegular(t, e, RegularEvent{Operation: "DECIDE", Decision: &d})
	after, _ := e.Load()
	if after.State.Regular.Preparation.Commits["B1"].After != "B1-original" || len(after.State.Regular.Plan.Batches) != 3 || after.State.Regular.DevelopmentIncrements[0].UserDisposition.Choices["batch:B1"] != "delegate" {
		t.Fatal("increment did not apply the user's bounded delegation")
	}
}
