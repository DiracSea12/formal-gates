package facade

import (
	"encoding/json"
	"strings"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
)

func TestQARevisionCarriesOriginalReviewExplanation(t *testing.T) {
	f := facadePreparation(t, []string{"blackbox"})
	run, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	claim := facadeExecution(t, f, "blackbox-design").Claim
	previous := run.Executions[claim.ActionID]
	qa := run.Regular.Preparation.QA["blackbox"]
	qa.PreviousDesignActionID = claim.ActionID
	qa.RetainedReview = &coverage.ReviewResult{SetStatus: coverage.StatusFail}
	qa.UserInstructions = "Use the revised fixture boundary."
	review := previous
	review.Claim.Task.Step = "blackbox-review"
	review.PreparationResult = &protocol.PreparationResult{Status: "FAIL", Message: "The amber branch lacks an observable output oracle.", Review: qa.RetainedReview}
	run.Executions["review"] = review
	unrelated := review
	unrelated.Claim.Task.Scope = "another-round"
	unrelated.PreparationResult = &protocol.PreparationResult{Status: "FAIL", Message: "unrelated obsolete gap", Review: qa.RetainedReview}
	run.Executions["unrelated"] = unrelated
	// Matching review bindings alone do not make an old round current. Even
	// a rejected result from this scope must not become repair instructions.
	rejected := review
	rejected.PreparationResult = &protocol.PreparationResult{ActionID: "rejected", Status: "FAIL", Message: "rejected obsolete gap", Review: qa.RetainedReview}
	run.Executions["rejected"] = rejected
	run.Regular.PreparationErrors = append(run.Regular.PreparationErrors, protocol.PreparationError{Result: *rejected.PreparationResult, Reason: "invalid result"})

	payload, workspace, err := preparationInput(run, claim)
	if err != nil {
		t.Fatal(err)
	}
	m := nativePromptMaterial{input: payload, workspace: workspace}
	if err := m.bindFiles(t.TempDir(), run.Regular.Requirement, f.root); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(m.files["qa-review.json"], &got); err != nil {
		t.Fatal(err)
	}
	raw := string(m.files["qa-review.json"])
	if !strings.Contains(raw, review.PreparationResult.Message) || strings.Contains(raw, "unrelated obsolete gap") || strings.Contains(raw, "rejected obsolete gap") {
		t.Fatalf("review explanation: %s", raw)
	}
	if payload["userInstructions"] != qa.UserInstructions {
		t.Fatal("scope instruction overwritten")
	}
}

type queuedMessageTransport struct {
	nativeExecutionTransport
	prompt string
}

func (q *queuedMessageTransport) turn(turn nativeTurn) (string, error) {
	q.prompt = turn.Prompt
	return turn.Claim.Identity, nil
}

func TestQueuedMessageNextLegalTurnAndInterruptedDelivery(t *testing.T) {
	f := facadePreparation(t, nil)
	x := facadeExecution(t, f, "development-worker")
	preparationSubmit(t, f, protocol.RegularEvent{Operation: "CLAIM", Claim: &x.Claim})
	identity := "fixture-native"
	run := preparationSubmit(t, f, protocol.RegularEvent{Operation: "SOURCE", Source: &protocol.ExecutionSource{ActionID: x.Claim.ActionID, AttemptID: x.Claim.AttemptID, Provider: x.Claim.Provider, Identity: identity, Status: "RUNNING"}})
	m := protocol.TaskMessage{ID: "interrupted", ActionID: x.Claim.ActionID, AttemptID: x.Claim.AttemptID, Text: "already attempted text"}
	run = preparationSubmit(t, f, protocol.RegularEvent{Operation: "MESSAGE", Message: &m})
	if _, err := f.pendingTaskMessage(x.Claim.ActionID, identity); err != nil {
		t.Fatal(err)
	}
	m.ID, m.Text = "pending", "next legal turn instruction"
	run = preparationSubmit(t, f, protocol.RegularEvent{Operation: "MESSAGE", Message: &m})
	claim := run.Executions[x.Claim.ActionID].Claim
	claim.Identity = identity
	adapter := &queuedMessageTransport{}
	_, err := f.callNativeTurn(run, claim, true, t.TempDir(), t.TempDir(), "bound task", map[string]any{"type": "object"}, "", false, adapter)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(adapter.prompt, m.Text) || strings.Contains(adapter.prompt, "already attempted text") {
		t.Fatalf("queued prompt: %s", adapter.prompt)
	}
	run, err = f.Show()
	if err != nil {
		t.Fatal(err)
	}
	messages := run.Executions[x.Claim.ActionID].Messages
	if messages["pending"].Status != "ACCEPTED" || messages["interrupted"].Status != "FAILED" {
		t.Fatalf("delivery state: %+v", messages)
	}
	if run.Executions[x.Claim.ActionID].Finished {
		t.Fatal("message acceptance ended task")
	}
}

func TestPendingMessageFollowsSameIdentityContinuation(t *testing.T) {
	f := facadePreparation(t, nil)
	x := facadeExecution(t, f, "development-worker")
	preparationSubmit(t, f, protocol.RegularEvent{Operation: "CLAIM", Claim: &x.Claim})
	identity := "fixture-native"
	source := protocol.ExecutionSource{ActionID: x.Claim.ActionID, AttemptID: x.Claim.AttemptID, Provider: x.Claim.Provider, Identity: identity, Status: "RUNNING"}
	preparationSubmit(t, f, protocol.RegularEvent{Operation: "SOURCE", Source: &source})
	m := protocol.TaskMessage{ID: "pending-note", ActionID: x.Claim.ActionID, AttemptID: x.Claim.AttemptID, Text: "Keep the amber observation during continuation."}
	preparationSubmit(t, f, protocol.RegularEvent{Operation: "MESSAGE", Message: &m})
	result := protocol.PreparationResult{ActionID: x.Claim.ActionID, AttemptID: x.Claim.AttemptID, Provider: x.Claim.Provider, Identity: identity, RequirementRevision: x.Claim.RequirementRevision, Candidate: x.Claim.Candidate, Status: "CONTINUE", Message: "fixture host interruption"}
	preparationSubmit(t, f, protocol.RegularEvent{Operation: "PREPARATION_RESULT", PreparationResult: &result})
	source.Status, source.Reason = "UNAVAILABLE", "fixture has no native usage"
	preparationSubmit(t, f, protocol.RegularEvent{Operation: "SOURCE", Source: &source})
	next := facadeExecution(t, f, "development-worker")
	if next.Claim.ActionID == x.Claim.ActionID {
		t.Fatal("no continuation")
	}
	delivered, err := f.pendingTaskMessage(next.Claim.ActionID, identity)
	if err != nil || delivered == nil || delivered.ActionID != x.Claim.ActionID {
		t.Fatalf("original pending message lost: %+v %v", delivered, err)
	}
	delivered.Status = "ACCEPTED"
	if err = f.recordTaskMessage(*delivered); err != nil {
		t.Fatal(err)
	}
	if again, err := f.pendingTaskMessage(next.Claim.ActionID, identity); err != nil || again != nil {
		t.Fatalf("continuation duplicated delivery: %+v %v", again, err)
	}
}
