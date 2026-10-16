package protocol

import (
	"encoding/json"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/runtime"
	"reflect"
	"testing"
)

func TestFiniteRecoveryBudgetOffersRetryAndReopensIneffectiveDecision(t *testing.T) {
	t.Parallel()
	task := runtime.TaskKey{Node: "review", Step: "review.worker"}
	s := NewState(decision.State{})
	s.Attempts[task] = Attempt{IssuedAction: decision.IssuedAction{ActionID: "review-action", Task: task}, ID: "review-attempt", Attempts: 1, MaxAttempts: 2}
	s.PendingActions["review-action"] = PendingAction{ActionID: "review-action", Task: task, AttemptID: "review-attempt"}
	e := Engine{cfg: Config{Definition: compiledCanonicalWorkflow(t)}}
	if e.guardEffectBudget(s, "review-action", 15) == nil {
		t.Fatal("finite recovery budget did not block")
	}
	var original PendingAsk
	for _, ask := range s.PendingAsks {
		original = ask
	}
	// A persisted decision keeps its original facts; the current offer owns repair.
	s.Decisions[original.RequestID] = RecordedDecision{RequestID: original.RequestID, Choice: "continue", Revision: 16}
	original.Resolved = true
	s.PendingAsks[original.RequestID] = original
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var reopened State
	if err := json.Unmarshal(raw, &reopened); err != nil {
		t.Fatal(err)
	}
	// Persisted choices may include continue at a finite retry boundary.
	ask := reopened.PendingAsks[original.RequestID]
	ask.Options = []AskOption{{ID: "continue", Label: "Unlock the current Ready batch once"}, {ID: "retry", Label: "Add one retry slot to the boundary action"}, {ID: "skip", Label: "Keep the boundary blocked"}, {ID: "abort", Label: "Abort this run"}}
	reopened.PendingAsks[original.RequestID] = ask
	preservedAsk := reopened.PendingAsks[original.RequestID]
	preservedDecision := reopened.Decisions[original.RequestID]
	if e.guardEffectBudget(&reopened, "review-action", 17) == nil {
		t.Fatal("continue bypassed the finite retry policy")
	}
	var current PendingAsk
	for _, ask := range reopened.PendingAsks {
		if !ask.Resolved {
			current = ask
		}
	}
	if current.RequestID == "" {
		t.Fatal("ineffective continue left recovery without a typed budget request")
	}
	for _, option := range current.Options {
		if option.ID == "continue" {
			t.Fatal("finite retry boundary advertises ineffective continue")
		}
	}
	if !reflect.DeepEqual(reopened.PendingAsks[original.RequestID], preservedAsk) || !reflect.DeepEqual(reopened.Decisions[original.RequestID], preservedDecision) {
		t.Fatal("original request or decision was rewritten")
	}
	var summary struct {
		Detail struct {
			ActionID, AttemptID                   string
			Attempts, MaxAttempts, ResumeInFlight int
		}
	}
	if err := json.Unmarshal(current.Validation.Summary, &summary); err != nil {
		t.Fatal(err)
	}
	if summary.Detail.ActionID != "review-action" || summary.Detail.AttemptID != "review-attempt" || summary.Detail.Attempts != 1 || summary.Detail.MaxAttempts != 2 || summary.Detail.ResumeInFlight != 0 {
		t.Fatal("budget summary lost original finite execution facts")
	}
	if e.guardEffectBudget(&reopened, "review-action", 18) == nil || len(reopened.PendingAsks) != 2 {
		t.Fatal("repeated boundary changed its original request")
	}
}
