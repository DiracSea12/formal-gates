package protocol

import (
	"encoding/json"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/runtime"
)

func TestQARevisionProjectionUsesCurrentOriginalDecision(t *testing.T) {
	mode := coverage.ReviewBlackbox
	history := CaseReviewEvidence{Manifest: coverage.AcceptanceManifest{Cases: []coverage.AcceptanceCase{{CaseID: "case"}}}}
	set := &CaseSet{AffectedCases: []string{"case"}, ReviewHistory: []CaseReviewEvidence{history}}
	binding := WorkBinding{RequirementRevision: "confirmed", Generation: 4, Task: runtime.TaskKey{Node: "qa", Step: "design"}}
	for _, kind := range []string{"THAW", "QA_ARTIFACT_REPAIR", "VALIDATION_DETAIL_DISPOSITION", "QA_REVIEW"} {
		t.Run(kind, func(t *testing.T) {
			request := ValidationRequest{Kind: kind, Mode: mode, Category: "CASE_EXECUTABILITY", AffectedCases: []string{"case"}, Binding: binding}
			request.Summary, _ = json.Marshal(struct{ Detail Validation }{Validation{Sets: map[coverage.ReviewKind]*CaseSet{mode: set}}})
			if kind == "QA_REVIEW" {
				request.Summary, _ = json.Marshal(struct{ Detail *CaseSet }{set})
			}
			decision := RecordedDecision{RequestID: "current", Choice: "approve", Revision: 12, Validation: &ValidationDecision{Reason: "exact current instruction", Remedy: "QA_ARTIFACT_REPAIR"}}
			if kind == "QA_REVIEW" {
				decision.Choice = "redesign"
			}
			state := &State{Validation: &Validation{Sets: map[coverage.ReviewKind]*CaseSet{mode: set}}, PendingAsks: map[string]PendingAsk{"current": {Resolved: true, Validation: &request}}, Decisions: map[string]RecordedDecision{"current": decision}}
			got := state.qaRevisionInstruction(mode, binding)
			if got == nil || got.Decision.RequestID != "current" || got.Decision.Validation.Reason != decision.Validation.Reason || got.Request.Binding != request.Binding || got.Request.Summary != nil {
				t.Fatal("projection changed original decision or binding")
			}
			fresh := binding
			fresh.Generation++
			if state.qaRevisionInstruction(mode, fresh) != nil {
				t.Fatal("old generation leaked original feedback")
			}
			fresh = binding
			fresh.RequirementRevision = "new-confirmed"
			if state.qaRevisionInstruction(mode, fresh) != nil {
				t.Fatal("old requirement leaked feedback")
			}
			replacement := *set
			replacement.AffectedCases = nil
			state.Validation.Sets[mode] = &replacement
			if state.qaRevisionInstruction(mode, binding) != nil {
				t.Fatal("finished revision retained active feedback")
			}
		})
	}
}
